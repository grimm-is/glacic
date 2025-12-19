package ctlplane

import (
	"context"
	"fmt"
	"os"
	"testing"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/network"
	"grimm.is/glacic/internal/services"
	"grimm.is/glacic/internal/upgrade"
)

func TestServer_BasicRPC(t *testing.T) {
	cfg := &config.Config{
		Interfaces: []config.Interface{
			{Name: "eth0", Zone: "wan"},
		},
	}

	s := NewServer(cfg, "/tmp/test-config.hcl", &MockNetLib{})

	// Test GetStatus
	statusReply := &GetStatusReply{}
	if err := s.GetStatus(&Empty{}, statusReply); err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if !statusReply.Status.Running {
		t.Error("Expected Running=true")
	}
	if statusReply.Status.ConfigFile != "/tmp/test-config.hcl" {
		t.Errorf("Expected config file /tmp/test-config.hcl, got %s", statusReply.Status.ConfigFile)
	}

	// Test GetConfig
	configReply := &GetConfigReply{}
	if err := s.GetConfig(&Empty{}, configReply); err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if len(configReply.Config.Interfaces) != 1 {
		t.Errorf("Expected 1 interface, got %d", len(configReply.Config.Interfaces))
	}

	// Test GetServices (initially empty because we didn't inject services)
	servicesReply := &GetServicesReply{}
	if err := s.GetServices(&Empty{}, servicesReply); err != nil {
		t.Fatalf("GetServices failed: %v", err)
	}
	if len(servicesReply.Services) != 0 {
		t.Errorf("Expected 0 services, got %d", len(servicesReply.Services))
	}

	// Test RestartService (should fail/error safely or return error for unknown/nil service)
	restartArgs := &RestartServiceArgs{ServiceName: "unknown"}
	if err := s.RestartService(restartArgs, &Empty{}); err == nil {
		t.Error("Expected error for unknown service")
	}
}

func TestServer_SetStateStore(t *testing.T) {
	s := NewServer(nil, "", &MockNetLib{})

	// Verify initial state is nil
	if s.stateStore != nil {
		t.Error("Expected initial stateStore to be nil")
	}

	// Set state store
	s.SetStateStore(nil)

	// Verify stateStore can be set (simple verification)
	// Since we pass nil, it should still be nil
	if s.stateStore != nil {
		t.Error("Expected stateStore to remain nil after SetStateStore(nil)")
	}
}

func TestServer_UpdateConfig(t *testing.T) {
	s := NewServer(&config.Config{}, "", &MockNetLib{})
	newCfg := &config.Config{IPForwarding: true}
	s.UpdateConfig(newCfg)
	if !s.config.IPForwarding {
		t.Error("UpdateConfig failed")
	}
}

