package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"

	"grimm.is/glacic/internal/config"
)

// TaskRegistry holds references to system components for task execution.
type TaskRegistry struct {
	ConfigPath    string
	BackupDir     string
	GetConfig     func() *config.Config
	ApplyConfig   func(*config.Config) error
	RefreshIPSets func() error
	RefreshDNS    func() error
}

// NewIPSetUpdateTask creates a task to update IPSets from external sources.
func NewIPSetUpdateTask(registry *TaskRegistry, interval time.Duration) *Task {
	return &Task{
		ID:          "ipset-update",
		Name:        "IPSet Update",
		Description: "Refresh IPSets from FireHOL and custom URLs",
		Schedule:    Every(interval),
		Enabled:     true,
		RunOnStart:  true,
		Timeout:     5 * time.Minute,
		Func: func(ctx context.Context) error {
			if registry.RefreshIPSets == nil {
				return fmt.Errorf("IPSet refresh function not configured")
			}
			return registry.RefreshIPSets()
		},
	}
}

// NewDNSBlocklistUpdateTask creates a task to update DNS blocklists.
func NewDNSBlocklistUpdateTask(registry *TaskRegistry, interval time.Duration) *Task {
	return &Task{
		ID:          "dns-blocklist-update",
		Name:        "DNS Blocklist Update",
		Description: "Refresh DNS blocklists from configured URLs",
		Schedule:    Every(interval),
		Enabled:     true,
		RunOnStart:  true,
		Timeout:     5 * time.Minute,
		Func: func(ctx context.Context) error {
			if registry.RefreshDNS == nil {
				return fmt.Errorf("DNS refresh function not configured")
			}
			return registry.RefreshDNS()
		},
	}
}

// NewConfigBackupTask creates a task to backup the configuration.
func NewConfigBackupTask(registry *TaskRegistry, schedule Schedule, keepCount int) *Task {
	if keepCount <= 0 {
		keepCount = 7 // Default to keeping 7 backups
	}

	return &Task{
		ID:          "config-backup",
		Name:        "Configuration Backup",
		Description: "Automatically backup configuration",
		Schedule:    schedule,
		Enabled:     true,
		RunOnStart:  false,
		Timeout:     1 * time.Minute,
		Func: func(ctx context.Context) error {
			if registry.GetConfig == nil {
				return fmt.Errorf("GetConfig function not configured")
			}
			if registry.BackupDir == "" {
				return fmt.Errorf("backup directory not configured")
			}

			// Ensure backup directory exists
			if err := os.MkdirAll(registry.BackupDir, 0755); err != nil {
				return fmt.Errorf("failed to create backup directory: %w", err)
			}

			// Get current config
			cfg := registry.GetConfig()
			if cfg == nil {
				return fmt.Errorf("no configuration available")
			}

			// Create backup file
			timestamp := clock.Now().Format("2006-01-02_15-04-05")
			filename := filepath.Join(registry.BackupDir, fmt.Sprintf("config_%s.json", timestamp))

			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}

			if err := os.WriteFile(filename, data, 0644); err != nil {
				return fmt.Errorf("failed to write backup: %w", err)
			}

			// Cleanup old backups
			if err := cleanupOldBackups(registry.BackupDir, keepCount); err != nil {
				// Log but don't fail the task
				logging.Warn("failed to cleanup old backups", "error", err)
			}

			return nil
		},
	}
}

// cleanupOldBackups removes old backup files, keeping only the most recent ones.
func cleanupOldBackups(dir string, keepCount int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Filter for config backup files
	var backups []os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			backups = append(backups, entry)
		}
	}

	// If we have more than keepCount, delete the oldest
	if len(backups) > keepCount {
		// Sort by modification time (oldest first)
		type fileInfo struct {
			entry   os.DirEntry
			modTime time.Time
		}
		var files []fileInfo
		for _, entry := range backups {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			files = append(files, fileInfo{entry: entry, modTime: info.ModTime()})
		}

		// Sort oldest first
		for i := 0; i < len(files)-1; i++ {
			for j := i + 1; j < len(files); j++ {
				if files[i].modTime.After(files[j].modTime) {
					files[i], files[j] = files[j], files[i]
				}
			}
		}

		// Delete oldest files
		toDelete := len(files) - keepCount
		for i := 0; i < toDelete; i++ {
			path := filepath.Join(dir, files[i].entry.Name())
			if err := os.Remove(path); err != nil {
				logging.Warn("failed to delete old backup", "path", path, "error", err)
			}
		}
	}

	return nil
}

