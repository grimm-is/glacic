package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCheck_ValidConfig(t *testing.T) {
	// Create temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "valid.hcl")

	validConfig := `
interface "eth0" {
    ipv4 = ["192.168.1.1/24"]
    zone = "LAN"
}
zone "LAN" {}
`
	if err := os.WriteFile(configPath, []byte(validConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := RunCheck(configPath, false); err != nil {
		t.Errorf("RunCheck() error = %v, wantHclErr false", err)
	}
}

func TestRunCheck_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.hcl")

	invalidConfig := `
interface "eth0" {
    # Missing closing brace
`
	if err := os.WriteFile(configPath, []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := RunCheck(configPath, false); err == nil {
		t.Error("RunCheck() error = nil, wantHclErr true")
	}
}
