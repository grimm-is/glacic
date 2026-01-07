// Package config provides configuration management including versioned backups.
package config

import (
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupManager handles versioned configuration backups.
type BackupManager struct {
	configPath string
	backupDir  string
	maxBackups int
}

// BackupInfo contains metadata about a backup.
type BackupInfo struct {
	Version       int       `json:"version"`
	Timestamp     time.Time `json:"timestamp"`
	Description   string    `json:"description"`
	Path          string    `json:"path"`
	Size          int64     `json:"size"`
	IsAuto        bool      `json:"is_auto"`        // Auto-backup vs manual
	Pinned        bool      `json:"pinned"`         // Pinned backups are never auto-pruned
	SchemaVersion string    `json:"schema_version"` // Config schema version at time of backup
}

// NewBackupManager creates a new backup manager.
func NewBackupManager(configPath string, maxBackups int) *BackupManager {
	if maxBackups <= 0 {
		maxBackups = 20 // Keep last 20 backups by default
	}

	backupDir := filepath.Join(filepath.Dir(configPath), "backups")

	return &BackupManager{
		configPath: configPath,
		backupDir:  backupDir,
		maxBackups: maxBackups,
	}
}

// ensureBackupDir creates the backup directory if it doesn't exist.
func (b *BackupManager) ensureBackupDir() error {
	return os.MkdirAll(b.backupDir, 0755)
}

// CreateBackup creates a new versioned backup of the current config.
func (b *BackupManager) CreateBackup(description string, isAuto bool) (*BackupInfo, error) {
	if err := b.ensureBackupDir(); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Read current config
	data, err := os.ReadFile(b.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Get next version number
	backups, _ := b.ListBackups()
	version := 1
	if len(backups) > 0 {
		version = backups[0].Version + 1
	}

	// Create backup filename with timestamp
	timestamp := clock.Now()
	filename := fmt.Sprintf("config.%d.%s.hcl", version, timestamp.Format("20060102-150405"))
	backupPath := filepath.Join(b.backupDir, filename)

	// Write backup
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write backup: %w", err)
	}

	// Try to extract schema version from config
	schemaVer := ""
	if cfg, err := LoadHCL(data, b.configPath); err == nil {
		schemaVer = cfg.SchemaVersion
	}
	if schemaVer == "" {
		schemaVer = CurrentSchemaVersion
	}

	// Write metadata
	info := &BackupInfo{
		Version:       version,
		Timestamp:     timestamp,
		Description:   description,
		Path:          backupPath,
		Size:          int64(len(data)),
		IsAuto:        isAuto,
		SchemaVersion: schemaVer,
	}

	metaPath := backupPath + ".meta.json"
	metaData, _ := json.MarshalIndent(info, "", "  ")
	os.WriteFile(metaPath, metaData, 0644)

	// Prune old backups
	b.pruneOldBackups()

	return info, nil
}

// ListBackups returns all backups sorted by version (newest first).
func (b *BackupManager) ListBackups() ([]BackupInfo, error) {
	if err := b.ensureBackupDir(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(b.backupDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []BackupInfo

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".hcl") {
			continue
		}

		backupPath := filepath.Join(b.backupDir, entry.Name())
		metaPath := backupPath + ".meta.json"

		var info BackupInfo

		// Try to read metadata
		if metaData, err := os.ReadFile(metaPath); err == nil {
			json.Unmarshal(metaData, &info)
		}

		// Fill in missing info from file
		if info.Path == "" {
			info.Path = backupPath
		}

		if fileInfo, err := entry.Info(); err == nil {
			if info.Timestamp.IsZero() {
				info.Timestamp = fileInfo.ModTime()
			}
			if info.Size == 0 {
				info.Size = fileInfo.Size()
			}
		}

		// Parse version from filename if not in metadata
		if info.Version == 0 {
			var v int
			fmt.Sscanf(entry.Name(), "config.%d.", &v)
			info.Version = v
		}

		backups = append(backups, info)
	}

	// Sort by version descending (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Version > backups[j].Version
	})

	return backups, nil
}