// ScheduledRule represents a firewall rule with a time-based schedule.
type ScheduledRule struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	PolicyName  string            `json:"policy_name"` // Which policy to add/remove from
	Rule        config.PolicyRule `json:"rule"`
	Schedule    string            `json:"schedule"` // Cron expression
	Action      string            `json:"action"`   // "enable" or "disable"
	Enabled     bool              `json:"enabled"`
}

// NewScheduledRuleTask creates a task to enable/disable a firewall rule on schedule.
func NewScheduledRuleTask(rule ScheduledRule, registry *TaskRegistry) (*Task, error) {
	schedule, err := Cron(rule.Schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule: %w", err)
	}

	return &Task{
		ID:          fmt.Sprintf("scheduled-rule-%s", rule.ID),
		Name:        rule.Name,
		Description: rule.Description,
		Schedule:    schedule,
		Enabled:     rule.Enabled,
		RunOnStart:  false,
		Timeout:     30 * time.Second,
		Func: func(ctx context.Context) error {
			if registry.GetConfig == nil || registry.ApplyConfig == nil {
				return fmt.Errorf("config functions not configured")
			}

			cfg := registry.GetConfig()
			if cfg == nil {
				return fmt.Errorf("no configuration available")
			}

			// Find the policy
			var policyIdx int = -1
			for i, p := range cfg.Policies {
				if p.Name == rule.PolicyName || p.From+"-"+p.To == rule.PolicyName {
					policyIdx = i
					break
				}
			}
			if policyIdx == -1 {
				return fmt.Errorf("policy %s not found", rule.PolicyName)
			}

			// Find or add the rule
			ruleIdx := -1
			for i, r := range cfg.Policies[policyIdx].Rules {
				if r.Name == rule.Rule.Name {
					ruleIdx = i
					break
				}
			}

			if rule.Action == "enable" {
				if ruleIdx == -1 {
					// Add the rule
					cfg.Policies[policyIdx].Rules = append(cfg.Policies[policyIdx].Rules, rule.Rule)
				}
				// Rule already exists, nothing to do
			} else if rule.Action == "disable" {
				if ruleIdx != -1 {
					// Remove the rule
					rules := cfg.Policies[policyIdx].Rules
					cfg.Policies[policyIdx].Rules = append(rules[:ruleIdx], rules[ruleIdx+1:]...)
				}
				// Rule doesn't exist, nothing to do
			}

			return registry.ApplyConfig(cfg)
		},
	}, nil
}

// NewHealthCheckTask creates a task to perform periodic health checks.
func NewHealthCheckTask(checkFunc func(context.Context) error, interval time.Duration) *Task {
	return &Task{
		ID:          "health-check",
		Name:        "Health Check",
		Description: "Periodic system health check",
		Schedule:    Every(interval),
		Enabled:     true,
		RunOnStart:  true,
		Timeout:     30 * time.Second,
		Func:        checkFunc,
	}
}

// NewMetricsCollectionTask creates a task to collect and export metrics.
func NewMetricsCollectionTask(collectFunc func(context.Context) error, interval time.Duration) *Task {
	return &Task{
		ID:          "metrics-collection",
		Name:        "Metrics Collection",
		Description: "Collect system metrics",
		Schedule:    Every(interval),
		Enabled:     true,
		RunOnStart:  false,
		Timeout:     10 * time.Second,
		Func:        collectFunc,
	}
}

// NewLogRotationTask creates a task to rotate log files.
func NewLogRotationTask(logDir string, maxSize int64, keepCount int) *Task {
	return &Task{
		ID:          "log-rotation",
		Name:        "Log Rotation",
		Description: "Rotate and cleanup old log files",
		Schedule:    Daily(3, 0), // Run at 3 AM
		Enabled:     true,
		RunOnStart:  false,
		Timeout:     5 * time.Minute,
		Func: func(ctx context.Context) error {
			// Implementation would rotate logs based on size/age
			// This is a placeholder
			return nil
		},
	}
}

// NewCertificateRenewalTask creates a task to check and renew TLS certificates.
func NewCertificateRenewalTask(certPath string, renewFunc func(context.Context) error) *Task {
	return &Task{
		ID:          "cert-renewal",
		Name:        "Certificate Renewal",
		Description: "Check and renew TLS certificates",
		Schedule:    Daily(4, 0), // Run at 4 AM
		Enabled:     true,
		RunOnStart:  true,
		Timeout:     5 * time.Minute,
		Func:        renewFunc,
	}
}
