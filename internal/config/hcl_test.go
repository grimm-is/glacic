package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHCLRoundTripPreservesComments(t *testing.T) {
	hclWithComments := `# This is a top-level comment about the firewall config
# It should be preserved through round-trips

# Enable IP forwarding for routing
ip_forwarding = true

# WAN interface configuration
interface "eth0" {
  description = "WAN - Internet uplink"
  zone        = "WAN"
  dhcp        = true
  # MTU optimized for PPPoE
  mtu = 1492
}

# LAN interface with VLANs
interface "eth1" {
  description = "LAN - Internal network"
  zone        = "Trusted"
  ipv4        = ["192.168.1.1/24"]

  # Guest VLAN for visitors
  vlan "10" {
    zone        = "Guest"
    description = "Guest network"
    ipv4        = ["192.168.10.1/24"]
  }

  # IoT VLAN for smart devices
  vlan "20" {
    zone        = "IoT"
    description = "IoT devices"
    ipv4        = ["192.168.20.1/24"]
  }
}

# DHCP server for LAN clients
dhcp {
  enabled = true

  # Pool range excludes static assignments
  scope "lan" {
    interface   = "eth1"
    range_start = "192.168.1.100"
    range_end   = "192.168.1.200"
    router      = "192.168.1.1"
  }
}
`

	// Load config from HCL with comments
	cf, err := LoadConfigFromBytes("test.hcl", []byte(hclWithComments))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify config was parsed correctly
	if !cf.Config.IPForwarding {
		t.Error("Expected ip_forwarding to be true")
	}
	if len(cf.Config.Interfaces) != 2 {
		t.Errorf("Expected 2 interfaces, got %d", len(cf.Config.Interfaces))
	}

	// Get the raw HCL back
	output := cf.GetRawHCL()

	// Check that comments are preserved
	commentChecks := []string{
		"# This is a top-level comment",
		"# Enable IP forwarding",
		"# WAN interface configuration",
		"# MTU optimized for PPPoE",
		"# Guest VLAN for visitors",
		"# IoT VLAN for smart devices",
		"# DHCP server for LAN clients",
		"# Pool range excludes static assignments",
	}

	for _, comment := range commentChecks {
		if !strings.Contains(output, comment) {
			t.Errorf("Comment not preserved: %q", comment)
		}
	}
}

func TestHCLRoundTripPreservesUnknownFields(t *testing.T) {
	// HCL with fields that might not be in our Go struct
	hclWithExtras := `ip_forwarding = true

interface "eth0" {
  description = "WAN"
  zone        = "WAN"
  dhcp        = true
}

# Custom section that UI doesn't know about
policy "wan" "trusted" {

  rule "allow_established" {
    action   = "accept"
    services = ["established"]
    # Custom comment about this rule
  }
}
`

	cf, err := LoadConfigFromBytes("test.hcl", []byte(hclWithExtras))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	output := cf.GetRawHCL()

	// The policy block should be preserved
	if !strings.Contains(output, `policy "wan" "trusted"`) {
		t.Error("Policy block not preserved")
	}
	if !strings.Contains(output, "# Custom comment about this rule") {
		t.Error("Inline comment not preserved")
	}
}

func TestSetRawHCL(t *testing.T) {
	original := `ip_forwarding = false

interface "eth0" {
  zone = "WAN"
  dhcp = true
}
`

	cf, err := LoadConfigFromBytes("test.hcl", []byte(original))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Modify via raw HCL
	newHCL := `# Updated config
ip_forwarding = true

interface "eth0" {
  zone        = "WAN"
  dhcp        = true
  description = "Internet"
}

interface "eth1" {
  zone = "LAN"
  ipv4 = ["10.0.0.1/24"]
}
`

	if err := cf.SetRawHCL(newHCL); err != nil {
		t.Fatalf("Failed to set raw HCL: %v", err)
	}

	// Verify config was updated
	if !cf.Config.IPForwarding {
		t.Error("ip_forwarding should be true after update")
	}
	if len(cf.Config.Interfaces) != 2 {
		t.Errorf("Expected 2 interfaces, got %d", len(cf.Config.Interfaces))
	}

	// Verify comment is in output
	if !strings.Contains(cf.GetRawHCL(), "# Updated config") {
		t.Error("Comment not preserved in updated HCL")
	}
}

func TestSetRawHCLValidation(t *testing.T) {
	cf, _ := LoadConfigFromBytes("test.hcl", []byte(`ip_forwarding = true`))

	// Invalid HCL syntax
	err := cf.SetRawHCL(`ip_forwarding = true
interface "eth0" {
  zone = "WAN"
  # Missing closing brace
`)
	if err == nil {
		t.Error("Expected error for invalid HCL syntax")
	}
}

