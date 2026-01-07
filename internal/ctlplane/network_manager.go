package ctlplane

import (
	"fmt"
	"log"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/network"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// NetworkManager handles network interface configuration.
type NetworkManager struct {
	config *config.Config
}

// NewNetworkManager creates a new network manager.
func NewNetworkManager(cfg *config.Config) *NetworkManager {
	return &NetworkManager{
		config: cfg,
	}
}

// UpdateConfig updates the configuration reference.
func (nm *NetworkManager) UpdateConfig(cfg *config.Config) {
	nm.config = cfg
}

// GetInterfaces returns the status of all configured interfaces with comprehensive state detection.
func (nm *NetworkManager) GetInterfaces() ([]InterfaceStatus, error) {
	var interfaces []InterfaceStatus

	// Try to create ethtool manager for additional info
	linkMgr, err := NewLinkManager()
	if err != nil {
		log.Printf("[NM] Warning: ethtool not available: %v", err)
	}
	defer func() {
		if linkMgr != nil {
			linkMgr.Close()
		}
	}()

	for _, iface := range nm.config.Interfaces {
		zoneName := iface.Zone
		// If zone is empty, try to find it in zone definitions (reverse lookup)
		if zoneName == "" {
			for _, z := range nm.config.Zones {
				for _, member := range z.Interfaces {
					if member == iface.Name {
						zoneName = z.Name
						break
					}
				}
				if zoneName != "" {
					break
				}
			}
		}

		status := InterfaceStatus{
			Name:           iface.Name,
			Type:           GetInterfaceType(iface.Name),
			Description:    iface.Description,
			Zone:           zoneName,
			ConfiguredIPv4: iface.IPv4,
			DHCPEnabled:    iface.DHCP,
			Gateway:        iface.Gateway,
			Disabled:       iface.Disabled,
		}

		// Check if interface is disabled in config
		if iface.Disabled {
			status.State = InterfaceStateDisabled
			status.AdminUp = false
			status.Carrier = false
			interfaces = append(interfaces, status)
			continue
		}

		// Try to get link from netlink
		link, err := netlink.LinkByName(iface.Name)
		if err != nil {
			// Interface configured but not found in system
			status.State = InterfaceStateMissing
			interfaces = append(interfaces, status)
			continue
		}

		attrs := link.Attrs()
		status.MAC = attrs.HardwareAddr.String()
		status.MTU = attrs.MTU
		status.AdminUp = attrs.Flags&unix.IFF_UP != 0

		// Read carrier from sysfs (more reliable than OperState for physical detection)
		carrier, _ := ReadCarrier(iface.Name)
		status.Carrier = carrier

		// Determine state based on admin status and carrier
		switch {
		case !status.AdminUp:
			status.State = InterfaceStateDown
		case !carrier && status.Type == "ethernet":
			status.State = InterfaceStateNoCarrier
		case attrs.OperState == netlink.OperUp:
			status.State = InterfaceStateUp
		default:
			status.State = InterfaceStateDown
		}

		// Get addresses
		addrs, err := netlink.AddrList(link, unix.AF_INET)
		if err == nil {
			for _, addr := range addrs {
				status.IPv4Addrs = append(status.IPv4Addrs, addr.IPNet.String())
			}
		}
		addrs, err = netlink.AddrList(link, unix.AF_INET6)
		if err == nil {
			for _, addr := range addrs {
				status.IPv6Addrs = append(status.IPv6Addrs, addr.IPNet.String())
			}
		}

		// Get interface stats from sysfs
		if stats, err := ReadInterfaceStats(iface.Name); err == nil {
			status.Stats = stats
		}

		// Handle bond-specific info
		if status.Type == "bond" {
			status.BondMode = ReadBondMode(iface.Name)

			// Get active slaves from system
			activeSlaves, _ := ReadBondActiveSlaves(iface.Name)
			status.BondActiveMembers = activeSlaves

			// Get configured members
			if iface.Bond != nil {
				status.BondMembers = iface.Bond.Interfaces

				// Check for missing members (degraded state)
				activeSet := make(map[string]bool)
				for _, s := range activeSlaves {
					activeSet[s] = true
				}
				for _, configured := range iface.Bond.Interfaces {
					if !activeSet[configured] {
						status.BondMissingMembers = append(status.BondMissingMembers, configured)
					}
				}

				// If we have missing members, mark as degraded
				if len(status.BondMissingMembers) > 0 && status.State == InterfaceStateUp {
					status.State = InterfaceStateDegraded
				}
			}
		}

		// Handle VLAN-specific info
		if status.Type == "vlan" {
			if parent, vlanID, err := ReadVLANInfo(iface.Name); err == nil {
				status.VLANParent = parent
				status.VLANID = vlanID
			}
		}

		// Get ethtool data if available
		// The ethtool wrapper handles virtual NIC detection and uses sysfs fallback
		if linkMgr != nil && (status.Type == "ethernet" || status.Type == "bond") {
			// Link info (speed, duplex, autoneg)
			if linkInfo, err := linkMgr.GetLinkInfo(iface.Name); err == nil {
				status.Speed = linkInfo.Speed
				status.Duplex = linkInfo.Duplex
				status.Autoneg = linkInfo.Autoneg
			}

			// Driver info
			if driverInfo, err := linkMgr.GetDriverInfo(iface.Name); err == nil {
				status.Driver = driverInfo.Driver
				status.DriverVersion = driverInfo.Version
				status.Firmware = driverInfo.Firmware
				status.BusInfo = driverInfo.BusInfo
			}

			// Offload features
			if offloads, err := linkMgr.GetOffloads(iface.Name); err == nil {
				status.Offloads = offloads
			}

			// Ring buffer settings
			if ringBuffer, err := linkMgr.GetRingBuffer(iface.Name); err == nil {
				status.RingBuffer = ringBuffer
			}

			// Coalesce settings
			if coalesce, err := linkMgr.GetCoalesce(iface.Name); err == nil {
				status.Coalesce = coalesce
			}
		}

		interfaces = append(interfaces, status)
	}

	// Include WireGuard interfaces from VPN config
	if nm.config.VPN != nil {
		for _, wg := range nm.config.VPN.WireGuard {
			if !wg.Enabled {
				continue
			}

			ifaceName := wg.Interface
			if ifaceName == "" {
				ifaceName = "wg0"
			}

			status := InterfaceStatus{
				Name:           ifaceName,
				Type:           "wireguard",
				Description:    "WireGuard VPN: " + wg.Name,
				Zone:           wg.Zone,
				ConfiguredIPv4: wg.Address,
				// WireGuard typicaly doesn't use DHCP but handles IPs internally
			}

			// Try to get link from netlink
			link, err := netlink.LinkByName(ifaceName)
			if err != nil {
				status.State = InterfaceStateMissing
				interfaces = append(interfaces, status)
				continue
			} else {
				// Fill runtime info
				attrs := link.Attrs()
				status.MAC = attrs.HardwareAddr.String()
				status.MTU = attrs.MTU
				status.AdminUp = attrs.Flags&unix.IFF_UP != 0

				// WireGuard is virtual, so carrier detection is different
				// Usually if AdminUp, it is "Up" unless handshake fails (which we can't easily see here)
				// We'll treat AdminUp as Up for now
				if status.AdminUp {
					status.State = InterfaceStateUp
				} else {
					status.State = InterfaceStateDown
				}

				// Get addresses
				addrs, err := netlink.AddrList(link, unix.AF_INET)
				if err == nil {
					for _, addr := range addrs {
						status.IPv4Addrs = append(status.IPv4Addrs, addr.IPNet.String())
					}
				}
				addrs6, err := netlink.AddrList(link, unix.AF_INET6)
				if err == nil {
					for _, addr := range addrs6 {
						status.IPv6Addrs = append(status.IPv6Addrs, addr.IPNet.String())
					}
				}

				if stats, err := ReadInterfaceStats(ifaceName); err == nil {
					status.Stats = stats
				}
			}
			interfaces = append(interfaces, status)
		}
	}

	return interfaces, nil
}

// GetAvailableInterfaces returns all physical interfaces available for configuration.
func (nm *NetworkManager) GetAvailableInterfaces() ([]AvailableInterface, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %w", err)
	}

	// Build a map of assigned interfaces from config
	assigned := make(map[string]bool)
	bondMembers := make(map[string]string) // interface -> bond name
	for _, iface := range nm.config.Interfaces {
		assigned[iface.Name] = true
		if iface.Bond != nil {
			for _, member := range iface.Bond.Interfaces {
				bondMembers[member] = iface.Name
			}
		}
	}

	var interfaces []AvailableInterface
	for _, link := range links {
		name := link.Attrs().Name

		// Skip loopback and virtual interfaces
		if name == "lo" || IsVirtualInterface(name) {
			continue
		}

		iface := AvailableInterface{
			Name:     name,
			MAC:      link.Attrs().HardwareAddr.String(),
			LinkUp:   link.Attrs().OperState == netlink.OperUp,
			Assigned: assigned[name],
		}

		// Check if in a bond
		if bondName, ok := bondMembers[name]; ok {
			iface.InBond = true
			iface.BondName = bondName
		}

		// Get driver info
		iface.Driver = GetDriverName(name)
		iface.Speed = GetLinkSpeedString(name)

		interfaces = append(interfaces, iface)
	}

	return interfaces, nil
}