// GetBackup returns a specific backup by version.
func (b *BackupManager) GetBackup(version int) (*BackupInfo, error) {
	backups, err := b.ListBackups()
	if err != nil {
		return nil, err
	}

	for _, backup := range backups {
		if backup.Version == version {
			return &backup, nil
		}
	}

	return nil, fmt.Errorf("backup version %d not found", version)
}

// GetBackupContent returns the content of a specific backup.
func (b *BackupManager) GetBackupContent(version int) ([]byte, error) {
	backup, err := b.GetBackup(version)
	if err != nil {
		return nil, err
	}

	return os.ReadFile(backup.Path)
}

// RestoreBackup restores a specific backup version.
// It automatically migrates old schema versions to current.
func (b *BackupManager) RestoreBackup(version int) error {
	return b.RestoreBackupWithOptions(version, true)
}

// RestoreBackupWithOptions restores a backup with migration control.
func (b *BackupManager) RestoreBackupWithOptions(version int, autoMigrate bool) error {
	content, err := b.GetBackupContent(version)
	if err != nil {
		return err
	}

	backup, err := b.GetBackup(version)
	if err != nil {
		return err
	}

	// Create a backup of current config before restoring
	b.CreateBackup("Auto-backup before restore", true)

	// If the backup is from an older schema version, migrate it
	if autoMigrate && backup.SchemaVersion != "" && backup.SchemaVersion != CurrentSchemaVersion {
		cfg, err := LoadHCL(content, backup.Path)
		if err != nil {
			return fmt.Errorf("failed to parse backup config: %w", err)
		}

		if err := MigrateToLatest(cfg); err != nil {
			return fmt.Errorf("failed to migrate backup from schema %s: %w", backup.SchemaVersion, err)
		}

		// Save the migrated config
		if err := SaveFile(cfg, b.configPath); err != nil {
			return fmt.Errorf("failed to save migrated config: %w", err)
		}

		return nil
	}

	// Write restored content directly (no migration needed)
	if err := os.WriteFile(b.configPath, content, 0644); err != nil {
		return fmt.Errorf("failed to restore config: %w", err)
	}

	return nil
}

// ValidateBackup checks if a backup can be restored (schema is supported).
func (b *BackupManager) ValidateBackup(version int) error {
	backup, err := b.GetBackup(version)
	if err != nil {
		return err
	}

	// Check if we can read this schema version
	schemaVer, err := ParseVersion(backup.SchemaVersion)
	if err != nil {
		return fmt.Errorf("invalid schema version in backup: %w", err)
	}

	if !IsSupportedVersion(schemaVer) {
		return fmt.Errorf("backup uses unsupported schema version %s", schemaVer)
	}

	// Try to actually parse the backup
	content, err := b.GetBackupContent(version)
	if err != nil {
		return err
	}

	_, err = LoadHCL(content, backup.Path)
	if err != nil {
		return fmt.Errorf("backup config is invalid: %w", err)
	}

	return nil
}

// GetLatestBackup returns the most recent backup.
func (b *BackupManager) GetLatestBackup() (*BackupInfo, error) {
	backups, err := b.ListBackups()
	if err != nil {
		return nil, err
	}

	if len(backups) == 0 {
		return nil, fmt.Errorf("no backups found")
	}

	return &backups[0], nil
}

// pruneOldBackups removes auto-backups beyond maxBackups limit.
// Pinned (user-initiated) backups are never pruned.
func (b *BackupManager) pruneOldBackups() {
	backups, err := b.ListBackups()
	if err != nil {
		return
	}

	// Count non-pinned backups
	var unpinnedBackups []BackupInfo
	for _, backup := range backups {
		if !backup.Pinned {
			unpinnedBackups = append(unpinnedBackups, backup)
		}
	}

	if len(unpinnedBackups) <= b.maxBackups {
		return
	}

	// Remove oldest unpinned backups
	for i := b.maxBackups; i < len(unpinnedBackups); i++ {
		os.Remove(unpinnedBackups[i].Path)
		os.Remove(unpinnedBackups[i].Path + ".meta.json")
	}
}

