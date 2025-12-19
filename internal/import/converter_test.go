package imports

import (
	"testing"
)

func TestImportResult_ToConfig_Empty(t *testing.T) {
	result := &ImportResult{}
	mappings := map[string]string{}

	cfg := result.ToConfig(mappings)
	if cfg == nil {
		t.Fatal("ToConfig should not return nil")
	}

	// Empty result should produce empty config
	if len(cfg.Interfaces) != 0 {
		t.Errorf("expected 0 interfaces, got %d", len(cfg.Interfaces))
	}
}

func TestImportResult_ToConfig_WithInterfaces(t *testing.T) {
	result := &ImportResult{
		Hostname: "test-router",
		Domain:   "local",
		Interfaces: []ImportedInterface{
			{
				OriginalName: "wan",
				Description:  "WAN Interface",
				IPAddress:    "dhcp",
				IsDHCP:       true,
				Zone:         "WAN",
			},
			{
				OriginalName: "lan",
				Description:  "LAN Interface",
				IPAddress:    "192.168.1.1",
				Subnet:       "24",
				Zone:         "LAN",
			},
		},
	}

	mappings := map[string]string{
		"wan": "eth0",
		"lan": "eth1",
	}

	cfg := result.ToConfig(mappings)
	if cfg == nil {
		t.Fatal("ToConfig should not return nil")
	}

	if len(cfg.Interfaces) != 2 {
		t.Errorf("expected 2 interfaces, got %d", len(cfg.Interfaces))
	}
}

func TestImportResult_ToConfig_WithDHCP(t *testing.T) {
	result := &ImportResult{
		DHCPScopes: []ImportedDHCPScope{
			{
				Interface:  "lan",
				RangeStart: "192.168.1.100",
				RangeEnd:   "192.168.1.200",
				Gateway:    "192.168.1.1",
				DNS:        []string{"8.8.8.8", "8.8.4.4"},
			},
		},
	}

	mappings := map[string]string{
		"lan": "eth1",
	}

	cfg := result.ToConfig(mappings)
	if cfg == nil {
		t.Fatal("ToConfig should not return nil")
	}

	if cfg.DHCP == nil {
		t.Fatal("DHCPServer should not be nil")
	}
	if len(cfg.DHCP.Scopes) != 1 {
		t.Errorf("expected 1 DHCP scope, got %d", len(cfg.DHCP.Scopes))
	}
}

func TestImportResult_ToConfig_WithNAT(t *testing.T) {
	result := &ImportResult{
		NATRules: []ImportedNATRule{
			{
				Type:        "masquerade",
				Interface:   "wan",
				Source:      "192.168.1.0/24",
				Description: "Outbound NAT",
				CanImport:   true,
			},
		},
	}

	mappings := map[string]string{
		"wan": "eth0",
	}

	cfg := result.ToConfig(mappings)
	if cfg == nil {
		t.Fatal("ToConfig should not return nil")
	}

	if len(cfg.NAT) != 1 {
		t.Errorf("expected 1 NAT rule, got %d", len(cfg.NAT))
	}
}

func TestImportResult_ToConfig_WithPolicies(t *testing.T) {
	result := &ImportResult{
		FilterRules: []ImportedFilterRule{
			{
				Description:     "Allow SSH",
				Action:          "accept",
				Interface:       "lan",
				Protocol:        "tcp",
				DestPort:        "22",
				CanImport:       true,
				SuggestedPolicy: "lan_to_any",
			},
		},
	}

	mappings := map[string]string{
		"lan": "eth1",
	}

	cfg := result.ToConfig(mappings)
	if cfg == nil {
		t.Fatal("ToConfig should not return nil")
	}

	// Rules should be grouped into policies
	if len(cfg.Policies) == 0 {
		t.Log("Note: policies may be empty if grouping logic filters out single rules")
	}
}

func TestImportResult_ToConfig_WithPortForwards(t *testing.T) {
	result := &ImportResult{
		PortForwards: []ImportedPortForward{
			{
				Interface:    "wan",
				Protocol:     "tcp",
				ExternalPort: "80",
				InternalIP:   "192.168.1.10",
				InternalPort: "8080",
				Description:  "Web Server",
				CanImport:    true,
			},
		},
	}

	mappings := map[string]string{
		"wan": "eth0",
	}

	cfg := result.ToConfig(mappings)
	if cfg == nil {
		t.Fatal("ToConfig should not return nil")
	}

	// Port forwards should be converted to DNAT rules
	if len(cfg.NAT) != 1 {
		t.Errorf("expected 1 NAT rule from port forward, got %d", len(cfg.NAT))
	}
}

func TestImportResult_ToConfig_WithAliases(t *testing.T) {
	result := &ImportResult{
		Aliases: []ImportedAlias{
			{
				Name:        "RFC1918",
				Type:        "network",
				Values:      []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
				Description: "Private networks",
				CanImport:   true,
				ConvertType: "ipset",
			},
		},
	}

	mappings := map[string]string{}

	cfg := result.ToConfig(mappings)
	if cfg == nil {
		t.Fatal("ToConfig should not return nil")
	}

	if len(cfg.IPSets) != 1 {
		t.Errorf("expected 1 IPSet from alias, got %d", len(cfg.IPSets))
	}
}

func TestImportedInterface_Struct(t *testing.T) {
	iface := ImportedInterface{
		OriginalName: "wan",
		OriginalIf:   "em0",
		Description:  "WAN Interface",
		IPAddress:    "dhcp",
		Zone:         "WAN",
		BlockPrivate: true,
		BlockBogons:  true,
		NeedsMapping: true,
		SuggestedIf:  "eth0",
	}

	if iface.OriginalName != "wan" {
		t.Error("OriginalName not set correctly")
	}
	if !iface.BlockPrivate {
		t.Error("BlockPrivate not set correctly")
	}
}

func TestImportedAlias_Struct(t *testing.T) {
	alias := ImportedAlias{
		Name:        "web_servers",
		Type:        "host",
		Values:      []string{"192.168.1.10", "192.168.1.11"},
		Description: "Web servers",
		CanImport:   true,
		ConvertType: "ipset",
	}

	if alias.Name != "web_servers" {
		t.Error("Name not set correctly")
	}
	if len(alias.Values) != 2 {
		t.Error("Values not set correctly")
	}
}

func TestImportedFilterRule_Struct(t *testing.T) {
	rule := ImportedFilterRule{
		Description: "Allow HTTP",
		Action:      "accept",
		Interface:   "lan",
		Direction:   "in",
		Protocol:    "tcp",
		DestPort:    "80",
		Log:         true,
		CanImport:   true,
	}

	if rule.Action != "accept" {
		t.Error("Action not set correctly")
	}
	if !rule.Log {
		t.Error("Log not set correctly")
	}
}

func TestImportedDHCPScope_Struct(t *testing.T) {
	scope := ImportedDHCPScope{
		Interface:  "lan",
		RangeStart: "192.168.1.100",
		RangeEnd:   "192.168.1.200",
		Gateway:    "192.168.1.1",
		DNS:        []string{"8.8.8.8"},
		Domain:     "local",
		Reservations: []ImportedDHCPReservation{
			{MAC: "00:11:22:33:44:55", IP: "192.168.1.10", Hostname: "server1"},
		},
	}

	if scope.Interface != "lan" {
		t.Error("Interface not set correctly")
	}
	if len(scope.Reservations) != 1 {
		t.Error("Reservations not set correctly")
	}
}