// UpdateInterface updates an interface's configuration.
func (nm *NetworkManager) UpdateInterface(args *UpdateInterfaceArgs) error {
	auditLog("UpdateInterface", fmt.Sprintf("name=%s action=%s", args.Name, args.Action))
	if args.Name == "" {
		return fmt.Errorf("interface name is required")
	}

	// Find interface in config
	var ifaceIdx int = -1
	for i, iface := range nm.config.Interfaces {
		if iface.Name == args.Name {
			ifaceIdx = i
			break
		}
	}

	switch args.Action {
	case ActionEnable:
		link, err := netlink.LinkByName(args.Name)
		if err != nil {
			return fmt.Errorf("interface not found: %w", err)
		}
		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("failed to enable interface: %w", err)
		}

	case ActionDisable:
		link, err := netlink.LinkByName(args.Name)
		if err != nil {
			return fmt.Errorf("interface not found: %w", err)
		}
		if err := netlink.LinkSetDown(link); err != nil {
			return fmt.Errorf("failed to disable interface: %w", err)
		}

	case ActionUpdate:
		if ifaceIdx < 0 {
			// Add new interface to config
			newIface := config.Interface{Name: args.Name}
			if args.Zone != nil {
				newIface.Zone = *args.Zone
			}
			if args.Description != nil {
				newIface.Description = *args.Description
			}
			if args.IPv4 != nil {
				newIface.IPv4 = args.IPv4
			}
			if args.DHCP != nil {
				newIface.DHCP = *args.DHCP
			}
			if args.MTU != nil {
				newIface.MTU = *args.MTU
			}
			nm.config.Interfaces = append(nm.config.Interfaces, newIface)
		} else {
			// Update existing interface
			if args.Zone != nil {
				nm.config.Interfaces[ifaceIdx].Zone = *args.Zone
			}
			if args.Description != nil {
				nm.config.Interfaces[ifaceIdx].Description = *args.Description
			}
			if args.IPv4 != nil {
				nm.config.Interfaces[ifaceIdx].IPv4 = args.IPv4
			}
			if args.DHCP != nil {
				nm.config.Interfaces[ifaceIdx].DHCP = *args.DHCP
			}
			if args.MTU != nil {
				nm.config.Interfaces[ifaceIdx].MTU = *args.MTU
			}
		}

		// Apply the changes to the system
		if err := nm.applyInterfaceConfig(args.Name); err != nil {
			return fmt.Errorf("failed to apply config: %w", err)
		}

	case ActionDelete:
		if ifaceIdx < 0 {
			return fmt.Errorf("interface not in configuration")
		}
		// Remove from config
		nm.config.Interfaces = append(nm.config.Interfaces[:ifaceIdx], nm.config.Interfaces[ifaceIdx+1:]...)

	default:
		return fmt.Errorf("unknown action: %s", args.Action)
	}

	return nil
}

