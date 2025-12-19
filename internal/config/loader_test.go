package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHCL_ModernConfig(t *testing.T) {
	hcl := `
schema_version = "1.0"
ip_forwarding = true

interface "eth0" {
  zone = "WAN"
  dhcp = true
}

policy "lan" "wan" {

  rule "allow_all" {
    action = "accept"
  }
}
`
	cfg, err := LoadHCL([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("LoadHCL() error = %v", err)
	}

	if cfg.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", cfg.SchemaVersion, "1.0")
	}

	if !cfg.IPForwarding {
		t.Error("IPForwarding = false, want true")
	}

	if len(cfg.Interfaces) != 1 {
		t.Errorf("len(Interfaces) = %d, want 1", len(cfg.Interfaces))
	}

	if len(cfg.Policies) != 1 {
		t.Errorf("len(Policies) = %d, want 1", len(cfg.Policies))
	}
}

func TestLoadHCL_LegacyConfig(t *testing.T) {
	// This is the old format that should be auto-transformed
	hcl := `
ip_forwarding = true

protection {
  syn_flood_protection = true
  syn_flood_rate = 25
}

interface "eth0" {
  zone = "WAN"
  dhcp = true
}
`
	result, err := LoadHCLWithOptions([]byte(hcl), "legacy.hcl", DefaultLoadOptions())
	if err != nil {
		t.Fatalf("LoadHCLWithOptions() error = %v", err)
	}

	// Should have warnings about legacy syntax
	if len(result.Warnings) == 0 {
		t.Error("Expected warnings for legacy syntax, got none")
	}

	// Config should be parsed correctly
	cfg := result.Config
	if cfg.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q (default)", cfg.SchemaVersion, "1.0")
	}

	// Protection should be parsed into the global_protection field
	// Protection should be parsed into "legacy_global" InterfaceProtection?
	// or "global_legacy" as per current LegacyTransforms
	if len(cfg.Protections) == 0 {
		t.Error("Protections is empty, expected legacy protection block to be parsed into Protections")
	} else {
		// Find the legacy block
		found := false
		for _, p := range cfg.Protections {
			if p.Name == "legacy_global" {
				found = true
				if p.Interface != "*" {
					t.Errorf("Interface = %q, want '*'", p.Interface)
				}
				if !p.SynFloodProtection {
					t.Error("SynFloodProtection = false, want true")
				}
				if p.SynFloodRate != 25 {
					t.Errorf("SynFloodRate = %d, want 25", p.SynFloodRate)
				}
			}
		}
		if !found {
			t.Error("Did not find 'legacy_global' protection block")
		}
	}
}

func TestLoadHCL_WithDHCPAndDNS(t *testing.T) {
	hcl := `
schema_version = "1.0"
ip_forwarding = true

interface "eth0" {
  zone = "WAN"
  dhcp = true
}

interface "eth1" {
  zone = "LAN"
  ipv4 = ["192.168.1.1/24"]
}

dhcp {
  mode = "builtin"
  enabled = true
  scope "lan" {
    interface = "eth1"
    range_start = "192.168.1.100"
    range_end = "192.168.1.200"
    router = "192.168.1.1"
    dns = ["192.168.1.1", "8.8.8.8"]
  }
}

dns_server {
  enabled = true
  listen_on = ["192.168.1.1"]
  forwarders = ["8.8.8.8", "1.1.1.1"]
}
`
	cfg, err := LoadHCL([]byte(hcl), "full.hcl")
	if err != nil {
		t.Fatalf("LoadHCL() error = %v", err)
	}

	if cfg.DHCP == nil {
		t.Fatal("DHCPServer is nil")
	}
	if !cfg.DHCP.Enabled {
		t.Error("DHCPServer.Enabled = false, want true")
	}
	if len(cfg.DHCP.Scopes) != 1 {
		t.Errorf("len(DHCPServer.Scopes) = %d, want 1", len(cfg.DHCP.Scopes))
	}

	if cfg.DNSServer == nil {
		t.Fatal("DNSServer is nil")
	}
	if !cfg.DNSServer.Enabled {
		t.Error("DNSServer.Enabled = false, want true")
	}
}

