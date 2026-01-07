package setup

import (
	"net"
	"os"
	"strings"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"192.168.1.1", true},
		{"10.0.5.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"172.32.0.1", false}, // Outside 172.16.0.0/12
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if got := IsPrivateIP(ip); got != tt.want {
				t.Errorf("IsPrivateIP(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestShouldExclude(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"eth0", false},
		{"wlan0", false},
		{"ens3", false},
		{"lo", true},
		{"docker0", true},
		{"br-123", true},
		{"veth123", true},
		{"tun0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldExclude(tt.name); got != tt.want {
				t.Errorf("shouldExclude(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestFindBestWANCandidate(t *testing.T) {
	hw := &DetectedHardware{
		Interfaces: []InterfaceInfo{
			{Name: "eth0", LinkUp: true, IsVirtual: false, IPs: []string{"192.168.1.50"}},
			{Name: "eth1", LinkUp: true, IsVirtual: false, IPs: nil}, // Best candidate (up, no IP)
			{Name: "eth2", LinkUp: false, IsVirtual: false, IPs: nil},
		},
	}

	best := hw.FindBestWANCandidate()
	if best == nil {
		t.Fatal("Expected best candidate, got nil")
	}
	if best.Name != "eth1" {
		t.Errorf("Expected eth1, got %s", best.Name)
	}
}

func TestFindBestWANCandidate_Fallback(t *testing.T) {
	hw := &DetectedHardware{
		Interfaces: []InterfaceInfo{
			{Name: "eth0", LinkUp: true, IsVirtual: false, IPs: []string{"192.168.1.50"}},
		},
	}

	// Falls back to first active link even if it has IP
	best := hw.FindBestWANCandidate()
	if best == nil {
		t.Fatal("Expected candidate, got nil")
	}
	if best.Name != "eth0" {
		t.Errorf("Expected eth0, got %s", best.Name)
	}
}

func TestFindBestLANCandidate(t *testing.T) {
	hw := &DetectedHardware{
		Interfaces: []InterfaceInfo{
			{Name: "eth0", LinkUp: true, IsVirtual: false},
			{Name: "eth1", LinkUp: true, IsVirtual: false},
		},
	}

	// If eth0 is WAN, eth1 should be LAN
	lan := hw.FindBestLANCandidate("eth0")
	if lan == nil {
		t.Fatal("Expected LAN candidate")
	}
	if lan.Name != "eth1" {
		t.Errorf("Expected eth1, got %s", lan.Name)
	}
}

func TestWizard_GenerateConfig(t *testing.T) {
	tmpDir := t.TempDir()
	w := NewWizard(tmpDir)

	result := &WizardResult{
		WANInterface:  "eth0",
		WANMethod:     "dhcp",
		WANIP:         "1.2.3.4",
		LANInterface:  "eth1",
		LANInterfaces: []string{"eth1"},
		LANIP:         "192.168.1.1",
		LANSubnet:     "192.168.1.0/24",
	}

	if err := w.GenerateConfig(result); err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}

	// Verify file exists
	content, err := os.ReadFile(w.configFile)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	// Quick check for expected content
	s := string(content)
	if !strings.Contains(s, `zone "WAN"`) {
		t.Error("Config missing WAN zone")
	}
	if !strings.Contains(s, `interface = "eth0"`) {
		t.Error("Config missing WAN interface assignment")
	}
	if !strings.Contains(s, `dhcp = true`) {
		t.Error("Config missing DHCP setting")
	}
	if !strings.Contains(s, `zone "LAN1"`) {
		t.Error("Config missing LAN1 zone")
	}
	if !strings.Contains(s, `ipv4 = ["192.168.1.1/24"]`) {
		t.Error("Config missing LAN IP")
	}
}

func TestWizard_NeedsSetup(t *testing.T) {
	tmpDir := t.TempDir()
	w := NewWizard(tmpDir)

	if !w.NeedsSetup() {
		t.Error("NeedsSetup should be true when config missing")
	}

	// Create dummy config
	os.WriteFile(w.configFile, []byte("test"), 0644)

	if w.NeedsSetup() {
		t.Error("NeedsSetup should be false when config exists")
	}
}
