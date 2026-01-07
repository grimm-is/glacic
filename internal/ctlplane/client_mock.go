package ctlplane

import (
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/device"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/learning/flowdb"
	"grimm.is/glacic/internal/services/scanner"

	"github.com/stretchr/testify/mock"
)

// MockControlPlaneClient is a mock implementation of ControlPlaneClient for testing.
type MockControlPlaneClient struct {
	mock.Mock
}

func (m *MockControlPlaneClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockControlPlaneClient) GetStatus() (*Status, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Status), args.Error(1)
}

func (m *MockControlPlaneClient) GetConfig() (*config.Config, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.Config), args.Error(1)
}

func (m *MockControlPlaneClient) GetInterfaces() ([]InterfaceStatus, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]InterfaceStatus), args.Error(1)
}

func (m *MockControlPlaneClient) GetServices() ([]ServiceStatus, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]ServiceStatus), args.Error(1)
}

func (m *MockControlPlaneClient) ApplyConfig(cfg *config.Config) error {
	return m.Called(cfg).Error(0)
}

func (m *MockControlPlaneClient) RestartService(serviceName string) error {
	return m.Called(serviceName).Error(0)
}

func (m *MockControlPlaneClient) Reboot() error {
	return m.Called().Error(0)
}

func (m *MockControlPlaneClient) GetDHCPLeases() ([]DHCPLease, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]DHCPLease), args.Error(1)
}

func (m *MockControlPlaneClient) GetAvailableInterfaces() ([]AvailableInterface, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]AvailableInterface), args.Error(1)
}

