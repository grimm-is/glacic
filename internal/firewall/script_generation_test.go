package firewall

import (
	"strings"
	"testing"

	"grimm.is/glacic/internal/config"
)

// TestRuleExpressionGeneration tests the buildRuleExpression function for various rule configurations.
func TestRuleExpressionGeneration(t *testing.T) {
	tests := []struct {
		name    string
		rule    config.PolicyRule
		want    string
		wantErr bool
	}{
		{
			name: "Basic TCP Accept",
			rule: config.PolicyRule{
				Protocol: "tcp",
				DestPort: 80,
				Action:   "accept",
			},
			want: "meta l4proto tcp tcp dport 80 counter accept",
		},
		{
			name: "Source IP",
			rule: config.PolicyRule{
				SrcIP:  "192.168.1.10",
				Action: "drop",
			},
			want: "ip saddr 192.168.1.10 limit rate 10/minute log group 0 prefix \"DROP_RULE: \" counter drop",
		},
		{
			name: "Dest IPSet",
			rule: config.PolicyRule{
				DestIPSet: "bad_hosts",
				Action:    "reject",
			},
			want: "ip daddr @bad_hosts limit rate 10/minute log group 0 prefix \"DROP_RULE: \" counter reject",
		},
		{
			name: "Connection State",
			rule: config.PolicyRule{
				ConnState: "established,related",
				Action:    "accept",
			},
			want: "ct state established,related counter accept",
		},
		{
			name: "Complex Rule",
			rule: config.PolicyRule{
				Protocol:  "udp",
				SrcIP:     "10.0.0.0/24",
				DestPort:  53,
				ConnState: "new",
				Action:    "accept",
			},
			want: "meta l4proto udp ip saddr 10.0.0.0/24 ct state new udp dport 53 counter accept",
		},
		// Sad Paths
		{
			name: "Invalid IPSet Name",
			rule: config.PolicyRule{
				SrcIPSet: "bad;set",
			},
			wantErr: true,
		},
		{
			name: "Invalid ConnState",
			rule: config.PolicyRule{
				ConnState: "hacking",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildRuleExpression(tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildRuleExpression() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.want {
				t.Errorf("buildRuleExpression() = %q, want %q", result, tt.want)
			}
		})
	}
}

// TestFilterTableGeneration tests high-level script generation.
func TestFilterTableGeneration(t *testing.T) {
	cfg := &config.Config{
		Zones: []config.Zone{
			{Name: "LAN", Interfaces: []string{"eth1"}},
			{Name: "WAN", Interfaces: []string{"eth0"}},
		},
		Policies: []config.Policy{
			{
				From: "LAN", To: "WAN", Action: "accept",
				Rules: []config.PolicyRule{
					{Protocol: "tcp", DestPort: 443, Action: "accept"},
				},
			},
			{
				From: "LAN", To: "Firewall", Action: "accept",
				Rules: []config.PolicyRule{
					{Protocol: "tcp", DestPort: 22, Action: "accept"},
				},
			},
		},
	}

	sb, err := BuildFilterTableScript(FromGlobalConfig(cfg), nil, "test_table", "")
	if err != nil {
		t.Fatalf("BuildFilterTableScript() error = %v", err)
	}
	script := sb.Build()

	// Verify Chain Creation
	if !strings.Contains(script, "add chain inet test_table policy_LAN_WAN") {
		t.Error("Missing policy chain creation")
	}

	// Verify Verdict Map Usage (Replacing linear jumps)
	if !strings.Contains(script, "add map inet test_table input_vmap") {
		t.Error("Missing input_vmap creation")
	}
	if !strings.Contains(script, "iifname vmap @input_vmap") {
		t.Error("Missing input vmap rule")
	}
	// The specific jump is now inside the map element, harder to grep linearly without parsing map def
	// But we can check for map element existence: "eth1" : "jump policy_LAN_WAN"
	// Wait, LAN->WAN is forward chain. Input chain is LAN->Firewall.
	// In this config: LAN->WAN (Forward).
	// So input map might be empty if no Input policies?
	// Config: From: LAN, To: WAN. That's a forward policy.
	// So input_vmap might not be created if empty?
	// Let's check logic: if len(inputMap) > 0.
	// Here verify forward vmap.

	if !strings.Contains(script, "add map inet test_table forward_vmap") {
		t.Error("Missing forward_vmap creation")
	}
	if !strings.Contains(script, `meta iifname . meta oifname vmap @forward_vmap`) {
		t.Error("Missing forward vmap rule")
	}

	// Verify Rule Content
	if !strings.Contains(script, "meta l4proto tcp tcp dport 443 counter accept") {
		t.Error("Missing policy rule content")
	}
}

func TestFlowOffloadGeneration(t *testing.T) {
	cfg := &config.Config{
		EnableFlowOffload: true,
		Interfaces: []config.Interface{
			{Name: "eth0"},
			{Name: "eth1"},
		},
	}

	sb, err := BuildFilterTableScript(FromGlobalConfig(cfg), nil, "test_table", "")
	if err != nil {
		t.Fatalf("BuildFilterTableScript() error = %v", err)
	}
	script := sb.Build()

	// Verify Flowtable Creation
	if !strings.Contains(script, "add flowtable inet test_table ft") {
		t.Error("Missing flowtable definition")
	}

	// Verify Flowtable Rule
	if !strings.Contains(script, "ip protocol { tcp, udp } flow add @ft") {
		t.Error("Missing flow offload rule")
	}
}

func TestConcatenatedSetsGeneration(t *testing.T) {
	cfg := &config.Config{
		Interfaces: []config.Interface{
			{
				Name:       "eth0",
				Management: &config.ZoneManagement{SSH: true, Web: true},
			},
			{
				Name:       "eth1",
				Management: &config.ZoneManagement{SSH: true},
			},
		},
	}

	sb, err := BuildFilterTableScript(FromGlobalConfig(cfg), nil, "test_table", "")
	if err != nil {
		t.Fatalf("BuildFilterTableScript() error = %v", err)
	}
	script := sb.Build()

	// Verify Rule Existence
	// We expect: iifname . tcp dport { ... } accept
	if !strings.Contains(script, "iifname . tcp dport {") {
		t.Error("Missing concatenated set rule for TCP")
	}

	// Verify Elements
	// ssh on eth0, eth1 -> "eth0" . 22, "eth1" . 22
	// web on eth0 -> "eth0" . 80, "eth0" . 443
	expectedElements := []string{
		`"eth0" . 22`,
		`"eth1" . 22`,
		`"eth0" . 80`,
		`"eth0" . 443`,
	}

	for _, elem := range expectedElements {
		if !strings.Contains(script, elem) {
			t.Errorf("Missing element %s in script\nScript:\n%s", elem, script)
		}
	}
}

// TestScriptBuilderComments verifies that comments are correctly added to nftables commands.
func TestScriptBuilderComments(t *testing.T) {
	t.Run("ChainWithComment", func(t *testing.T) {
		sb := NewScriptBuilder("test", "inet")
		sb.AddChain("input", "filter", "input", 0, "drop", "[base] Incoming traffic")
		script := sb.Build()

		if !strings.Contains(script, `comment "[base] Incoming traffic"`) {
			t.Errorf("Chain comment not found in output:\n%s", script)
		}
	})

	t.Run("ChainWithoutComment", func(t *testing.T) {
		sb := NewScriptBuilder("test", "inet")
		sb.AddChain("input", "filter", "input", 0, "drop")
		script := sb.Build()

		if strings.Contains(script, "comment") {
			t.Errorf("Unexpected comment in output:\n%s", script)
		}
	})

	t.Run("RuleWithComment", func(t *testing.T) {
		sb := NewScriptBuilder("test", "inet")
		sb.AddTable()
		sb.AddRule("input", "ct state established accept", "[base] Stateful")
		script := sb.Build()

		if !strings.Contains(script, `comment "[base] Stateful"`) {
			t.Errorf("Rule comment not found in output:\n%s", script)
		}
	})

	t.Run("RuleWithoutComment", func(t *testing.T) {
		sb := NewScriptBuilder("test", "inet")
		sb.AddTable()
		sb.AddRule("input", "ct state established accept")
		script := sb.Build()

		// Count occurrences of "comment" - should be 0
		if strings.Contains(script, "comment") {
			t.Errorf("Unexpected comment in output:\n%s", script)
		}
	})

	t.Run("RuleSkipsDuplicateComment", func(t *testing.T) {
		sb := NewScriptBuilder("test", "inet")
		sb.AddTable()
		// Rule expression already has a comment
		sb.AddRule("input", `tcp dport 22 accept comment "user-defined"`, "[source] should be skipped")
		script := sb.Build()

		// Should only have the original comment, not the added one
		if strings.Contains(script, "[source] should be skipped") {
			t.Errorf("Duplicate comment was incorrectly added:\n%s", script)
		}
		if !strings.Contains(script, `comment "user-defined"`) {
			t.Errorf("Original comment missing:\n%s", script)
		}
	})

	t.Run("SetWithComment", func(t *testing.T) {
		sb := NewScriptBuilder("test", "inet")
		sb.AddTable()
		sb.AddSet("blocklist", "ipv4_addr", "[ipset:blocklist]", 0, "interval")
		script := sb.Build()

		if !strings.Contains(script, `comment "[ipset:blocklist]"`) {
			t.Errorf("Set comment not found in output:\n%s", script)
		}
	})

	t.Run("MapWithComment", func(t *testing.T) {
		sb := NewScriptBuilder("test", "inet")
		sb.AddTable()
		sb.AddMap("input_vmap", "ifname", "verdict", "[base] Input dispatch", nil, nil)
		script := sb.Build()

		if !strings.Contains(script, `comment "[base] Input dispatch"`) {
			t.Errorf("Map comment not found in output:\n%s", script)
		}
	})

	t.Run("FlowtableWithComment", func(t *testing.T) {
		sb := NewScriptBuilder("test", "inet")
		sb.AddTable()
		sb.AddFlowtable("ft", []string{"eth0"}, "[feature] Flow offload")
		script := sb.Build()

		if !strings.Contains(script, `comment "[feature] Flow offload"`) {
			t.Errorf("Flowtable comment not found in output:\n%s", script)
		}
	})
}

// TestBuildFilterTableScriptComments verifies that BuildFilterTableScript generates expected comments.
func TestBuildFilterTableScriptComments(t *testing.T) {
	cfg := &Config{
		Zones: []config.Zone{
			{Name: "LAN"},
			{Name: "WAN"},
		},
		Interfaces: []config.Interface{
			{Name: "eth0", Zone: "LAN"},
			{Name: "eth1", Zone: "WAN"},
		},
		Policies: []config.Policy{
			{From: "LAN", To: "WAN", Action: "accept"},
		},
	}

	sb, err := BuildFilterTableScript(cfg, nil, "glacic", "abc123")
	if err != nil {
		t.Fatalf("BuildFilterTableScript error: %v", err)
	}
	script := sb.Build()

	// Verify base chain comments
	expectedComments := []string{
		`[base] Incoming traffic`,
		`[base] Routed traffic`,
		`[base] Outgoing traffic`,
		`[base] Loopback`,
		`[base] Stateful`,
	}

	for _, comment := range expectedComments {
		if !strings.Contains(script, comment) {
			t.Errorf("Expected comment %q not found in script", comment)
		}
	}

	// Verify policy chain comment
	if !strings.Contains(script, `[policy:LAN→WAN]`) {
		t.Errorf("Policy chain comment not found in script")
	}
}

func TestDNSSetFilterGeneration(t *testing.T) {
	cfg := &config.Config{
		IPSets: []config.IPSet{
			{Name: "google_dns", Type: "dns", Domains: []string{"google.com"}, Size: 100},
			{Name: "yahoo_dns", Type: "dns", Domains: []string{"yahoo.com"}}, // Default size
		},
	}

	sb, err := BuildFilterTableScript(FromGlobalConfig(cfg), nil, "test_table", "")
	if err != nil {
		t.Fatalf("BuildFilterTableScript() error = %v", err)
	}
	script := sb.Build()

	// Verify standard DNS set (ipv4_addr, size explicitly set)
	if !strings.Contains(script, `add set inet test_table google_dns { type ipv4_addr; size 100; comment "[ipset:google_dns]"; }`) {
		t.Errorf("Missing or incorrect google_dns set definition:\n%s", script)
	}

	// Verify default size optimization for DNS set
	if !strings.Contains(script, `add set inet test_table yahoo_dns { type ipv4_addr; size 65535; comment "[ipset:yahoo_dns]"; }`) {
		t.Errorf("Missing or incorrect yahoo_dns set definition (optimization check):\n%s", script)
	}
}