// CreateVLAN creates a VLAN interface.
// L2 creation is delegated to LinkManager; this method handles L3 (IP addresses) and config updates.
func (nm *NetworkManager) CreateVLAN(args *CreateVLANArgs) (string, error) {
	auditLog("CreateVLAN", fmt.Sprintf("parent=%s vlan=%d", args.ParentInterface, args.VLANID))

	// Create LinkManager for L2 operations
	linkMgr, err := NewLinkManager()
	if err != nil {
		return "", fmt.Errorf("failed to create link manager: %w", err)
	}
	defer linkMgr.Close()

	// L2: Create the VLAN interface (delegated to LinkManager)
	vlanName, err := linkMgr.CreateVLAN(args)
	if err != nil {
		return "", err
	}

	// L3: Add IP addresses
	if len(args.IPv4) > 0 {
		link, err := netlink.LinkByName(vlanName)
		if err != nil {
			log.Printf("[NM] Warning: could not get VLAN link for IP assignment: %v", err)
		} else {
			for _, ipStr := range args.IPv4 {
				addr, err := netlink.ParseAddr(ipStr)
				if err != nil {
					log.Printf("[NM] Invalid IP address %s: %v", ipStr, err)
					continue
				}
				if err := netlink.AddrAdd(link, addr); err != nil {
					log.Printf("[NM] Failed to add IP %s: %v", ipStr, err)
				}
			}
		}
	}

	// Config: Add to config
	for i, iface := range nm.config.Interfaces {
		if iface.Name == args.ParentInterface {
			nm.config.Interfaces[i].VLANs = append(nm.config.Interfaces[i].VLANs, config.VLAN{
				ID:          fmt.Sprintf("%d", args.VLANID),
				Zone:        args.Zone,
				Description: args.Description,
				IPv4:        args.IPv4,
			})
			break
		}
	}

	return vlanName, nil
}

