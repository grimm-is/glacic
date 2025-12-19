package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupManager(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.hcl")

	// Create initial config
	initialConfig := `# Test config
ip_forwarding = true

interface "eth0" {
  zone = "WAN"
  dhcp = true
}
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to write initial config: %v", err)
	}

	// Create backup manager
	bm := NewBackupManager(configPath, 5)

	// Test creating backup
	backup1, err := bm.CreateBackup("First backup", false)
	if err != nil {
		t.Fatalf("Failed to create backup: %v", err)
	}

	if backup1.Version != 1 {
		t.Errorf("Expected version 1, got %d", backup1.Version)
	}
	if backup1.Description != "First backup" {
		t.Errorf("Expected description 'First backup', got %s", backup1.Description)
	}
	if backup1.IsAuto {
		t.Error("Expected IsAuto to be false")
	}

	// Modify config
	modifiedConfig := `# Modified config
ip_forwarding = false

interface "eth0" {
  zone = "WAN"
  dhcp = false
}
`
	if err := os.WriteFile(configPath, []byte(modifiedConfig), 0644); err != nil {
		t.Fatalf("Failed to write modified config: %v", err)
	}

	// Create second backup
	backup2, err := bm.CreateBackup("Second backup", true)
	if err != nil {
		t.Fatalf("Failed to create second backup: %v", err)
	}

	if backup2.Version != 2 {
		t.Errorf("Expected version 2, got %d", backup2.Version)
	}
	if !backup2.IsAuto {
		t.Error("Expected IsAuto to be true")
	}

	// List backups
	backups, err := bm.ListBackups()
	if err != nil {
		t.Fatalf("Failed to list backups: %v", err)
	}

	if len(backups) != 2 {
		t.Errorf("Expected 2 backups, got %d", len(backups))
	}

	// Should be sorted newest first
	if backups[0].Version != 2 {
		t.Error("Backups should be sorted newest first")
	}

	// Get backup content
	content, err := bm.GetBackupContent(1)
	if err != nil {
		t.Fatalf("Failed to get backup content: %v", err)
	}

	if string(content) != initialConfig {
		t.Error("Backup content doesn't match original")
	}

	// Restore backup
	if err := bm.RestoreBackup(1); err != nil {
		t.Fatalf("Failed to restore backup: %v", err)
	}

	// Verify restored content
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read restored config: %v", err)
	}

	if string(restored) != initialConfig {
		t.Error("Restored config doesn't match backup")
	}

	// Should have created auto-backup before restore
	backups, _ = bm.ListBackups()
	if len(backups) != 3 {
		t.Errorf("Expected 3 backups after restore, got %d", len(backups))
	}
}

func TestBackupPruning(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.hcl")

	// Create config
	if err := os.WriteFile(configPath, []byte("ip_forwarding = true"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Create backup manager with max 3 backups
	bm := NewBackupManager(configPath, 3)

	// Create 5 backups
	for i := 1; i <= 5; i++ {
		_, err := bm.CreateBackup("", true)
		if err != nil {
			t.Fatalf("Failed to create backup %d: %v", i, err)
		}
	}

	// Should only have 3 backups
	backups, err := bm.ListBackups()
	if err != nil {
		t.Fatalf("Failed to list backups: %v", err)
	}

	if len(backups) != 3 {
		t.Errorf("Expected 3 backups after pruning, got %d", len(backups))
	}

	// Should have versions 5, 4, 3 (newest)
	if backups[0].Version != 5 {
		t.Errorf("Expected newest backup to be version 5, got %d", backups[0].Version)
	}
}

func TestBackupNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.hcl")

	if err := os.WriteFile(configPath, []byte("ip_forwarding = true"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	bm := NewBackupManager(configPath, 5)

	// Try to get non-existent backup
	_, err := bm.GetBackup(999)
	if err == nil {
		t.Error("Expected error for non-existent backup")
	}

	// Try to restore non-existent backup
	err = bm.RestoreBackup(999)
	if err == nil {
		t.Error("Expected error for restoring non-existent backup")
	}
}

func TestPinnedBackups(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.hcl")

	// Create config
	if err := os.WriteFile(configPath, []byte("ip_forwarding = true"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Create backup manager with max 2 auto-backups
	bm := NewBackupManager(configPath, 2)

	// Create a pinned backup
	pinned, err := bm.CreatePinnedBackup("Important checkpoint")
	if err != nil {
		t.Fatalf("Failed to create pinned backup: %v", err)
	}

	if !pinned.Pinned {
		t.Error("Expected backup to be pinned")
	}

	// Create 3 auto-backups (should prune to 2)
	for i := 0; i < 3; i++ {
		bm.CreateBackup("", true)
	}

	// List backups - should have 2 auto + 1 pinned = 3 total
	backups, err := bm.ListBackups()
	if err != nil {
		t.Fatalf("Failed to list backups: %v", err)
	}

	// Count pinned vs unpinned
	pinnedCount := 0
	unpinnedCount := 0
	for _, b := range backups {
		if b.Pinned {
			pinnedCount++
		} else {
			unpinnedCount++
		}
	}

	if pinnedCount != 1 {
		t.Errorf("Expected 1 pinned backup, got %d", pinnedCount)
	}
	if unpinnedCount != 2 {
		t.Errorf("Expected 2 unpinned backups, got %d", unpinnedCount)
	}

	// Verify pinned backup still exists
	found := false
	for _, b := range backups {
		if b.Version == pinned.Version {
			found = true
			if !b.Pinned {
				t.Error("Pinned backup lost its pinned status")
			}
		}
	}
	if !found {
		t.Error("Pinned backup was incorrectly pruned")
	}

	// Test unpinning
	if err := bm.UnpinBackup(pinned.Version); err != nil {
		t.Fatalf("Failed to unpin backup: %v", err)
	}

	backup, _ := bm.GetBackup(pinned.Version)
	if backup.Pinned {
		t.Error("Backup should be unpinned")
	}

	// Test pinning
	if err := bm.PinBackup(pinned.Version); err != nil {
		t.Fatalf("Failed to pin backup: %v", err)
	}

	backup, _ = bm.GetBackup(pinned.Version)
	if !backup.Pinned {
		t.Error("Backup should be pinned")
	}
}

func TestMaxBackupsConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.hcl")

	if err := os.WriteFile(configPath, []byte("ip_forwarding = true"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	bm := NewBackupManager(configPath, 5)

	if bm.GetMaxBackups() != 5 {
		t.Errorf("Expected max backups 5, got %d", bm.GetMaxBackups())
	}

	bm.SetMaxBackups(10)
	if bm.GetMaxBackups() != 10 {
		t.Errorf("Expected max backups 10, got %d", bm.GetMaxBackups())
	}

	// Invalid value should be ignored
	bm.SetMaxBackups(0)
	if bm.GetMaxBackups() != 10 {
		t.Errorf("Expected max backups to remain 10, got %d", bm.GetMaxBackups())
	}
}

func TestCompareWithCurrent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.hcl")

	// Create initial config
	if err := os.WriteFile(configPath, []byte("ip_forwarding = true"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	bm := NewBackupManager(configPath, 5)

	// Create backup
	bm.CreateBackup("", false)

	// Modify config
	if err := os.WriteFile(configPath, []byte("ip_forwarding = false"), 0644); err != nil {
		t.Fatalf("Failed to modify config: %v", err)
	}

	// Compare
	diff, err := bm.CompareWithCurrent(1)
	if err != nil {
		t.Fatalf("Failed to compare: %v", err)
	}

	if diff == "No differences" {
		t.Error("Expected differences but got none")
	}
}
