//go:build linux
// +build linux

package ctlplane

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/safchain/ethtool"
	"github.com/vishvananda/netlink"
)

// LinkManager provides L1/L2 link layer operations including hardware info,
// carrier status, and link creation (bonds, VLANs).
type LinkManager struct {
	handle *ethtool.Ethtool
}

// NewLinkManager creates a new link manager.
func NewLinkManager() (*LinkManager, error) {
	h, err := ethtool.NewEthtool()
	if err != nil {
		return nil, fmt.Errorf("failed to open ethtool handle: %w", err)
	}
	return &LinkManager{handle: h}, nil
}

// Close closes the ethtool handle.
func (em *LinkManager) Close() {
	em.handle.Close()
}

// LinkInfo contains link speed and settings.
type LinkInfo struct {
	Speed   uint32 // Mb/s
	Duplex  string // "full", "half", "unknown"
	Autoneg bool
}

// GetLinkInfo returns link speed, duplex, and autoneg status.
// For virtual NICs (virtio, veth, etc.), uses sysfs to avoid noisy warnings.
// For real hardware, uses the ethtool library to get full information.
func (em *LinkManager) GetLinkInfo(iface string) (*LinkInfo, error) {
	// Check if this is a virtual NIC that doesn't support GetLinkSettings properly
	if isVirtualNIC(iface) {
		return em.getLinkInfoFromSysfs(iface)
	}

	// Real hardware: use ethtool library for full information
	settings, err := em.handle.GetLinkSettings(iface)
	if err != nil {
		// Fallback to sysfs on error
		return em.getLinkInfoFromSysfs(iface)
	}

	duplex := "unknown"
	switch settings.Duplex {
	case ethtool.DUPLEX_FULL:
		duplex = "full"
	case ethtool.DUPLEX_HALF:
		duplex = "half"
	}

	return &LinkInfo{
		Speed:   settings.Speed,
		Duplex:  duplex,
		Autoneg: settings.Autoneg != 0,
	}, nil
}

// getLinkInfoFromSysfs reads link info from sysfs (no ethtool warnings).
func (em *LinkManager) getLinkInfoFromSysfs(iface string) (*LinkInfo, error) {
	speed := uint32(0)
	speedPath := fmt.Sprintf("/sys/class/net/%s/speed", iface)
	if data, err := os.ReadFile(speedPath); err == nil {
		speedStr := strings.TrimSpace(string(data))
		if speedStr != "-1" && speedStr != "" {
			fmt.Sscanf(speedStr, "%d", &speed)
		}
	}

	duplex := "unknown"
	duplexPath := fmt.Sprintf("/sys/class/net/%s/duplex", iface)
	if data, err := os.ReadFile(duplexPath); err == nil {
		duplexStr := strings.TrimSpace(string(data))
		if duplexStr == "full" || duplexStr == "half" {
			duplex = duplexStr
		}
	}

	return &LinkInfo{
		Speed:   speed,
		Duplex:  duplex,
		Autoneg: true, // Default to true as sysfs doesn't expose this reliably
	}, nil
}

// isVirtualNIC detects virtual NICs that don't support full ethtool features.
// These include virtio (QEMU/KVM), veth (containers), xen, etc.
func isVirtualNIC(name string) bool {
	// Check driver name via sysfs
	driverPath := fmt.Sprintf("/sys/class/net/%s/device/driver", name)
	if target, err := os.Readlink(driverPath); err == nil {
		// Get basename of driver path
		driver := target
		for i := len(target) - 1; i >= 0; i-- {
			if target[i] == '/' {
				driver = target[i+1:]
				break
			}
		}

		virtualDrivers := map[string]bool{
			"virtio_net": true, "veth": true, "tun": true, "tap": true,
			"bridge": true, "dummy": true, "xen_netfront": true, "vmxnet3": true,
			"hv_netvsc": true, "e1000": true, "e1000e": true,
		}
		if virtualDrivers[driver] {
			return true
		}
	}

	// Check modalias for "virtio" (reliable for KVM/QEMU)
	modaliasPath := fmt.Sprintf("/sys/class/net/%s/device/modalias", name)
	if data, err := os.ReadFile(modaliasPath); err == nil {
		modalias := string(data)
		if len(modalias) >= 6 && modalias[:6] == "virtio" {
			return true
		}
	}

	// No device directory means virtual interface
	devicePath := fmt.Sprintf("/sys/class/net/%s/device", name)
	if _, err := os.Stat(devicePath); os.IsNotExist(err) {
		return true
	}

	return false
}

