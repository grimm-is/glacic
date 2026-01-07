// Package ctlplane provides the RPC interface between the privileged control plane
// and the unprivileged API server.
//
// # Type Categories
//
// This file contains RPC request/response types organized by domain:
//
// ## Status Types
//   - [Status]: System status (running, uptime, safe mode)
//   - [ServiceStatus]: Individual service status (DHCP, DNS)
//   - [InterfaceStatus]: Full interface details with stats
//   - [InterfaceState]: Interface state enum (up, down, no_carrier)
//
// ## Interface Management
//   - [UpdateInterfaceArgs]: Enable/disable/update interfaces
//   - [CreateVLANArgs], [CreateVLANReply]: VLAN creation
//   - [CreateBondArgs], [CreateBondReply]: Bond creation
//   - [InterfaceStats]: Traffic counters
//   - [InterfaceOffloads]: NIC offload features
//
// ## DHCP
//   - [DHCPLease]: Active lease with enriched data (hostname, vendor)
//   - [GetDHCPLeasesReply]: Lease list response
//   - [DHCPScope*]: DHCP scope CRUD types
//
// ## DNS
//   - [DNSSettings]: DNS server configuration
//   - [DNSRecord]: Local DNS records
//   - [SplitHorizon*]: Split-horizon DNS types
//
// ## Firewall
//   - [ZoneInfo]: Zone configuration with interfaces
//   - [PolicyInfo]: Policy rules between zones
//   - [FirewallDiagnostics]: Rule counters, chain stats
//
// ## VPN
//   - [VPNStatus]: WireGuard/Tailscale status
//   - [VPNPeerStatus]: Peer connection details
//   - [WireGuard*]: WireGuard-specific types
//
// ## Learning Engine
//   - [PendingFlow]: Unclassified traffic for learning
//   - [LearnedRule]: Suggested firewall rules
//
// ## System
//   - [ApplyConfigArgs]: Config reload request
//   - [SystemStatsReply]: CPU, memory, disk stats
//   - [BackupReply], [RestoreArgs]: Backup/restore
//
// # RPC Naming Convention
//
// All RPC types follow the pattern:
//   - Request: {MethodName}Args
//   - Response: {MethodName}Reply
//
// Empty is used for methods with no arguments.
package ctlplane

import (
	"time"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/device"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/learning/flowdb"
	"grimm.is/glacic/internal/services/scanner"
)

// GetSocketPath returns the path to the control plane socket.
// This uses brand.GetSocketPath() which supports environment overrides.
func GetSocketPath() string {
	return brand.GetSocketPath()
}

// SocketPath is the default Unix socket used for control plane communication.
// Deprecated: Use GetSocketPath() for environment-aware path resolution.
var SocketPath = brand.GetSocketPath()

// Status represents the current system status
type Status struct {
	Running         bool   `json:"running"`
	Uptime          string `json:"uptime"`
	ConfigFile      string `json:"config_file"`
	FirewallActive  bool   `json:"firewall_active"`            // Whether firewall rules are applied
	FirewallApplied string `json:"firewall_applied,omitempty"` // Timestamp of last rule application
	SafeMode        bool   `json:"safe_mode"`                  // True if system is in safe mode
}

// InterfaceState represents the operational state of a network interface
type InterfaceState string

const (
	InterfaceStateUp        InterfaceState = "up"         // Link is up and operational
	InterfaceStateDown      InterfaceState = "down"       // Link is administratively down
	InterfaceStateNoCarrier InterfaceState = "no_carrier" // Link is up but no cable/carrier
	InterfaceStateMissing   InterfaceState = "missing"    // Configured but not found in system
	InterfaceStateDisabled  InterfaceState = "disabled"   // Disabled in config
	InterfaceStateDegraded  InterfaceState = "degraded"   // Bond with missing members
	InterfaceStateError     InterfaceState = "error"      // Error state (e.g., driver issue)
)

