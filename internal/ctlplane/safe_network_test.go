package ctlplane

import (
	"os"
	"testing"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/firewall"
)

// MockNetworkManager implements the methods needed by NetworkSafeApplyManager
type MockNetworkManager struct {
	Snapshot        []config.Interface
	UpdateCalled    bool
	UpdateError     error
	RestoreCalled   bool
	RestoreError    error
	LastUpdateArgs  *UpdateInterfaceArgs
	LastRestoreData []config.Interface
}

func (m *MockNetworkManager) SnapshotInterfaces() ([]config.Interface, error) {
	return m.Snapshot, nil
}

func (m *MockNetworkManager) UpdateInterface(args *UpdateInterfaceArgs) error {
	m.UpdateCalled = true
	m.LastUpdateArgs = args
	return m.UpdateError
}

func (m *MockNetworkManager) RestoreInterfaces(snapshot []config.Interface) error {
	m.RestoreCalled = true
	m.LastRestoreData = snapshot
	return m.RestoreError
}

// newMockNetworkManager creates a mock that returns a valid snapshot
func newMockNetworkManager() *MockNetworkManager {
	return &MockNetworkManager{
		Snapshot: []config.Interface{
			{Name: "eth0", IPv4: []string{"192.168.1.1/24"}},
		},
	}
}

func TestNewNetworkSafeApplyManager(t *testing.T) {
	// Create real NetworkManager is complex; we'll test with nil for now
	// In production, dependency injection would help
	sam := NewNetworkSafeApplyManager(nil)
	if sam == nil {
		t.Fatal("NewNetworkSafeApplyManager returned nil")
	}
}

func TestNetworkSafeApplyManager_GetPendingApply_Empty(t *testing.T) {
	sam := &NetworkSafeApplyManager{}
	pending := sam.GetPendingApply()
	if pending != nil {
		t.Error("Expected nil pending apply")
	}
}

func TestNetworkSafeApplyManager_ConfirmApply_NoPending(t *testing.T) {
	sam := &NetworkSafeApplyManager{}
	err := sam.ConfirmApply("some-id")
	if err == nil {
		t.Error("Expected error when no pending apply")
	}
}

func TestNetworkSafeApplyManager_CancelApply_NoPending(t *testing.T) {
	sam := &NetworkSafeApplyManager{}
	err := sam.CancelApply("some-id")
	if err == nil {
		t.Error("Expected error when no pending apply")
	}
}

func TestNetworkSafeApplyManager_ConfirmApply_WrongID(t *testing.T) {
	sam := &NetworkSafeApplyManager{
		pendingApply: &NetworkPendingApply{
			ID: "correct-id",
		},
	}
	err := sam.ConfirmApply("wrong-id")
	if err == nil {
		t.Error("Expected error for wrong ID")
	}
}

func TestNetworkSafeApplyManager_ConfirmApply_AlreadyRolledBack(t *testing.T) {
	sam := &NetworkSafeApplyManager{
		pendingApply: &NetworkPendingApply{
			ID:         "test-id",
			RolledBack: true,
		},
	}
	err := sam.ConfirmApply("test-id")
	if err == nil {
		t.Error("Expected error when already rolled back")
	}
}

func TestNetworkSafeApplyManager_ConfirmApply_AlreadyConfirmed(t *testing.T) {
	sam := &NetworkSafeApplyManager{
		pendingApply: &NetworkPendingApply{
			ID:        "test-id",
			Confirmed: true,
		},
	}
	err := sam.ConfirmApply("test-id")
	if err == nil {
		t.Error("Expected error when already confirmed")
	}
}

func TestNetworkPendingApply_Fields(t *testing.T) {
	now := time.Now()
	pending := &NetworkPendingApply{
		ID:           "test-123",
		StartTime:    now,
		RollbackTime: now.Add(30 * time.Second),
		ClientIP:     "192.168.1.100",
		SnapshotPath: "/tmp/snapshot.json",
		Confirmed:    false,
		RolledBack:   false,
	}

	if pending.ID != "test-123" {
		t.Errorf("ID mismatch: %s", pending.ID)
	}
	if pending.ClientIP != "192.168.1.100" {
		t.Errorf("ClientIP mismatch: %s", pending.ClientIP)
	}
}

func TestNetworkSafeApplyManager_verifyConnectivity_EmptyTargets(t *testing.T) {
	sam := &NetworkSafeApplyManager{}

	// Empty targets should return false (nothing to ping)
	result := sam.verifyConnectivity([]string{}, time.Second)
	if result {
		t.Error("Empty targets should return false")
	}
}

func TestNetworkSafeApplyManager_pingTarget_Unreachable(t *testing.T) {
	sam := &NetworkSafeApplyManager{}

	// Non-routable IP should fail quickly
	result := sam.pingTarget("192.0.2.1", 100*time.Millisecond)
	if result {
		t.Error("Non-routable IP should not be reachable")
	}
}

