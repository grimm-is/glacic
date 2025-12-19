package config

import (
	"testing"

	"github.com/hashicorp/hcl/v2/hclsimple"
)

func TestParseIPSetBasic(t *testing.T) {
	hclContent := `
ipset "blocklist" {
  description = "Blocked IPs"
  type        = "ipv4_addr"
  entries     = ["192.168.1.100", "10.0.0.0/8", "172.16.0.0/12"]
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.IPSets) != 1 {
		t.Fatalf("Expected 1 IPSet, got %d", len(cfg.IPSets))
	}

	ipset := cfg.IPSets[0]
	if ipset.Name != "blocklist" {
		t.Errorf("Expected name 'blocklist', got %q", ipset.Name)
	}
	if ipset.Description != "Blocked IPs" {
		t.Errorf("Expected description 'Blocked IPs', got %q", ipset.Description)
	}
	if ipset.Type != "ipv4_addr" {
		t.Errorf("Expected type 'ipv4_addr', got %q", ipset.Type)
	}
	if len(ipset.Entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(ipset.Entries))
	}
}

func TestParseIPSetFireHOL(t *testing.T) {
	hclContent := `
ipset "firehol_level1" {
  description   = "FireHOL Level 1 blocklist"
  firehol_list  = "firehol_level1"
  auto_update   = true
  refresh_hours = 24
  action        = "drop"
  apply_to      = "input"
  match_on_source = true
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.IPSets) != 1 {
		t.Fatalf("Expected 1 IPSet, got %d", len(cfg.IPSets))
	}

	ipset := cfg.IPSets[0]
	if ipset.FireHOLList != "firehol_level1" {
		t.Errorf("Expected firehol_list 'firehol_level1', got %q", ipset.FireHOLList)
	}
	if !ipset.AutoUpdate {
		t.Error("Expected auto_update to be true")
	}
	if ipset.RefreshHours != 24 {
		t.Errorf("Expected refresh_hours 24, got %d", ipset.RefreshHours)
	}
	if ipset.Action != "drop" {
		t.Errorf("Expected action 'drop', got %q", ipset.Action)
	}
	if ipset.ApplyTo != "input" {
		t.Errorf("Expected apply_to 'input', got %q", ipset.ApplyTo)
	}
	if !ipset.MatchOnSource {
		t.Error("Expected match_on_source to be true")
	}
}

func TestParseIPSetCustomURL(t *testing.T) {
	hclContent := `
ipset "custom_blocklist" {
  description   = "Custom blocklist from URL"
  url           = "https://example.com/blocklist.txt"
  auto_update   = true
  refresh_hours = 12
  action        = "reject"
  apply_to      = "both"
  match_on_source = true
  match_on_dest   = true
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	ipset := cfg.IPSets[0]
	if ipset.URL != "https://example.com/blocklist.txt" {
		t.Errorf("Expected URL, got %q", ipset.URL)
	}
	if ipset.RefreshHours != 12 {
		t.Errorf("Expected refresh_hours 12, got %d", ipset.RefreshHours)
	}
	if ipset.Action != "reject" {
		t.Errorf("Expected action 'reject', got %q", ipset.Action)
	}
	if ipset.ApplyTo != "both" {
		t.Errorf("Expected apply_to 'both', got %q", ipset.ApplyTo)
	}
	if !ipset.MatchOnDest {
		t.Error("Expected match_on_dest to be true")
	}
}

func TestParseIPSetIPv6(t *testing.T) {
	hclContent := `
ipset "ipv6_blocklist" {
  type    = "ipv6_addr"
  entries = ["2001:db8::/32", "fe80::/10"]
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	ipset := cfg.IPSets[0]
	if ipset.Type != "ipv6_addr" {
		t.Errorf("Expected type 'ipv6_addr', got %q", ipset.Type)
	}
	if len(ipset.Entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(ipset.Entries))
	}
}