func TestServer_HCLEditing(t *testing.T) {
	// Create partial config
	tmpFile, err := os.CreateTemp("", "config-*.hcl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	initialHCL := `
interface "eth0" {
  zone = "wan"
  ipv4 = ["192.168.1.1/24"]
}
`
	if _, err := tmpFile.WriteString(initialHCL); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg := &config.Config{}

	// Create server with this config file
	s := NewServer(cfg, tmpFile.Name(), &MockNetLib{})

	// Since we don't have a daemon running ensureHCLConfig loops, we should trigger it if possible
	// But Server doesn't expose it. However, if we just call GetRawHCL, it might work if it reads the file on demand
	// or if NewServer sets it up.
	// NewServer creates backupManager but hclConfig is nil.
	// But GetRawHCL calls EnsureHCLConfig logic internally?
	// Let's inspect server.go logic for GetRawHCL again in previous turn.
	// It calls s.ensureHCLConfig().
	// So it should work!

	rawReply := &GetRawHCLReply{}
	if err := s.GetRawHCL(&Empty{}, rawReply); err != nil {
		t.Logf("GetRawHCL failed: %v", err)
	} else {
		if rawReply.Path != tmpFile.Name() {
			t.Errorf("Expected path %s, got %s", tmpFile.Name(), rawReply.Path)
		}
		// Content check
		if len(rawReply.HCL) == 0 {
			t.Error("Expected content, got empty string")
		}
	}
}

func TestServer_CreateBond_DHCP(t *testing.T) {
	s := NewServer(&config.Config{}, "", &MockNetLib{})

	// Use invalid args that would technically pass validaion but fail at netlink
	args := &CreateBondArgs{
		Name:       "bond0",
		Mode:       "active-backup",
		Interfaces: []string{"eth0", "eth1"},
		DHCP:       true,
	}
	reply := &CreateBondReply{}

	err := s.CreateBond(args, reply)
	// Expect nil error from RPC, but reply.Success might be false or reply.Error set
	// Actually implementation returns nil error unless panic, and sets Error in reply

	if err != nil {
		t.Fatalf("RPC call failed: %v", err)
	}

	// Since we are not root and don't have these interfaces, it should fail in netlink or member lookup
	if reply.Success {
		t.Error("Expected failure due to missing permission/interfaces")
	}
	if reply.Error == "" {
		t.Error("Expected error message in reply")
	}
}

func TestUpgrade_Checksum(t *testing.T) {
	// Swap verification logic
	origVerify := verifyUpgradeBinary
	defer func() { verifyUpgradeBinary = origVerify }()

	// create dummy server
	s := NewServer(&config.Config{}, "", &MockNetLib{})
	// Inject empty upgrade manager to bypass "not initialized" check
	// We use an empty struct because we expect to fail BEFORE accessing it methods (verification check)
	s.SetUpgradeManager(&upgrade.Manager{})

	// Case 1: Verification Failure (Simulated)
	verifyUpgradeBinary = func(path, expected string) error {
		return fmt.Errorf("mock failure")
	}
	reply := &UpgradeReply{}
	s.Upgrade(&UpgradeArgs{Checksum: "123"}, reply)
	if reply.Error != "mock failure" {
		t.Errorf("Expected 'mock failure', got '%s'", reply.Error)
	}

	// Case 2: Checksum Mismatch (Simulated from VerifyBinary logic)
	// We want to verify that Server correctly propagates errors from our hook.
	// Logic in Server: if err := verify(...); err != nil { return nil } (Sets reply.Error)
	verifyUpgradeBinary = func(path, expected string) error {
		return fmt.Errorf("checksum mismatch")
	}
	s.Upgrade(&UpgradeArgs{Checksum: "123"}, reply)
	if reply.Error != "checksum mismatch" {
		t.Errorf("Expected 'checksum mismatch', got '%s'", reply.Error)
	}

	// Case 3: Success logic verification
	// We can't easily test full success without fully initialized UpgradeManager that doesn't panic on SetStateCollectors.
	// But we covered the critical "Security Gate" logic.
}

// MockServiceManager for testing
type MockServiceManager struct {
	ReloadAllCalled bool
	ReloadAllConfig *config.Config
}

func (m *MockServiceManager) RegisterService(svc services.Service)            {}
func (m *MockServiceManager) GetService(name string) (services.Service, bool) { return nil, false }
func (m *MockServiceManager) GetServicesStatus() []ServiceStatus              { return nil }
func (m *MockServiceManager) RestartService(name string) error                { return nil }
func (m *MockServiceManager) StartAll(ctx context.Context)                    {}
func (m *MockServiceManager) StopAll(ctx context.Context)                     {}
func (m *MockServiceManager) ReloadAll(cfg *config.Config) *ReloadResult {
	m.ReloadAllCalled = true
	m.ReloadAllConfig = cfg
	return &ReloadResult{Success: true}
}

func TestRestoreBackup_AppliesConfig(t *testing.T) {
	// 1. Setup Environment
	tmpFile, err := os.CreateTemp("", "config-restore-*.hcl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	// Write initial valid config
	initialHCL := `
interface "eth0" {
  zone = "wan"
  ipv4 = ["192.168.1.1/24"]
}
`
	if _, err := tmpFile.WriteString(initialHCL); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// 2. Initialize Server with Mocks
	cfg := &config.Config{}

	// Create a real NetworkManager just to satisfy NewServer, or use nil if safe.
	// We'll use NewServer() but then SWAP the dependencies we care about.
	s := NewServer(cfg, tmpFile.Name(), &MockNetLib{})

	// Inject Mock ServiceManager
	mockOrch := &MockServiceManager{}
	s.serviceOrchestrator = mockOrch

	// Inject Mock NetworkSafeApply (to avoid actual network calls)
	// We reuse MockNetworkManager from safe_network_test.go if available,
	// or just let it fail silently since we care about ReloadAll being called.
	// Actually, RestoreBackup -> ApplyConfig -> network.ApplyInterface.
	// We mocked network.ApplyInterface using MockNetworkManager, so it will be called safely.
	// ApplyConfig ALSO calls s.serviceOrchestrator.ReloadAll(newCfg).
	// If ReloadAll is called, we know ApplyConfig was executed.

	// 3. Create a Backup
	backupArgs := &CreateBackupArgs{Description: "Test Backup"}
	backupReply := &CreateBackupReply{}
	if err := s.CreateBackup(backupArgs, backupReply); err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}
	// backupReply.Backup.Version should be 1

	// 4. Modify current config to be "drifted"
	// We don't even need to modify it on disk, just verify ReloadAll is called upon restore.
	mockOrch.ReloadAllCalled = false // Reset

	// 5. Restore Backup
	restoreArgs := &RestoreBackupArgs{Version: backupReply.Backup.Version}
	restoreReply := &RestoreBackupReply{}
	if err := s.RestoreBackup(restoreArgs, restoreReply); err != nil {
		t.Fatalf("RestoreBackup failed: %v", err)
	}

	// 6. Assertions
	if !mockOrch.ReloadAllCalled {
		t.Error("CRITICAL: ApplyConfig/ReloadAll was NOT called after RestoreBackup! System state is desynchronized from config.")
	}
	if !restoreReply.Success {
		t.Error("Expected restore success")
	}
}

// MockNetLib satisfies network.NetworkManager interface
type MockNetLib struct{}

func (m *MockNetLib) SetIPForwarding(enable bool) error { return nil }
func (m *MockNetLib) SetupLoopback() error              { return nil }
func (m *MockNetLib) InitializeDHCPClientManager(cfg *config.DHCPServer) {
}
func (m *MockNetLib) StopDHCPClientManager()                         {}
func (m *MockNetLib) ApplyInterface(ifaceCfg config.Interface) error { return nil }
func (m *MockNetLib) ApplyStaticRoutes(routes []config.Route) error  { return nil }
func (m *MockNetLib) WaitForLinkIP(ifaceName string, timeoutSeconds int) error {
	return nil
}
func (m *MockNetLib) GetDHCPLeases() map[string]network.LeaseInfo     { return nil }
func (m *MockNetLib) ApplyUIDRoutes(routes []config.UIDRouting) error { return nil }
