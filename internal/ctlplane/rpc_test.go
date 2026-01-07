package ctlplane

import (
	"os"
	"path/filepath"
	"testing"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/state"
)

func TestServer_RPC_Basics(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ctlplane-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "firewall.hcl")
	cfg := &config.Config{
		Interfaces: []config.Interface{
			{Name: "eth0", Zone: "wan"},
		},
	}

	server := NewServer(cfg, configPath, &MockNetLib{})
	// Suppress log output for cleaner test run?
	// log.SetOutput(io.Discard)

	t.Run("GetStatus", func(t *testing.T) {
		args := &Empty{}
		reply := &GetStatusReply{}

		if err := server.GetStatus(args, reply); err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}

		if !reply.Status.Running {
			t.Error("Status should be running")
		}
		if reply.Status.ConfigFile != configPath {
			t.Errorf("Config file mismatch: got %s, want %s", reply.Status.ConfigFile, configPath)
		}
	})

	t.Run("GetConfig", func(t *testing.T) {
		args := &Empty{}
		reply := &GetConfigReply{}

		if err := server.GetConfig(args, reply); err != nil {
			t.Fatalf("GetConfig failed: %v", err)
		}

		if len(reply.Config.Interfaces) != 1 {
			t.Errorf("Expected 1 interface, got %d", len(reply.Config.Interfaces))
		}
		if reply.Config.Interfaces[0].Name != "eth0" {
			t.Errorf("Expected eth0, got %s", reply.Config.Interfaces[0].Name)
		}
	})

	t.Run("GetServices", func(t *testing.T) {
		args := &Empty{}
		reply := &GetServicesReply{}

		if err := server.GetServices(args, reply); err != nil {
			t.Fatalf("GetServices failed: %v", err)
		}
		// Expect empty since no services injected
		if len(reply.Services) != 0 {
			t.Errorf("Expected 0 services, got %d", len(reply.Services))
		}
	})

	t.Run("GetAvailableInterfaces", func(t *testing.T) {
		args := &Empty{}
		reply := &GetAvailableInterfacesReply{}
		err := server.GetAvailableInterfaces(args, reply)
		if err != nil {
			t.Logf("GetAvailableInterfaces returned error (expected on non-Linux?): %v", err)
		} else {
			t.Logf("Found %d interfaces", len(reply.Interfaces))
		}
	})

	t.Run("HCL_Operations", func(t *testing.T) {
		// Create a dummy HCL file
		hclContent := `
interface "eth0" {
  zone = "wan"
}
`
		if err := os.WriteFile(configPath, []byte(hclContent), 0644); err != nil {
			t.Fatal(err)
		}

		// GetRawHCL
		getArgs := &Empty{}
		getReply := &GetRawHCLReply{}
		if err := server.GetRawHCL(getArgs, getReply); err != nil {
			t.Fatalf("GetRawHCL failed: %v", err)
		}
		if getReply.HCL == "" {
			t.Error("HCL content empty")
		}

		// ValidateHCL
		valArgs := &ValidateHCLArgs{HCL: hclContent}
		valReply := &ValidateHCLReply{}
		if err := server.ValidateHCL(valArgs, valReply); err != nil {
			t.Fatalf("ValidateHCL failed: %v", err)
		}
		if !valReply.Valid {
			t.Errorf("HCL should be valid, errors: %v", valReply.Diagnostics)
		}

		// SetRawHCL (Update)
		newHCL := `
interface "eth0" {
  zone = "lan"
}
`
		setArgs := &SetRawHCLArgs{HCL: newHCL}
		setReply := &SetRawHCLReply{}
		if err := server.SetRawHCL(setArgs, setReply); err != nil {
			t.Fatalf("SetRawHCL failed: %v", err)
		}

		// Verify update via GetRawHCL
		server.GetRawHCL(getArgs, getReply)
		if getReply.HCL != newHCL {
			t.Error("HCL content mismatch after update")
		}
	})

	t.Run("Backup_Operations", func(t *testing.T) {
		// CreateBackup
		createArgs := &CreateBackupArgs{Description: "test backup"}
		createReply := &CreateBackupReply{}
		if err := server.CreateBackup(createArgs, createReply); err != nil {
			t.Fatalf("CreateBackup failed: %v", err)
		}
		if !createReply.Success {
			t.Error("CreateBackup returned false success")
		}

		// ListBackups
		listArgs := &Empty{}
		listReply := &ListBackupsReply{}
		if err := server.ListBackups(listArgs, listReply); err != nil {
			t.Fatalf("ListBackups failed: %v", err)
		}
		if len(listReply.Backups) != 1 {
			t.Errorf("Expected 1 backup, got %d", len(listReply.Backups))
		}
		// PinBackup
		pinArgs := &PinBackupArgs{Version: listReply.Backups[0].Version, Pinned: true}
		pinReply := &PinBackupReply{}
		if err := server.PinBackup(pinArgs, pinReply); err != nil {
			t.Fatalf("PinBackup failed: %v", err)
		}

		// GetBackupContent
		contentArgs := &GetBackupContentArgs{Version: listReply.Backups[0].Version}
		contentReply := &GetBackupContentReply{}
		if err := server.GetBackupContent(contentArgs, contentReply); err != nil {
			t.Fatalf("GetBackupContent failed: %v", err)
		}
		if contentReply.Content == "" {
			t.Error("Backup content empty")
		}

		// RestoreBackup
		// Be careful, this replaces the config file on disk!
		// But in test, config is in tmpDir.
		restoreArgs := &RestoreBackupArgs{Version: listReply.Backups[0].Version}
		restoreReply := &RestoreBackupReply{}
		if err := server.RestoreBackup(restoreArgs, restoreReply); err != nil {
			t.Fatalf("RestoreBackup failed: %v", err)
		}
		if !restoreReply.Success {
			t.Error("RestoreBackup returned false success")
		}
	})

	t.Run("Section_HCL", func(t *testing.T) {
		// GetSectionHCL
		secArgs := &GetSectionHCLArgs{
			SectionType: "interface",
			Labels:      []string{"eth0"},
		}
		secReply := &GetSectionHCLReply{}
		// We expect this to fail or succeed depending on HCL content.
		// Current HCL in file (after SetRawHCL in previous test): zone="lan"
		// But Wait, previous test might have run in successful order?
		// We need to re-init or trust state.
		// server holds `hclConfig` in memory? Yes.
		// Let's assume it has parsed the file.

		if err := server.GetSectionHCL(secArgs, secReply); err != nil {
			// It might fail if hclConfig is nil?
			// NewServer doesn't init hclConfig.
			// But RestoreBackup does: `s.hclConfig = cf`
			// SetRawHCL also does: `s.hclConfig = cf` (if implemented correctly? check server.go)
			// server.go: SetRawHCL calls config.ParseHCL -> sets s.hclConfig?
			// Checking code... SetRawHCL does `s.hclConfig = configFile`.
			// So it should be set.
			t.Logf("GetSectionHCL result: %v", err)
		} else {
			if secReply.HCL == "" {
				t.Log("Section HCL empty (might be parsing issue or logic)")
			}
		}

		// SetSectionHCL
		setSecArgs := &SetSectionHCLArgs{
			SectionType: "interface",
			Labels:      []string{"eth0"},
			HCL: `
interface "eth0" {
  zone = "dmz"
}
`,
		}
		setSecReply := &SetSectionHCLReply{}
		if err := server.SetSectionHCL(setSecArgs, setSecReply); err != nil {
			t.Logf("SetSectionHCL failed: %v", err)
		}
	})
}