func TestGetSection(t *testing.T) {
	hcl := `ip_forwarding = true

dhcp {
  enabled = true

  scope "lan" {
    interface   = "eth1"
    range_start = "192.168.1.100"
    range_end   = "192.168.1.200"
    router      = "192.168.1.1"
  }
}

dns_server {
  enabled = true
}
`

	cf, err := LoadConfigFromBytes("test.hcl", []byte(hcl))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Get DHCP section
	dhcpSection, err := cf.GetSection("dhcp")
	if err != nil {
		t.Fatalf("Failed to get dhcp section: %v", err)
	}

	if !strings.Contains(dhcpSection, "enabled") {
		t.Error("DHCP section missing 'enabled' attribute")
	}
	if !strings.Contains(dhcpSection, "range_start") {
		t.Error("DHCP section missing 'range_start' attribute")
	}
}

func TestGetSectionByLabel(t *testing.T) {
	hcl := `interface "eth0" {
  zone = "WAN"
  dhcp = true
}

interface "eth1" {
  zone = "LAN"
  ipv4 = ["192.168.1.1/24"]
}
`

	cf, err := LoadConfigFromBytes("test.hcl", []byte(hcl))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Get eth1 section
	eth1Section, err := cf.GetSectionByLabel("interface", []string{"eth1"})
	if err != nil {
		t.Fatalf("Failed to get interface eth1: %v", err)
	}

	if !strings.Contains(eth1Section, "LAN") {
		t.Error("eth1 section should contain 'LAN'")
	}
	if strings.Contains(eth1Section, "WAN") {
		t.Error("eth1 section should not contain 'WAN'")
	}
}

func TestSetSection(t *testing.T) {
	hcl := `ip_forwarding = true

dhcp {
  enabled = true
}
`

	cf, err := LoadConfigFromBytes("test.hcl", []byte(hcl))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Replace DHCP section with one that has a scope
	newDHCP := `dhcp {
  enabled = true

  scope "lan" {
    interface   = "eth1"
    range_start = "192.168.1.50"
    range_end   = "192.168.1.150"
    router      = "192.168.1.1"
    lease_time  = "12h"
  }
}
`

	if err := cf.SetSection("dhcp", newDHCP); err != nil {
		t.Fatalf("Failed to set section: %v", err)
	}

	// Verify config updated
	if cf.Config.DHCP == nil || len(cf.Config.DHCP.Scopes) == 0 {
		t.Fatal("DHCPServer should have scopes after update")
	}
	if cf.Config.DHCP.Scopes[0].RangeStart != "192.168.1.50" {
		t.Errorf("Expected range_start 192.168.1.50, got %s", cf.Config.DHCP.Scopes[0].RangeStart)
	}
}

func TestAddAndRemoveSection(t *testing.T) {
	hcl := `ip_forwarding = true
`

	cf, err := LoadConfigFromBytes("test.hcl", []byte(hcl))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Add DHCP section
	newDHCP := `dhcp {
  enabled = true
}
`

	if err := cf.AddSection(newDHCP); err != nil {
		t.Fatalf("Failed to add section: %v", err)
	}

	if cf.Config.DHCP == nil {
		t.Error("DHCPServer should not be nil after adding section")
	}

	// Remove it
	if err := cf.RemoveSection("dhcp"); err != nil {
		t.Fatalf("Failed to remove section: %v", err)
	}

	if cf.Config.DHCP != nil {
		t.Error("DHCP should be nil after removing section")
	}
}

func TestListSections(t *testing.T) {
	hcl := `ip_forwarding = true

interface "eth0" {
  zone = "WAN"
}

interface "eth1" {
  zone = "LAN"
}

dhcp {
  enabled = true
}

policy "wan" "lan" {
}
`

	cf, err := LoadConfigFromBytes("test.hcl", []byte(hcl))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	sections := cf.ListSections()

	// Should have: 2 interfaces, 1 dhcp, 1 policy
	if len(sections) != 4 {
		t.Errorf("Expected 4 sections, got %d", len(sections))
	}

	// Check for labeled sections
	foundEth0 := false
	foundEth1 := false
	for _, s := range sections {
		if s.Type == "interface" && s.Label == "eth0" {
			foundEth0 = true
		}
		if s.Type == "interface" && s.Label == "eth1" {
			foundEth1 = true
		}
	}

	if !foundEth0 {
		t.Error("Missing interface eth0 in sections")
	}
	if !foundEth1 {
		t.Error("Missing interface eth1 in sections")
	}
}

