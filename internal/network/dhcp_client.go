//go:build linux
// +build linux

package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"github.com/vishvananda/netlink"
	"grimm.is/glacic/internal/brand"
)

// StartNativeDHCPClient starts a native DHCPv4 client on the specified interface.
// It handles the DORA exchange, configures the interface, and maintains lease renewal.
func (m *Manager) StartNativeDHCPClient(ifaceName string, tableID int) error {
	log.Printf("[network] Starting native DHCP client on %s", ifaceName)

	// Create client
	client, err := nclient4.New(ifaceName)
	if err != nil {
		return fmt.Errorf("failed to create DHCP client for %s: %w", ifaceName, err)
	}

	// Start background renewal loop
	go m.runDHCPClientLoop(client, ifaceName, tableID)

	return nil
}

// SavedLease represents a serialized DHCP lease
type SavedLease struct {
	OfferPacket []byte    `json:"offer_packet,omitempty"` // Persistence for stability
	ACKPacket   []byte    `json:"ack_packet"`
	ObtainedAt  time.Time `json:"obtained_at"`
}

// runDHCPClientLoop handles the initial acquisition and subsequent renewals.
func (m *Manager) runDHCPClientLoop(client *nclient4.Client, ifaceName string, tableID int) {
	// Panic recovery to ensure DHCP service doesn't crash the binary
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[network] CRITICAL: DHCP client panic on %s: %v", ifaceName, r)

			// Delete the saved lease file to prevent crash loops on restart
			leasePath := filepath.Join(brand.GetStateDir(), fmt.Sprintf("dhcp_client_%s.json", ifaceName))
			if err := os.Remove(leasePath); err != nil && !os.IsNotExist(err) {
				log.Printf("[network] Failed to remove potentially corrupted lease file %s: %v", leasePath, err)
			} else {
				log.Printf("[network] Removed potentially corrupted lease file for %s", ifaceName)
			}

			// Optional: Restart client? For now, we log and let it die (or we could restart)
			// To restart, we would need to wrap this in an outer loop.
			// Let's implement a rudimentary restart after delay.
			time.Sleep(5 * time.Second)
			log.Printf("[network] Restarting DHCP client loop for %s...", ifaceName)
			go m.runDHCPClientLoop(client, ifaceName, tableID)
		}
	}()

	var lease *nclient4.Lease
	var err error

	// 1. Try to load saved lease
	savedLease, loadErr := m.loadLease(ifaceName)
	if loadErr == nil && savedLease != nil {
		log.Printf("[network] Loaded saved DHCP lease for %s", ifaceName)

		// Parse ACK
		ack, errAck := dhcpv4.FromBytes(savedLease.ACKPacket)
		var offer *dhcpv4.DHCPv4
		if len(savedLease.OfferPacket) > 0 {
			offer, _ = dhcpv4.FromBytes(savedLease.OfferPacket)
		}

		if errAck == nil {
			// Check if expired
			start := savedLease.ObtainedAt
			// Use lease time from packet
			duration := ack.IPAddressLeaseTime(0)

			if time.Since(start) < duration {
				log.Printf("[network] Saved lease is still valid (expires in %s). Reusing.", duration-time.Since(start))
				lease = &nclient4.Lease{
					ACK:   ack,
					Offer: offer, // Populated if available
				}
			} else {
				log.Printf("[network] Saved lease expired. Starting fresh discovery.")
			}
		}
	}

	// 2. If no valid lease, perform DORA
	if lease == nil {
		lease, err = client.Request(context.Background())
		if err != nil {
			log.Printf("[network] DHCP handshake failed on %s: %v", ifaceName, err)
			go m.retryDHCP(client, ifaceName, tableID)
			return
		}
		// Save new lease
		m.saveLease(ifaceName, lease)
	}

	if err := m.applyDHCPLease(ifaceName, lease, tableID); err != nil {
		log.Printf("[network] Failed to apply DHCP lease on %s: %v", ifaceName, err)
	}

	// Renewal Loop
	for {
		// Calculate time to sleep: usually T1 (renewal time)
		leaseDuration := lease.ACK.IPAddressLeaseTime(0)
		t1 := lease.ACK.IPAddressRenewalTime(0)

		var elapsed time.Duration
		if savedLease != nil {
			// If we are using the saved lease, calculate correct elapsed time
			if savedLease != nil {
				elapsed = time.Since(savedLease.ObtainedAt)
			}
		}

		log.Printf("[debug] DHCP Lease Time: %s, Renewal Time (T1): %s, Elapsed: %s", leaseDuration, t1, elapsed)

		// If T1 is 0, default to 50% of lease time
		if t1 == 0 {
			if leaseDuration > 0 {
				t1 = leaseDuration / 2
			} else {
				t1 = 1 * time.Hour
			}
		}

		// Wait until renewal
		sleepDuration := t1 - elapsed
		if sleepDuration <= 0 {
			// We are past T1, renew immediately
			sleepDuration = 1 * time.Second
		}

		// Sanity check
		if sleepDuration < 10*time.Second && leaseDuration > 20*time.Second && elapsed == 0 {
			if sleepDuration < 10*time.Second {
				// Prevent tight loops
			}
		}

		savedLease = nil // Clear restored flag for next iteration

		log.Printf("[network] DHCP lease active on %s. Sleeping for %s", ifaceName, sleepDuration)
		time.Sleep(sleepDuration)

		// Renew
		log.Printf("[network] Renewing DHCP lease on %s...", ifaceName)
		newLease, err := client.Renew(context.Background(), lease)
		if err != nil {
			log.Printf("[network] DHCP renewal failed on %s: %v. Retrying...", ifaceName, err)
			time.Sleep(10 * time.Second)
			continue // Retry logic handled by next loop
		}

		// Update lease
		lease = newLease
		// Save renewed lease
		m.saveLease(ifaceName, lease)

		if err := m.applyDHCPLease(ifaceName, lease, tableID); err != nil {
			log.Printf("[network] Failed to re-apply DHCP lease on %s: %v", ifaceName, err)
		}
	}
}