// DriverInfo contains driver metadata from ethtool.
type DriverInfo struct {
	Driver   string
	Version  string
	Firmware string
	BusInfo  string
}

// GetDriverInfo returns driver information for an interface.
func (em *LinkManager) GetDriverInfo(iface string) (*DriverInfo, error) {
	info, err := em.handle.DriverInfo(iface)
	if err != nil {
		return nil, fmt.Errorf("ethtool DriverInfo failed for %s: %w", iface, err)
	}

	return &DriverInfo{
		Driver:   info.Driver,
		Version:  info.Version,
		Firmware: info.FwVersion,
		BusInfo:  info.BusInfo,
	}, nil
}

// GetStats returns interface statistics (name -> value map).
func (em *LinkManager) GetStats(iface string) (map[string]uint64, error) {
	stats, err := em.handle.Stats(iface)
	if err != nil {
		return nil, fmt.Errorf("ethtool Stats failed for %s: %w", iface, err)
	}
	return stats, nil
}

// GetOffloads returns the current offload feature state for an interface.
func (em *LinkManager) GetOffloads(iface string) (*InterfaceOffloads, error) {
	features, err := em.handle.Features(iface)
	if err != nil {
		return nil, fmt.Errorf("ethtool Features failed for %s: %w", iface, err)
	}

	offloads := &InterfaceOffloads{}

	// Map ethtool feature names to our struct
	// Feature names can vary slightly by driver, so we try common variants
	if val, ok := features["tcp-segmentation-offload"]; ok {
		offloads.TSO = val
	} else if val, ok := features["tx-tcp-segmentation"]; ok {
		offloads.TSO = val
	}

	if val, ok := features["generic-segmentation-offload"]; ok {
		offloads.GSO = val
	}

	if val, ok := features["generic-receive-offload"]; ok {
		offloads.GRO = val
	}

	if val, ok := features["large-receive-offload"]; ok {
		offloads.LRO = val
	}

	if val, ok := features["tx-checksum-ipv4"]; ok {
		offloads.TxChecksum = val
	} else if val, ok := features["tx-checksumming"]; ok {
		offloads.TxChecksum = val
	}

	if val, ok := features["rx-checksum"]; ok {
		offloads.RxChecksum = val
	} else if val, ok := features["rx-checksumming"]; ok {
		offloads.RxChecksum = val
	}

	if val, ok := features["tx-vlan-hw-insert"]; ok {
		offloads.TxVLAN = val
	}

	if val, ok := features["rx-vlan-hw-parse"]; ok {
		offloads.RxVLAN = val
	}

	if val, ok := features["scatter-gather"]; ok {
		offloads.SG = val
	}

	if val, ok := features["udp-fragmentation-offload"]; ok {
		offloads.UFO = val
	}

	if val, ok := features["ntuple-filters"]; ok {
		offloads.NTUPLE = val
	}

	if val, ok := features["receive-hashing"]; ok {
		offloads.RXHASH = val
	}

	return offloads, nil
}

// GetRingBuffer returns current ring buffer settings.
func (em *LinkManager) GetRingBuffer(iface string) (*RingBufferSettings, error) {
	ring, err := em.handle.GetRing(iface)
	if err != nil {
		return nil, fmt.Errorf("ethtool GetRing failed for %s: %w", iface, err)
	}

	return &RingBufferSettings{
		RxCurrent: ring.RxPending,
		RxMax:     ring.RxMaxPending,
		TxCurrent: ring.TxPending,
		TxMax:     ring.TxMaxPending,
	}, nil
}

