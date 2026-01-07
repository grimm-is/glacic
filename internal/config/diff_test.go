package config

import (
	"strings"
	"testing"
)

func TestConfigFile_Diff(t *testing.T) {
	original := `ip_forwarding = false`
	modified := `ip_forwarding = true`

	cf, err := LoadConfigFromBytes("test.hcl", []byte(original))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Update the config
	if err := cf.SetRawHCL(modified); err != nil {
		t.Fatalf("Failed to set raw HCL: %v", err)
	}

	diff := cf.Diff()

	expectedLines := []string{
		"--- original",
		"+++ modified",
		`-ip_forwarding = false`,
		`+ip_forwarding = true`,
	}

	for _, line := range expectedLines {
		if !strings.Contains(diff, line) {
			t.Errorf("Diff output missing expected line: %q. Got:\n%s", line, diff)
		}
	}
}

func TestConfigFile_Diff_NoChanges(t *testing.T) {
	original := `ip_forwarding = true`

	cf, err := LoadConfigFromBytes("test.hcl", []byte(original))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	diff := cf.Diff()
	if diff != "" {
		t.Errorf("Expected empty diff for no changes, got:\n%s", diff)
	}
}
