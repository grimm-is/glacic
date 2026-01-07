//go:build linux

package firewall

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/testutil"
)

func TestManager_SafeApply_Success(t *testing.T) {
	// Skip in precompiled test mode - this test requires real network stack
	testutil.RequireVM(t)
	// 1. Setup Manager
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	// 2. Setup SafeApplyManager
	tmpFiles := []string{"/tmp/firewall_test.hcl", "/tmp/firewall_test_rollback.hcl"}
	for _, f := range tmpFiles {
		if err := exec.Command("touch", f).Run(); err != nil {
			t.Fatalf("Failed to create tmp file %s: %v", f, err)
		}
	}

	safeMgr := NewSafeApplyManager(mgr, "/tmp/firewall_test.hcl")

	// ... (rest of SafeApply_Success setup)

	// ... (Monitor changes)
	// 4. Tamper with Ruleset (Add a rule to INPUT)

	// 3. Create Valid Config
	cfg := &config.Config{
		Zones: []config.Zone{
			{Name: "wan", Action: "drop"},
			{Name: "lan", Action: "accept"},
			{Name: "mgmt", Action: "accept", Interfaces: []string{"lo"}},
		},
		Policies: []config.Policy{
			{Name: "lan-to-wan", From: "lan", To: "wan", Action: "accept"},
			{Name: "mgmt-access", From: "mgmt", To: "mgmt", Action: "accept"},
		},
	}

	// 4. Run SafeApply (Success)
	// Use localhost as ping target to actually test the verification logic
	// instead of skipping it with empty PingTargets
	safeConfig := &SafeApplyConfig{
		PingTargets:         []string{"127.0.0.1"}, // Actually verify connectivity
		PingTimeout:         2 * time.Second,
		RollbackDelay:       1 * time.Second,
		RequireConfirmation: false,
	}

	result, err := safeMgr.SafeApply(cfg, "", safeConfig)
	if err != nil {
		t.Fatalf("SafeApply failed: %v", err)
	}
	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}
}

func TestManager_SafeApply_Rollback(t *testing.T) {
	// 1. Setup Manager
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	// 2. Setup SafeApplyManager
	// File created in Success test setup or we create here if needed
	if err := exec.Command("touch", "/tmp/firewall_test_rollback.hcl").Run(); err != nil {
		t.Fatalf("Failed to create tmp file: %v", err)
	}
	safeMgr := NewSafeApplyManager(mgr, "/tmp/firewall_test_rollback.hcl")

	// 3. Create Valid Config
	cfg := &config.Config{
		Zones: []config.Zone{
			{Name: "wan", Action: "drop"},
		},
	}

	// 4. Run SafeApply with Unreachable Target
	safeConfig := &SafeApplyConfig{
		PingTargets:         []string{"192.0.2.254"}, // Reserved test network, unlikely to exist
		PingTimeout:         1 * time.Second,
		RollbackDelay:       1 * time.Second,
		RequireConfirmation: false,
	}

	result, err := safeMgr.SafeApply(cfg, "", safeConfig)
	if err != nil {
		// It might return error or result with Success=false
		// Logic: if verify fails, it returns result with Success=false, err=nil
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Verify failure
	if result.Success {
		t.Error("Expected failure for unreachable target, got success")
	}

	if !strings.Contains(result.Message, "rolled back") {
		t.Errorf("Expected rollback message, got: %s", result.Message)
	}
}

func TestManager_AtomicApply_Failure(t *testing.T) {
	// 1. Setup Manager
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	// 2. Apply initial valid config
	initialCfg := &config.Config{
		Zones: []config.Zone{
			{Name: "mgmt", Action: "accept", Interfaces: []string{"lo"}},
		},
		Policies: []config.Policy{
			{Name: "mgmt-access", From: "mgmt", To: "mgmt", Action: "accept"},
		},
	}
	// Convert using FromGlobalConfig
	if err := mgr.ApplyConfig(FromGlobalConfig(initialCfg)); err != nil {
		t.Fatalf("Failed to apply initial config: %v", err)
	}

	// Snapshot ruleset
	initialRuleset, err := exec.Command("nft", "list", "ruleset").CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to list ruleset: %v", err)
	}

	// 3. Attempt to apply invalid config (referencing non-existent interface in a way that might pass basic checks but fail nft validation)
	// Actually, ValidateScript (nft -c) catches most syntax/reference errors.
	// If ValidateScript fails, ApplyConfig returns error immediately.
	// This proves that we don't apply incomplete configs.
	invalidCfg := &config.Config{
		Zones: []config.Zone{
			{Name: "broken", Action: "accept", Interfaces: []string{"nonexistent0"}}, // nft might accept this if it assumes interface might appear later?
			// Actually interface names are strings.
			// Let's try to inject invalid syntax via a field that is passed raw?
			// Protocol "invalidprotocol"
		},
		Policies: []config.Policy{
			{
				Name: "broken-rule",
				From: "broken",
				To:   "broken",
				Rules: []config.PolicyRule{
					{Name: "bad-proto", Protocol: "invalid_proto_xyz", Action: "accept"},
				},
			},
		},
	}

	// 4. Apply should fail
	if err := mgr.ApplyConfig(FromGlobalConfig(invalidCfg)); err == nil {
		t.Fatal("Expected ApplyConfig to fail with invalid config, but it succeeded")
	} else {
		t.Logf("ApplyConfig failed as expected: %v", err)
	}

	// 5. Verify ruleset is UNCHANGED
	finalRuleset, err := exec.Command("nft", "list", "ruleset").CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to list final ruleset: %v", err)
	}

	if string(finalRuleset) != string(initialRuleset) {
		t.Errorf("Ruleset changed after failed apply!\nDiff:\n%s",
			diffStrings(string(initialRuleset), string(finalRuleset)))
	}
}