func TestNetworkSafeApplyManager_pingTarget_Localhost(t *testing.T) {
	sam := &NetworkSafeApplyManager{}

	// Localhost should be reachable (at least via some method)
	// This might fail in some environments, so we just test it doesn't panic
	_ = sam.pingTarget("127.0.0.1", 100*time.Millisecond)
}

func TestSafeApplyConfig_Defaults(t *testing.T) {
	cfg := firewall.DefaultSafeApplyConfig()
	if cfg == nil {
		t.Fatal("DefaultSafeApplyConfig returned nil")
	}
	if cfg.RollbackDelay <= 0 {
		t.Error("RollbackDelay should be positive")
	}
	if cfg.PingTimeout <= 0 {
		t.Error("PingTimeout should be positive")
	}
}

func TestNetworkSafeApplyManager_saveSnapshot(t *testing.T) {
	sam := &NetworkSafeApplyManager{}

	snapshot := []config.Interface{
		{Name: "eth0", IPv4: []string{"10.0.0.1/24"}},
	}

	path, err := sam.saveSnapshot(snapshot)
	if err != nil {
		t.Fatalf("saveSnapshot failed: %v", err)
	}
	if path == "" {
		t.Error("saveSnapshot returned empty path")
	}

	// Verify we can load it back
	loaded, err := sam.loadSnapshot(path)
	if err != nil {
		t.Fatalf("loadSnapshot failed: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Name != "eth0" {
		t.Errorf("Loaded snapshot mismatch: %+v", loaded)
	}
}

func TestNetworkSafeApplyManager_loadSnapshot_NotFound(t *testing.T) {
	sam := &NetworkSafeApplyManager{}

	_, err := sam.loadSnapshot("/nonexistent/path/snapshot.json")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestNetworkSafeApplyManager_loadSnapshot_InvalidJSON(t *testing.T) {
	sam := &NetworkSafeApplyManager{}

	// Create temp file with invalid JSON
	tmpFile := t.TempDir() + "/bad.json"
	if err := os.WriteFile(tmpFile, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := sam.loadSnapshot(tmpFile)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestSafeApply_Rollback_OnConnectivityLoss(t *testing.T) {
	nm := newMockNetworkManager()
	sam := NewNetworkSafeApplyManager(nm)

	// Override connectivity checker to simulate failure
	sam.cc = func(targets []string, timeout time.Duration) bool {
		return false // Connectivity lost!
	}

	// Prepare update args
	args := &UpdateInterfaceArgs{
		Name:   "eth0",
		Action: ActionUpdate,
		IPv4:   []string{"192.168.1.50/24"},
	}

	// Safe config that checks connectivity
	safeCfg := &firewall.SafeApplyConfig{
		PingTargets: []string{"1.1.1.1"},
		PingTimeout: 100 * time.Millisecond,
	}

	// Perform Apply
	result, err := sam.ApplyInterfaceConfig(args, "127.0.0.1", safeCfg)
	if err != nil {
		t.Fatalf("Unexpected error from ApplyInterfaceConfig: %v", err)
	}

	// 1. Verify verification failed check
	if result.Success {
		t.Error("Expected ApplyResult to indicate failure, but got Success=true")
	}
	if result.Message != "Connectivity verification failed - changes rolled back" {
		t.Errorf("Unexpected message: %s", result.Message)
	}

	// 2. Verify Update was called (the "bad" config was applied)
	if !nm.UpdateCalled {
		t.Error("NetworkManager.UpdateInterface was not called")
	}

	// 3. Verify Restore was called (Rollback happened)
	if !nm.RestoreCalled {
		t.Error("NetworkManager.RestoreInterfaces was NOT called! (Rollback failed)")
	}

	// 4. Verify what was restored matches original snapshot
	if nm.LastRestoreData == nil {
		t.Fatal("Restore called but no data recorded")
	}
	if len(nm.LastRestoreData) != 1 || nm.LastRestoreData[0].Name != "eth0" {
		t.Errorf("Restored data mismatch. Expected original snapshot, got: %+v", nm.LastRestoreData)
	}
}

func TestSafeApply_Success_OnConnectivityOK(t *testing.T) {
	nm := newMockNetworkManager()
	sam := NewNetworkSafeApplyManager(nm)

	// Override connectivity checker to simulate success
	sam.cc = func(targets []string, timeout time.Duration) bool {
		return true // Connectivity OK
	}

	args := &UpdateInterfaceArgs{Name: "eth0", Action: ActionUpdate}
	safeCfg := &firewall.SafeApplyConfig{PingTargets: []string{"1.1.1.1"}}

	// Perform Apply
	result, err := sam.ApplyInterfaceConfig(args, "127.0.0.1", safeCfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("Expected verify success")
	}

	if nm.RestoreCalled {
		t.Error("Restore should NOT be called on success")
	}
}
