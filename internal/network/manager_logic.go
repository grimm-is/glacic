package network

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"grimm.is/glacic/internal/config"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// DefaultNetlinker is the default RealNetlinker instance.
// Ensure this is only used if build permits?
// Ideally DefaultNetlinker might be in network_linux.go but it's used here.
// However RealNetlinker isn't available on Darwin.
// We should remove DefaultNetlinker from here if possible, or move it to platform file.
// Let's remove DefaultNetlinker variable if unused, or move it.
// It seems unused in this file except initialization?
// Ah checks "var DefaultNetlinker" in line 22.

// manager handles network configuration via netlink.
type Manager struct {
	nl              Netlinker
	sys             SystemController
	cmd             CommandExecutor
	dns             DNSUpdater
	uidRulePriority int
}

// DNSUpdater is an interface for updating the DNS service dynamically.

// SetDNSUpdater sets the DNS updater for the manager.
func (m *Manager) SetDNSUpdater(updater DNSUpdater) {
	m.dns = updater
}

// NewManagerWithDeps creates a new manager with injected dependencies.
func NewManagerWithDeps(nl Netlinker, sys SystemController, cmd CommandExecutor) *Manager {
	return &Manager{
		nl:  nl,
		sys: sys,
		cmd: cmd,
	}
}

// SetIPForwarding enables or disables IP forwarding.
func (m *Manager) SetIPForwarding(enable bool) error {
	val := "0"
	if enable {
		val = "1"
	}

	// IPv4 forwarding
	if err := m.sys.WriteSysctl("/proc/sys/net/ipv4/ip_forward", val); err != nil {
		return fmt.Errorf("failed to set net.ipv4.ip_forward: %w", err)
	}

	// IPv6 forwarding
	if err := m.sys.WriteSysctl("/proc/sys/net/ipv6/conf/all/forwarding", val); err != nil {
		log.Printf("Warning: failed to set net.ipv6.conf.all.forwarding: %v", err)
	}
	if err := m.sys.WriteSysctl("/proc/sys/net/ipv6/conf/default/forwarding", val); err != nil {
		log.Printf("Warning: failed to set net.ipv6.conf.default.forwarding: %v", err)
	}

	return nil
}

// SetupLoopback ensures the loopback interface is up.
func (m *Manager) SetupLoopback() error {
	link, err := m.nl.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("failed to get loopback interface: %w", err)
	}
	if err := m.nl.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring up loopback interface: %w", err)
	}
	return nil
}

