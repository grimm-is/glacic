package firewall

import (
	"testing"

	"grimm.is/glacic/internal/config"
)

func TestFromGlobalConfig_Nil(t *testing.T) {
	result := FromGlobalConfig(nil)
	if result == nil {
		t.Fatal("FromGlobalConfig(nil) should return empty Config, not nil")
	}
	if len(result.Zones) != 0 {
		t.Error("Zones should be empty")
	}
}

func TestFromGlobalConfig_Full(t *testing.T) {
	global := &config.Config{
		Zones: []config.Zone{
			{Name: "wan"},
			{Name: "lan"},
		},
		Interfaces: []config.Interface{
			{Name: "eth0", Zone: "wan"},
		},
		IPSets: []config.IPSet{
			{Name: "blocklist"},
		},
		Policies: []config.Policy{
			{Name: "allow-all"},
		},
		NAT: []config.NATRule{
			{Type: "masquerade"},
		},
		API: &config.APIConfig{
			Listen: ":8080",
		},
	}

	result := FromGlobalConfig(global)

	if len(result.Zones) != 2 {
		t.Errorf("Expected 2 zones, got %d", len(result.Zones))
	}
	if len(result.Interfaces) != 1 {
		t.Errorf("Expected 1 interface, got %d", len(result.Interfaces))
	}
	if len(result.IPSets) != 1 {
		t.Errorf("Expected 1 IPSet, got %d", len(result.IPSets))
	}
	if len(result.Policies) != 1 {
		t.Errorf("Expected 1 policy, got %d", len(result.Policies))
	}
	if len(result.NAT) != 1 {
		t.Errorf("Expected 1 NAT rule, got %d", len(result.NAT))
	}
	if result.API == nil || result.API.Listen != ":8080" {
		t.Error("API config not copied correctly")
	}
}
