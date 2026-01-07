package ctlplane

import (
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/device"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/learning/flowdb"
	"grimm.is/glacic/internal/services/scanner"
)

// ControlPlaneClient defines the interface for communicating with the control plane.
// This interface enables mocking in unit tests.
type ControlPlaneClient interface {
	Close() error

	// --- Status & Config ---
	GetStatus() (*Status, error)
	GetConfig() (*config.Config, error)
	GetInterfaces() ([]InterfaceStatus, error)
	GetServices() ([]ServiceStatus, error)
	ApplyConfig(cfg *config.Config) error
	RestartService(serviceName string) error
	Reboot() error
	GetDHCPLeases() ([]DHCPLease, error)

	// --- Interface Management ---
	GetAvailableInterfaces() ([]AvailableInterface, error)
	UpdateInterface(args *UpdateInterfaceArgs) (*UpdateInterfaceReply, error)
	CreateVLAN(args *CreateVLANArgs) (*CreateVLANReply, error)
	DeleteVLAN(ifaceName string) (*UpdateInterfaceReply, error)
	CreateBond(args *CreateBondArgs) (*CreateBondReply, error)
	DeleteBond(name string) (*UpdateInterfaceReply, error)

	// --- HCL Editing ---
	GetRawHCL() (*GetRawHCLReply, error)
	GetSectionHCL(sectionType string, labels ...string) (*GetSectionHCLReply, error)
	SetRawHCL(hcl string) (*SetRawHCLReply, error)
	SetSectionHCL(sectionType string, hcl string, labels ...string) (*SetSectionHCLReply, error)
	DeleteSection(sectionType string) (*DeleteSectionReply, error)
	DeleteSectionByLabel(sectionType string, labels ...string) (*DeleteSectionReply, error)
	ValidateHCL(hcl string) (*ValidateHCLReply, error)
	SaveConfig() (*SaveConfigReply, error)
	GetConfigDiff() (string, error)

	// --- Backup Management ---
	ListBackups() (*ListBackupsReply, error)
	Upgrade(checksum string) error
	StageBinary(data []byte, checksum, arch string) (*StageBinaryReply, error)
	CreateBackup(description string, pinned bool) (*CreateBackupReply, error)
	RestoreBackup(version int) (*RestoreBackupReply, error)
	GetBackupContent(version int) (*GetBackupContentReply, error)
	PinBackup(version int, pinned bool) (*PinBackupReply, error)
	SetMaxBackups(maxBackups int) (*SetMaxBackupsReply, error)

	// --- Logs ---
	GetLogs(args *GetLogsArgs) (*GetLogsReply, error)
	GetLogSources() (*GetLogSourcesReply, error)
	GetLogStats() (*GetLogStatsReply, error)

	// --- Tasks ---
	TriggerTask(taskName string) error

	// --- Safe Apply ---
	SafeApplyInterface(args *SafeApplyInterfaceArgs) (*firewall.ApplyResult, error)
	ConfirmApplyInterface(applyID string) error
	CancelApplyInterface(applyID string) error

	// --- IPSet Management ---
	ListIPSets() ([]firewall.IPSetMetadata, error)
	GetIPSet(name string) (*firewall.IPSetMetadata, error)
	RefreshIPSet(name string) error
	GetIPSetElements(name string) ([]string, error)
	GetIPSetCacheInfo() (map[string]interface{}, error)
	ClearIPSetCache() error
	AddToIPSet(name, ip string) error
	RemoveFromIPSet(name, ip string) error
	CheckIPSet(name, ip string) (bool, error)

	// --- System Operations ---
	SystemReboot(force bool) (string, error)
	GetSystemStats() (*SystemStats, error)
	GetRoutes() ([]Route, error)
	GetNotifications(sinceID int64) ([]Notification, int64, error)

	// --- Learning Firewall ---
	GetLearningRules(status string) ([]*learning.PendingRule, error)
	GetLearningRule(id string) (*learning.PendingRule, error)
	ApproveRule(id, user string) (*learning.PendingRule, error)
	DenyRule(id, user string) (*learning.PendingRule, error)
	IgnoreRule(id string) (*learning.PendingRule, error)
	DeleteRule(id string) error
	GetLearningStats() (map[string]interface{}, error)
	GetTopology() (*GetTopologyReply, error)
	GetNetworkDevices() ([]NetworkDevice, error)

	// --- Uplink Management ---
	GetUplinkGroups() ([]UplinkGroupStatus, error)
	SwitchUplink(groupName, uplinkName string) error
	ToggleUplink(groupName, uplinkName string, enabled bool) error

	// --- Flow Management ---
	GetFlows(state string, limit, offset int) ([]flowdb.FlowWithHints, map[string]int64, error)
	ApproveFlow(id int64) error
	DenyFlow(id int64) error
	DeleteFlow(id int64) error

	// --- Network Scanner ---
	StartScanNetwork(cidr string, timeoutSeconds int) error
	GetScanStatus() (bool, *scanner.ScanResult, error)
	GetScanResult() (*scanner.ScanResult, error)
	GetCommonPorts() ([]scanner.Port, error)
	ScanHost(ip string) (*scanner.HostResult, error)

	// --- Wake-on-LAN ---
	WakeOnLAN(mac, iface string) error

	// --- Device Identity Management ---
	UpdateDeviceIdentity(args *UpdateDeviceIdentityArgs) (*device.DeviceIdentity, error)
	LinkMAC(mac, identityID string) error
	UnlinkMAC(mac string) error

	// --- Ping (Connectivity Verification) ---
	Ping(target string, timeoutSeconds int) (*PingReply, error)

	// --- Safe Mode ---
	IsInSafeMode() (bool, error)
	EnterSafeMode() error
	ExitSafeMode() error
}

// Compile-time check that Client implements ControlPlaneClient
var _ ControlPlaneClient = (*Client)(nil)