func TestSaveAndReload(t *testing.T) {
	hcl := `# Test config
ip_forwarding = true

interface "eth0" {
  zone = "WAN"
  dhcp = true
}
`

	// Create temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.hcl")

	cf, err := LoadConfigFromBytes(tmpFile, []byte(hcl))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Save to disk
	if err := cf.Save(); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	// Reload from disk
	cf2, err := LoadConfigFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}

	// Verify content
	if !cf2.Config.IPForwarding {
		t.Error("ip_forwarding should be true after reload")
	}
	if !strings.Contains(cf2.GetRawHCL(), "# Test config") {
		t.Error("Comment not preserved after save/reload")
	}
}

func TestHasChanges(t *testing.T) {
	hcl := `ip_forwarding = true`

	cf, _ := LoadConfigFromBytes("test.hcl", []byte(hcl))

	if cf.HasChanges() {
		t.Error("Should not have changes immediately after load")
	}

	// Make a change
	cf.SetRawHCL(`ip_forwarding = false`)

	if !cf.HasChanges() {
		t.Error("Should have changes after modification")
	}
}

func TestValidateHCL(t *testing.T) {
	// Valid HCL
	err := ValidateHCL(`ip_forwarding = true
interface "eth0" {
  zone = "WAN"
}
`)
	if err != nil {
		t.Errorf("Valid HCL should not error: %v", err)
	}

	// Invalid syntax
	err = ValidateHCL(`ip_forwarding = true
interface "eth0" {
  zone = "WAN"
`)
	if err == nil {
		t.Error("Invalid HCL should error")
	}
}

func TestFormatHCL(t *testing.T) {
	messy := `ip_forwarding=true
interface "eth0" {
zone="WAN"
dhcp=true
}`

	formatted, err := FormatHCL(messy)
	if err != nil {
		t.Fatalf("Failed to format HCL: %v", err)
	}

	// Should have proper spacing
	if !strings.Contains(formatted, "ip_forwarding = true") {
		t.Error("Formatting should add spaces around =")
	}
}

func TestParseHCLWithDiagnostics(t *testing.T) {
	// HCL with error
	diags, err := ParseHCLWithDiagnostics(`ip_forwarding = true
interface "eth0" {
  zone = "WAN"
  invalid syntax here
}
`)

	if err == nil {
		t.Error("Should return error for invalid HCL")
	}

	if len(diags) == 0 {
		t.Error("Should return diagnostics")
	}

	// Check diagnostic has line info
	if diags[0].Line == 0 {
		t.Error("Diagnostic should have line number")
	}
}

func TestBackupCreatedOnSave(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.hcl")

	// Create initial file
	os.WriteFile(tmpFile, []byte(`ip_forwarding = false`), 0644)

	// Load and modify
	cf, _ := LoadConfigFile(tmpFile)
	cf.SetRawHCL(`ip_forwarding = true`)
	cf.Save()

	// Check backup exists
	backupFile := tmpFile + ".bak"
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		t.Error("Backup file should be created on save")
	}

	// Backup should have original content
	backup, _ := os.ReadFile(backupFile)
	if !strings.Contains(string(backup), "false") {
		t.Error("Backup should contain original content")
	}
}
func TestGetSectionByMultipleLabels(t *testing.T) {
	hcl := `policy "wan" "lan" {
  rule "allow_ssh" {
    action = "accept"
  }
}

policy "lan" "trusted" {
}
`
	cf, err := LoadConfigFromBytes("test.hcl", []byte(hcl))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Match multiple labels
	sec, err := cf.GetSectionByLabel("policy", []string{"wan", "lan"})
	if err != nil {
		t.Fatalf("Failed to match multiple labels: %v", err)
	}
	if !strings.Contains(sec, "allow_ssh") {
		t.Error("Matched wrong section")
	}

	// Should NOT match partial if more labels exist
	_, err = cf.GetSectionByLabel("policy", []string{"wan"})
	if err == nil {
		t.Error("Should not match partial labels if more labels exist in block (unless name fallback)")
	}
}

func TestRemoveSectionByNameFallback(t *testing.T) {
	hcl := `policy "wan" "lan" {
  name = "block_bad"
  rule "deny" {
    action = "drop"
  }
}
`
	cf, err := LoadConfigFromBytes("test.hcl", []byte(hcl))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Remove by name attribute fallback
	err = cf.RemoveSectionByLabel("policy", []string{"block_bad"})
	if err != nil {
		t.Fatalf("Failed to remove by name fallback: %v", err)
	}

	if strings.Contains(cf.GetRawHCL(), "block_bad") {
		t.Error("Section should have been removed")
	}
}