func diffStrings(a, b string) string {
	if a == b {
		return ""
	}
	return "Ruleset content differs (full diff omitted for brevity)"
}

func TestManager_MonitorIntegrity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping monitor test in short mode")
	}
	// Skip in precompiled test mode - this test requires real nftables
	testutil.RequireVM(t)

	// 1. Setup Manager
	mockConn := NewMockNFTablesConn()
	// ApplyConfig calls monitor init which calls getRulesetGenID which calls ListTables
	mockConn.On("ListTables").Return([]string{"glacic"}, nil).Maybe()
	mgr := NewManagerWithConn(mockConn, nil, "")

	// 2. Apply Config with Monitoring
	// Need to check how MonitorIntegrity reads setting. It reads from config.Features.IntegrityMonitoring
	cfg := &config.Config{
		Features: &config.Features{
			IntegrityMonitoring: true,
		},
		Zones: []config.Zone{
			{Name: "wan", Action: "drop"},
		},
	}

	if err := mgr.ApplyConfig(FromGlobalConfig(cfg)); err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	// 3. Start Monitor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go mgr.MonitorIntegrity(ctx, cfg)

	// Wait for monitor to sync
	time.Sleep(1 * time.Second)

	// 4. Tamper with Ruleset (Add a rule to INPUT)
	cmd := exec.Command("nft", "add", "rule", "inet", "glacic", "input", "tcp", "dport", "666", "accept")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to tamper ruleset: %v: %s", err, string(out))
	}

	// 5. Wait for detection (Ticker is 2s)
	time.Sleep(3 * time.Second)

	// 6. Verify Reversion
	// Check if rule exists (it shouldn't)
	// We can check by listing rules
	out, err := exec.Command("nft", "list", "ruleset").CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to list ruleset: %v", err)
	}
	if strings.Contains(string(out), "dport 666") {
		t.Error("Monitor failed to revert: tamper rule still exists")
	}
}

