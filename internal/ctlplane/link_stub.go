//go:build !linux
// +build !linux

package ctlplane

import "fmt"

// LinkManager provides L1/L2 link layer operations (Stub).
type LinkManager struct{}

// NewLinkManager creates a new link manager (Stub).
func NewLinkManager() (*LinkManager, error) {
	return &LinkManager{}, nil
}

// Close closes the ethtool handle (Stub).
func (em *LinkManager) Close() {}

// LinkInfo contains link speed and settings.
type LinkInfo struct {
	Speed   uint32 // Mb/s
	Duplex  string // "full", "half", "unknown"
	Autoneg bool
}

// GetLinkInfo returns link info (Stub).
func (em *LinkManager) GetLinkInfo(iface string) (*LinkInfo, error) {
	return &LinkInfo{Speed: 1000, Duplex: "full", Autoneg: true}, nil
}

// DriverInfo contains driver metadata.
type DriverInfo struct {
	Driver   string
	Version  string
	Firmware string
	BusInfo  string
}

// GetDriverInfo returns driver info (Stub).
func (em *LinkManager) GetDriverInfo(iface string) (*DriverInfo, error) {
	return &DriverInfo{Driver: "stub", Version: "1.0", Firmware: "0.0", BusInfo: "0000:00:00.0"}, nil
}

// GetStats returns interface stats (Stub).
func (em *LinkManager) GetStats(iface string) (map[string]uint64, error) {
	return map[string]uint64{}, nil
}

// GetOffloads returns offload settings (Stub).
func (em *LinkManager) GetOffloads(iface string) (*InterfaceOffloads, error) {
	return &InterfaceOffloads{}, nil
}

// GetRingBuffer returns ring buffer settings (Stub).
func (em *LinkManager) GetRingBuffer(iface string) (*RingBufferSettings, error) {
	return &RingBufferSettings{}, nil
}

// GetCoalesce returns coalesce settings (Stub).
func (em *LinkManager) GetCoalesce(iface string) (*CoalesceSettings, error) {
	return &CoalesceSettings{}, nil
}

// ReadCarrier reads the carrier status (Stub).
func ReadCarrier(iface string) (bool, error) {
	return true, nil
}

// ReadOperState reads the operational state (Stub).
func ReadOperState(iface string) string {
	return "up"
}

// ReadInterfaceStats reads basic interface stats (Stub).
func ReadInterfaceStats(iface string) (*InterfaceStats, error) {
	return &InterfaceStats{}, nil
}

// ReadBondSlaves reads bond slaves (Stub).
func ReadBondSlaves(bondIface string) ([]string, error) {
	return []string{}, nil
}

// ReadBondMode reads bond mode (Stub).
func ReadBondMode(bondIface string) string {
	return "balance-rr"
}

// ReadBondActiveSlaves reads active bond slaves (Stub).
func ReadBondActiveSlaves(bondIface string) ([]string, error) {
	return []string{}, nil
}

// ReadVLANInfo reads VLAN info (Stub).
func ReadVLANInfo(iface string) (parent string, vlanID int, err error) {
	return "", 0, fmt.Errorf("not supported on this platform")
}

// GetInterfaceType determines interface type (Stub).
func GetInterfaceType(iface string) string {
	return "ethernet"
}

func IsVirtualInterface(name string) bool {
	return false
}

func GetDriverName(name string) string {
	return "stub"
}

func GetLinkSpeedString(name string) string {
	return "1000 Mbps"
}

// CreateVLAN creates a VLAN interface (Stub).
func (lm *LinkManager) CreateVLAN(args *CreateVLANArgs) (string, error) {
	return "", fmt.Errorf("not supported on this platform")
}

// DeleteVLAN deletes a VLAN interface (Stub).
func (lm *LinkManager) DeleteVLAN(name string) error {
	return fmt.Errorf("not supported on this platform")
}

// CreateBond creates a bond interface (Stub).
func (lm *LinkManager) CreateBond(args *CreateBondArgs) error {
	return fmt.Errorf("not supported on this platform")
}

// DeleteBond deletes a bond interface (Stub).
func (lm *LinkManager) DeleteBond(name string) error {
	return fmt.Errorf("not supported on this platform")
}

// SetLinkUp brings an interface up (Stub).
func (lm *LinkManager) SetLinkUp(name string) error {
	return nil
}

// SetLinkDown brings an interface down (Stub).
func (lm *LinkManager) SetLinkDown(name string) error {
	return nil
}

// SetMTU sets the MTU (Stub).
func (lm *LinkManager) SetMTU(name string, mtu int) error {
	return nil
}

// GetMAC returns the MAC address (Stub).
func (lm *LinkManager) GetMAC(name string) (string, error) {
	return "00:00:00:00:00:00", nil
}

// GetMTU returns the MTU (Stub).
func (lm *LinkManager) GetMTU(name string) (int, error) {
	return 1500, nil
}
