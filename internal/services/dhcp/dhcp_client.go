//go:build linux
// +build linux

package dhcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"

	"grimm.is/glacic/internal/config"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// Package-level logger with component tag
var dhcpLog = logging.WithComponent("dhcp")

// LeaseInfo holds information about a DHCP lease
type LeaseInfo struct {
	IPAddress     net.IP
	SubnetMask    net.IPMask
	Router        net.IP
	DNSServers    []net.IP
	LeaseTime     time.Duration
	RenewalTime   time.Duration
	RebindingTime time.Duration
	ObtainedAt    time.Time
	Interface     string
}

// Client represents a DHCP client for a specific interface
type Client struct {
	ifaceName string
	iface     *net.Interface
	client    *nclient4.Client
	lease     *LeaseInfo
	cancel    context.CancelFunc
	mu        sync.RWMutex
	running   bool
}

// ClientManager manages multiple DHCP clients
type ClientManager struct {
	clients map[string]*Client
	mu      sync.RWMutex
	config  *config.DHCPServer
}

// NewClientManager creates a new DHCP client manager
func NewClientManager(cfg *config.DHCPServer) *ClientManager {
	return &ClientManager{
		clients: make(map[string]*Client),
		config:  cfg,
	}
}

// StartClient starts a DHCP client for the specified interface
func (cm *ClientManager) StartClient(ifaceName string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if client already exists
	if _, exists := cm.clients[ifaceName]; exists {
		return fmt.Errorf("DHCP client already running for interface %s", ifaceName)
	}

	// Get network interface
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", ifaceName, err)
	}

	// Create DHCP client
	client, err := nclient4.New(ifaceName)
	if err != nil {
		return fmt.Errorf("failed to create DHCP client for %s: %w", ifaceName, err)
	}

	// Create client instance
	dhcpClient := &Client{
		ifaceName: ifaceName,
		iface:     iface,
		client:    client,
	}

	// Start the client in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	dhcpClient.cancel = cancel

	go dhcpClient.run(ctx)

	cm.clients[ifaceName] = dhcpClient
	dhcpLog.Info("Started client", "interface", ifaceName)

	return nil
}

// StopClient stops a DHCP client for the specified interface
func (cm *ClientManager) StopClient(ifaceName string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	client, exists := cm.clients[ifaceName]
	if !exists {
		return fmt.Errorf("no DHCP client running for interface %s", ifaceName)
	}

	client.cancel()
	delete(cm.clients, ifaceName)
	dhcpLog.Info("Stopped client", "interface", ifaceName)

	return nil
}

// GetLease returns the current lease for an interface
func (cm *ClientManager) GetLease(ifaceName string) (*LeaseInfo, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	client, exists := cm.clients[ifaceName]
	if !exists {
		return nil, fmt.Errorf("no DHCP client running for interface %s", ifaceName)
	}

	client.mu.RLock()
	defer client.mu.RUnlock()

	if client.lease == nil {
		return nil, fmt.Errorf("no lease obtained for interface %s", ifaceName)
	}

	return client.lease, nil
}

// GetAllLeases returns all active DHCP leases
func (cm *ClientManager) GetAllLeases() map[string]*LeaseInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	leases := make(map[string]*LeaseInfo)
	for ifaceName, client := range cm.clients {
		client.mu.RLock()
		if client.lease != nil {
			leases[ifaceName] = client.lease
		}
		client.mu.RUnlock()
	}
	return leases
}

// Stop stops all DHCP clients
func (cm *ClientManager) Stop() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for ifaceName, client := range cm.clients {
		client.cancel()
		dhcpLog.Info("Stopped client", "interface", ifaceName)
	}

	cm.clients = make(map[string]*Client)
}

// run is the main loop for the DHCP client
func (c *Client) run(ctx context.Context) {
	c.running = true
	defer func() {
		c.running = false
	}()

	// Initial lease acquisition
	if err := c.acquireLease(); err != nil {
		dhcpLog.Warn("Failed to acquire initial lease", "interface", c.ifaceName, "error", err)
		return
	}

	// Start renewal loop
	c.renewalLoop(ctx)
}