// TestManager_AddDynamicNATRule tests adding dynamic NAT rules
func TestManager_AddDynamicNATRule(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	// First apply a base config to initialize baseConfig
	baseCfg := &config.Config{
		Zones: []config.Zone{
			{Name: "wan", Interfaces: []string{"eth0"}},
			{Name: "lan", Interfaces: []string{"eth1"}},
		},
		NAT: []config.NATRule{
			{Name: "masq", Type: "masquerade", OutInterface: "eth0"},
		},
	}

	if err := mgr.ApplyConfig(FromGlobalConfig(baseCfg)); err != nil {
		t.Fatalf("Initial ApplyConfig failed: %v", err)
	}

	// Add a dynamic rule
	dynamicRule := config.NATRule{
		Name:        "upnp-8080",
		Type:        "dnat",
		Protocol:    "tcp",
		InInterface: "eth0",
		DestPort:    "8080",
		ToIP:        "192.168.1.100",
		ToPort:      "80",
		Description: "UPnP Test",
	}

	if err := mgr.AddDynamicNATRule(dynamicRule); err != nil {
		t.Fatalf("AddDynamicNATRule failed: %v", err)
	}

	// Verify rule was added to dynamicRules
	mgr.mu.RLock()
	if len(mgr.dynamicRules) != 1 {
		t.Errorf("Expected 1 dynamic rule, got %d", len(mgr.dynamicRules))
	}
	if mgr.dynamicRules[0].DestPort != "8080" {
		t.Errorf("Dynamic rule port mismatch")
	}
	mgr.mu.RUnlock()
}

// TestManager_AddDynamicNATRule_NotInitialized tests error when baseConfig is nil
func TestManager_AddDynamicNATRule_NotInitialized(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	// Don't apply any config - baseConfig will be nil
	dynamicRule := config.NATRule{
		Name: "test",
		Type: "dnat",
	}

	err := mgr.AddDynamicNATRule(dynamicRule)
	if err == nil {
		t.Error("Expected error when firewall not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("Expected 'not initialized' error, got: %v", err)
	}
}

// TestManager_RemoveDynamicNATRule tests removing dynamic NAT rules
func TestManager_RemoveDynamicNATRule(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	// Setup base config
	baseCfg := &config.Config{
		Zones: []config.Zone{
			{Name: "wan", Interfaces: []string{"eth0"}},
		},
	}

	if err := mgr.ApplyConfig(FromGlobalConfig(baseCfg)); err != nil {
		t.Fatalf("Initial ApplyConfig failed: %v", err)
	}

	// Add two dynamic rules
	rule1 := config.NATRule{Name: "rule1", Type: "dnat", DestPort: "8080"}
	rule2 := config.NATRule{Name: "rule2", Type: "dnat", DestPort: "9090"}

	mgr.AddDynamicNATRule(rule1)
	mgr.AddDynamicNATRule(rule2)

	// Remove rule1
	err := mgr.RemoveDynamicNATRule(func(r config.NATRule) bool {
		return r.DestPort == "8080"
	})
	if err != nil {
		t.Fatalf("RemoveDynamicNATRule failed: %v", err)
	}

	// Verify only rule2 remains
	mgr.mu.RLock()
	if len(mgr.dynamicRules) != 1 {
		t.Errorf("Expected 1 dynamic rule, got %d", len(mgr.dynamicRules))
	}
	if mgr.dynamicRules[0].DestPort != "9090" {
		t.Errorf("Wrong rule remained")
	}
	mgr.mu.RUnlock()
}

// TestManager_RemoveDynamicNATRule_NoMatch tests removing non-existent rule
func TestManager_RemoveDynamicNATRule_NoMatch(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	// Setup base config
	baseCfg := &config.Config{
		Zones: []config.Zone{{Name: "wan"}},
	}
	mgr.ApplyConfig(FromGlobalConfig(baseCfg))

	// Add a rule
	mgr.AddDynamicNATRule(config.NATRule{Name: "test", DestPort: "8080"})

	// Try to remove non-existent rule
	err := mgr.RemoveDynamicNATRule(func(r config.NATRule) bool {
		return r.DestPort == "9999" // Doesn't exist
	})

	// Should succeed silently (no-op)
	if err != nil {
		t.Errorf("RemoveDynamicNATRule should succeed for no-match: %v", err)
	}

	// Original rule should still exist
	mgr.mu.RLock()
	if len(mgr.dynamicRules) != 1 {
		t.Errorf("Rule count changed unexpectedly")
	}
	mgr.mu.RUnlock()
}
