package config

import (
	"testing"

	"github.com/hashicorp/hcl/v2/hclsimple"
)

func TestParseConfig(t *testing.T) {
	hclContent := `
interface "eth0" {
  description = "WAN"
  zone        = "WAN"
  ipv4        = ["1.2.3.4/24"]
}

interface "eth1" {
  description = "LAN"
  zone        = "Trusted"
  ipv4        = ["10.0.0.1/24"]

  vlan "10" {
    zone = "IoT"
    ipv4 = ["10.0.10.1/24"]
  }
}
`

	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.Interfaces) != 2 {
		t.Errorf("Expected 2 interfaces, got %d", len(cfg.Interfaces))
	}

	eth0 := cfg.Interfaces[0]
	if eth0.Name != "eth0" {
		t.Errorf("Expected first interface name eth0, got %s", eth0.Name)
	}
	if eth0.Zone != "WAN" {
		t.Errorf("Expected zone WAN, got %s", eth0.Zone)
	}

	eth1 := cfg.Interfaces[1]
	if len(eth1.VLANs) != 1 {
		t.Errorf("Expected 1 VLAN on eth1, got %d", len(eth1.VLANs))
	}
	if eth1.VLANs[0].ID != "10" {
		t.Errorf("Expected VLAN ID 10, got %s", eth1.VLANs[0].ID)
	}
}
