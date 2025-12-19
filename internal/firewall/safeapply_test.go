package firewall

import (
	"os"
	"testing"
	"time"
)

func TestDefaultSafeApplyConfig(t *testing.T) {
	cfg := DefaultSafeApplyConfig()

	if cfg == nil {
		t.Fatal("DefaultSafeApplyConfig returned nil")
	}

	if cfg.PingTargets != nil {
		t.Error("PingTargets should be nil by default")
	}

	if cfg.PingTimeout != 5*time.Second {
		t.Errorf("Expected PingTimeout 5s, got %v", cfg.PingTimeout)
	}

	if cfg.RollbackDelay != 30*time.Second {
		t.Errorf("Expected RollbackDelay 30s, got %v", cfg.RollbackDelay)
	}

	if !cfg.RequireConfirmation {
		t.Error("RequireConfirmation should be true by default")
	}
}

func TestNewSafeApplyManager(t *testing.T) {
	// Create temp config file
	tmpFile, err := os.CreateTemp("", "test-config-*.hcl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Create manager
	mgr := NewSafeApplyManager(nil, tmpFile.Name())

	if mgr == nil {
		t.Fatal("NewSafeApplyManager returned nil")
	}

	if mgr.configPath != tmpFile.Name() {
		t.Errorf("Expected configPath %s, got %s", tmpFile.Name(), mgr.configPath)
	}

	if mgr.backupManager == nil {
		t.Error("backupManager should not be nil")
	}

	if mgr.rollbackManager == nil {
		t.Error("rollbackManager should not be nil")
	}
}

func TestSafeApplyManager_GetPendingApply_Empty(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-config-*.hcl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	mgr := NewSafeApplyManager(nil, tmpFile.Name())
	pending := mgr.GetPendingApply()

	if pending != nil {
		t.Error("Expected nil pending apply initially")
	}
}

func TestApplyResult_Struct(t *testing.T) {
	result := ApplyResult{
		Success:         true,
		PendingID:       "test-123",
		Message:         "Applied successfully",
		RollbackTime:    "2025-01-01T00:00:00Z",
		RequiresConfirm: true,
		BackupVersion:   5,
	}

	if !result.Success {
		t.Error("Success should be true")
	}
	if result.PendingID != "test-123" {
		t.Error("PendingID mismatch")
	}
	if result.BackupVersion != 5 {
		t.Error("BackupVersion mismatch")
	}
}

func TestSafeApplyConfig_CustomValues(t *testing.T) {
	cfg := &SafeApplyConfig{
		PingTargets:         []string{"8.8.8.8", "1.1.1.1"},
		PingTimeout:         10 * time.Second,
		RollbackDelay:       60 * time.Second,
		RequireConfirmation: false,
	}

	if len(cfg.PingTargets) != 2 {
		t.Errorf("Expected 2 ping targets, got %d", len(cfg.PingTargets))
	}
	if cfg.PingTimeout != 10*time.Second {
		t.Errorf("Wrong PingTimeout: %v", cfg.PingTimeout)
	}
	if cfg.RollbackDelay != 60*time.Second {
		t.Errorf("Wrong RollbackDelay: %v", cfg.RollbackDelay)
	}
	if cfg.RequireConfirmation {
		t.Error("RequireConfirmation should be false")
	}
}
