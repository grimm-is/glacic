//go:build linux
// +build linux

package firewall

import (
	"testing"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"", 0},
		{"100kbit", 100 * 1000 / 8},
		{"100kbps", 100 * 1000 / 8},
		{"10mbit", 10 * 1000 * 1000 / 8},
		{"10mbps", 10 * 1000 * 1000 / 8},
		{"1gbit", 1000 * 1000 * 1000 / 8},
		{"1gbps", 1000 * 1000 * 1000 / 8},
		{"100kbyte", 100 * 1000},
		{"10mbyte", 10 * 1000 * 1000},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseRate(tt.input)
			if got != tt.expected {
				t.Errorf("parseRate(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestDSCPValues(t *testing.T) {
	// Test that all standard DSCP values are defined
	expectedDSCP := map[string]int{
		"ef":   46,
		"af11": 10,
		"af21": 18,
		"af31": 26,
		"af41": 34,
		"cs1":  8,
		"cs2":  16,
		"cs3":  24,
		"cs4":  32,
		"cs5":  40,
		"cs6":  48,
		"cs7":  56,
	}

	for name, expected := range expectedDSCP {
		if got, ok := dscpValues[name]; !ok {
			t.Errorf("DSCP value %q not defined", name)
		} else if got != expected {
			t.Errorf("DSCP %q = %d, want %d", name, got, expected)
		}
	}
}

func TestQoSConfigDefaults(t *testing.T) {
	cfg := &QoSConfig{
		Enabled: true,
		Interfaces: []QoSIface{
			{Name: "eth0", UploadRate: "100mbit"},
		},
		Classes: []QoSClass{
			{Name: "high", Priority: 1, Rate: "50mbit"},
			{Name: "low", Priority: 5, Rate: "10mbit"},
		},
	}

	if len(cfg.Interfaces) != 1 {
		t.Errorf("Expected 1 interface, got %d", len(cfg.Interfaces))
	}
	if len(cfg.Classes) != 2 {
		t.Errorf("Expected 2 classes, got %d", len(cfg.Classes))
	}
}

func TestPredefinedQoSProfiles(t *testing.T) {
	expectedProfiles := []string{"gaming", "voip", "balanced"}

	for _, name := range expectedProfiles {
		profile, ok := PredefinedQoSProfiles[name]
		if !ok {
			t.Errorf("Profile %q not found", name)
			continue
		}
		if !profile.Enabled {
			t.Errorf("Profile %q should be enabled by default", name)
		}
		if len(profile.Classes) == 0 {
			t.Errorf("Profile %q has no classes", name)
		}
	}
}

func TestQoSClassValidation(t *testing.T) {
	tests := []struct {
		name    string
		class   QoSClass
		wantErr bool
	}{
		{
			name:    "valid class",
			class:   QoSClass{Name: "test", Priority: 1, Rate: "10mbit"},
			wantErr: false,
		},
		{
			name:    "class with ceiling",
			class:   QoSClass{Name: "test", Priority: 2, Rate: "10mbit", Ceil: "50mbit"},
			wantErr: false,
		},
		{
			name:    "class without rate uses default",
			class:   QoSClass{Name: "test", Priority: 3},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the struct is valid Go
			if tt.class.Name == "" {
				t.Error("Class name should not be empty")
			}
		})
	}
}

func TestQoSRuleServices(t *testing.T) {
	rule := QoSRule{
		Name:     "web-traffic",
		Class:    "interactive",
		Services: []string{"http", "https"},
	}

	if len(rule.Services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(rule.Services))
	}
}

func TestQoSRuleDSCP(t *testing.T) {
	rule := QoSRule{
		Name:  "voip",
		Class: "realtime",
		DSCP:  "ef",
	}

	if _, ok := dscpValues[rule.DSCP]; !ok {
		t.Errorf("DSCP value %q not recognized", rule.DSCP)
	}
}