// GetCoalesce returns current interrupt coalescing settings.
func (em *LinkManager) GetCoalesce(iface string) (*CoalesceSettings, error) {
	coal, err := em.handle.GetCoalesce(iface)
	if err != nil {
		return nil, fmt.Errorf("ethtool GetCoalesce failed for %s: %w", iface, err)
	}

	return &CoalesceSettings{
		RxUsecs:    coal.RxCoalesceUsecs,
		TxUsecs:    coal.TxCoalesceUsecs,
		RxFrames:   coal.RxMaxCoalescedFrames,
		TxFrames:   coal.TxMaxCoalescedFrames,
		AdaptiveRx: coal.UseAdaptiveRxCoalesce != 0,
		AdaptiveTx: coal.UseAdaptiveTxCoalesce != 0,
	}, nil
}

// ReadCarrier reads the carrier status from sysfs.
// Returns true if carrier is detected, false otherwise.
func ReadCarrier(iface string) (bool, error) {
	path := fmt.Sprintf("/sys/class/net/%s/carrier", iface)
	data, err := os.ReadFile(path)
	if err != nil {
		// If file doesn't exist or can't be read, assume no carrier
		return false, nil
	}
	return strings.TrimSpace(string(data)) == "1", nil
}

// ReadOperState reads the operational state from sysfs.
// Returns "up", "down", "unknown", etc.
func ReadOperState(iface string) string {
	path := fmt.Sprintf("/sys/class/net/%s/operstate", iface)
	data, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// ReadInterfaceStats reads basic stats from /sys/class/net/<iface>/statistics/
func ReadInterfaceStats(iface string) (*InterfaceStats, error) {
	basePath := fmt.Sprintf("/sys/class/net/%s/statistics", iface)

	readUint64 := func(name string) uint64 {
		data, err := os.ReadFile(fmt.Sprintf("%s/%s", basePath, name))
		if err != nil {
			return 0
		}
		val, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		return val
	}

	return &InterfaceStats{
		RxBytes:    readUint64("rx_bytes"),
		TxBytes:    readUint64("tx_bytes"),
		RxPackets:  readUint64("rx_packets"),
		TxPackets:  readUint64("tx_packets"),
		RxErrors:   readUint64("rx_errors"),
		TxErrors:   readUint64("tx_errors"),
		RxDropped:  readUint64("rx_dropped"),
		TxDropped:  readUint64("tx_dropped"),
		Collisions: readUint64("collisions"),
	}, nil
}

// ReadBondSlaves reads the list of slave interfaces for a bond.
func ReadBondSlaves(bondIface string) ([]string, error) {
	path := fmt.Sprintf("/sys/class/net/%s/bonding/slaves", bondIface)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	slaves := strings.Fields(strings.TrimSpace(string(data)))
	return slaves, nil
}

// ReadBondMode reads the bonding mode for a bond interface.
func ReadBondMode(bondIface string) string {
	path := fmt.Sprintf("/sys/class/net/%s/bonding/mode", bondIface)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Format is "balance-rr 0" - we want just the name
	parts := strings.Fields(strings.TrimSpace(string(data)))
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// ReadBondActiveSlaves reads currently active slaves for a bond.
func ReadBondActiveSlaves(bondIface string) ([]string, error) {
	path := fmt.Sprintf("/sys/class/net/%s/bonding/active_slave", bondIface)
	data, err := os.ReadFile(path)
	if err != nil {
		// For LACP/round-robin, all slaves are active, read from slaves
		return ReadBondSlaves(bondIface)
	}
	active := strings.TrimSpace(string(data))
	if active == "" {
		return []string{}, nil
	}
	return []string{active}, nil
}

// ReadVLANInfo reads VLAN parent and ID from /proc/net/vlan/<iface>.
func ReadVLANInfo(iface string) (parent string, vlanID int, err error) {
	path := fmt.Sprintf("/proc/net/vlan/%s", iface)
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Example line: "eth0.100  VID: 100       REORDER_HDR: 1  dev->priv_flags: 1"
		if strings.Contains(line, "VID:") {
			parts := strings.Fields(line)
			if len(parts) >= 1 {
				parent = strings.Split(parts[0], ".")[0]
			}
			for i, p := range parts {
				if p == "VID:" && i+1 < len(parts) {
					vlanID, _ = strconv.Atoi(parts[i+1])
					break
				}
			}
			return parent, vlanID, nil
		}
	}
	return "", 0, fmt.Errorf("VLAN info not found for %s", iface)
}

// GetInterfaceType determines the type of interface.
func GetInterfaceType(iface string) string {
	// Check for bond
	if _, err := os.Stat(fmt.Sprintf("/sys/class/net/%s/bonding", iface)); err == nil {
		return "bond"
	}
	// Check for VLAN (contains .)
	if strings.Contains(iface, ".") {
		return "vlan"
	}
	// Check for bridge
	if _, err := os.Stat(fmt.Sprintf("/sys/class/net/%s/bridge", iface)); err == nil {
		return "bridge"
	}
	// Check for WireGuard
	if strings.HasPrefix(iface, "wg") {
		return "wireguard"
	}
	// Check for tunnel
	if strings.HasPrefix(iface, "tun") || strings.HasPrefix(iface, "tap") {
		return "tunnel"
	}
	// Check for loopback
	if iface == "lo" {
		return "loopback"
	}
	// Default to ethernet
	return "ethernet"
}

// ============================================================================
// L1 Helper Functions
// ============================================================================

// IsVirtualInterface checks if an interface is a virtual container/VM interface
// by checking common prefix patterns (veth, docker, br-, virbr, vnet, tun, tap).
func IsVirtualInterface(name string) bool {
	virtPrefixes := []string{"veth", "docker", "br-", "virbr", "vnet", "tun", "tap"}
	for _, prefix := range virtPrefixes {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// GetDriverName returns the driver name for an interface from sysfs.
func GetDriverName(name string) string {
	path := fmt.Sprintf("/sys/class/net/%s/device/driver", name)
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	// Return just the driver name (basename)
	for i := len(target) - 1; i >= 0; i-- {
		if target[i] == '/' {
			return target[i+1:]
		}
	}
	return target
}

// GetLinkSpeedString returns the link speed as a formatted string (e.g., "1000 Mbps").
func GetLinkSpeedString(name string) string {
	path := fmt.Sprintf("/sys/class/net/%s/speed", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	speed := string(data)
	if speed == "-1\n" || speed == "" {
		return ""
	}
	return strings.TrimSpace(speed) + " Mbps"
}

// ============================================================================
// L2 Link Operations (VLAN, Bond, Link Control)
// ============================================================================

// CreateVLAN creates a VLAN interface on a parent interface.
// Uses *CreateVLANArgs from types.go, but only uses L2 fields (ParentInterface, VLANID).
// Returns the created VLAN interface name (e.g., "eth0.100").
func (lm *LinkManager) CreateVLAN(args *CreateVLANArgs) (string, error) {
	if args.ParentInterface == "" {
		return "", fmt.Errorf("parent interface is required")
	}
	if args.VLANID < 1 || args.VLANID > 4094 {
		return "", fmt.Errorf("VLAN ID must be between 1 and 4094")
	}

	// Get parent link
	parent, err := netlink.LinkByName(args.ParentInterface)
	if err != nil {
		return "", fmt.Errorf("parent interface not found: %w", err)
	}

	vlanName := fmt.Sprintf("%s.%d", args.ParentInterface, args.VLANID)

	// Create VLAN interface
	vlan := &netlink.Vlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        vlanName,
			ParentIndex: parent.Attrs().Index,
		},
		VlanId: args.VLANID,
	}

	if err := netlink.LinkAdd(vlan); err != nil {
		return "", fmt.Errorf("failed to create VLAN: %w", err)
	}

	// Bring up the VLAN interface
	if err := netlink.LinkSetUp(vlan); err != nil {
		return "", fmt.Errorf("VLAN created but failed to bring up: %w", err)
	}

	log.Printf("[Link] Created VLAN %s on %s", vlanName, args.ParentInterface)
	return vlanName, nil
}

// DeleteVLAN deletes a VLAN interface.
func (lm *LinkManager) DeleteVLAN(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("VLAN interface not found: %w", err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete VLAN: %w", err)
	}

	log.Printf("[Link] Deleted VLAN %s", name)
	return nil
}

// CreateBond creates a bonded interface.
// Uses *CreateBondArgs from types.go, but only uses L2 fields (Name, Mode, Interfaces).
func (lm *LinkManager) CreateBond(args *CreateBondArgs) error {
	if args.Name == "" {
		return fmt.Errorf("bond name is required")
	}
	if len(args.Interfaces) < 1 {
		return fmt.Errorf("at least one member interface is required")
	}

	// Map bond mode string to netlink constant
	mode := netlink.BOND_MODE_802_3AD // Default to LACP
	switch args.Mode {
	case "802.3ad", "lacp":
		mode = netlink.BOND_MODE_802_3AD
	case "active-backup":
		mode = netlink.BOND_MODE_ACTIVE_BACKUP
	case "balance-rr":
		mode = netlink.BOND_MODE_BALANCE_RR
	case "balance-xor":
		mode = netlink.BOND_MODE_BALANCE_XOR
	case "broadcast":
		mode = netlink.BOND_MODE_BROADCAST
	case "balance-tlb":
		mode = netlink.BOND_MODE_BALANCE_TLB
	case "balance-alb":
		mode = netlink.BOND_MODE_BALANCE_ALB
	}

	// Create bond interface
	bond := netlink.NewLinkBond(netlink.LinkAttrs{Name: args.Name})
	bond.Mode = mode

	if err := netlink.LinkAdd(bond); err != nil {
		return fmt.Errorf("failed to create bond: %w", err)
	}

	// Add member interfaces to bond
	for _, memberName := range args.Interfaces {
		member, err := netlink.LinkByName(memberName)
		if err != nil {
			log.Printf("[Link] Member interface %s not found: %v", memberName, err)
			continue
		}

		// Must bring down member before adding to bond
		if err := netlink.LinkSetDown(member); err != nil {
			log.Printf("[Link] Failed to bring down %s: %v", memberName, err)
		}

		if err := netlink.LinkSetMaster(member, bond); err != nil {
			log.Printf("[Link] Failed to add %s to bond: %v", memberName, err)
		}
	}

	// Bring up the bond
	if err := netlink.LinkSetUp(bond); err != nil {
		return fmt.Errorf("bond created but failed to bring up: %w", err)
	}

	log.Printf("[Link] Created bond %s with mode %s", args.Name, args.Mode)
	return nil
}

// DeleteBond deletes a bonded interface.
func (lm *LinkManager) DeleteBond(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("bond not found: %w", err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete bond: %w", err)
	}

	log.Printf("[Link] Deleted bond %s", name)
	return nil
}

// SetLinkUp brings an interface up.
func (lm *LinkManager) SetLinkUp(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("interface not found: %w", err)
	}
	return netlink.LinkSetUp(link)
}

// SetLinkDown brings an interface down.
func (lm *LinkManager) SetLinkDown(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("interface not found: %w", err)
	}
	return netlink.LinkSetDown(link)
}

// SetMTU sets the MTU on an interface.
func (lm *LinkManager) SetMTU(name string, mtu int) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("interface not found: %w", err)
	}
	return netlink.LinkSetMTU(link, mtu)
}

// GetMAC returns the MAC address of an interface.
func (lm *LinkManager) GetMAC(name string) (string, error) {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return "", fmt.Errorf("interface not found: %w", err)
	}
	return link.Attrs().HardwareAddr.String(), nil
}

// GetMTU returns the current MTU of an interface.
func (lm *LinkManager) GetMTU(name string) (int, error) {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return 0, fmt.Errorf("interface not found: %w", err)
	}
	return link.Attrs().MTU, nil
}
