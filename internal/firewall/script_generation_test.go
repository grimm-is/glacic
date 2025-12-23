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

	sb, err := BuildFilterTableScript(FromGlobalConfig(cfg), nil, "test_table")
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

	sb, err := BuildFilterTableScript(FromGlobalConfig(cfg), nil, "test_table")
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

	sb, err := BuildFilterTableScript(FromGlobalConfig(cfg), nil, "test_table")
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