// DeleteBackup removes a specific backup.
func (b *BackupManager) DeleteBackup(version int) error {
	backup, err := b.GetBackup(version)
	if err != nil {
		return err
	}

	os.Remove(backup.Path)
	os.Remove(backup.Path + ".meta.json")

	return nil
}

// CompareWithCurrent compares a backup with the current config.
func (b *BackupManager) CompareWithCurrent(version int) (string, error) {
	backupContent, err := b.GetBackupContent(version)
	if err != nil {
		return "", err
	}

	currentContent, err := os.ReadFile(b.configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read current config: %w", err)
	}

	if string(backupContent) == string(currentContent) {
		return "No differences", nil
	}

	// Simple line-by-line diff
	backupLines := strings.Split(string(backupContent), "\n")
	currentLines := strings.Split(string(currentContent), "\n")

	var diff strings.Builder
	diff.WriteString(fmt.Sprintf("Backup v%d vs Current:\n", version))

	maxLines := len(backupLines)
	if len(currentLines) > maxLines {
		maxLines = len(currentLines)
	}

	for i := 0; i < maxLines; i++ {
		backupLine := ""
		currentLine := ""

		if i < len(backupLines) {
			backupLine = backupLines[i]
		}
		if i < len(currentLines) {
			currentLine = currentLines[i]
		}

		if backupLine != currentLine {
			if backupLine != "" {
				diff.WriteString(fmt.Sprintf("- %s\n", backupLine))
			}
			if currentLine != "" {
				diff.WriteString(fmt.Sprintf("+ %s\n", currentLine))
			}
		}
	}

	return diff.String(), nil
}

// CreatePinnedBackup creates a user-initiated backup that won't be auto-pruned.
func (b *BackupManager) CreatePinnedBackup(description string) (*BackupInfo, error) {
	backup, err := b.CreateBackup(description, false)
	if err != nil {
		return nil, err
	}

	// Pin the backup
	backup.Pinned = true

	// Update metadata
	metaPath := backup.Path + ".meta.json"
	metaData, _ := json.MarshalIndent(backup, "", "  ")
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return nil, fmt.Errorf("failed to update backup metadata: %w", err)
	}

	return backup, nil
}

// PinBackup marks a backup as pinned (won't be auto-pruned).
func (b *BackupManager) PinBackup(version int) error {
	return b.setBackupPinned(version, true)
}

// UnpinBackup removes the pinned status from a backup.
func (b *BackupManager) UnpinBackup(version int) error {
	return b.setBackupPinned(version, false)
}

// setBackupPinned updates the pinned status of a backup.
func (b *BackupManager) setBackupPinned(version int, pinned bool) error {
	backup, err := b.GetBackup(version)
	if err != nil {
		return err
	}

	backup.Pinned = pinned

	// Update metadata
	metaPath := backup.Path + ".meta.json"
	metaData, _ := json.MarshalIndent(backup, "", "  ")
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return fmt.Errorf("failed to update backup metadata: %w", err)
	}

	return nil
}

// SetMaxBackups updates the maximum number of auto-backups to retain.
func (b *BackupManager) SetMaxBackups(max int) {
	if max > 0 {
		b.maxBackups = max
		b.pruneOldBackups()
	}
}

// GetMaxBackups returns the current max backups setting.
func (b *BackupManager) GetMaxBackups() int {
	return b.maxBackups
}

// GetBackupStats returns statistics about backups.
func (b *BackupManager) GetBackupStats() (total, pinned, auto int, totalSize int64) {
	backups, err := b.ListBackups()
	if err != nil {
		return 0, 0, 0, 0
	}

	for _, backup := range backups {
		total++
		totalSize += backup.Size
		if backup.Pinned {
			pinned++
		}
		if backup.IsAuto {
			auto++
		}
	}

	return total, pinned, auto, totalSize
}