// InterfaceStatus represents the current state of a network interface
type InterfaceStatus struct {
	// Identity
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"` // ethernet, vlan, bond, bridge, wireguard
	Description string `json:"description,omitempty"`

	// State
	State    InterfaceState `json:"state"`
	AdminUp  bool           `json:"admin_up"` // IFF_UP flag
	Carrier  bool           `json:"carrier"`  // Physical link detected
	Disabled bool           `json:"disabled"` // Config disabled flag

	// Addressing
	Zone           string   `json:"zone,omitempty"`
	IPv4Addrs      []string `json:"ipv4_addrs,omitempty"`
	IPv6Addrs      []string `json:"ipv6_addrs,omitempty"`
	ConfiguredIPv4 []string `json:"configured_ipv4,omitempty"` // From config
	DHCPEnabled    bool     `json:"dhcp_enabled"`
	Gateway        string   `json:"gateway,omitempty"`

	// Hardware
	MAC     string `json:"mac"`
	MTU     int    `json:"mtu"`
	Speed   uint32 `json:"speed,omitempty"`  // Mb/s (0 = unknown)
	Duplex  string `json:"duplex,omitempty"` // full, half, unknown
	Autoneg bool   `json:"autoneg"`          // Auto-negotiation enabled

	// Driver Info
	Driver        string `json:"driver,omitempty"`
	DriverVersion string `json:"driver_version,omitempty"`
	Firmware      string `json:"firmware,omitempty"`
	BusInfo       string `json:"bus_info,omitempty"` // PCI slot etc.

	// Bond specific
	BondMode           string   `json:"bond_mode,omitempty"`
	BondMembers        []string `json:"bond_members,omitempty"`
	BondMissingMembers []string `json:"bond_missing_members,omitempty"`
	BondActiveMembers  []string `json:"bond_active_members,omitempty"`

	// VLAN specific
	VLANParent string `json:"vlan_parent,omitempty"`
	VLANID     int    `json:"vlan_id,omitempty"`

	// Statistics (counters)
	Stats *InterfaceStats `json:"stats,omitempty"`

	// Offload Features
	Offloads *InterfaceOffloads `json:"offloads,omitempty"`

	// Ring Buffer Settings
	RingBuffer *RingBufferSettings `json:"ring_buffer,omitempty"`

	// Coalesce Settings (interrupt moderation)
	Coalesce *CoalesceSettings `json:"coalesce,omitempty"`

	// Enriched Data
	Vendor string `json:"vendor,omitempty"`
	Alias  string `json:"alias,omitempty"`
}

// InterfaceStats contains interface traffic counters
type InterfaceStats struct {
	RxBytes    uint64 `json:"rx_bytes"`
	TxBytes    uint64 `json:"tx_bytes"`
	RxPackets  uint64 `json:"rx_packets"`
	TxPackets  uint64 `json:"tx_packets"`
	RxErrors   uint64 `json:"rx_errors"`
	TxErrors   uint64 `json:"tx_errors"`
	RxDropped  uint64 `json:"rx_dropped"`
	TxDropped  uint64 `json:"tx_dropped"`
	Collisions uint64 `json:"collisions"`
}

// InterfaceOffloads represents NIC offload feature status
type InterfaceOffloads struct {
	// TCP/UDP Segmentation
	TSO bool `json:"tso"` // TCP Segmentation Offload
	GSO bool `json:"gso"` // Generic Segmentation Offload
	GRO bool `json:"gro"` // Generic Receive Offload
	LRO bool `json:"lro"` // Large Receive Offload

	// Checksum
	TxChecksum bool `json:"tx_checksum"` // TX checksum offload
	RxChecksum bool `json:"rx_checksum"` // RX checksum offload

	// VLAN
	TxVLAN bool `json:"tx_vlan"` // TX VLAN offload
	RxVLAN bool `json:"rx_vlan"` // RX VLAN offload

	// Other
	SG     bool `json:"sg"`     // Scatter-gather
	UFO    bool `json:"ufo"`    // UDP Fragmentation Offload
	NTUPLE bool `json:"ntuple"` // N-tuple filtering
	RXHASH bool `json:"rxhash"` // Receive hashing
}

// RingBufferSettings contains NIC ring buffer configuration
type RingBufferSettings struct {
	RxCurrent uint32 `json:"rx_current"`
	RxMax     uint32 `json:"rx_max"`
	TxCurrent uint32 `json:"tx_current"`
	TxMax     uint32 `json:"tx_max"`
}

// CoalesceSettings contains interrupt coalescing configuration
type CoalesceSettings struct {
	RxUsecs    uint32 `json:"rx_usecs"`
	TxUsecs    uint32 `json:"tx_usecs"`
	RxFrames   uint32 `json:"rx_frames"`
	TxFrames   uint32 `json:"tx_frames"`
	AdaptiveRx bool   `json:"adaptive_rx"`
	AdaptiveTx bool   `json:"adaptive_tx"`
}

// ServiceStatus represents the status of a service (DHCP, DNS, etc.)
type ServiceStatus struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

