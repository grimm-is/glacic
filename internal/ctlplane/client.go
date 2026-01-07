package ctlplane

import (
	"errors"
	"fmt"
	"net/rpc"
	"strings"

	"sync"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/device"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/learning/flowdb"
	"grimm.is/glacic/internal/services/scanner"
)

// Client is the RPC client for communicating with the control plane
type Client struct {
	client *rpc.Client
	mu     sync.RWMutex
}

// NewClient creates a new control plane client
func NewClient() (*Client, error) {
	client, err := rpc.Dial("unix", SocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to control plane at %s: %w", SocketPath, err)
	}
	return &Client{client: client}, nil
}

// Close closes the RPC connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// call wraps the RPC call with reconnection logic
func (c *Client) call(serviceMethod string, args any, reply any) error {
	// First attempt
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		// If nil, try to connect initially
		if err := c.reconnect(nil); err != nil {
			return err
		}
		c.mu.RLock()
		client = c.client
		c.mu.RUnlock()
	}

	err := client.Call(serviceMethod, args, reply)
	if err == nil {
		return nil
	}

	// Internal Go RPC error for shutdown/closed connection
	if err == rpc.ErrShutdown || isNetworkError(err) {
		// Log attempt?
		// Try to reconnect, passing the failed client to avoid racing
		if recErr := c.reconnect(client); recErr != nil {
			// Return RECONNECTION error so we know why we failed (e.g. server down)
			// wrapping original error might be noisy, but knowing "connection refused" is key.
			return fmt.Errorf("RPC call failed (%v) and reconnection failed: %w", err, recErr)
		}

		// Retry with new client
		c.mu.RLock()
		client = c.client
		c.mu.RUnlock()
		return client.Call(serviceMethod, args, reply)
	}

	return err
}

// reconnect attempts to establish a new connection
func (c *Client) reconnect(oldClient *rpc.Client) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if someone else reconnected while we waited
	if c.client != oldClient && c.client != nil {
		// Already reconnected
		return nil
	}

	if c.client != nil {
		c.client.Close()
	}

	client, err := rpc.Dial("unix", SocketPath)
	if err != nil {
		return fmt.Errorf("failed to reconnect to control plane: %w", err)
	}

	c.client = client
	return nil
}

func isNetworkError(err error) bool {
	// Simple check for common network/socket errors
	msg := err.Error()
	return strings.Contains(msg, "connection is shut down") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "bad file descriptor") ||
		strings.Contains(msg, "unexpected EOF") ||
		strings.Contains(msg, "use of closed network connection")
}