func TestParseIPSetServicePorts(t *testing.T) {
	hclContent := `
ipset "blocked_ports" {
  type    = "inet_service"
  entries = ["22", "23", "3389"]
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	ipset := cfg.IPSets[0]
	if ipset.Type != "inet_service" {
		t.Errorf("Expected type 'inet_service', got %q", ipset.Type)
	}
	if len(ipset.Entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(ipset.Entries))
	}
}

func TestParseMultipleIPSets(t *testing.T) {
	hclContent := `
ipset "blocklist" {
  firehol_list = "firehol_level1"
  action       = "drop"
  apply_to     = "input"
}

ipset "allowlist" {
  entries = ["10.0.0.0/8"]
}

ipset "spamhaus" {
  firehol_list  = "spamhaus_drop"
  auto_update   = true
  refresh_hours = 6
  action        = "drop"
  apply_to      = "both"
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.IPSets) != 3 {
		t.Fatalf("Expected 3 IPSets, got %d", len(cfg.IPSets))
	}

	// Verify names
	names := make(map[string]bool)
	for _, ipset := range cfg.IPSets {
		names[ipset.Name] = true
	}
	for _, expected := range []string{"blocklist", "allowlist", "spamhaus"} {
		if !names[expected] {
			t.Errorf("Missing IPSet %q", expected)
		}
	}
}

func TestParseIPSetInPolicyRule(t *testing.T) {
	hclContent := `
ipset "blocklist" {
  firehol_list = "firehol_level1"
}

policy "wan" "lan" {
  description = "Block known bad IPs"

  rule "block_inbound" {
    src_ipset = "blocklist"
    action    = "drop"
  }

  rule "block_outbound" {
    dest_ipset = "blocklist"
    action     = "drop"
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.IPSets) != 1 {
		t.Fatalf("Expected 1 IPSet, got %d", len(cfg.IPSets))
	}
	if len(cfg.Policies) != 1 {
		t.Fatalf("Expected 1 Policy, got %d", len(cfg.Policies))
	}

	policy := cfg.Policies[0]
	if len(policy.Rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(policy.Rules))
	}

	if policy.Rules[0].SrcIPSet != "blocklist" {
		t.Errorf("Expected src_ipset 'blocklist', got %q", policy.Rules[0].SrcIPSet)
	}
	if policy.Rules[1].DestIPSet != "blocklist" {
		t.Errorf("Expected dest_ipset 'blocklist', got %q", policy.Rules[1].DestIPSet)
	}
}

func TestParseInterfaceProtection(t *testing.T) {
	hclContent := `
protection "wan_protection" {
  interface = "eth0"
  enabled   = true

  anti_spoofing     = true
  bogon_filtering   = true
  private_filtering = true
  invalid_packets   = true

  syn_flood_protection = true
  syn_flood_rate       = 100
  syn_flood_burst      = 200

  icmp_rate_limit = true
  icmp_rate       = 50
  icmp_burst      = 100

  port_scan_protection = true
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.Protections) != 1 {
		t.Fatalf("Expected 1 InterfaceProtection, got %d", len(cfg.Protections))
	}

	prot := cfg.Protections[0]
	if prot.Name != "wan_protection" {
		t.Errorf("Expected name 'wan_protection', got %q", prot.Name)
	}
	if prot.Interface != "eth0" {
		t.Errorf("Expected interface 'eth0', got %q", prot.Interface)
	}
	if !prot.Enabled {
		t.Error("Expected enabled to be true")
	}
	if !prot.AntiSpoofing {
		t.Error("Expected anti_spoofing to be true")
	}
	if !prot.BogonFiltering {
		t.Error("Expected bogon_filtering to be true")
	}
	if !prot.PrivateFiltering {
		t.Error("Expected private_filtering to be true")
	}
	if !prot.InvalidPackets {
		t.Error("Expected invalid_packets to be true")
	}
	if !prot.SynFloodProtection {
		t.Error("Expected syn_flood_protection to be true")
	}
	if prot.SynFloodRate != 100 {
		t.Errorf("Expected syn_flood_rate 100, got %d", prot.SynFloodRate)
	}
	if !prot.ICMPRateLimit {
		t.Error("Expected icmp_rate_limit to be true")
	}
	if prot.ICMPRate != 50 {
		t.Errorf("Expected icmp_rate 50, got %d", prot.ICMPRate)
	}
	if !prot.PortScanProtection {
		t.Error("Expected port_scan_protection to be true")
	}
}
