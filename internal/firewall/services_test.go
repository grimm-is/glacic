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
