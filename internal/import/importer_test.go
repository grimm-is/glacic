package imports

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseMikroTikVPN(t *testing.T) {
	t.Skip("Skipping MikroTik VPN import test - feature is stashed for future implementation")

	// Get the directory of this test file
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get current file path")
	}
	testDir := filepath.Dir(currentFile)

	// Path to sample file (relative to this test file's directory)
	path := filepath.Join(testDir, "..", "..", "tests", "data", "mikrotik_vpn.rsc")

	// Run parser
	cfg, err := ParseMikroTikExport(path)
	if err != nil {
		t.Fatalf("Failed to parse MikroTik export: %v", err)
	}

	// Verify WireGuard Interfaces
	// Note: The current parser puts everything into "Interfaces"
	// We expect to find generic interfaces named "wg0" and "wg1"
	foundWg0 := false
	foundWg1 := false

	for _, iface := range cfg.Interfaces {
		if iface.Name == "wg0" {
			foundWg0 = true
			if iface.MTU != 1420 {
				t.Errorf("Expected wg0 MTU 1420, got %d", iface.MTU)
			}
		}
		if iface.Name == "wg1" {
			foundWg1 = true
		}
	}

	if !foundWg0 {
		t.Error("Failed to find wg0 interface")
	}
	if !foundWg1 {
		t.Error("Failed to find wg1 interface")
	}

	// Verify Addresses
	// 10.100.0.1/24 on wg0
	foundAddr := false
	for _, addr := range cfg.Addresses {
		if addr.Interface == "wg0" && addr.Address == "10.100.0.1/24" {
			foundAddr = true
			break
		}
	}
	if !foundAddr {
		t.Error("Failed to find IP address for wg0")
	}

	// Run Import Conversion
	importResult := cfg.ToImportResult()

	// Verify Imported Interfaces
	// We expect "wg0" to handle "suggested interface" gracefully (likely keeping it as wg0 or similar)
	foundImported := false
	for _, iface := range importResult.Interfaces {
		if iface.OriginalName == "wg0" {
			foundImported = true
			// WireGuard interfaces on Linux are typically just "wg0", so SuggestedIf should match or be empty (default)
			if iface.SuggestedIf != "" && iface.SuggestedIf != "wg0" {
				t.Logf("Notice: Suggested interface for wg0 is %s", iface.SuggestedIf)
			}
		}
	}
	if !foundImported {
		t.Error("Import result missing wg0 interface")
	}
}