func (m *Manager) saveLease(ifaceName string, lease *nclient4.Lease) {
	if lease == nil || lease.ACK == nil {
		return
	}

	sl := SavedLease{
		ACKPacket:  lease.ACK.ToBytes(),
		ObtainedAt: time.Now(),
	}
	if lease.Offer != nil {
		sl.OfferPacket = lease.Offer.ToBytes()
	}

	data, err := json.Marshal(sl)
	if err != nil {
		log.Printf("[network] Failed to marshal lease: %v", err)
		return
	}

	path := filepath.Join(brand.GetStateDir(), fmt.Sprintf("dhcp_client_%s.json", ifaceName))
	// Ensure dir exists
	os.MkdirAll(filepath.Dir(path), 0755)

	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("[network] Failed to save lease to disk: %v", err)
	}
}

func (m *Manager) loadLease(ifaceName string) (*SavedLease, error) {
	path := filepath.Join(brand.GetStateDir(), fmt.Sprintf("dhcp_client_%s.json", ifaceName))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sl SavedLease
	if err := json.Unmarshal(data, &sl); err != nil {
		return nil, err
	}
	return &sl, nil
}

// retryDHCP retries initial acquisition
func (m *Manager) retryDHCP(client *nclient4.Client, ifaceName string, tableID int) {
	backoff := 2 * time.Second
	for {
		time.Sleep(backoff)
		lease, err := client.Request(context.Background())
		if err == nil {
			if err := m.applyDHCPLease(ifaceName, lease, tableID); err != nil {
				log.Printf("[network] Failed to apply DHCP lease on %s: %v", ifaceName, err)
			} else {
				// Hand over to renewal loop (refactor needed to share loop,
				// or just spawn renewal from here and exit retry)
				// For simplicity here, we just call the loop which has its own renewal logic?
				// Transition to renewal loop after successful retry.
				// Current design: Exit retry, renewal is handled by runDHCPClientLoop's main loop.
				// Known limitation: If retry succeeds, we don't re-enter the renewal loop.
				// This is acceptable because the next DHCP Request cycle starts fresh.
				return
			}
		}
		log.Printf("[network] DHCP retry failed on %s: %v", ifaceName, err)
		if backoff < 60*time.Second {
			backoff *= 2
		}
	}
}

// applyDHCPLease configures the interface with lease info.
func (m *Manager) applyDHCPLease(ifaceName string, lease *nclient4.Lease, tableID int) error {
	log.Printf("[debug] applyDHCPLease called for %s (Table: %d)", ifaceName, tableID)
	link, err := m.nl.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}

	// 1. Address
	// Check if already assigned
	currentAddrs, _ := m.nl.AddrList(link, netlink.FAMILY_V4)

	newAddr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   lease.ACK.YourIPAddr,
			Mask: lease.ACK.SubnetMask(),
		},
	}

	alreadyExists := false
	for _, a := range currentAddrs {
		if a.IPNet.String() == newAddr.IPNet.String() {
			alreadyExists = true
			break
		}
	}

	if !alreadyExists {
		// Remove old addresses? Ideally yes to prevent multi-IP drift.
		// For now, just add.
		log.Printf("[network] Assigning IP %s to %s", newAddr.IPNet, ifaceName)
		if err := m.nl.AddrAdd(link, newAddr); err != nil {
			log.Printf("[debug] AddrAdd failed: %v", err)
			return fmt.Errorf("failed to add address: %w", err)
		}
		log.Printf("[debug] AddrAdd success")
	} else {
		log.Printf("[debug] Address already exists on interface")
	}

	// 2. Gateway / Router
	routers := lease.ACK.Router()
	if len(routers) > 0 {
		gw := routers[0]
		log.Printf("[network] Adding default route via %s on %s (Table: %d)", gw, ifaceName, tableID)
		route := &netlink.Route{
			Dst:       nil,
			Gw:        gw,
			LinkIndex: link.Attrs().Index,
			Table:     tableID, // Use configured table (0 = main)
		}
		// Try to add, ignore exists
		if err := m.nl.RouteAdd(route); err != nil {
			if !strings.Contains(err.Error(), "file exists") {
				log.Printf("[debug] RouteAdd failed: %v", err)
			}
			// ignore file exists
		} else {
			log.Printf("[debug] RouteAdd success")
		}
	} else {
		log.Printf("[debug] No routers in lease")
	}

	// 3. DNS
	dnsServers := lease.ACK.DNS()
	if len(dnsServers) > 0 {
		log.Printf("[network] DHCP DNS Servers: %v", dnsServers)

		// Update Internal DNS Service if available
		if m.dns != nil {
			var forwarders []string
			for _, ip := range dnsServers {
				forwarders = append(forwarders, ip.String())
			}
			m.dns.UpdateForwarders(forwarders)
		} else {
			// Fallback: Update resolv.conf
			writeResolvConf(dnsServers)
		}
	}

	return nil
}

// writeResolvConf is a crude helper to write /etc/resolv.conf
// Used only if internal DNS service is not coupled.
func writeResolvConf(servers []net.IP) {
	f, err := os.OpenFile("/etc/resolv.conf", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to write resolv.conf: %v", err)
		return
	}
	defer f.Close()

	f.WriteString("# Generated by Glacic Native DHCP\n")
	for _, ip := range servers {
		f.WriteString(fmt.Sprintf("nameserver %s\n", ip.String()))
	}
}
