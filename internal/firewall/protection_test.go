//go:build linux
// +build linux

package firewall

import (
	"testing"

	"grimm.is/glacic/internal/config"
)

func TestDefaultProtection(t *testing.T) {
	cfg := DefaultProtection()
	if cfg == nil {
		t.Fatal("DefaultProtection returned nil")
	}

	// Check defaults are sensible
	if !cfg.AntiSpoofing {
		t.Error("AntiSpoofing should be enabled by default")
	}
	if !cfg.BogonFiltering {
		t.Error("BogonFiltering should be enabled by default")
	}
	if !cfg.InvalidPackets {
		t.Error("InvalidPackets should be enabled by default")
	}
	if !cfg.SynFloodProtection {
		t.Error("SynFloodProtection should be enabled by default")
	}
	if !cfg.ICMPRateLimit {
		t.Error("ICMPRateLimit should be enabled by default")
	}

	// Check rate limits have values
	if cfg.SynFloodRate == 0 {
		t.Error("SynFloodRate should have a default value")
	}
	if cfg.SynFloodBurst == 0 {
		t.Error("SynFloodBurst should have a default value")
	}
	if cfg.ICMPRate == 0 {
		t.Error("ICMPRate should have a default value")
	}
}

func TestPrivateNetworks(t *testing.T) {
	// Test RFC1918 private networks are defined
	if len(privateNetworks) != 3 {
		t.Errorf("Expected 3 private networks (RFC1918), got %d", len(privateNetworks))
	}

	// Verify the three RFC1918 ranges
	expected := []struct {
		network string
		ip      []byte
	}{
		{"10.0.0.0/8", []byte{10, 0, 0, 0}},
		{"172.16.0.0/12", []byte{172, 16, 0, 0}},
		{"192.168.0.0/16", []byte{192, 168, 0, 0}},
	}

	for i, exp := range expected {
		if i >= len(privateNetworks) {
			t.Errorf("Missing private network %s", exp.network)
			continue
		}
		net := privateNetworks[i]
		for j, b := range exp.ip {
			if net.ip[j] != b {
				t.Errorf("Private network %d IP mismatch at byte %d: got %d, want %d",
					i, j, net.ip[j], b)
			}
		}
	}
}

func TestBogonNetworks(t *testing.T) {
	// Verify bogon networks are defined
	if len(bogonNetworks) == 0 {
		t.Error("No bogon networks defined")
	}

	// Check for key bogon ranges
	expectedBogons := [][]byte{
		{0, 0, 0, 0},     // 0.0.0.0/8
		{127, 0, 0, 0},   // 127.0.0.0/8 loopback
		{169, 254, 0, 0}, // 169.254.0.0/16 link-local
		{224, 0, 0, 0},   // 224.0.0.0/4 multicast
		{240, 0, 0, 0},   // 240.0.0.0/4 reserved
	}

	for _, exp := range expectedBogons {
		found := false
		for _, bogon := range bogonNetworks {
			match := true
			for i, b := range exp {
				if bogon.ip[i] != b {
					match = false
					break
				}
			}
			if match {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Missing bogon network starting with %v", exp)
		}
	}
}

func TestBogonNetworkMasks(t *testing.T) {
	// Verify all bogon networks have valid masks
	for i, bogon := range bogonNetworks {
		if len(bogon.ip) != 4 {
			t.Errorf("Bogon %d has invalid IP length: %d", i, len(bogon.ip))
		}
		if len(bogon.mask) != 4 {
			t.Errorf("Bogon %d has invalid mask length: %d", i, len(bogon.mask))
		}

		// Mask should have contiguous 1 bits
		maskVal := uint32(bogon.mask[0])<<24 | uint32(bogon.mask[1])<<16 |
			uint32(bogon.mask[2])<<8 | uint32(bogon.mask[3])

		// Check mask is valid (all 1s followed by all 0s)
		if maskVal != 0 {
			// Find first 0 bit
			foundZero := false
			for bit := 31; bit >= 0; bit-- {
				isOne := (maskVal & (1 << bit)) != 0
				if !isOne {
					foundZero = true
				} else if foundZero {
					t.Errorf("Bogon %d has non-contiguous mask: %v", i, bogon.mask)
					break
				}
			}
		}
	}
}

func TestPrivateNetworkMasks(t *testing.T) {
	// Verify private network masks match expected CIDR
	expectedMasks := [][]byte{
		{255, 0, 0, 0},   // /8
		{255, 240, 0, 0}, // /12
		{255, 255, 0, 0}, // /16
	}

	for i, exp := range expectedMasks {
		if i >= len(privateNetworks) {
			break
		}
		for j, b := range exp {
			if privateNetworks[i].mask[j] != b {
				t.Errorf("Private network %d mask mismatch at byte %d: got %d, want %d",
					i, j, privateNetworks[i].mask[j], b)
			}
		}
	}
}

// TestApplyProtection_GeneratesRules verifies that ApplyProtection actually
// creates nftables rules, not just data structures.
func TestApplyProtection_GeneratesRules(t *testing.T) {
	t.Skip("Skipping - requires mock expectations setup for AddChain")
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	cfg := DefaultProtection()
	wanInterfaces := []string{"eth0"}

	// Apply protection rules
	err := mgr.ApplyProtection(cfg, wanInterfaces)
	if err != nil {
		t.Fatalf("ApplyProtection failed: %v", err)
	}

	// Verify rules were actually created
	if len(mockConn.rules) == 0 {
		t.Error("ApplyProtection generated no rules - expected anti-spoofing, bogon, rate limit rules")
	}

	// Count rule types by checking the mock conn
	// At minimum we should have:
	// - 1 invalid packet rule
	// - 3 anti-spoofing rules (for 3 RFC1918 ranges)
	// - 9 bogon rules (for 9 bogon ranges)
	// - 1 SYN flood rule
	// - 1 ICMP rate limit rule
	minExpectedRules := 1 + 3 + 9 + 1 + 1 // = 15

	if len(mockConn.rules) < minExpectedRules {
		t.Errorf("Expected at least %d rules, got %d", minExpectedRules, len(mockConn.rules))
	}

	t.Logf("ApplyProtection generated %d nftables rules", len(mockConn.rules))
}

// TestApplyProtection_DisabledFeatures verifies that disabled features don't generate rules.
func TestApplyProtection_DisabledFeatures(t *testing.T) {
	t.Skip("Skipping - requires mock expectations setup for AddChain")
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	// Create config with all features disabled
	cfg := &config.ProtectionConfig{
		AntiSpoofing:       false,
		BogonFiltering:     false,
		InvalidPackets:     false,
		SynFloodProtection: false,
		ICMPRateLimit:      false,
	}

	err := mgr.ApplyProtection(cfg, []string{"eth0"})
	if err != nil {
		t.Fatalf("ApplyProtection failed: %v", err)
	}

	// With all features disabled, should have no protection rules
	if len(mockConn.rules) > 0 {
		t.Errorf("Expected 0 rules with all protection disabled, got %d", len(mockConn.rules))
	}
}

// TestApplyProtection_NilConfig verifies graceful handling of nil config.
func TestApplyProtection_NilConfig(t *testing.T) {
	mockConn := NewMockNFTablesConn()
	mgr := NewManagerWithConn(mockConn, nil, "")

	err := mgr.ApplyProtection(nil, []string{"eth0"})
	if err != nil {
		t.Errorf("ApplyProtection with nil config should not error, got: %v", err)
	}
}