func TestLoadFile_DetectsFormat(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test HCL file
	hclPath := filepath.Join(tmpDir, "config.hcl")
	hclContent := `
schema_version = "1.0"
ip_forwarding = true
`
	if err := os.WriteFile(hclPath, []byte(hclContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(hclPath)
	if err != nil {
		t.Fatalf("LoadFile(hcl) error = %v", err)
	}
	if cfg.SchemaVersion != "1.0" {
		t.Errorf("HCL SchemaVersion = %q, want %q", cfg.SchemaVersion, "1.0")
	}

	// Test JSON file
	jsonPath := filepath.Join(tmpDir, "config.json")
	jsonContent := `{
  "schema_version": "1.0",
  "ip_forwarding": true
}`
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err = LoadFile(jsonPath)
	if err != nil {
		t.Fatalf("LoadFile(json) error = %v", err)
	}
	if cfg.SchemaVersion != "1.0" {
		t.Errorf("JSON SchemaVersion = %q, want %q", cfg.SchemaVersion, "1.0")
	}
}

func TestLoadHCLWithOptions_StrictVersion(t *testing.T) {
	hcl := `
schema_version = "1.0"
ip_forwarding = true
`
	opts := LoadOptions{
		AutoMigrate:   false,
		StrictVersion: true,
	}

	_, err := LoadHCLWithOptions([]byte(hcl), "test.hcl", opts)
	if err != nil {
		t.Errorf("StrictVersion should pass for current version, got error: %v", err)
	}
}

func TestLoadHCL_UnsupportedVersion(t *testing.T) {
	hcl := `
schema_version = "99.0"
ip_forwarding = true
`
	_, err := LoadHCL([]byte(hcl), "future.hcl")
	if err == nil {
		t.Error("Expected error for unsupported version, got nil")
	}
}

func TestSaveHCL(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/test_generated.hcl"

	// Create a complex config to test structure
	cfg := &Config{
		SchemaVersion: "1.0",
		IPForwarding:  true,
		Interfaces: []Interface{
			{
				Name:        "eth0",
				Description: "WAN Interface",
				Zone:        "wan",
				DHCP:        true,
			},
			{
				Name:        "eth1",
				Description: "LAN Interface",
				Zone:        "lan",
				IPv4:        []string{"192.168.1.1/24"},
			},
		},
		DHCP: &DHCPServer{
			Enabled: true,
			Scopes: []DHCPScope{
				{
					Name:       "lan_scope",
					Interface:  "eth1",
					RangeStart: "192.168.1.100",
					RangeEnd:   "192.168.1.200",
					Router:     "192.168.1.1",
				},
			},
		},
	}

	// Save
	if err := SaveHCL(cfg, path); err != nil {
		t.Fatalf("SaveHCL failed: %v", err)
	}

	// Verify content is HCL-like (not JSON)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	s := string(content)

	// Check for HCL block syntax identifiers
	if !contains(s, `interface "eth0" {`) {
		t.Error("Output does not look like HCL block syntax (missing interface block)")
	}
	if contains(s, `{"interfaces":`) {
		t.Error("Output looks like JSON")
	}

	// Load it back to ensure validity
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("Failed to reload generated HCL: %v", err)
	}

	// Compare key fields
	if loaded.IPForwarding != cfg.IPForwarding {
		t.Error("IPForwarding mismatch after reload")
	}
	if len(loaded.Interfaces) != 2 {
		t.Errorf("Interface count mismatch: got %d, want 2", len(loaded.Interfaces))
	}
	if len(loaded.DHCP.Scopes) != 1 {
		t.Errorf("DHCP scope count mismatch")
	}
	if loaded.Interfaces[0].Name != "eth0" {
		t.Errorf("Interface 0 name mismatch: got %s", loaded.Interfaces[0].Name)
	}
}

// Helper (copied from setup tests)
func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