// acquireLease acquires a DHCP lease
func (c *Client) acquireLease() error {
	dhcpLog.Info("Acquiring lease", "interface", c.ifaceName)

	// Send DHCP discover
	lease, err := c.client.Request(context.Background())
	if err != nil {
		return fmt.Errorf("DHCP request failed: %w", err)
	}

	// Parse lease information from the ACK packet
	leaseInfo := c.parseLease(lease.ACK)

	// Apply the IP address to the interface
	if err := c.applyLeaseToInterface(leaseInfo); err != nil {
		return fmt.Errorf("failed to apply lease to interface: %w", err)
	}

	c.mu.Lock()
	c.lease = leaseInfo
	c.mu.Unlock()

	dhcpLog.Info("Obtained lease", "interface", c.ifaceName, "ip", leaseInfo.IPAddress, "lease_time", leaseInfo.LeaseTime)

	return nil
}

func (c *Client) applyLeaseToInterface(leaseInfo *LeaseInfo) error {
	// Get the network link using netlink
	link, err := netlink.LinkByName(c.ifaceName)
	if err != nil {
		return fmt.Errorf("failed to get link %s: %w", c.ifaceName, err)
	}

	// Create a new IP address for the interface
	ones, _ := leaseInfo.SubnetMask.Size()
	ipAddr, err := netlink.ParseAddr(fmt.Sprintf("%s/%d",
		leaseInfo.IPAddress.String(), ones))
	if err != nil {
		// Fallback: create address manually
		ipAddr = &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   leaseInfo.IPAddress,
				Mask: leaseInfo.SubnetMask,
			},
		}
	}

	// Remove only DHCP-assigned addresses (localhost and link-local are preserved)
	addrs, err := netlink.AddrList(link, unix.AF_INET)
	if err == nil {
		for _, addr := range addrs {
			// Don't remove localhost (127.0.0.0/8) or link-local (169.254.0.0/16) addresses
			if addr.IP.IsLoopback() || addr.IP.IsLinkLocalUnicast() {
				continue
			}
			// Remove other addresses that might be from previous DHCP leases
			if err := netlink.AddrDel(link, &addr); err != nil {
				dhcpLog.Warn("Failed to remove existing address", "ip", addr.IP, "error", err)
			}
		}
	}

	// Add the new IP address to the interface
	if err := netlink.AddrAdd(link, ipAddr); err != nil {
		// If the error is "file exists" (EEXIST), it's fine
		if err != unix.EEXIST {
			return fmt.Errorf("failed to add IP address to interface: %w", err)
		}
	}

	// Set the interface up
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to set interface up: %w", err)
	}

	dhcpLog.Info("Applied IP", "ip", leaseInfo.IPAddress, "interface", c.ifaceName)

	// Add default route via the router (gateway) if provided
	if leaseInfo.Router != nil && !leaseInfo.Router.IsUnspecified() {
		// Remove existing default routes via this interface first
		routes, err := netlink.RouteList(link, unix.AF_INET)
		if err == nil {
			for _, route := range routes {
				if route.Dst == nil { // Default route has nil Dst
					if err := netlink.RouteDel(&route); err != nil {
						dhcpLog.Warn("Failed to remove existing default route", "error", err)
					}
				}
			}
		}

		// Add new default route
		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Gw:        leaseInfo.Router,
			Dst:       nil, // nil = default route (0.0.0.0/0)
		}
		if err := netlink.RouteAdd(route); err != nil {
			// If route already exists, that's ok
			if err != unix.EEXIST {
				dhcpLog.Warn("Failed to add default route", "gateway", leaseInfo.Router, "error", err)
			}
		} else {
			dhcpLog.Info("Added default route", "gateway", leaseInfo.Router, "interface", c.ifaceName)
		}
	}

	return nil
}

