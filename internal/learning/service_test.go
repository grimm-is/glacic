package learning

import (
	"testing"
	"time"

	"grimm.is/glacic/internal/config"
)

func TestService_Lifecycle(t *testing.T) {
	cfg := &config.RuleLearningConfig{
		Enabled:       true,
		RetentionDays: 1,
	}

	// Use in-memory DB
	svc, err := NewService(cfg, ":memory:")
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if err := svc.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer svc.Stop()

	if !svc.IsRunning() {
		t.Error("Service should be running")
	}

	// Double start
	if err := svc.Start(); err == nil {
		t.Error("Double start should fail")
	}

	// Update config
	newCfg := &config.RuleLearningConfig{
		Enabled:       true,
		RetentionDays: 2,
	}
	if err := svc.UpdateConfig(newCfg); err != nil {
		t.Errorf("UpdateConfig failed: %v", err)
	}
	if svc.GetConfig().RetentionDays != 2 {
		t.Error("Config not updated")
	}

	svc.Stop()
	if svc.IsRunning() {
		t.Error("Service should be stopped")
	}
}

func TestService_RuleManagement(t *testing.T) {
	cfg := &config.RuleLearningConfig{Enabled: true, LearningMode: false}
	svc, err := NewService(cfg, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	// Ingest a packet to create a pending rule
	pkt := PacketInfo{
		SrcMAC:   "00:11:22:33:44:55",
		SrcIP:    "192.168.1.10",
		DstIP:    "1.1.1.1",
		DstPort:  80,
		Protocol: "tcp",
		Policy:   "drop_all",
	}
	svc.IngestPacket(pkt)

	// Wait for processing (engine runs in goroutine/channel)
	time.Sleep(100 * time.Millisecond)

	// Test GetPendingRules
	// Engine default state is Pending if learning mode is false
	rules, err := svc.GetPendingRules("pending")
	if err != nil {
		t.Fatalf("GetPendingRules failed: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("Expected at least 1 rule, got 0")
	}
	ruleID := rules[0].ID

	// Test ApproveRule
	approved, err := svc.ApproveRule(ruleID, "user")
	if err != nil {
		t.Fatalf("ApproveRule failed: %v", err)
	}
	if approved.Status != "approved" {
		t.Error("Rule not approved")
	}

	// Verify state change
	rules, _ = svc.GetPendingRules("approved")
	found := false
	for _, r := range rules {
		if r.ID == ruleID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Rule not found in approved list")
	}

	// Ingest another for Deny
	pkt2 := pkt
	pkt2.SrcMAC = "AA:BB:CC:DD:EE:FF"
	svc.IngestPacket(pkt2)
	time.Sleep(100 * time.Millisecond)

	rules, _ = svc.GetPendingRules("pending")
	if len(rules) == 0 {
		t.Fatal("Expected new rule for deny test")
	}
	ruleID2 := rules[0].ID

	denied, err := svc.DenyRule(ruleID2, "user")
	if err != nil {
		t.Fatal(err)
	}
	if denied.Status != "denied" {
		t.Error("Rule not denied")
	}

	// Test DeleteRule
	if err := svc.DeleteRule(ruleID2); err != nil {
		t.Fatal(err)
	}
	// Verify deleted
	_, err = svc.GetPendingRule(ruleID2)
	if err == nil {
		t.Error("Rule should be deleted (not found error expected)")
	}
}

func TestService_Stats(t *testing.T) {
	cfg := &config.RuleLearningConfig{Enabled: true}
	svc, err := NewService(cfg, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc.Start()
	defer svc.Stop()

	stats, err := svc.GetStats()
	if err != nil {
		t.Fatal(err)
	}

	if stats["service_running"] != true {
		t.Error("Stats missing service_running=true")
	}
}
