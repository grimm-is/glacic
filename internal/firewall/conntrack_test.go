//go:build linux
// +build linux

package firewall

import (
	"testing"
)

func TestBuiltinHelpers(t *testing.T) {
	expectedHelpers := []string{"ftp", "tftp", "sip", "h323", "pptp", "irc"}

	for _, name := range expectedHelpers {
		helper, ok := BuiltinHelpers[name]
		if !ok {
			t.Errorf("Helper %q not found", name)
			continue
		}
		if helper.Module == "" {
			t.Errorf("Helper %q has no kernel module defined", name)
		}
		if len(helper.Ports) == 0 {
			t.Errorf("Helper %q has no ports defined", name)
		}
		if helper.Protocol != "tcp" && helper.Protocol != "udp" {
			t.Errorf("Helper %q has invalid protocol: %s", name, helper.Protocol)
		}
	}
}

func TestConntrackHelperPorts(t *testing.T) {
	tests := []struct {
		helper   string
		port     int
		protocol string
	}{
		{"ftp", 21, "tcp"},
		{"tftp", 69, "udp"},
		{"sip", 5060, "udp"},
		{"h323", 1720, "tcp"},
		{"pptp", 1723, "tcp"},
		{"irc", 6667, "tcp"},
	}

	for _, tt := range tests {
		t.Run(tt.helper, func(t *testing.T) {
			helper, ok := BuiltinHelpers[tt.helper]
			if !ok {
				t.Fatalf("Helper %q not found", tt.helper)
			}

			if helper.Protocol != tt.protocol {
				t.Errorf("Helper %q protocol = %s, want %s", tt.helper, helper.Protocol, tt.protocol)
			}

			found := false
			for _, p := range helper.Ports {
				if p == tt.port {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Helper %q missing port %d", tt.helper, tt.port)
			}
		})
	}
}

func TestConntrackHelperModules(t *testing.T) {
	// Verify module names follow kernel naming convention
	for name, helper := range BuiltinHelpers {
		if helper.Module == "" {
			t.Errorf("Helper %q has empty module name", name)
		}
		// All conntrack modules should start with nf_conntrack_
		if len(helper.Module) < 13 || helper.Module[:13] != "nf_conntrack_" {
			t.Errorf("Helper %q module %q doesn't follow nf_conntrack_* pattern", name, helper.Module)
		}
	}
}

func TestNewConntrackManager(t *testing.T) {
	mgr := NewConntrackManager()
	if mgr == nil {
		t.Fatal("NewConntrackManager returned nil")
	}
	if mgr.enabledHelpers == nil {
		t.Error("enabledHelpers map not initialized")
	}
}

func TestConntrackManagerListAvailable(t *testing.T) {
	mgr := NewConntrackManager()
	helpers := mgr.ListAvailableHelpers()

	if len(helpers) == 0 {
		t.Error("ListAvailableHelpers returned empty list")
	}

	// All should be disabled initially
	for _, h := range helpers {
		if h.Enabled {
			t.Errorf("Helper %q should be disabled initially", h.Name)
		}
	}
}

func TestConntrackManagerGetEnabled(t *testing.T) {
	mgr := NewConntrackManager()
	enabled := mgr.GetEnabledHelpers()

	if len(enabled) != 0 {
		t.Errorf("Expected 0 enabled helpers initially, got %d", len(enabled))
	}
}

func TestConntrackHelperDescriptions(t *testing.T) {
	for name, helper := range BuiltinHelpers {
		if helper.Description == "" {
			t.Errorf("Helper %q has no description", name)
		}
		if helper.Name != name {
			t.Errorf("Helper %q has mismatched Name field: %s", name, helper.Name)
		}
	}
}