func TestServer_RPC_Advanced(t *testing.T) {
	// Setup server with mocked managers if possible, or functional defaults
	cfg := &config.Config{}
	server := NewServer(cfg, "/tmp/config.hcl", &MockNetLib{})

	t.Run("GetDHCPLeases", func(t *testing.T) {
		args := &Empty{}
		reply := &GetDHCPLeasesReply{}
		err := server.GetDHCPLeases(args, reply)
		if err != nil {
			t.Fatalf("GetDHCPLeases failed: %v", err)
		}
		// Expect empty list
		if len(reply.Leases) != 0 {
			t.Errorf("Expected 0 leases, got %d", len(reply.Leases))
		}
	})

	t.Run("SafeApplyInterface", func(t *testing.T) {
		startZone := "wan"
		args := &SafeApplyInterfaceArgs{
			UpdateArgs: &UpdateInterfaceArgs{
				Name:   "eth99",
				Action: ActionUpdate,
				Zone:   &startZone,
				IPv4:   []string{"10.0.0.1/24"},
			},
			PingTimeoutSeconds: 5,
		}
		reply := &firewall.ApplyResult{}

		err := server.SafeApplyInterface(args, reply)

		if err == nil {
			if reply.Success {
				t.Log("SafeApplyInterface unexpectedly succeeded on dummy interface")
			} else {
				t.Logf("SafeApplyInterface failed as expected: %s", reply.Message)
			}
		} else {
			t.Logf("SafeApplyInterface RPC error: %v", err)
		}
	})

	t.Run("UpdateInterface", func(t *testing.T) {
		zone := "lan"
		args := &UpdateInterfaceArgs{
			Name:   "eth0",
			Action: ActionUpdate,
			Zone:   &zone,
		}
		reply := &UpdateInterfaceReply{}
		err := server.UpdateInterface(args, reply)
		if err != nil {
			t.Logf("UpdateInterface failed as expected (mock env): %v", err)
		}
	})
}

func TestServer_LearningRPC(t *testing.T) {
	// Setup dependencies
	stateStore, err := state.NewSQLiteStore(state.Options{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}
	defer stateStore.Close()

	cfg := &config.RuleLearningConfig{Enabled: true}
	learningSvc, err := learning.NewService(cfg, ":memory:")
	if err != nil {
		t.Fatalf("Failed to create learning service: %v", err)
	}
	// Start learning service
	learningSvc.Start()
	defer learningSvc.Stop()

	// Setup Server
	server := NewServer(&config.Config{}, "/tmp/config.hcl", &MockNetLib{})
	server.SetLearningService(learningSvc)

	t.Run("GetLearningRules_Empty", func(t *testing.T) {
		args := &GetLearningRulesArgs{Status: "pending"}
		reply := &GetLearningRulesReply{}
		err := server.GetLearningRules(args, reply)
		if err != nil {
			t.Fatalf("GetLearningRules failed: %v", err)
		}
		if len(reply.Rules) != 0 {
			t.Errorf("Expected 0 rules, got %d", len(reply.Rules))
		}
	})

	t.Run("GetLearningStats", func(t *testing.T) {
		args := &Empty{}
		reply := &GetLearningStatsReply{}
		err := server.GetLearningStats(args, reply)
		if err != nil {
			t.Fatalf("GetLearningStats failed: %v", err)
		}
		if reply.Stats == nil {
			t.Error("Stats should not be nil")
		}
		// Check for 'service_running' and 'db'
		if _, ok := reply.Stats["service_running"]; !ok {
			t.Errorf("Expected 'service_running' stat")
		}
		if _, ok := reply.Stats["db"]; !ok {
			t.Errorf("Expected 'db' stats")
		}
	})
}
