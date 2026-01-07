package firewall

import (
	"testing"
)

func TestBuiltinServices(t *testing.T) {
	// Verify all expected services exist
	expectedServices := []string{"dns", "ntp", "ssh", "icmp", "snmp", "syslog", "http", "https", "web", "dhcp", "mdns"}
	for _, name := range expectedServices {
		svc := GetService(name)
		if svc == nil {
			t.Errorf("Expected service %q not found", name)
		}
	}
}

func TestService_NFTMatch_TCP(t *testing.T) {
	ssh := GetService("ssh")
	if ssh == nil {
		t.Fatal("ssh service not found")
	}

	match := ssh.NFTMatch()
	expected := "tcp dport 22"
	if match != expected {
		t.Errorf("ssh.NFTMatch() = %q, want %q", match, expected)
	}
}

func TestService_NFTMatch_UDP(t *testing.T) {
	ntp := GetService("ntp")
	if ntp == nil {
		t.Fatal("ntp service not found")
	}

	match := ntp.NFTMatch()
	expected := "udp dport 123"
	if match != expected {
		t.Errorf("ntp.NFTMatch() = %q, want %q", match, expected)
	}
}

func TestService_NFTMatch_ICMP(t *testing.T) {
	icmp := GetService("icmp")
	if icmp == nil {
		t.Fatal("icmp service not found")
	}

	match := icmp.NFTMatch()
	expected := "meta l4proto icmp"
	if match != expected {
		t.Errorf("icmp.NFTMatch() = %q, want %q", match, expected)
	}
}

func TestService_NFTMatch_MultiplePorts(t *testing.T) {
	web := GetService("web")
	if web == nil {
		t.Fatal("web service not found")
	}

	match := web.NFTMatch()
	expected := "tcp dport { 80, 443 }"
	if match != expected {
		t.Errorf("web.NFTMatch() = %q, want %q", match, expected)
	}
}

func TestCustomService(t *testing.T) {
	custom := CustomService("myapp", ProtoTCP, 8080,
		WithDescription("My Application"),
		WithDirection("in"),
	)

	if custom.Name != "myapp" {
		t.Errorf("Name = %q, want %q", custom.Name, "myapp")
	}
	if custom.Protocol != ProtoTCP {
		t.Errorf("Protocol = %v, want %v", custom.Protocol, ProtoTCP)
	}
	if custom.Port != 8080 {
		t.Errorf("Port = %d, want %d", custom.Port, 8080)
	}

	match := custom.NFTMatch()
	expected := "tcp dport 8080"
	if match != expected {
		t.Errorf("NFTMatch() = %q, want %q", match, expected)
	}
}

func TestCustomService_PortRange(t *testing.T) {
	custom := CustomService("rtp", ProtoUDP, 0,
		WithPortRange(16384, 32767),
	)

	match := custom.NFTMatch()
	expected := "udp dport 16384-32767"
	if match != expected {
		t.Errorf("NFTMatch() = %q, want %q", match, expected)
	}
}

func TestService_NFTMatches_MultiProtocol(t *testing.T) {
	dns := GetService("dns")
	if dns == nil {
		t.Fatal("dns service not found")
	}

	matches := dns.NFTMatches()
	if len(matches) != 2 {
		t.Fatalf("DNS NFTMatches() = %d matches, want 2", len(matches))
	}

	// Should have both TCP and UDP rules
	if matches[0] != "tcp dport 53" {
		t.Errorf("matches[0] = %q, want %q", matches[0], "tcp dport 53")
	}
	if matches[1] != "udp dport 53" {
		t.Errorf("matches[1] = %q, want %q", matches[1], "udp dport 53")
	}
}
func TestConntrackServices(t *testing.T) {
	expectedHelpers := []string{"ftp", "tftp", "sip", "h323", "pptp", "irc"}

	for _, name := range expectedHelpers {
		svc, ok := BuiltinServices[name]
		if !ok {
			t.Errorf("Service %q not found", name)
			continue
		}
		if svc.Module == "" {
			t.Errorf("Service %q has no kernel module defined", name)
		}
		if svc.Port == 0 && len(svc.Ports) == 0 {
			t.Errorf("Service %q has no ports defined", name)
		}
		if svc.Protocol != ProtoTCP && svc.Protocol != ProtoUDP {
			t.Errorf("Service %q has invalid protocol: %s", name, svc.Protocol)
		}
	}
}

func TestConntrackHelperPorts(t *testing.T) {
	tests := []struct {
		helper   string
		port     int
		protocol Protocol
	}{
		{"ftp", 21, ProtoTCP},
		{"tftp", 69, ProtoUDP},
		{"sip", 5060, ProtoUDP},
		{"h323", 1720, ProtoTCP},
		{"pptp", 1723, ProtoTCP},
		{"irc", 6667, ProtoTCP},
	}

	for _, tt := range tests {
		t.Run(tt.helper, func(t *testing.T) {
			svc, ok := BuiltinServices[tt.helper]
			if !ok {
				t.Fatalf("Service %q not found", tt.helper)
			}

			if svc.Protocol != tt.protocol {
				t.Errorf("Service %q protocol = %s, want %s", tt.helper, svc.Protocol, tt.protocol)
			}

			found := false
			if svc.Port == tt.port {
				found = true
			} else {
				for _, p := range svc.Ports {
					if p == tt.port {
						found = true
						break
					}
				}
			}

			if !found {
				t.Errorf("Service %q missing port %d", tt.helper, tt.port)
			}
		})
	}
}

func TestConntrackHelperModules(t *testing.T) {
	// Verify module names follow kernel naming convention
	for name, svc := range BuiltinServices {
		if svc.Module == "" {
			continue
		}

		// All conntrack modules should start with nf_conntrack_
		if len(svc.Module) < 13 || svc.Module[:13] != "nf_conntrack_" {
			t.Errorf("Service %q module %q doesn't follow nf_conntrack_* pattern", name, svc.Module)
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
	for name, svc := range BuiltinServices {
		if svc.Description == "" {
			t.Errorf("Service %q has no description", name)
		}
		if svc.Name != name {
			t.Errorf("Service %q has mismatched Name field: %s", name, svc.Name)
		}
	}
}