// GetStatus returns the current system status
func (c *Client) GetStatus() (*Status, error) {
	var reply GetStatusReply
	if err := c.call("Server.GetStatus", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return &reply.Status, nil
}

// GetConfig returns the current configuration
func (c *Client) GetConfig() (*config.Config, error) {
	var reply GetConfigReply
	if err := c.call("Server.GetConfig", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return &reply.Config, nil
}

// GetInterfaces returns the status of all interfaces
func (c *Client) GetInterfaces() ([]InterfaceStatus, error) {
	var reply GetInterfacesReply
	if err := c.call("Server.GetInterfaces", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Interfaces, nil
}

// GetServices returns the status of all services
func (c *Client) GetServices() ([]ServiceStatus, error) {
	var reply GetServicesReply
	if err := c.call("Server.GetServices", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Services, nil
}

// ApplyConfig applies a new configuration
func (c *Client) ApplyConfig(cfg *config.Config) error {
	return c.call("Server.ApplyConfig", &ApplyConfigArgs{Config: *cfg}, &Empty{})
}

// RestartService restarts a specific service
func (c *Client) RestartService(serviceName string) error {
	return c.call("Server.RestartService", &RestartServiceArgs{ServiceName: serviceName}, &Empty{})
}

// Reboot triggers a system reboot
func (c *Client) Reboot() error {
	return c.call("Server.Reboot", &Empty{}, &Empty{})
}

// GetDHCPLeases returns active DHCP client leases
func (c *Client) GetDHCPLeases() ([]DHCPLease, error) {
	var reply GetDHCPLeasesReply
	if err := c.call("Server.GetDHCPLeases", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Leases, nil
}

// --- Interface Management ---

// GetAvailableInterfaces returns all physical interfaces
func (c *Client) GetAvailableInterfaces() ([]AvailableInterface, error) {
	var reply GetAvailableInterfacesReply
	if err := c.call("Server.GetAvailableInterfaces", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Interfaces, nil
}

// UpdateInterface updates an interface's configuration
func (c *Client) UpdateInterface(args *UpdateInterfaceArgs) (*UpdateInterfaceReply, error) {
	var reply UpdateInterfaceReply
	if err := c.call("Server.UpdateInterface", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// CreateVLAN creates a new VLAN interface
func (c *Client) CreateVLAN(args *CreateVLANArgs) (*CreateVLANReply, error) {
	var reply CreateVLANReply
	if err := c.call("Server.CreateVLAN", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// DeleteVLAN deletes a VLAN interface
func (c *Client) DeleteVLAN(ifaceName string) (*UpdateInterfaceReply, error) {
	var reply UpdateInterfaceReply
	if err := c.call("Server.DeleteVLAN", &DeleteVLANArgs{InterfaceName: ifaceName}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// CreateBond creates a new bond interface
func (c *Client) CreateBond(args *CreateBondArgs) (*CreateBondReply, error) {
	var reply CreateBondReply
	if err := c.call("Server.CreateBond", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// DeleteBond deletes a bond interface
func (c *Client) DeleteBond(name string) (*UpdateInterfaceReply, error) {
	var reply UpdateInterfaceReply
	if err := c.call("Server.DeleteBond", &DeleteBondArgs{Name: name}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// --- HCL Editing (Advanced Mode) ---

// GetConfigDiff returns the difference between running and saved config
func (c *Client) GetConfigDiff() (string, error) {
	var reply GetConfigDiffReply
	if err := c.call("Server.GetConfigDiff", &Empty{}, &reply); err != nil {
		return "", err
	}
	return reply.Diff, nil
}

// GetRawHCL returns the entire config file as raw HCL
func (c *Client) GetRawHCL() (*GetRawHCLReply, error) {
	var reply GetRawHCLReply
	if err := c.call("Server.GetRawHCL", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// GetSectionHCL returns a specific section as raw HCL
func (c *Client) GetSectionHCL(sectionType string, labels ...string) (*GetSectionHCLReply, error) {
	var reply GetSectionHCLReply
	args := &GetSectionHCLArgs{SectionType: sectionType, Labels: labels}
	if err := c.call("Server.GetSectionHCL", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// SetRawHCL replaces the entire config with new HCL
func (c *Client) SetRawHCL(hcl string) (*SetRawHCLReply, error) {
	var reply SetRawHCLReply
	if err := c.call("Server.SetRawHCL", &SetRawHCLArgs{HCL: hcl}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// SetSectionHCL replaces a specific section's HCL content
func (c *Client) SetSectionHCL(sectionType string, hcl string, labels ...string) (*SetSectionHCLReply, error) {
	var reply SetSectionHCLReply
	if err := c.call("Server.SetSectionHCL", &SetSectionHCLArgs{
		SectionType: sectionType,
		Labels:      labels,
		HCL:         hcl,
	}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// DeleteSection removes a specific section from the configuration
func (c *Client) DeleteSection(sectionType string) (*DeleteSectionReply, error) {
	var reply DeleteSectionReply
	if err := c.call("Server.DeleteSection", &DeleteSectionArgs{
		SectionType: sectionType,
	}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// DeleteSectionByLabel removes a specific labeled section from the configuration
func (c *Client) DeleteSectionByLabel(sectionType string, labels ...string) (*DeleteSectionReply, error) {
	var reply DeleteSectionReply
	if err := c.call("Server.DeleteSectionByLabel", &DeleteSectionByLabelArgs{
		SectionType: sectionType,
		Labels:      labels,
	}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// ValidateHCL validates HCL without applying it
func (c *Client) ValidateHCL(hcl string) (*ValidateHCLReply, error) {
	var reply ValidateHCLReply
	if err := c.call("Server.ValidateHCL", &ValidateHCLArgs{HCL: hcl}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// SaveConfig saves the current config to disk
func (c *Client) SaveConfig() (*SaveConfigReply, error) {
	var reply SaveConfigReply
	if err := c.call("Server.SaveConfig", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// --- Backup Management ---

// ListBackups returns all available config backups
func (c *Client) ListBackups() (*ListBackupsReply, error) {
	var reply ListBackupsReply
	if err := c.call("Server.ListBackups", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// Upgrade initiates a hot binary upgrade using the hardcoded upgrade binary
func (c *Client) Upgrade(checksum string) error {
	var reply UpgradeReply
	err := c.call("Server.Upgrade", &UpgradeArgs{Checksum: checksum}, &reply)
	if err != nil {
		return err
	}
	if !reply.Success {
		return fmt.Errorf("upgrade failed: %s", reply.Error)
	}
	return nil
}

// StageBinary sends binary data to the control plane for staging
func (c *Client) StageBinary(data []byte, checksum, arch string) (*StageBinaryReply, error) {
	args := &StageBinaryArgs{
		Data:     data,
		Checksum: checksum,
		Arch:     arch,
	}
	var reply StageBinaryReply
	if err := c.call("Server.StageBinary", args, &reply); err != nil {
		return nil, err
	}
	if !reply.Success {
		return nil, fmt.Errorf("staging failed: %s", reply.Error)
	}
	return &reply, nil
}

// CreateBackup creates a new manual backup
func (c *Client) CreateBackup(description string, pinned bool) (*CreateBackupReply, error) {
	var reply CreateBackupReply
	if err := c.call("Server.CreateBackup", &CreateBackupArgs{Description: description, Pinned: pinned}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// RestoreBackup restores a specific backup version
func (c *Client) RestoreBackup(version int) (*RestoreBackupReply, error) {
	var reply RestoreBackupReply
	if err := c.call("Server.RestoreBackup", &RestoreBackupArgs{Version: version}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// GetBackupContent returns the content of a specific backup
func (c *Client) GetBackupContent(version int) (*GetBackupContentReply, error) {
	var reply GetBackupContentReply
	if err := c.call("Server.GetBackupContent", &GetBackupContentArgs{Version: version}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// PinBackup sets or clears the pinned status of a backup
func (c *Client) PinBackup(version int, pinned bool) (*PinBackupReply, error) {
	var reply PinBackupReply
	if err := c.call("Server.PinBackup", &PinBackupArgs{Version: version, Pinned: pinned}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// SetMaxBackups updates the maximum number of auto-backups to retain
func (c *Client) SetMaxBackups(maxBackups int) (*SetMaxBackupsReply, error) {
	var reply SetMaxBackupsReply
	if err := c.call("Server.SetMaxBackups", &SetMaxBackupsArgs{MaxBackups: maxBackups}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// GetLogs retrieves system logs via the control plane (which runs as root)
func (c *Client) GetLogs(args *GetLogsArgs) (*GetLogsReply, error) {
	var reply GetLogsReply
	if err := c.call("Server.GetLogs", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// GetLogSources returns available log sources
func (c *Client) GetLogSources() (*GetLogSourcesReply, error) {
	var reply GetLogSourcesReply
	if err := c.call("Server.GetLogSources", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// GetLogStats returns firewall log statistics
func (c *Client) GetLogStats() (*GetLogStatsReply, error) {
	var reply GetLogStatsReply
	if err := c.call("Server.GetLogStats", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// TriggerTask triggers a system task via the scheduler
func (c *Client) TriggerTask(taskName string) error {
	var reply TriggerTaskReply
	if err := c.call("Server.TriggerTask", &TriggerTaskArgs{TaskName: taskName}, &reply); err != nil {
		return err
	}
	if !reply.Success {
		return fmt.Errorf("task trigger failed: %s", reply.Error)
	}
	return nil
}

// SafeApplyInterface applies interface config with rollback protection
func (c *Client) SafeApplyInterface(args *SafeApplyInterfaceArgs) (*firewall.ApplyResult, error) {
	var reply firewall.ApplyResult
	if err := c.call("Server.SafeApplyInterface", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// ConfirmApplyInterface confirms a pending interface apply
func (c *Client) ConfirmApplyInterface(applyID string) error {
	return c.call("Server.ConfirmApplyInterface", &ConfirmApplyArgs{PendingID: applyID}, &Empty{})
}

// CancelApplyInterface cancels a pending interface apply
func (c *Client) CancelApplyInterface(applyID string) error {
	return c.call("Server.CancelApplyInterface", &CancelApplyArgs{ApplyID: applyID}, &Empty{})
}

// --- IPSet Management ---

// ListIPSets returns all IPSet metadata
func (c *Client) ListIPSets() ([]firewall.IPSetMetadata, error) {
	var reply ListIPSetsReply
	if err := c.call("Server.ListIPSets", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.IPSets, nil
}

// GetIPSet returns metadata for a specific IPSet
func (c *Client) GetIPSet(name string) (*firewall.IPSetMetadata, error) {
	var reply GetIPSetReply
	if err := c.call("Server.GetIPSet", &GetIPSetArgs{Name: name}, &reply); err != nil {
		return nil, err
	}
	return &reply.Metadata, nil
}

// RefreshIPSet forces an update of an IPSet
func (c *Client) RefreshIPSet(name string) error {
	return c.call("Server.RefreshIPSet", &RefreshIPSetArgs{Name: name}, &Empty{})
}

// GetIPSetElements returns the elements in an IPSet
func (c *Client) GetIPSetElements(name string) ([]string, error) {
	var reply GetIPSetElementsReply
	if err := c.call("Server.GetIPSetElements", &GetIPSetElementsArgs{Name: name}, &reply); err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New(reply.Error)
	}
	return reply.Elements, nil
}

// GetIPSetCacheInfo returns information about the IPSet cache
func (c *Client) GetIPSetCacheInfo() (map[string]interface{}, error) {
	var reply GetIPSetCacheInfoReply
	if err := c.call("Server.GetIPSetCacheInfo", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Info, nil
}

// ClearIPSetCache clears the IPSet cache
func (c *Client) ClearIPSetCache() error {
	return c.call("Server.ClearIPSetCache", &Empty{}, &Empty{})
}

// AddToIPSet adds an IP to a named IPSet
func (c *Client) AddToIPSet(name, ip string) error {
	var reply AddIPSetEntryReply
	if err := c.call("Server.AddIPSetEntry", &AddIPSetEntryArgs{Name: name, IP: ip}, &reply); err != nil {
		return err
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	return nil
}

// RemoveFromIPSet removes an IP from a named IPSet
func (c *Client) RemoveFromIPSet(name, ip string) error {
	var reply RemoveIPSetEntryReply
	if err := c.call("Server.RemoveIPSetEntry", &RemoveIPSetEntryArgs{Name: name, IP: ip}, &reply); err != nil {
		return err
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	return nil
}

// CheckIPSet checks if an IP is in a named IPSet
func (c *Client) CheckIPSet(name, ip string) (bool, error) {
	var reply CheckIPSetEntryReply
	if err := c.call("Server.CheckIPSetEntry", &CheckIPSetEntryArgs{Name: name, IP: ip}, &reply); err != nil {
		return false, err
	}
	if reply.Error != "" {
		return false, errors.New(reply.Error)
	}
	return reply.Exists, nil
}

// --- System Operations ---

// SystemReboot reboots the system
func (c *Client) SystemReboot(force bool) (string, error) {
	var reply SystemRebootReply
	if err := c.call("Server.SystemReboot", &SystemRebootArgs{Force: force}, &reply); err != nil {
		return "", err
	}
	return reply.Message, nil
}

// GetSystemStats returns system resource usage statistics
func (c *Client) GetSystemStats() (*SystemStats, error) {
	var reply GetSystemStatsReply
	if err := c.call("Server.GetSystemStats", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return &reply.Stats, nil
}

// GetRoutes returns the current kernel routing table
func (c *Client) GetRoutes() ([]Route, error) {
	var reply GetRoutesReply
	if err := c.call("Server.GetRoutes", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Routes, nil
}

// GetNotifications returns notifications since the given ID
func (c *Client) GetNotifications(sinceID int64) ([]Notification, int64, error) {
	var reply GetNotificationsReply
	err := c.call("Server.GetNotifications", &GetNotificationsArgs{SinceID: sinceID}, &reply)
	if err != nil {
		return nil, 0, err
	}
	return reply.Notifications, reply.LastID, nil
}

// --- Learning Firewall ---

// GetLearningRules returns pending rules
func (c *Client) GetLearningRules(status string) ([]*learning.PendingRule, error) {
	var reply GetLearningRulesReply
	if err := c.call("Server.GetLearningRules", &GetLearningRulesArgs{Status: status}, &reply); err != nil {
		return nil, err
	}
	return reply.Rules, nil
}

// GetLearningRule returns a specific rule
func (c *Client) GetLearningRule(id string) (*learning.PendingRule, error) {
	var reply GetLearningRuleReply
	if err := c.call("Server.GetLearningRule", &GetLearningRuleArgs{ID: id}, &reply); err != nil {
		return nil, err
	}
	return reply.Rule, nil
}

// ApproveRule approves a pending rule
func (c *Client) ApproveRule(id, user string) (*learning.PendingRule, error) {
	var reply LearningRuleActionReply
	if err := c.call("Server.ApproveRule", &LearningRuleActionArgs{ID: id, User: user}, &reply); err != nil {
		return nil, err
	}
	return reply.Rule, nil
}

// DenyRule denies a pending rule
func (c *Client) DenyRule(id, user string) (*learning.PendingRule, error) {
	var reply LearningRuleActionReply
	if err := c.call("Server.DenyRule", &LearningRuleActionArgs{ID: id, User: user}, &reply); err != nil {
		return nil, err
	}
	return reply.Rule, nil
}

// IgnoreRule ignores a pending rule
func (c *Client) IgnoreRule(id string) (*learning.PendingRule, error) {
	var reply LearningRuleActionReply
	if err := c.call("Server.IgnoreRule", &LearningRuleActionArgs{ID: id}, &reply); err != nil {
		return nil, err
	}
	return reply.Rule, nil
}

// DeleteRule deletes a pending rule
func (c *Client) DeleteRule(id string) error {
	var reply LearningRuleActionReply
	if err := c.call("Server.DeleteRule", &LearningRuleActionArgs{ID: id}, &reply); err != nil {
		return err
	}
	return nil
}

// GetLearningStats returns learning statistics
func (c *Client) GetLearningStats() (map[string]interface{}, error) {
	var reply GetLearningStatsReply
	if err := c.call("Server.GetLearningStats", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Stats, nil
}

// GetTopology returns discovered LLDP neighbors
func (c *Client) GetTopology() (*GetTopologyReply, error) {
	var reply GetTopologyReply
	if err := c.call("Server.GetTopology", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// GetNetworkDevices returns all discovered devices on the network
func (c *Client) GetNetworkDevices() ([]NetworkDevice, error) {
	var reply GetNetworkDevicesReply
	if err := c.call("Server.GetNetworkDevices", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Devices, nil
}

// --- Uplink Management ---

// GetUplinkGroups returns all uplink groups
func (c *Client) GetUplinkGroups() ([]UplinkGroupStatus, error) {
	var reply GetUplinkGroupsReply
	err := c.call("Server.GetUplinkGroups", &Empty{}, &reply)
	if err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, fmt.Errorf("%s", reply.Error)
	}
	return reply.Groups, nil
}

// SwitchUplink switches an uplink group to a specific uplink or best available
func (c *Client) SwitchUplink(groupName, uplinkName string) error {
	args := &SwitchUplinkArgs{
		GroupName:  groupName,
		UplinkName: uplinkName,
	}
	var reply SwitchUplinkReply
	err := c.call("Server.SwitchUplink", args, &reply)
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("%s", reply.Error)
	}
	return nil
}

// ToggleUplink enables or disables an uplink
func (c *Client) ToggleUplink(groupName, uplinkName string, enabled bool) error {
	args := &ToggleUplinkArgs{
		GroupName:  groupName,
		UplinkName: uplinkName,
		Enabled:    enabled,
	}
	var reply ToggleUplinkReply
	err := c.call("Server.ToggleUplink", args, &reply)
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("%s", reply.Error)
	}
	return nil
}

// --- Flow Management ---

// GetFlows returns flows matching criteria
func (c *Client) GetFlows(state string, limit, offset int) ([]flowdb.FlowWithHints, map[string]int64, error) {
	args := &GetFlowsArgs{
		State:  state,
		Limit:  limit,
		Offset: offset,
	}
	var reply GetFlowsReply
	err := c.call("Server.GetFlows", args, &reply)
	if err != nil {
		return nil, nil, err
	}
	if reply.Error != "" {
		return nil, nil, fmt.Errorf("%s", reply.Error)
	}
	return reply.Flows, reply.TotalCounts, nil
}

// ApproveFlow approves a flow
func (c *Client) ApproveFlow(id int64) error {
	args := &FlowActionArgs{ID: id}
	var reply FlowActionReply
	err := c.call("Server.ApproveFlow", args, &reply)
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("%s", reply.Error)
	}
	return nil
}

// DenyFlow denies a flow
func (c *Client) DenyFlow(id int64) error {
	args := &FlowActionArgs{ID: id}
	var reply FlowActionReply
	err := c.call("Server.DenyFlow", args, &reply)
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("%s", reply.Error)
	}
	return nil
}

// DeleteFlow deletes a flow
func (c *Client) DeleteFlow(id int64) error {
	args := &FlowActionArgs{ID: id}
	var reply FlowActionReply
	err := c.call("Server.DeleteFlow", args, &reply)
	if err != nil {
		return err
	}
	if reply.Error != "" {
		return fmt.Errorf("%s", reply.Error)
	}
	return nil
}

// --- Network Scanner ---

// StartScanNetwork starts a network scan asynchronously
func (c *Client) StartScanNetwork(cidr string, timeoutSeconds int) error {
	args := &StartScanNetworkArgs{CIDR: cidr, TimeoutSeconds: timeoutSeconds}
	var reply StartScanNetworkReply
	if err := c.call("Server.StartScanNetwork", args, &reply); err != nil {
		return err
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	return nil
}

// GetScanStatus returns the current scan status and last result metadata
func (c *Client) GetScanStatus() (bool, *scanner.ScanResult, error) {
	var reply GetScanStatusReply
	if err := c.call("Server.GetScanStatus", &Empty{}, &reply); err != nil {
		return false, nil, err
	}
	return reply.Scanning, reply.LastResult, nil
}

// GetScanResult returns the full last scan result
func (c *Client) GetScanResult() (*scanner.ScanResult, error) {
	var reply GetScanResultReply
	if err := c.call("Server.GetScanResult", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Result, nil
}

// GetCommonPorts returns list of common ports
func (c *Client) GetCommonPorts() ([]scanner.Port, error) {
	var reply GetCommonPortsReply
	if err := c.call("Server.GetCommonPorts", &Empty{}, &reply); err != nil {
		return nil, err
	}
	return reply.Ports, nil
}

// ScanHost scans a specific host
func (c *Client) ScanHost(ip string) (*scanner.HostResult, error) {
	args := &ScanHostArgs{IP: ip}
	var reply ScanHostReply
	if err := c.call("Server.ScanHost", args, &reply); err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New(reply.Error)
	}
	return reply.Result, nil
}

// --- Wake-on-LAN ---

// --- Wake-on-LAN ---

// WakeOnLAN sends a magic packet
func (c *Client) WakeOnLAN(mac, iface string) error {
	args := &WakeOnLANArgs{MAC: mac, Interface: iface}
	var reply WakeOnLANReply
	if err := c.call("Server.WakeOnLAN", args, &reply); err != nil {
		return err
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	return nil
}

// --- Device Identity Management ---

// UpdateDeviceIdentity updates a device identity
func (c *Client) UpdateDeviceIdentity(args *UpdateDeviceIdentityArgs) (*device.DeviceIdentity, error) {
	var reply UpdateDeviceIdentityReply
	if err := c.call("Server.UpdateDeviceIdentity", args, &reply); err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New(reply.Error)
	}
	return reply.Identity, nil
}

// LinkMAC links a MAC address to a device identity
func (c *Client) LinkMAC(mac, identityID string) error {
	args := &LinkMACArgs{MAC: mac, IdentityID: identityID}
	var reply Empty
	if err := c.call("Server.LinkMAC", args, &reply); err != nil {
		return err
	}
	return nil
}

// UnlinkMAC removes a MAC address link
func (c *Client) UnlinkMAC(mac string) error {
	args := &UnlinkMACArgs{MAC: mac}
	var reply Empty
	if err := c.call("Server.UnlinkMAC", args, &reply); err != nil {
		return err
	}
	return nil
}

// --- Ping (Connectivity Verification) ---

// Ping pings a target IP address to verify connectivity
func (c *Client) Ping(target string, timeoutSeconds int) (*PingReply, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 5 // Default 5 second timeout
	}
	args := &PingArgs{Target: target, TimeoutSeconds: timeoutSeconds}
	var reply PingReply
	if err := c.call("Server.Ping", args, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// --- Safe Mode Operations ---

// IsInSafeMode checks if safe mode is currently active.
func (c *Client) IsInSafeMode() (bool, error) {
	var reply SafeModeStatusReply
	if err := c.call("Server.IsInSafeMode", &Empty{}, &reply); err != nil {
		return false, err
	}
	return reply.InSafeMode, nil
}

// EnterSafeMode activates safe mode (emergency lockdown).
func (c *Client) EnterSafeMode() error {
	return c.call("Server.EnterSafeMode", &Empty{}, &Empty{})
}

// ExitSafeMode deactivates safe mode and restores normal operation.
func (c *Client) ExitSafeMode() error {
	return c.call("Server.ExitSafeMode", &Empty{}, &Empty{})
}