// DeleteVLAN deletes a VLAN interface.
// L2 deletion is delegated to LinkManager; this method handles config updates.
func (nm *NetworkManager) DeleteVLAN(interfaceName string) error {
	auditLog("DeleteVLAN", fmt.Sprintf("name=%s", interfaceName))

	// Create LinkManager for L2 operations
	linkMgr, err := NewLinkManager()
	if err != nil {
		return fmt.Errorf("failed to create link manager: %w", err)
	}
	defer linkMgr.Close()

	// L2: Delete the VLAN interface (delegated to LinkManager)
	if err := linkMgr.DeleteVLAN(interfaceName); err != nil {
		return err
	}

	// Config: Remove from config
	for i := range nm.config.Interfaces {
		for j, vlan := range nm.config.Interfaces[i].VLANs {
			expectedName := fmt.Sprintf("%s.%s", nm.config.Interfaces[i].Name, vlan.ID)
			if expectedName == interfaceName {
				nm.config.Interfaces[i].VLANs = append(
					nm.config.Interfaces[i].VLANs[:j],
					nm.config.Interfaces[i].VLANs[j+1:]...,
				)
				log.Printf("[NM] Removed VLAN %s from config", interfaceName)
				return nil
			}
		}
	}

	log.Printf("[NM] VLAN %s not found in config (orphaned or manually created)", interfaceName)
	return nil
}

// CreateBond creates a bonded interface.
// L2 creation is delegated to LinkManager; this method handles L3 (IP/DHCP) and config updates.
func (nm *NetworkManager) CreateBond(args *CreateBondArgs) error {
	auditLog("CreateBond", fmt.Sprintf("name=%s members=%v", args.Name, args.Interfaces))

	// Create LinkManager for L2 operations
	linkMgr, err := NewLinkManager()
	if err != nil {
		return fmt.Errorf("failed to create link manager: %w", err)
	}
	defer linkMgr.Close()

	// L2: Create the bond interface (delegated to LinkManager)
	if err := linkMgr.CreateBond(args); err != nil {
		return err
	}

	// L3: Add IP addresses or start DHCP
	if args.DHCP {
		if err := network.StartDHCPClient(args.Name); err != nil {
			log.Printf("[NM] Failed to start DHCP on bond %s: %v", args.Name, err)
			return fmt.Errorf("bond created but DHCP failed: %w", err)
		}
	} else if len(args.IPv4) > 0 {
		link, err := netlink.LinkByName(args.Name)
		if err != nil {
			log.Printf("[NM] Warning: could not get bond link for IP assignment: %v", err)
		} else {
			for _, ipStr := range args.IPv4 {
				addr, err := netlink.ParseAddr(ipStr)
				if err != nil {
					continue
				}
				netlink.AddrAdd(link, addr)
			}
		}
	}

	// Config: Add to config
	nm.config.Interfaces = append(nm.config.Interfaces, config.Interface{
		Name:        args.Name,
		Description: args.Description,
		Zone:        args.Zone,
		IPv4:        args.IPv4,
		DHCP:        args.DHCP,
		Bond: &config.Bond{
			Mode:       args.Mode,
			Interfaces: args.Interfaces,
		},
	})

	return nil
}