// ApplyInterface applies the configuration for a single network interface.
func (m *Manager) ApplyInterface(ifaceCfg config.Interface) error {
	var link netlink.Link
	var err error

	// Try to find the interface
	link, err = m.nl.LinkByName(ifaceCfg.Name)

	if err != nil {
		// If it's a bond interface config and it doesn't exist, we attempt to create it.
		if ifaceCfg.Bond != nil {
			bond := netlink.NewLinkBond(netlink.LinkAttrs{Name: ifaceCfg.Name})
			// Set bond mode from config
			switch strings.ToLower(ifaceCfg.Bond.Mode) {
			case "802.3ad", "lacp":
				bond.Mode = netlink.BOND_MODE_802_3AD
			case "active-backup":
				bond.Mode = netlink.BOND_MODE_ACTIVE_BACKUP
			default:
				return fmt.Errorf("unsupported bond mode: %s", ifaceCfg.Bond.Mode)
			}

			if err := m.nl.LinkAdd(bond); err != nil {
				return fmt.Errorf("failed to add bond %s: %w", ifaceCfg.Name, err)
			}
			link, err = m.nl.LinkByName(ifaceCfg.Name) // Get newly created bond link
			if err != nil {
				return fmt.Errorf("failed to get newly created bond link %s: %w", ifaceCfg.Name, err)
			}
		} else {
			// Not a bond or VLAN, so if it's not found, it's an error.
			return fmt.Errorf("interface %s not found: %w", ifaceCfg.Name, err)
		}
	}

	// Bring down interface first to apply changes cleanly
	if err := m.nl.LinkSetDown(link); err != nil {
		log.Printf("Warning: failed to bring down interface %s: %v", ifaceCfg.Name, err)
	}

	// Set MTU
	if ifaceCfg.MTU > 0 {
		if err := m.nl.LinkSetMTU(link, ifaceCfg.MTU); err != nil {
			log.Printf("Warning: failed to set MTU on %s: %v", ifaceCfg.Name, err)
		}
	}

	// Link must be UP for DHCP to work (sending packets).
	// We bring it up early to ensure the DHCP blocking/async calls have a working link.
	if err := m.nl.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring up interface %s: %w", ifaceCfg.Name, err)
	}

	// Determine if we should manage IP addresses
	manageIPs := ifaceCfg.DHCPClient == "" || ifaceCfg.DHCPClient == "builtin"

	// Clear existing IP addresses ONLY if we are managing IPs
	if manageIPs {
		addrs, err := m.nl.AddrList(link, unix.AF_UNSPEC) // AF_UNSPEC for both IPv4 and IPv6
		if err == nil {
			for _, addr := range addrs {
				if err := m.nl.AddrDel(link, &addr); err != nil {
					log.Printf("Warning: failed to delete IP address %s from %s: %v", addr.IPNet.String(), ifaceCfg.Name, err)
				}
			}
		}
	} else {
		log.Printf("[network] Interface %s is in '%s' mode. Preserving existing IP addresses.", ifaceCfg.Name, ifaceCfg.DHCPClient)
	}

	// Apply IPv4 addresses
	if !ifaceCfg.DHCP {
		for _, ipStr := range ifaceCfg.IPv4 {
			addr, err := m.nl.ParseAddr(ipStr)
			if err != nil {
				log.Printf("Warning: invalid IP address %s for %s: %v", ipStr, ifaceCfg.Name, err)
				continue
			}
			if err := m.nl.AddrAdd(link, addr); err != nil {
				log.Printf("Warning: failed to add IP address %s to %s: %v", ipStr, ifaceCfg.Name, err)
			}
		}
	} else if manageIPs {
		// DHCP enabled and we are managing IPs
		// Check client preference: "native" (default), "system", "builtin" (alias for native)
		clientType := ifaceCfg.DHCPClient
		if clientType == "" {
			clientType = "native"
		}

		if clientType == "native" || clientType == "builtin" {
			if err := m.StartNativeDHCPClient(ifaceCfg.Name, ifaceCfg.Table); err != nil {
				log.Printf("Error starting native DHCP client on %s: %v. Falling back to system client.", ifaceCfg.Name, err)
				// Fallback
				if err := StartDHCPClient(ifaceCfg.Name); err != nil {
					log.Printf("Error starting system DHCP client on %s: %v", ifaceCfg.Name, err)
				}
			}
		} else {
			// Explicitly requested external/system client
			if err := StartDHCPClient(ifaceCfg.Name); err != nil {
				log.Printf("Error starting system DHCP client on %s: %v", ifaceCfg.Name, err)
			}
		}
	}

	// IPv6 Configuration
	// Only apply if we manage IPs. If external, we assume they handle RA/DHCPv6 too,
	// or user explicitly configured v6 settings here which conflicts with "external".
	// However, user might want "external" for v4 but let us handle v6?
	// Config doesn't split v4/v6 management mode. Assuming "dhcp_client" applies to whole interface.

	if manageIPs {
		// 1. Sysctl for RA/Forwarding
		if ifaceCfg.RA {
			// Server Mode
			_ = m.sys.WriteSysctl(fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/forwarding", ifaceCfg.Name), "1")
			_ = m.sys.WriteSysctl(fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/accept_ra", ifaceCfg.Name), "0")
		} else if ifaceCfg.DHCPv6 {
			// Client Mode
			_ = m.sys.WriteSysctl(fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/accept_ra", ifaceCfg.Name), "2")

			if err := StartDHCPv6Client(ifaceCfg.Name); err != nil {
				log.Printf("Error starting DHCPv6 client on %s: %v", ifaceCfg.Name, err)
			}
		}

		// 2. Static IPv6 Addresses
		for _, ipStr := range ifaceCfg.IPv6 {
			addr, err := m.nl.ParseAddr(ipStr)
			if err != nil {
				log.Printf("Warning: invalid IPv6 address %s for %s: %v", ipStr, ifaceCfg.Name, err)
				continue
			}
			if err := m.nl.AddrAdd(link, addr); err != nil {
				log.Printf("Warning: failed to add IPv6 address %s to %s: %v", ipStr, ifaceCfg.Name, err)
			}
		}
	}

	// Interface is already brought up above.

	// Apply VLANs
	for _, vlanCfg := range ifaceCfg.VLANs {
		if err := m.applyVLAN(link, vlanCfg); err != nil {
			log.Printf("Error applying VLAN %s on %s: %v", vlanCfg.ID, ifaceCfg.Name, err)
		}
	}

	// If this is a bond master interface, add its members
	if ifaceCfg.Bond != nil {
		for _, memberName := range ifaceCfg.Bond.Interfaces {
			memberLink, err := m.nl.LinkByName(memberName)
			if err != nil {
				log.Printf("Warning: bond member interface %s not found: %v", memberName, err)
				continue
			}
			if err := m.nl.LinkSetDown(memberLink); err != nil {
				log.Printf("Warning: failed to bring down member link %s for bond %s: %v", memberName, ifaceCfg.Name, err)
			}
			if err := m.nl.LinkSetMaster(memberLink, link); err != nil { // 'link' here is the bond master
				log.Printf("Error adding %s to bond %s: %v", memberName, ifaceCfg.Name, err)
			}
		}
	}

	// Apply Policy Routing (Table + Rules)
	if ifaceCfg.Table > 0 && ifaceCfg.Table != 254 {
		// Only proceed if we have an IP address managed (assumed from earlier logic)
		// We need the interface to be up.
		if err := m.setupPolicyRouting(link, ifaceCfg); err != nil {
			log.Printf("Error setting up policy routing on %s: %v", ifaceCfg.Name, err)
		}
	}

	return nil
}

// applyVLAN creates and configures a VLAN interface.
func (m *Manager) applyVLAN(parentLink netlink.Link, vlanCfg config.VLAN) error {
	vlanID, err := strconv.Atoi(vlanCfg.ID)
	if err != nil {
		return fmt.Errorf("invalid VLAN ID '%s': %w", vlanCfg.ID, err)
	}

	vlanName := fmt.Sprintf("%s.%d", parentLink.Attrs().Name, vlanID)
	link, err := m.nl.LinkByName(vlanName)
	if err != nil {
		// VLAN does not exist, create it
		vlan := &netlink.Vlan{
			LinkAttrs: netlink.LinkAttrs{
				Name:        vlanName,
				ParentIndex: parentLink.Attrs().Index,
			},
			VlanId: vlanID,
		}
		if err := m.nl.LinkAdd(vlan); err != nil {
			return fmt.Errorf("failed to add VLAN %s: %w", vlanName, err)
		}
		link, err = m.nl.LinkByName(vlanName) // Get the newly created link
		if err != nil {
			return fmt.Errorf("failed to get newly created VLAN link %s: %w", vlanName, err)
		}
	}

	// Bring up VLAN interface
	if err := m.nl.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring up VLAN interface %s: %w", vlanName, err)
	}

	// Apply IP addresses
	for _, ipStr := range vlanCfg.IPv4 {
		addr, err := m.nl.ParseAddr(ipStr)
		if err != nil {
			log.Printf("Warning: invalid IP address %s for VLAN %s: %v", ipStr, vlanName, err)
			continue
		}
		if err := m.nl.AddrAdd(link, addr); err != nil {
			log.Printf("Warning: failed to add IP address %s to VLAN %s: %v", ipStr, vlanName, err)
		}
	}
	// Apply IPv6 addresses
	for _, ipStr := range vlanCfg.IPv6 {
		addr, err := m.nl.ParseAddr(ipStr)
		if err != nil {
			log.Printf("Warning: invalid IPv6 address %s for VLAN %s: %v", ipStr, vlanName, err)
			continue
		}
		if err := m.nl.AddrAdd(link, addr); err != nil {
			log.Printf("Warning: failed to add IPv6 address %s to VLAN %s: %v", ipStr, vlanName, err)
		}
	}
	return nil
}

// ApplyStaticRoutes applies a list of static routes.
func (m *Manager) ApplyStaticRoutes(routes []config.Route) error {
	// Clear existing static routes managed by us (optional, but good for idempotency)
	// For simplicity, we'll just add new ones for now and let the system handle duplicates or updates.

	for _, routeCfg := range routes {
		dst, err := m.nl.ParseIPNet(routeCfg.Destination)
		if err != nil {
			log.Printf("Warning: invalid destination '%s' for route '%s': %v", routeCfg.Destination, routeCfg.Name, err)
			continue
		}

		route := &netlink.Route{Dst: dst}

		if routeCfg.Gateway != "" {
			route.Gw = net.ParseIP(routeCfg.Gateway)
		}

		if routeCfg.Interface != "" {
			link, err := m.nl.LinkByName(routeCfg.Interface)
			if err != nil {
				log.Printf("Warning: interface '%s' for route '%s' not found: %v", routeCfg.Interface, routeCfg.Name, err)
				continue
			}
			route.LinkIndex = link.Attrs().Index
		}

		if err := m.nl.RouteAdd(route); err != nil {
			if strings.Contains(err.Error(), "file exists") {
				log.Printf("[network] Route %s already exists, skipping.", routeCfg.Destination)
			} else {
				log.Printf("Warning: failed to add route '%s': %v", routeCfg.Name, err)
			}
		}
	}
	return nil
}

// WaitForLinkIP waits for a link to have at least one IP address.
func (m *Manager) WaitForLinkIP(ifaceName string, timeoutSeconds int) error {
	for i := 0; i < timeoutSeconds; i++ {
		link, err := m.nl.LinkByName(ifaceName)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		addrs, err := m.nl.AddrList(link, unix.AF_INET)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		if len(addrs) > 0 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout waiting for IP address on interface %s", ifaceName)
}

// GetDHCPLeases gets active DHCP client leases for monitoring.
func (m *Manager) GetDHCPLeases() map[string]LeaseInfo {
	leases := make(map[string]LeaseInfo)
	// For now, this is a placeholder. A real implementation would parse dhclient leases file.
	return leases
}

// InitializeDHCPClientManager initializes the DHCP client manager.
func (m *Manager) InitializeDHCPClientManager(cfg *config.DHCPServer) {
	// Placeholder
}

// StopDHCPClientManager stops the DHCP client manager.
func (m *Manager) StopDHCPClientManager() {
	// Placeholder
}

// isVirtualInterface checks if an interface is a virtual one (veth, docker, etc.).
func isVirtualInterface(name string) bool {
	virtPrefixes := []string{"veth", "docker", "br-", "virbr", "vnet", "tun", "tap"}
	for _, prefix := range virtPrefixes {
		if len(name) >= len(prefix) && strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// LeaseInfo represents information about a DHCP lease.

// applyManagementRoutes adds routes for private networks via the management gateway.
// setupPolicyRouting configures a separate routing table for split-routing traffic.
func (m *Manager) setupPolicyRouting(link netlink.Link, ifaceCfg config.Interface) error {
	// 1. Validate Gateway
	// If DHCP is enabled, we might not have a gateway yet.
	// We allow empty gateway if DHCP is enabled.
	if ifaceCfg.Gateway == "" && !ifaceCfg.DHCP {
		return fmt.Errorf("split-routing interface requires a gateway (DHCP=%v, Name=%s)", ifaceCfg.DHCP, ifaceCfg.Name)
	}
	// Proceed even if gateway is empty (for DHCP, we'll get it later or via applyDHCPLease)
	var gwIP net.IP
	if ifaceCfg.Gateway != "" {
		gwIP = net.ParseIP(ifaceCfg.Gateway)
		if gwIP == nil {
			return fmt.Errorf("invalid gateway IP: %s", ifaceCfg.Gateway)
		}
	}

	tableID := ifaceCfg.Table
	mark := uint32(tableID) // Use table ID as fwmark

	// 2. Add Default Route to Table
	// Only if we have a static gateway
	if ifaceCfg.Gateway != "" {
		gwIP := net.ParseIP(ifaceCfg.Gateway)
		if gwIP == nil {
			return fmt.Errorf("invalid gateway IP: %s", ifaceCfg.Gateway)
		}
		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       nil, // default
			Gw:        gwIP,
			Table:     tableID,
		}
		if err := m.nl.RouteAdd(route); err != nil && !strings.Contains(err.Error(), "file exists") {
			return fmt.Errorf("failed to add default route to table %d: %w", tableID, err)
		}
	}

	// 3. Add Rule: Source Policy Routing (Return traffic)
	// We need the interface IP.
	// If DHCP, we might not have it yet.
	// Check ifaceCfg.IPv4
	for _, ipCIDR := range ifaceCfg.IPv4 {
		ip, _, err := net.ParseCIDR(ipCIDR)
		if err == nil && ip != nil {
			rule := netlink.NewRule()
			rule.Src = &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
			rule.Table = tableID
			rule.Priority = 20000 // Priority before main (32766) to ensure return traffic works

			if err := m.nl.RuleAdd(rule); err != nil && !strings.Contains(err.Error(), "file exists") {
				log.Printf("Warning: failed to add source rule for %s: %v", ip, err)
			}
		}
	}

	// 4. Add Rule: FwMark Policy Routing (Connection-Assigned traffic)
	ruleMark := netlink.NewRule()
	ruleMark.Mark = mark
	ruleMark.Table = tableID
	ruleMark.Priority = 20010

	if err := m.nl.RuleAdd(ruleMark); err != nil && !strings.Contains(err.Error(), "file exists") {
		return fmt.Errorf("failed to add fwmark rule: %w", err)
	}

	log.Printf("[network] Configured policy routing (Table %d) on %s", tableID, ifaceCfg.Name)
	return nil
}

// ApplyUIDRoutes applies UID-based routing rules.
func (m *Manager) ApplyUIDRoutes(routes []config.UIDRouting) error {
	// 1. List existing UID rules (priority range 15000-15999)
	existingRules, err := m.nl.RuleList(unix.AF_INET)
	if err != nil {
		return fmt.Errorf("failed to list rules: %w", err)
	}

	// 2. Identify rules to delete (our priority range)
	// We use a fixed priority base for simplicity for now.
	basePriority := m.uidRulePriority

	// Remove old rules
	for _, r := range existingRules {
		if r.Priority >= basePriority && r.Priority < basePriority+1000 {
			if err := m.nl.RuleDel(&r); err != nil && !strings.Contains(err.Error(), "no such file") {
				log.Printf("Warning: failed to delete old UID rule: %v", err)
			}
		}
	}

	// 3. Add new rules
	for i, route := range routes {
		if !route.Enabled {
			continue
		}

		// Resolve UID
		var uid int
		if route.Username != "" {
			// user.Lookup requires os/user. Assuming simple mapping or numeric for now if os/user fails.
			// Implementing simple /etc/passwd parser or fallback?
			// For now, rely on UID field if Username fails or usage of `id`.
			// Since we want to avoid CGO `os/user` issues, let's try `id -u` command?
			// Or just respect `route.UID`.
			if route.UID == 0 {
				// Try to resolve
				resolvedUID, err := resolveReqUID(route.Username)
				if err != nil {
					log.Printf("Warning: failed to resolve username %s: %v", route.Username, err)
					continue
				}
				uid = resolvedUID
			} else {
				uid = route.UID
			}
		} else {
			uid = route.UID
		}

		if uid == 0 {
			continue // Skip invalid/root (rarely routed this way)
		}

		// Resolve Table
		tableID := m.resolveTableID(route)
		if tableID == 0 {
			log.Printf("Warning: could not resolve table for UID rule %s", route.Name)
			continue
		}

		// Create Rule
		// UIDRange is struct {Start, End}.
		rangeVal := netlink.RuleUIDRange{Start: uint32(uid), End: uint32(uid)}

		rule := netlink.NewRule()
		rule.UIDRange = &rangeVal
		rule.Table = int(tableID)
		rule.Priority = basePriority + i

		if err := m.nl.RuleAdd(rule); err != nil {
			return fmt.Errorf("failed to add rule for %s (UID %d): %w", route.Name, uid, err)
		}

		log.Printf("[network] Added UID route for %s (UID %d) -> Table %d", route.Name, uid, tableID)
	}

	return nil
}

// resolveReqUID resolves username to UID using system command or file.
func resolveReqUID(username string) (int, error) {
	// Validate username to prevent flag injection or malformed input
	// Standard Linux usernames: alphanumeric, _, -, . (usually shouldn't start with -)
	if len(username) == 0 || len(username) > 32 {
		return 0, fmt.Errorf("invalid username length")
	}
	for _, r := range username {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return 0, fmt.Errorf("invalid characters in username")
		}
	}
	if strings.HasPrefix(username, "-") {
		return 0, fmt.Errorf("username cannot start with -")
	}

	// Try `id -u <username>`
	out, err := exec.Command("id", "-u", "--", username).Output()
	if err != nil {
		return 0, err
	}
	val := strings.TrimSpace(string(out))
	return strconv.Atoi(val)
}

// resolveTableID resolves config references to a table ID.
func (m *Manager) resolveTableID(cfg config.UIDRouting) int {
	if cfg.Uplink != "" {
		// Map uplink name to table
		// Simple mapping for standard names
		switch cfg.Uplink {
		case "wan1", "wan":
			return int(TableWAN1)
		case "wan2":
			return int(TableWAN2)
		case "wan3":
			return int(TableWAN3)
		case "wan4":
			return int(TableWAN4)
		}
		// Static WAN name mapping. Supports: wan, wan1-4.
		// Future enhancement: Dynamic lookup via UplinkManager for custom uplink names.
	}
	if cfg.VPNLink != "" {
		// Map VPN
		// e.g. "wg0" -> TableWireGuardBase + index
		if strings.HasPrefix(cfg.VPNLink, "wg") {
			// Parse index
			var idx int
			fmt.Sscanf(cfg.VPNLink, "wg%d", &idx)
			return int(TableWireGuardBase) + idx
		}
	}
	if cfg.Interface != "" {
		// Look up interface table?
		// We don't have access to interface config here easily unless passed.
		// So we rely on mapping logic or assume standard tables.
	}
	// Fallback
	return int(TableMain)
}