// DHCPLease represents an active DHCP client lease
type DHCPLease struct {
	Interface  string    `json:"interface"`
	IPAddress  string    `json:"ip_address"`
	MAC        string    `json:"mac"`
	SubnetMask string    `json:"subnet_mask"`
	Router     string    `json:"router"`
	DNSServers []string  `json:"dns_servers"`
	LeaseTime  string    `json:"lease_time"`
	ObtainedAt time.Time `json:"obtained_at"`
	ExpiresAt  time.Time `json:"expires_at"`

	// Enriched Data
	Hostname string   `json:"hostname,omitempty"`
	Vendor   string   `json:"vendor,omitempty"`
	Alias    string   `json:"alias,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	DeviceID string   `json:"device_id,omitempty"`
	Owner    string   `json:"owner,omitempty"`
	Type     string   `json:"type,omitempty"`
}

// --- RPC Request/Response types ---
// Go's net/rpc requires exported methods with signature:
//   func (t *T) MethodName(args *Args, reply *Reply) error

// Empty is used for RPC methods that take no arguments
type Empty struct{}

// UpgradeArgs is the request for Upgrade
type UpgradeArgs struct {
	// Checksum is the SHA256 hash of the new binary (required for security)
	Checksum string `json:"checksum"`
}

// UpgradeReply reply for system upgrade
type UpgradeReply struct {
	Success bool
	Error   string
}

// StageBinaryArgs is the request for StageBinary
type StageBinaryArgs struct {
	// Data is the binary data (base64 encoded for RPC transport)
	Data []byte `json:"data"`
	// Checksum is the expected SHA256 hash of the binary
	Checksum string `json:"checksum"`
	// Arch is the architecture of the binary (e.g., "linux/arm64")
	Arch string `json:"arch"`
}

// StageBinaryReply is the response for StageBinary
type StageBinaryReply struct {
	Success bool
	Error   string
	Path    string // Path where binary was staged
}

// GetStatusReply is the response for GetStatus
type GetStatusReply struct {
	Status Status
}

// GetConfigReply is the response for GetConfig
type GetConfigReply struct {
	Config config.Config
}

// GetInterfacesReply is the response for GetInterfaces
type GetInterfacesReply struct {
	Interfaces []InterfaceStatus
}

// GetServicesReply is the response for GetServices
type GetServicesReply struct {
	Services []ServiceStatus
}

// GetDHCPLeasesReply is the response for GetDHCPLeases
type GetDHCPLeasesReply struct {
	Leases []DHCPLease
}

// ApplyConfigArgs is the request for ApplyConfig
type ApplyConfigArgs struct {
	Config config.Config
}

// RestartServiceArgs is the request for RestartService
type RestartServiceArgs struct {
	ServiceName string // "dhcp", "dns", "firewall"
}

// --- Interface Management ---

// InterfaceAction represents an action to perform on an interface
type InterfaceAction string

const (
	ActionEnable  InterfaceAction = "enable"
	ActionDisable InterfaceAction = "disable"
	ActionUpdate  InterfaceAction = "update"
	ActionDelete  InterfaceAction = "delete"
)

// UpdateInterfaceArgs is the request for UpdateInterface
type UpdateInterfaceArgs struct {
	Name        string          `json:"name"`        // Interface name (required)
	Action      InterfaceAction `json:"action"`      // enable, disable, update, delete
	Zone        *string         `json:"zone"`        // Zone assignment (nil = no change)
	Description *string         `json:"description"` // Description (nil = no change)
	IPv4        []string        `json:"ipv4"`        // IPv4 addresses (nil = no change)
	DHCP        *bool           `json:"dhcp"`        // Use DHCP (nil = no change)
	MTU         *int            `json:"mtu"`         // MTU (nil = no change)
}

// SafeApplyInterfaceArgs is the request for SafeApplyInterface
type SafeApplyInterfaceArgs struct {
	UpdateArgs           *UpdateInterfaceArgs `json:"update_args"`
	ClientIP             string               `json:"client_ip"`
	RequireConfirmation  bool                 `json:"require_confirm"`
	PingTargets          []string             `json:"ping_targets"`
	PingTimeoutSeconds   int                  `json:"ping_timeout_seconds"`
	RollbackDelaySeconds int                  `json:"rollback_delay_seconds"`
}

// UpdateInterfaceReply is the response for UpdateInterface
type UpdateInterfaceReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// CreateVLANArgs is the request for CreateVLAN
type CreateVLANArgs struct {
	ParentInterface string   `json:"parent_interface"` // Parent interface name
	VLANID          int      `json:"vlan_id"`          // VLAN ID (1-4094)
	Zone            string   `json:"zone"`             // Zone assignment
	Description     string   `json:"description"`      // Description
	IPv4            []string `json:"ipv4"`             // IPv4 addresses
}

// CreateVLANReply is the response for CreateVLAN
type CreateVLANReply struct {
	Success       bool   `json:"success"`
	InterfaceName string `json:"interface_name"` // e.g., "eth0.100"
	Error         string `json:"error,omitempty"`
}

// DeleteVLANArgs is the request for DeleteVLAN
type DeleteVLANArgs struct {
	InterfaceName string `json:"interface_name"` // e.g., "eth0.100"
}

// CreateBondArgs is the request for CreateBond
type CreateBondArgs struct {
	Name        string   `json:"name"`        // Bond interface name (e.g., "bond0")
	Mode        string   `json:"mode"`        // 802.3ad, active-backup, balance-rr, etc.
	Interfaces  []string `json:"interfaces"`  // Member interfaces
	Zone        string   `json:"zone"`        // Zone assignment
	Description string   `json:"description"` // Description
	IPv4        []string `json:"ipv4"`        // IPv4 addresses
	DHCP        bool     `json:"dhcp"`        // Use DHCP
}

// CreateBondReply is the response for CreateBond
type CreateBondReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// DeleteBondArgs is the request for DeleteBond
type DeleteBondArgs struct {
	Name string `json:"name"` // Bond interface name
}

// GetAvailableInterfacesReply returns unassigned physical interfaces
type GetAvailableInterfacesReply struct {
	Interfaces []AvailableInterface `json:"interfaces"`
}

// AvailableInterface represents an interface that can be configured
type AvailableInterface struct {
	Name     string `json:"name"`
	MAC      string `json:"mac"`
	Driver   string `json:"driver"`
	LinkUp   bool   `json:"link_up"`
	Speed    string `json:"speed,omitempty"`
	Assigned bool   `json:"assigned"`  // Already in config
	InBond   bool   `json:"in_bond"`   // Part of a bond
	BondName string `json:"bond_name"` // Bond name if InBond
}

// --- HCL Editing (Advanced Mode) ---

// GetRawHCLReply is the response for GetRawHCL
type GetRawHCLReply struct {
	HCL          string               `json:"hcl"`
	Path         string               `json:"path"`
	LastModified string               `json:"last_modified"`
	Sections     []config.SectionInfo `json:"sections"`
}

// GetSectionHCLArgs is the request for GetSectionHCL
type GetSectionHCLArgs struct {
	SectionType string   `json:"section_type"` // e.g., "dhcp", "dns_server"
	Labels      []string `json:"labels"`       // For labeled blocks
}

// GetSectionHCLReply is the response for GetSectionHCL
type GetSectionHCLReply struct {
	HCL   string `json:"hcl"`
	Error string `json:"error,omitempty"`
}

// SetRawHCLArgs is the request for SetRawHCL
type SetRawHCLArgs struct {
	HCL string `json:"hcl"`
}

// SetRawHCLReply is the response for SetRawHCL
type SetRawHCLReply struct {
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
	RestartHint string `json:"restart_hint,omitempty"` // Suggestion to restart if needed
}

// SetSectionHCLArgs is the request for SetSectionHCL
type SetSectionHCLArgs struct {
	SectionType string   `json:"section_type"` // e.g., "dhcp"
	Labels      []string `json:"labels"`       // For labeled blocks
	HCL         string   `json:"hcl"`          // New HCL content for section
}

// SetSectionHCLReply is the response for SetSectionHCL
type SetSectionHCLReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// DeleteSectionArgs is the request for DeleteSection
type DeleteSectionArgs struct {
	SectionType string `json:"section_type"` // e.g., "dhcp", "dns_server"
}

// DeleteSectionByLabelArgs is the request for DeleteSectionByLabel
type DeleteSectionByLabelArgs struct {
	SectionType string   `json:"section_type"` // e.g., "interface", "policy"
	Labels      []string `json:"labels"`       // Labels for the block
}

// DeleteSectionReply is the response for DeleteSection and DeleteSectionByLabel
type DeleteSectionReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ValidateHCLArgs is the request for ValidateHCL
type ValidateHCLArgs struct {
	HCL string `json:"hcl"`
}

// ValidateHCLReply is the response for ValidateHCL
type ValidateHCLReply struct {
	Valid       bool                   `json:"valid"`
	Diagnostics []config.HCLDiagnostic `json:"diagnostics,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

// SaveConfigReply is the response for SaveConfig
type SaveConfigReply struct {
	Success    bool   `json:"success"`
	BackupPath string `json:"backup_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

// --- Safe Apply with Rollback ---

// SafeApplyArgs is the request for SafeApply
type SafeApplyArgs struct {
	Config          config.Config `json:"config"`
	ClientIP        string        `json:"client_ip"`
	RequireConfirm  bool          `json:"require_confirm"`
	RollbackSeconds int           `json:"rollback_seconds"` // 0 = default (30s)
}

// SafeApplyReply is the response for SafeApply
type SafeApplyReply struct {
	Success         bool   `json:"success"`
	PendingID       string `json:"pending_id,omitempty"`
	Message         string `json:"message"`
	RollbackTime    string `json:"rollback_time,omitempty"`
	RequiresConfirm bool   `json:"requires_confirm"`
	BackupVersion   int    `json:"backup_version,omitempty"`
	Error           string `json:"error,omitempty"`
}

// TriggerTaskArgs is the request for TriggerTask
type TriggerTaskArgs struct {
	TaskName string `json:"task_name"`
}

// TriggerTaskReply is the response for TriggerTask
type TriggerTaskReply struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// ConfirmApplyArgs is the request for ConfirmApply
type ConfirmApplyArgs struct {
	PendingID string `json:"pending_id"`
}

// CancelApplyArgs is the request for CancelApplyInterface
type CancelApplyArgs struct {
	ApplyID string `json:"apply_id"`
}

// ConfirmApplyReply is the response for ConfirmApply
type ConfirmApplyReply struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// GetPendingApplyReply is the response for GetPendingApply
type GetPendingApplyReply struct {
	HasPending    bool   `json:"has_pending"`
	PendingID     string `json:"pending_id,omitempty"`
	RollbackTime  string `json:"rollback_time,omitempty"`
	SecondsLeft   int    `json:"seconds_left,omitempty"`
	ClientIP      string `json:"client_ip,omitempty"`
	BackupVersion int    `json:"backup_version,omitempty"`
}

// --- Backup Management ---

// BackupInfo contains metadata about a backup
type BackupInfo struct {
	Version     int    `json:"version"`
	Timestamp   string `json:"timestamp"`
	Description string `json:"description"`
	Size        int64  `json:"size"`
	IsAuto      bool   `json:"is_auto"`
	Pinned      bool   `json:"pinned"` // Pinned backups are never auto-pruned
}

// ListBackupsReply is the response for ListBackups
type ListBackupsReply struct {
	Backups    []BackupInfo `json:"backups"`
	MaxBackups int          `json:"max_backups"`
	Error      string       `json:"error,omitempty"`
}

// CreateBackupArgs is the request for CreateBackup
type CreateBackupArgs struct {
	Description string `json:"description"`
	Pinned      bool   `json:"pinned"` // If true, backup won't be auto-pruned
}

// CreateBackupReply is the response for CreateBackup
type CreateBackupReply struct {
	Success bool       `json:"success"`
	Backup  BackupInfo `json:"backup,omitempty"`
	Error   string     `json:"error,omitempty"`
}

// RestoreBackupArgs is the request for RestoreBackup
type RestoreBackupArgs struct {
	Version int `json:"version"`
}

// RestoreBackupReply is the response for RestoreBackup
type RestoreBackupReply struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// GetBackupContentArgs is the request for GetBackupContent
type GetBackupContentArgs struct {
	Version int `json:"version"`
}

// GetBackupContentReply is the response for GetBackupContent
type GetBackupContentReply struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

// PinBackupArgs is the request for PinBackup/UnpinBackup
type PinBackupArgs struct {
	Version int  `json:"version"`
	Pinned  bool `json:"pinned"`
}

// PinBackupReply is the response for PinBackup/UnpinBackup
type PinBackupReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// SetMaxBackupsArgs is the request for SetMaxBackups
type SetMaxBackupsArgs struct {
	MaxBackups int `json:"max_backups"`
}

// SetMaxBackupsReply is the response for SetMaxBackups
type SetMaxBackupsReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// --- System Logs ---

// LogSource represents a log source type
type LogSource string

const (
	LogSourceDmesg    LogSource = "dmesg"
	LogSourceSyslog   LogSource = "syslog"
	LogSourceNftables LogSource = "nftables"
	LogSourceDHCP     LogSource = "dhcp"
	LogSourceDNS      LogSource = "dns"
	LogSourceFirewall LogSource = "firewall"
	LogSourceAPI      LogSource = "api"
	LogSourceCtlPlane LogSource = "ctlplane"
	LogSourceGateway  LogSource = "gateway"
	LogSourceAuth     LogSource = "auth"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp string            `json:"timestamp"`
	Source    LogSource         `json:"source"`
	Level     string            `json:"level"` // debug, info, warn, error
	Message   string            `json:"message"`
	Facility  string            `json:"facility,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// LogSourceInfo provides metadata about a log source
type LogSourceInfo struct {
	ID          LogSource `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

// GetLogsArgs is the request for GetLogs
type GetLogsArgs struct {
	Source string `json:"source,omitempty"`
	Level  string `json:"level,omitempty"`
	Search string `json:"search,omitempty"`
	Since  string `json:"since,omitempty"`
	Until  string `json:"until,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// GetLogsReply is the response for GetLogs
type GetLogsReply struct {
	Entries []LogEntry `json:"entries"`
	Error   string     `json:"error,omitempty"`
}

// GetLogSourcesReply is the response for GetLogSources
type GetLogSourcesReply struct {
	Sources []LogSourceInfo `json:"sources"`
}

// LogStats provides statistics about firewall logging
type LogStats struct {
	TotalPackets    int64            `json:"total_packets"`
	DroppedPackets  int64            `json:"dropped_packets"`
	AcceptedPackets int64            `json:"accepted_packets"`
	ByInterface     map[string]int64 `json:"by_interface"`
	ByProtocol      map[string]int64 `json:"by_protocol"`
	TopSources      []IPCount        `json:"top_sources"`
	TopDestinations []IPCount        `json:"top_destinations"`
}

// IPCount represents an IP address with a count
type IPCount struct {
	IP    string `json:"ip"`
	Count int64  `json:"count"`
}

// GetLogStatsReply is the response for GetLogStats
type GetLogStatsReply struct {
	Stats LogStats `json:"stats"`
	Error string   `json:"error,omitempty"`
}

// --- IPSet Management ---

// ListIPSetsReply is the response for ListIPSets
type ListIPSetsReply struct {
	IPSets []firewall.IPSetMetadata
	Error  string
}

// GetIPSetArgs is the request for GetIPSet
type GetIPSetArgs struct {
	Name string
}

// GetIPSetReply is the response for GetIPSet
type GetIPSetReply struct {
	Metadata firewall.IPSetMetadata
	Error    string
}

// RefreshIPSetArgs is the request for RefreshIPSet
type RefreshIPSetArgs struct {
	Name string
}

// GetIPSetElementsArgs is the request for GetIPSetElements
type GetIPSetElementsArgs struct {
	Name string
}

// GetIPSetElementsReply is the response for GetIPSetElements
type GetIPSetElementsReply struct {
	Elements []string
	Error    string
}

// GetIPSetCacheInfoReply is the response for GetIPSetCacheInfo
type GetIPSetCacheInfoReply struct {
	Info map[string]interface{}
}

// AddIPSetEntryArgs is the request for AddIPSetEntry
type AddIPSetEntryArgs struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// AddIPSetEntryReply is the response for AddIPSetEntry
type AddIPSetEntryReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// RemoveIPSetEntryArgs is the request for RemoveIPSetEntry
type RemoveIPSetEntryArgs struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// RemoveIPSetEntryReply is the response for RemoveIPSetEntry
type RemoveIPSetEntryReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// CheckIPSetEntryArgs is the request for CheckIPSetEntry
type CheckIPSetEntryArgs struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// CheckIPSetEntryReply is the response for CheckIPSetEntry
type CheckIPSetEntryReply struct {
	Exists bool   `json:"exists"`
	Error  string `json:"error,omitempty"`
}

// --- System Operations ---

// SystemRebootArgs is the request for SystemReboot
type SystemRebootArgs struct {
	Force bool `json:"force"`
}

// SystemRebootReply is the response for SystemReboot
type SystemRebootReply struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// SystemStats represents system resource usage
type SystemStats struct {
	CPUUsage    float64 `json:"cpu_usage"`    // Percentage (0-100)
	MemoryTotal uint64  `json:"memory_total"` // Bytes
	MemoryUsed  uint64  `json:"memory_used"`  // Bytes
	DiskTotal   uint64  `json:"disk_total"`   // Bytes
	DiskUsed    uint64  `json:"disk_used"`    // Bytes
	LoadAverage float64 `json:"load_average"` // 1-minute load avg
	Uptime      uint64  `json:"uptime"`       // Seconds
	SafeMode    bool    `json:"safe_mode"`    // True if system is in safe mode due to crash loops
}

// GetSystemStatsReply is the response for GetSystemStats
type GetSystemStatsReply struct {
	Stats SystemStats `json:"stats"`
	Error string      `json:"error,omitempty"`
}

// GetNotificationsArgs is the request for GetNotifications
type GetNotificationsArgs struct {
	SinceID int64 `json:"since_id"` // Return notifications with ID > SinceID
}

// GetNotificationsReply is the response for GetNotifications
type GetNotificationsReply struct {
	Notifications []Notification `json:"notifications"`
	LastID        int64          `json:"last_id"` // Latest notification ID for cursoring
}

// --- Learning Firewall ---

// GetLearningRulesArgs is the request for GetLearningRules
type GetLearningRulesArgs struct {
	Status string `json:"status"` // pending, approved, denied, ignored
}

// GetLearningRulesReply is the response for GetLearningRules
type GetLearningRulesReply struct {
	Rules []*learning.PendingRule `json:"rules"`
	Error string                  `json:"error,omitempty"`
}

// GetLearningRuleArgs is the request for GetLearningRule
type GetLearningRuleArgs struct {
	ID string `json:"id"`
}

// GetLearningRuleReply is the response for GetLearningRule
type GetLearningRuleReply struct {
	Rule  *learning.PendingRule `json:"rule"`
	Error string                `json:"error,omitempty"`
}

// LearningRuleActionArgs is the request for Approve/Deny/Ignore/Delete Rule
type LearningRuleActionArgs struct {
	ID   string `json:"id"`
	User string `json:"user,omitempty"` // Who performed the action
}

// LearningRuleActionReply is the response for Approve/Deny/Ignore/Delete Rule
type LearningRuleActionReply struct {
	Success bool                  `json:"success"`
	Rule    *learning.PendingRule `json:"rule,omitempty"`
	Error   string                `json:"error,omitempty"`
}

// GetLearningStatsReply is the response for GetLearningStats
type GetLearningStatsReply struct {
	Stats map[string]interface{} `json:"stats"`
	Error string                 `json:"error,omitempty"`
}

// --- Uplink Management ---

// UplinkStatus represents the status of an uplink (DTO)
type UplinkStatus struct {
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	Interface     string            `json:"interface"`
	Gateway       string            `json:"gateway,omitempty"`
	PublicIP      string            `json:"public_ip,omitempty"`
	Healthy       bool              `json:"healthy"`
	Enabled       bool              `json:"enabled"`
	Latency       string            `json:"latency"`
	PacketLoss    float64           `json:"packet_loss"`
	Throughput    uint64            `json:"throughput"`
	Tier          int               `json:"tier"`
	Weight        int               `json:"weight"`
	DynamicWeight int               `json:"dynamic_weight,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
}

// UplinkGroupStatus represents the status of an uplink group
type UplinkGroupStatus struct {
	Name            string         `json:"name"`
	Uplinks         []UplinkStatus `json:"uplinks"`
	ActiveUplinks   []string       `json:"active_uplinks"`
	ActiveTier      int            `json:"active_tier"`
	FailoverMode    string         `json:"failover_mode"`
	LoadBalanceMode string         `json:"load_balance_mode"`
}

// GetUplinkGroupsReply is the response for GetUplinkGroups
type GetUplinkGroupsReply struct {
	Groups []UplinkGroupStatus `json:"groups"`
	Error  string              `json:"error,omitempty"`
}

// SwitchUplinkArgs is the request for SwitchUplink
type SwitchUplinkArgs struct {
	GroupName  string `json:"group_name"`
	UplinkName string `json:"uplink_name"` // Empty for auto/best
}

// SwitchUplinkReply is the response for SwitchUplink
type SwitchUplinkReply struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// ToggleUplinkArgs is the request for ToggleUplink
type ToggleUplinkArgs struct {
	GroupName  string `json:"group_name"`
	UplinkName string `json:"uplink_name"`
	Enabled    bool   `json:"enabled"`
}

// ToggleUplinkReply is the response for ToggleUplink
type ToggleUplinkReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// --- Flow Management ---

// GetFlowsArgs is the request for GetFlows
type GetFlowsArgs struct {
	State  string `json:"state"` // pending, allowed, denied
	Search string `json:"search,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// GetFlowsReply is the response for GetFlows
type GetFlowsReply struct {
	Flows       []flowdb.FlowWithHints `json:"flows"`
	TotalCounts map[string]int64       `json:"total_counts"` // pending, allowed, etc.
	Error       string                 `json:"error,omitempty"`
}

// FlowActionArgs is the request for ApproveFlow/DenyFlow/DeleteFlow
type FlowActionArgs struct {
	ID    int64  `json:"id"`
	State string `json:"state,omitempty"` // For update state
}

// FlowActionReply is the response for FlowAction
type FlowActionReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// --- Topology / Discovery ---

// TopologyNeighbor represents a discovered neighbor device
type TopologyNeighbor struct {
	Interface       string `json:"interface"`
	ChassisID       string `json:"chassis_id"`
	PortID          string `json:"port_id"`
	SystemName      string `json:"system_name"`
	SystemDesc      string `json:"system_desc"`
	LastSeenSeconds int    `json:"last_seen_seconds"`

	// Enriched Data
	Vendor string `json:"vendor,omitempty"`
	Alias  string `json:"alias,omitempty"`
}

// TopologyGraph represents the network graph
type TopologyGraph struct {
	Nodes []TopologyNode `json:"nodes"`
	Links []TopologyLink `json:"links"`
}

// TopologyNode represents a node in the graph
type TopologyNode struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Type        string `json:"type"` // router, switch, device
	Group       int    `json:"group"`
	IP          string `json:"ip,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
}

// TopologyLink represents a link between nodes
type TopologyLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

// GetTopologyReply is the response for GetTopology
type GetTopologyReply struct {
	Neighbors []TopologyNeighbor `json:"neighbors"`
	Graph     TopologyGraph      `json:"graph"`
	Error     string             `json:"error,omitempty"`
}

// --- Network Scanner ---

// StartScanNetworkArgs is the request for StartScanNetwork
type StartScanNetworkArgs struct {
	CIDR           string `json:"cidr"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// StartScanNetworkReply is the response for StartScanNetwork
type StartScanNetworkReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// GetScanStatusReply is the response for GetScanStatus
type GetScanStatusReply struct {
	Scanning   bool                `json:"scanning"`
	LastResult *scanner.ScanResult `json:"last_result,omitempty"` // Metadata only? Or full?
	// Let's assume full result or a summary. The UI handler splits it.
	// Ideally we return metadata here and full result in separate call if large.
	// The UI `Status` endpoint returns `last_scan` metadata.
	// `LastResult` returns full.
}

// Full result RPC
type GetScanResultReply struct {
	Result *scanner.ScanResult `json:"result,omitempty"`
}

// GetCommonPortsReply is the response for GetCommonPorts
type GetCommonPortsReply struct {
	Ports []scanner.Port `json:"ports"`
}

// ScanHostArgs is the request for ScanHost
type ScanHostArgs struct {
	IP string `json:"ip"`
}

// ScanHostReply is the response for ScanHost
type ScanHostReply struct {
	Result *scanner.HostResult `json:"result,omitempty"`
	Error  string              `json:"error,omitempty"`
}

// --- Wake-on-LAN ---

// WakeOnLANArgs is the request for WakeOnLAN
type WakeOnLANArgs struct {
	MAC       string `json:"mac"`
	Interface string `json:"interface,omitempty"`
}

// WakeOnLANReply is the response for WakeOnLAN
type WakeOnLANReply struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// --- Ping (Connectivity Verification) ---

// PingArgs is the request for Ping
type PingArgs struct {
	Target         string `json:"target"`          // IP address to ping
	TimeoutSeconds int    `json:"timeout_seconds"` // Timeout per ping (default 5)
}

// PingReply is the response for Ping
type PingReply struct {
	Reachable bool   `json:"reachable"`
	RTTMs     int    `json:"rtt_ms,omitempty"` // Round-trip time in milliseconds
	Error     string `json:"error,omitempty"`
}

// --- Device Identity Management ---

// UpdateDeviceIdentityArgs is the request for UpdateDeviceIdentity
type UpdateDeviceIdentityArgs struct {
	ID    string   `json:"id"`
	Alias *string  `json:"alias"`
	Owner *string  `json:"owner"`
	Type  *string  `json:"type"`
	Tags  []string `json:"tags"`
}

// UpdateDeviceIdentityReply is the response for UpdateDeviceIdentity
type UpdateDeviceIdentityReply struct {
	Identity *device.DeviceIdentity `json:"identity,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

// LinkMACArgs is the request for LinkMAC
type LinkMACArgs struct {
	MAC        string `json:"mac"`
	IdentityID string `json:"identity_id"`
}

// UnlinkMACArgs is the request for UnlinkMAC
type UnlinkMACArgs struct {
	MAC string `json:"mac"`
}

// ListDevicesReply is the response for ListDevices (optional for management UI)
type ListDevicesReply struct {
	Devices []device.DeviceIdentity `json:"devices"`
	Error   string                  `json:"error,omitempty"`
}

// --- Network Device Discovery ---

// NetworkDevice represents a device observed on the network
type NetworkDevice struct {
	MAC         string   `json:"mac"`
	IPs         []string `json:"ips"`
	Interface   string   `json:"interface"`
	FirstSeen   int64    `json:"first_seen"` // Unix timestamp
	LastSeen    int64    `json:"last_seen"`  // Unix timestamp
	Hostname    string   `json:"hostname,omitempty"`
	Vendor      string   `json:"vendor,omitempty"`
	Alias       string   `json:"alias,omitempty"`
	HopCount    int      `json:"hop_count"`
	Flags       []string `json:"flags,omitempty"`
	PacketCount int64    `json:"packet_count"`

	// mDNS Profiling
	MDNSServices   []string          `json:"mdns_services,omitempty"`
	MDNSHostname   string            `json:"mdns_hostname,omitempty"`
	MDNSTXTRecords map[string]string `json:"mdns_txt,omitempty"`

	// DHCP Profiling
	DHCPFingerprint string           `json:"dhcp_fingerprint,omitempty"`
	DHCPVendorClass string           `json:"dhcp_vendor_class,omitempty"`
	DHCPHostname    string           `json:"dhcp_hostname,omitempty"`
	DHCPClientID    string           `json:"dhcp_client_id,omitempty"`
	DHCPOptions     map[uint8]string `json:"dhcp_options,omitempty"`

	// Classification
	DeviceType  string `json:"device_type,omitempty"`
	DeviceModel string `json:"device_model,omitempty"`
}

// GetNetworkDevicesReply is the response for GetNetworkDevices
type GetNetworkDevicesReply struct {
	Devices []NetworkDevice `json:"devices"`
	Error   string          `json:"error,omitempty"`
}

// GetConfigDiffRequest is the request for GetConfigDiff
type GetConfigDiffRequest struct{}

// GetConfigDiffReply is the response for GetConfigDiff
type GetConfigDiffReply struct {
	Diff  string `json:"diff"`
	Error string `json:"error,omitempty"`
}

// --- Safe Mode ---

// SafeModeStatusReply is the response for IsInSafeMode
type SafeModeStatusReply struct {
	InSafeMode bool `json:"in_safe_mode"`
}