// DeleteBond deletes a bonded interface.
// L2 deletion is delegated to LinkManager; this method handles L3 (DHCP) and config updates.
func (nm *NetworkManager) DeleteBond(args *DeleteBondArgs) error {
	auditLog("DeleteBond", fmt.Sprintf("name=%s", args.Name))

	// Create LinkManager for L2 operations
	linkMgr, err := NewLinkManager()
	if err != nil {
		return fmt.Errorf("failed to create link manager: %w", err)
	}
	defer linkMgr.Close()

	// L2: Delete the bond interface (delegated to LinkManager)
	if err := linkMgr.DeleteBond(args.Name); err != nil {
		return err
	}

	// L3: Stop DHCP client if running
	if err := network.StopDHCPClient(args.Name); err != nil {
		log.Printf("[NM] Warning: failed to stop DHCP on %s: %v", args.Name, err)
	}

	// Config: Remove from config
	for i, iface := range nm.config.Interfaces {
		if iface.Name == args.Name {
			nm.config.Interfaces = append(nm.config.Interfaces[:i], nm.config.Interfaces[i+1:]...)
			break
		}
	}

	return nil
}

// applyInterfaceConfig applies configuration to a specific interface.
func (nm *NetworkManager) applyInterfaceConfig(ifaceName string) error {
	var ifaceCfg *config.Interface
	for i := range nm.config.Interfaces {
		if nm.config.Interfaces[i].Name == ifaceName {
			ifaceCfg = &nm.config.Interfaces[i]
			break
		}
	}
	if ifaceCfg == nil {
		return fmt.Errorf("interface %s not in config", ifaceName)
	}

	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return err
	}

	// Set MTU if specified
	if ifaceCfg.MTU > 0 {
		if err := netlink.LinkSetMTU(link, ifaceCfg.MTU); err != nil {
			log.Printf("[NM] Failed to set MTU on %s: %v", ifaceName, err)
		}
	}

	// Flush existing addresses
	addrs, _ := netlink.AddrList(link, unix.AF_INET)
	for _, addr := range addrs {
		netlink.AddrDel(link, &addr)
	}

	// Add configured addresses
	if !ifaceCfg.DHCP {
		for _, ipStr := range ifaceCfg.IPv4 {
			addr, err := netlink.ParseAddr(ipStr)
			if err != nil {
				log.Printf("[NM] Invalid IP %s: %v", ipStr, err)
				continue
			}
			if err := netlink.AddrAdd(link, addr); err != nil {
				log.Printf("[NM] Failed to add IP %s: %v", ipStr, err)
			}
		}
	}

	// Bring interface up
	if err := netlink.LinkSetUp(link); err != nil {
		log.Printf("[NM] Failed to bring up %s: %v", ifaceName, err)
	}

	return nil
}

// SnapshotInterfaces returns a deep copy of the current interface configuration.
func (nm *NetworkManager) SnapshotInterfaces() ([]config.Interface, error) {
	interfaces := make([]config.Interface, len(nm.config.Interfaces))
	copy(interfaces, nm.config.Interfaces)
	return interfaces, nil
}

// RestoreInterfaces restores the interface configuration from a snapshot.
func (nm *NetworkManager) RestoreInterfaces(snapshot []config.Interface) error {
	log.Printf("[NM] Restoring interface snapshot with %d interfaces", len(snapshot))

	// Update config
	nm.config.Interfaces = snapshot

	// Re-apply all interfaces in the snapshot
	// This ensures we return to the exact state
	var errs []error
	for _, iface := range snapshot {
		if err := nm.applyInterfaceConfig(iface.Name); err != nil {
			log.Printf("[NM] Failed to restore interface %s: %v", iface.Name, err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("encountered %d errors during restore (first: %v)", len(errs), errs[0])
	}

	return nil
}