func (m *MockControlPlaneClient) UpdateInterface(args *UpdateInterfaceArgs) (*UpdateInterfaceReply, error) {
	callArgs := m.Called(args)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*UpdateInterfaceReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) CreateVLAN(args *CreateVLANArgs) (*CreateVLANReply, error) {
	callArgs := m.Called(args)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*CreateVLANReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) DeleteVLAN(ifaceName string) (*UpdateInterfaceReply, error) {
	callArgs := m.Called(ifaceName)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*UpdateInterfaceReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) CreateBond(args *CreateBondArgs) (*CreateBondReply, error) {
	callArgs := m.Called(args)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*CreateBondReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) DeleteBond(name string) (*UpdateInterfaceReply, error) {
	callArgs := m.Called(name)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*UpdateInterfaceReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetRawHCL() (*GetRawHCLReply, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*GetRawHCLReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetSectionHCL(sectionType string, labels ...string) (*GetSectionHCLReply, error) {
	callArgs := m.Called(sectionType, labels)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*GetSectionHCLReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) SetRawHCL(hcl string) (*SetRawHCLReply, error) {
	callArgs := m.Called(hcl)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*SetRawHCLReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) SetSectionHCL(sectionType string, hcl string, labels ...string) (*SetSectionHCLReply, error) {
	callArgs := m.Called(sectionType, hcl, labels)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*SetSectionHCLReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) DeleteSection(sectionType string) (*DeleteSectionReply, error) {
	callArgs := m.Called(sectionType)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*DeleteSectionReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) DeleteSectionByLabel(sectionType string, labels ...string) (*DeleteSectionReply, error) {
	callArgs := m.Called(sectionType, labels)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*DeleteSectionReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) ValidateHCL(hcl string) (*ValidateHCLReply, error) {
	callArgs := m.Called(hcl)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*ValidateHCLReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) SaveConfig() (*SaveConfigReply, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*SaveConfigReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetConfigDiff() (string, error) {
	args := m.Called()
	return args.String(0), args.Error(1)
}

func (m *MockControlPlaneClient) ListBackups() (*ListBackupsReply, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*ListBackupsReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) Upgrade(checksum string) error {
	return m.Called(checksum).Error(0)
}

func (m *MockControlPlaneClient) StageBinary(data []byte, checksum, arch string) (*StageBinaryReply, error) {
	callArgs := m.Called(data, checksum, arch)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*StageBinaryReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) CreateBackup(description string, pinned bool) (*CreateBackupReply, error) {
	callArgs := m.Called(description, pinned)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*CreateBackupReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) RestoreBackup(version int) (*RestoreBackupReply, error) {
	callArgs := m.Called(version)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*RestoreBackupReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetBackupContent(version int) (*GetBackupContentReply, error) {
	callArgs := m.Called(version)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*GetBackupContentReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) PinBackup(version int, pinned bool) (*PinBackupReply, error) {
	callArgs := m.Called(version, pinned)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*PinBackupReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) SetMaxBackups(maxBackups int) (*SetMaxBackupsReply, error) {
	callArgs := m.Called(maxBackups)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*SetMaxBackupsReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetLogs(args *GetLogsArgs) (*GetLogsReply, error) {
	callArgs := m.Called(args)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*GetLogsReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetLogSources() (*GetLogSourcesReply, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*GetLogSourcesReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetLogStats() (*GetLogStatsReply, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*GetLogStatsReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) TriggerTask(taskName string) error {
	return m.Called(taskName).Error(0)
}

func (m *MockControlPlaneClient) SafeApplyInterface(args *SafeApplyInterfaceArgs) (*firewall.ApplyResult, error) {
	callArgs := m.Called(args)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*firewall.ApplyResult), callArgs.Error(1)
}

func (m *MockControlPlaneClient) ConfirmApplyInterface(applyID string) error {
	return m.Called(applyID).Error(0)
}

func (m *MockControlPlaneClient) CancelApplyInterface(applyID string) error {
	return m.Called(applyID).Error(0)
}

func (m *MockControlPlaneClient) ListIPSets() ([]firewall.IPSetMetadata, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).([]firewall.IPSetMetadata), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetIPSet(name string) (*firewall.IPSetMetadata, error) {
	callArgs := m.Called(name)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*firewall.IPSetMetadata), callArgs.Error(1)
}

func (m *MockControlPlaneClient) RefreshIPSet(name string) error {
	return m.Called(name).Error(0)
}

func (m *MockControlPlaneClient) GetIPSetElements(name string) ([]string, error) {
	callArgs := m.Called(name)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).([]string), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetIPSetCacheInfo() (map[string]interface{}, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(map[string]interface{}), callArgs.Error(1)
}

func (m *MockControlPlaneClient) ClearIPSetCache() error {
	return m.Called().Error(0)
}

func (m *MockControlPlaneClient) AddToIPSet(name, ip string) error {
	return m.Called(name, ip).Error(0)
}

func (m *MockControlPlaneClient) RemoveFromIPSet(name, ip string) error {
	return m.Called(name, ip).Error(0)
}

func (m *MockControlPlaneClient) CheckIPSet(name, ip string) (bool, error) {
	args := m.Called(name, ip)
	return args.Bool(0), args.Error(1)
}

func (m *MockControlPlaneClient) SystemReboot(force bool) (string, error) {
	callArgs := m.Called(force)
	return callArgs.String(0), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetSystemStats() (*SystemStats, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*SystemStats), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetRoutes() ([]Route, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).([]Route), callArgs.Error(1)
}

// --- Device Identity Management ---

func (m *MockControlPlaneClient) UpdateDeviceIdentity(args *UpdateDeviceIdentityArgs) (*device.DeviceIdentity, error) {
	return nil, nil
}

func (m *MockControlPlaneClient) LinkMAC(mac, identityID string) error {
	return nil
}

func (m *MockControlPlaneClient) UnlinkMAC(mac string) error {
	return nil
}

// --- Ping (Connectivity Verification) ---

func (m *MockControlPlaneClient) GetNotifications(sinceID int64) ([]Notification, int64, error) {
	callArgs := m.Called(sinceID)
	if callArgs.Get(0) == nil {
		return nil, 0, callArgs.Error(2)
	}
	return callArgs.Get(0).([]Notification), callArgs.Get(1).(int64), callArgs.Error(2)
}

func (m *MockControlPlaneClient) GetLearningRules(status string) ([]*learning.PendingRule, error) {
	callArgs := m.Called(status)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).([]*learning.PendingRule), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetLearningRule(id string) (*learning.PendingRule, error) {
	callArgs := m.Called(id)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*learning.PendingRule), callArgs.Error(1)
}

func (m *MockControlPlaneClient) ApproveRule(id, user string) (*learning.PendingRule, error) {
	callArgs := m.Called(id, user)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*learning.PendingRule), callArgs.Error(1)
}

func (m *MockControlPlaneClient) DenyRule(id, user string) (*learning.PendingRule, error) {
	callArgs := m.Called(id, user)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*learning.PendingRule), callArgs.Error(1)
}

func (m *MockControlPlaneClient) IgnoreRule(id string) (*learning.PendingRule, error) {
	callArgs := m.Called(id)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*learning.PendingRule), callArgs.Error(1)
}

func (m *MockControlPlaneClient) DeleteRule(id string) error {
	return m.Called(id).Error(0)
}

func (m *MockControlPlaneClient) GetLearningStats() (map[string]interface{}, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(map[string]interface{}), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetTopology() (*GetTopologyReply, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*GetTopologyReply), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetNetworkDevices() ([]NetworkDevice, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).([]NetworkDevice), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetUplinkGroups() ([]UplinkGroupStatus, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).([]UplinkGroupStatus), callArgs.Error(1)
}

func (m *MockControlPlaneClient) SwitchUplink(groupName, uplinkName string) error {
	return m.Called(groupName, uplinkName).Error(0)
}

func (m *MockControlPlaneClient) ToggleUplink(groupName, uplinkName string, enabled bool) error {
	return m.Called(groupName, uplinkName, enabled).Error(0)
}

func (m *MockControlPlaneClient) GetFlows(state string, limit, offset int) ([]flowdb.FlowWithHints, map[string]int64, error) {
	callArgs := m.Called(state, limit, offset)
	var flows []flowdb.FlowWithHints
	var counts map[string]int64
	if callArgs.Get(0) != nil {
		flows = callArgs.Get(0).([]flowdb.FlowWithHints)
	}
	if callArgs.Get(1) != nil {
		counts = callArgs.Get(1).(map[string]int64)
	}
	return flows, counts, callArgs.Error(2)
}

func (m *MockControlPlaneClient) ApproveFlow(id int64) error {
	return m.Called(id).Error(0)
}

func (m *MockControlPlaneClient) DenyFlow(id int64) error {
	return m.Called(id).Error(0)
}

func (m *MockControlPlaneClient) DeleteFlow(id int64) error {
	return m.Called(id).Error(0)
}

func (m *MockControlPlaneClient) StartScanNetwork(cidr string, timeoutSeconds int) error {
	return m.Called(cidr, timeoutSeconds).Error(0)
}

func (m *MockControlPlaneClient) GetScanStatus() (bool, *scanner.ScanResult, error) {
	callArgs := m.Called()
	var result *scanner.ScanResult
	if callArgs.Get(1) != nil {
		result = callArgs.Get(1).(*scanner.ScanResult)
	}
	return callArgs.Bool(0), result, callArgs.Error(2)
}

func (m *MockControlPlaneClient) GetScanResult() (*scanner.ScanResult, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*scanner.ScanResult), callArgs.Error(1)
}

