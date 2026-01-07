package scheduler

import (
	"context"
	"encoding/json"
	"grimm.is/glacic/internal/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewConfigBackupTask(t *testing.T) {
	// Create temp dir for backups
	tmpDir, err := os.MkdirTemp("", "scheduler-backup-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Mock config
	mockCfg := &config.Config{
		SchemaVersion: "1.0",
		IPForwarding:  true,
	}

	registry := &TaskRegistry{
		BackupDir: tmpDir,
		GetConfig: func() *config.Config {
			return mockCfg
		},
	}

	task := NewConfigBackupTask(registry, Every(time.Hour), 2)

	// Execute task manually
	if err := task.Func(context.Background()); err != nil {
		t.Fatalf("Task execution failed: %v", err)
	}

	// Verify backup file created
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("Expected 1 backup file, got %d", len(entries))
	}

	// Read content
	content, err := os.ReadFile(filepath.Join(tmpDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	var savedCfg config.Config
	if err := json.Unmarshal(content, &savedCfg); err != nil {
		t.Fatal(err)
	}
	if savedCfg.SchemaVersion != "1.0" {
		t.Error("Config content mismatch")
	}

	// Test cleanup logic
	// Create 5 dummy files
	for i := 0; i < 5; i++ {
		fname := filepath.Join(tmpDir, "dummy_"+time.Now().Add(time.Duration(i)*time.Second).Format("20060102150405")+".json")
		os.WriteFile(fname, []byte("{}"), 0644)
		time.Sleep(10 * time.Millisecond)
	}

	// Run task again
	if err := task.Func(context.Background()); err != nil {
		t.Fatalf("Task execution failure 2: %v", err)
	}

	entries, _ = os.ReadDir(tmpDir)
	var jsonFiles []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}

	if len(jsonFiles) != 2 {
		t.Errorf("Expected 2 backup files after cleanup, got %d", len(jsonFiles))
	}
}

func TestNewScheduledRuleTask(t *testing.T) {
	rule := ScheduledRule{
		ID:         "rule-1",
		Name:       "Test Rule",
		PolicyName: "wan-lan",
		Rule:       config.PolicyRule{Name: "Allow SSH"},
		Schedule:   "* * * * *",
		Action:     "enable",
		Enabled:    true,
	}

	mockCfg := &config.Config{
		Policies: []config.Policy{
			{Name: "wan-lan", Rules: []config.PolicyRule{}},
		},
	}

	var appliedCfg *config.Config
	registry := &TaskRegistry{
		GetConfig: func() *config.Config { return mockCfg },
		ApplyConfig: func(c *config.Config) error {
			appliedCfg = c
			return nil
		},
	}

	task, err := NewScheduledRuleTask(rule, registry)
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Execute enable
	if err := task.Func(context.Background()); err != nil {
		t.Fatalf("Task execution failed: %v", err)
	}

	if appliedCfg == nil {
		t.Fatal("ApplyConfig not called")
	}
	if len(appliedCfg.Policies[0].Rules) != 1 {
		t.Fatal("Rule not added")
	}

	// Test disable logic
	rule.Action = "disable"
	taskDisable, _ := NewScheduledRuleTask(rule, registry)

	// Execute disable
	if err := taskDisable.Func(context.Background()); err != nil {
		t.Fatalf("Task execution disable failed: %v", err)
	}

	if len(appliedCfg.Policies[0].Rules) != 0 {
		t.Fatal("Rule not removed")
	}
}

func TestTaskConstructors(t *testing.T) {
	reg := &TaskRegistry{
		RefreshIPSets: func() error { return nil },
		RefreshDNS:    func() error { return nil },
	}

	// Just verify they return non-nil and have correct metadata
	t1 := NewIPSetUpdateTask(reg, time.Hour)
	if t1.ID != "ipset-update" {
		t.Error("Wrong ID for IPSet task")
	}
	if err := t1.Func(context.Background()); err != nil {
		t.Error(err)
	}

	t2 := NewDNSBlocklistUpdateTask(reg, time.Hour)
	if t2.ID != "dns-blocklist-update" {
		t.Error("Wrong ID for DNS task")
	}
	if err := t2.Func(context.Background()); err != nil {
		t.Error(err)
	}

	t3 := NewHealthCheckTask(func(ctx context.Context) error { return nil }, time.Hour)
	if t3.ID != "health-check" {
		t.Error("Wrong ID for Health task")
	}
	if err := t3.Func(context.Background()); err != nil {
		t.Error(err)
	}

	t4 := NewMetricsCollectionTask(func(ctx context.Context) error { return nil }, time.Minute)
	if t4.ID != "metrics-collection" {
		t.Error("Wrong ID for Metrics task")
	}
	if err := t4.Func(context.Background()); err != nil {
		t.Error(err)
	}

	t5 := NewLogRotationTask("/tmp", 100, 5)
	if t5.ID != "log-rotation" {
		t.Error("Wrong ID for Log task")
	}
	if err := t5.Func(context.Background()); err != nil {
		t.Error(err)
	}

	t6 := NewCertificateRenewalTask("/path", func(ctx context.Context) error { return nil })
	if t6.ID != "cert-renewal" {
		t.Error("Wrong ID for Cert task")
	}
	if err := t6.Func(context.Background()); err != nil {
		t.Error(err)
	}
}
