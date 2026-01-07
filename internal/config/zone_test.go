package config

import (
	"testing"
)

func TestZone_SimpleInterface(t *testing.T) {
	hcl := `
schema_version = "1.0"

zone "wan" {
  interface = "eth0"
  dhcp = true
}
`
	cfg, err := LoadHCL([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("LoadHCL() error = %v", err)
	}

	if len(cfg.Zones) != 1 {
		t.Fatalf("len(Zones) = %d, want 1", len(cfg.Zones))
	}

	zone := cfg.Zones[0]
	if zone.Interface != "eth0" {
		t.Errorf("Interface = %q, want %q", zone.Interface, "eth0")
	}
	if !zone.DHCP {
		t.Error("DHCP = false, want true")
	}
}

func TestZone_InterfaceWildcard(t *testing.T) {
	hcl := `
schema_version = "1.0"

zone "vpn" {
  interface = "wg+"
  management {
    web = true
  }
}
`
	cfg, err := LoadHCL([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("LoadHCL() error = %v", err)
	}

	if len(cfg.Zones) != 1 {
		t.Fatalf("len(Zones) = %d, want 1", len(cfg.Zones))
	}

	zone := cfg.Zones[0]
	if zone.Interface != "wg+" {
		t.Errorf("Interface = %q, want %q", zone.Interface, "wg+")
	}
}

func TestZone_SourceMatch(t *testing.T) {
	hcl := `
schema_version = "1.0"

zone "guest" {
  interface = "eth1"
  src = "192.168.10.0/24"
  vlan = 100
  services {
    dns = true
  }
}
`
	cfg, err := LoadHCL([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("LoadHCL() error = %v", err)
	}

	zone := cfg.Zones[0]
	if zone.Interface != "eth1" {
		t.Errorf("Interface = %q, want %q", zone.Interface, "eth1")
	}
	if zone.Src != "192.168.10.0/24" {
		t.Errorf("Src = %q, want %q", zone.Src, "192.168.10.0/24")
	}
	if zone.VLAN != 100 {
		t.Errorf("VLAN = %d, want %d", zone.VLAN, 100)
	}
}

func TestZone_MatchBlocks(t *testing.T) {
	hcl := `
schema_version = "1.0"

zone "dmz" {
  match {
    interface = "eth2"
  }
  match {
    interface = "eth3"
  }
  management {
    web = true
    ssh = true
  }
}
`
	cfg, err := LoadHCL([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("LoadHCL() error = %v", err)
	}

	zone := cfg.Zones[0]
	if len(zone.Matches) != 2 {
		t.Fatalf("len(Matches) = %d, want 2", len(zone.Matches))
	}
	if zone.Matches[0].Interface != "eth2" {
		t.Errorf("Matches[0].Interface = %q, want %q", zone.Matches[0].Interface, "eth2")
	}
	if zone.Matches[1].Interface != "eth3" {
		t.Errorf("Matches[1].Interface = %q, want %q", zone.Matches[1].Interface, "eth3")
	}
}

func TestZone_AdditiveInheritance(t *testing.T) {
	hcl := `
schema_version = "1.0"

zone "guest" {
  src = "192.168.10.0/24"

  match {
    interface = "eth1"
  }
  match {
    interface = "wlan0"
  }
}
`
	cfg, err := LoadHCL([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("LoadHCL() error = %v", err)
	}

	zone := cfg.Zones[0]
	// Global src should be present
	if zone.Src != "192.168.10.0/24" {
		t.Errorf("Src = %q, want %q", zone.Src, "192.168.10.0/24")
	}
	// Matches should be present
	if len(zone.Matches) != 2 {
		t.Fatalf("len(Matches) = %d, want 2", len(zone.Matches))
	}
}

func TestZone_DeprecatedInterfaces(t *testing.T) {
	hcl := `
schema_version = "1.0"

zone "lan" {
  interfaces = ["eth1"]
}
`
	result, err := LoadHCLWithOptions([]byte(hcl), "test.hcl", DefaultLoadOptions())
	if err != nil {
		t.Fatalf("LoadHCLWithOptions() error = %v", err)
	}

	// Should have a warning about deprecated syntax
	hasWarning := false
	for _, w := range result.Warnings {
		if contains(w, "interfaces") || contains(w, "deprecated") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("Expected deprecation warning for 'interfaces' field")
	}

	// Config should still parse
	if len(result.Config.Zones) != 1 {
		t.Fatalf("len(Zones) = %d, want 1", len(result.Config.Zones))
	}
}

func TestZone_WithIPAssignment(t *testing.T) {
	hcl := `
schema_version = "1.0"

zone "lan" {
  interface = "eth1"
  ipv4 = ["192.168.1.1/24"]
  management {
    web = true
  }
}
`
	cfg, err := LoadHCL([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("LoadHCL() error = %v", err)
	}

	zone := cfg.Zones[0]
	if zone.Interface != "eth1" {
		t.Errorf("Interface = %q, want %q", zone.Interface, "eth1")
	}
	if len(zone.IPv4) != 1 || zone.IPv4[0] != "192.168.1.1/24" {
		t.Errorf("IPv4 = %v, want [192.168.1.1/24]", zone.IPv4)
	}
}