func (m *MockControlPlaneClient) GetCommonPorts() ([]scanner.Port, error) {
	callArgs := m.Called()
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).([]scanner.Port), callArgs.Error(1)
}

func (m *MockControlPlaneClient) ScanHost(ip string) (*scanner.HostResult, error) {
	callArgs := m.Called(ip)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*scanner.HostResult), callArgs.Error(1)
}

func (m *MockControlPlaneClient) WakeOnLAN(mac, iface string) error {
	return m.Called(mac, iface).Error(0)
}

func (m *MockControlPlaneClient) Ping(target string, timeoutSeconds int) (*PingReply, error) {
	callArgs := m.Called(target, timeoutSeconds)
	if callArgs.Get(0) == nil {
		return nil, callArgs.Error(1)
	}
	return callArgs.Get(0).(*PingReply), callArgs.Error(1)
}

// --- Safe Mode ---

func (m *MockControlPlaneClient) IsInSafeMode() (bool, error) {
	callArgs := m.Called()
	return callArgs.Bool(0), callArgs.Error(1)
}

func (m *MockControlPlaneClient) EnterSafeMode() error {
	return m.Called().Error(0)
}

func (m *MockControlPlaneClient) ExitSafeMode() error {
	return m.Called().Error(0)
}

// Compile-time check
var _ ControlPlaneClient = (*MockControlPlaneClient)(nil)