// parseLease converts DHCPv4 packet to LeaseInfo
func (c *Client) parseLease(lease *dhcpv4.DHCPv4) *LeaseInfo {
	leaseInfo := &LeaseInfo{
		IPAddress:  lease.YourIPAddr,
		Interface:  c.ifaceName,
		ObtainedAt: clock.Now(),
	}

	// Get subnet mask
	if lease.Options != nil {
		if subnetMask := lease.Options.Get(dhcpv4.OptionSubnetMask); len(subnetMask) > 0 {
			leaseInfo.SubnetMask = net.IPMask(subnetMask)
		}

		// Get router
		if router := lease.Options.Get(dhcpv4.OptionRouter); len(router) > 0 {
			leaseInfo.Router = net.IP(router[:4]) // Take first router
		}

		// Get DNS servers
		if dns := lease.Options.Get(dhcpv4.OptionDomainNameServer); len(dns) > 0 {
			for i := 0; i < len(dns); i += 4 {
				if i+4 <= len(dns) {
					leaseInfo.DNSServers = append(leaseInfo.DNSServers, net.IP(dns[i:i+4]))
				}
			}
		}

		// Get lease times
		if leaseTime := lease.Options.Get(dhcpv4.OptionIPAddressLeaseTime); len(leaseTime) == 4 {
			seconds := binary.BigEndian.Uint32(leaseTime)
			leaseInfo.LeaseTime = time.Duration(seconds) * time.Second
		}

		// Renewal and rebinding times may not be available in all DHCP servers
		// Using default values calculated from lease time
		// if renewalTime := lease.Options.Get(dhcpv4.OptionRenewalTime); len(renewalTime) == 4 {
		//	seconds := binary.BigEndian.Uint32(renewalTime)
		//	leaseInfo.RenewalTime = time.Duration(seconds) * time.Second
		// }

		// if rebindingTime := lease.Options.Get(dhcpv4.OptionRebindingTime); len(rebindingTime) == 4 {
		//	seconds := binary.BigEndian.Uint32(rebindingTime)
		//	leaseInfo.RebindingTime = time.Duration(seconds) * time.Second
		// }
	}

	// Set default renewal times if not provided
	if leaseInfo.RenewalTime == 0 && leaseInfo.LeaseTime > 0 {
		leaseInfo.RenewalTime = leaseInfo.LeaseTime / 2 // 50% of lease time
	}
	if leaseInfo.RebindingTime == 0 && leaseInfo.LeaseTime > 0 {
		leaseInfo.RebindingTime = leaseInfo.LeaseTime * 7 / 10 // 70% of lease time
	}

	return leaseInfo
}

// renewalLoop handles lease renewals
func (c *Client) renewalLoop(ctx context.Context) {
	for {
		c.mu.RLock()
		lease := c.lease
		c.mu.RUnlock()

		if lease == nil {
			// Try to acquire new lease
			if err := c.acquireLease(); err != nil {
				dhcpLog.Warn("Failed to acquire lease", "interface", c.ifaceName, "error", err)
				time.Sleep(5 * time.Second)
				continue
			}
			c.mu.RLock()
			lease = c.lease
			c.mu.RUnlock()
		}

		// Calculate next renewal time
		var nextRenewal time.Duration
		if lease.RenewalTime > 0 {
			nextRenewal = lease.RenewalTime
		} else {
			nextRenewal = lease.LeaseTime / 2 // Default to 50%
		}

		// Don't renew too frequently
		if nextRenewal < 30*time.Second {
			nextRenewal = 30 * time.Second
		}

		dhcpLog.Info("Next renewal", "interface", c.ifaceName, "in", nextRenewal)

		// Wait for renewal time or context cancellation
		select {
		case <-time.After(nextRenewal):
			// Time to renew
			if err := c.renewLease(); err != nil {
				dhcpLog.Warn("Failed to renew lease", "interface", c.ifaceName, "error", err)
				// Try to acquire new lease on renewal failure
				if err := c.acquireLease(); err != nil {
					dhcpLog.Warn("Failed to acquire new lease", "interface", c.ifaceName, "error", err)
				}
			}
		case <-ctx.Done():
			dhcpLog.Info("Stopping renewal loop", "interface", c.ifaceName)
			return
		}
	}
}

// renewLease renews the current DHCP lease
func (c *Client) renewLease() error {
	dhcpLog.Info("Renewing lease", "interface", c.ifaceName)

	// Send DHCP request for renewal
	lease, err := c.client.Request(context.Background())
	if err != nil {
		return fmt.Errorf("DHCP renewal failed: %w", err)
	}

	// Update lease information from the ACK packet
	leaseInfo := c.parseLease(lease.ACK)

	// Apply the renewed IP address to the interface
	if err := c.applyLeaseToInterface(leaseInfo); err != nil {
		return fmt.Errorf("failed to apply renewed lease to interface: %w", err)
	}

	c.mu.Lock()
	c.lease = leaseInfo
	c.mu.Unlock()

	dhcpLog.Info("Renewed lease", "interface", c.ifaceName, "ip", leaseInfo.IPAddress, "lease_time", leaseInfo.LeaseTime)

	return nil
}

// IsRunning returns true if the client is currently running
func (c *Client) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}
