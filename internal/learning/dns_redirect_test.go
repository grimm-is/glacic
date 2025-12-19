package learning

import (
	"strings"
	"testing"

	"grimm.is/glacic/internal/config"
)

func TestGenerateDNSInspectRules(t *testing.T) {
	inspects := []config.DNSInspect{
		{
			Zone:          "LAN",
			Mode:          "redirect",
			ExcludeRouter: true,
		},
		{
			Zone: "Guest",
			Mode: "passive",
		},
	}

	zoneToIfaces := map[string][]string{
		"LAN":   {"eth1"},
		"Guest": {"eth2"},
	}

	routerIPs := []string{"192.168.1.1"}

	rules := GenerateDNSInspectRules(inspects, zoneToIfaces, "127.0.0.1", 53, routerIPs)

	// Check for redirect rules
	foundRedirect := false
	foundPassive := false
	foundMasq := false

	for _, r := range rules {
		if strings.Contains(r, "iifname \"eth1\"") && strings.Contains(r, "dnat to 127.0.0.1:53") {
			foundRedirect = true
			if !strings.Contains(r, "ip saddr != 192.168.1.1") {
				t.Errorf("Redirect rule missing router IP exclusion: %s", r)
			}
		}
		if strings.Contains(r, "iifname \"eth2\"") && strings.Contains(r, "log group 100") {
			foundPassive = true
		}
		if strings.Contains(r, "masquerade") && strings.Contains(r, "127.0.0.1") {
			foundMasq = true
		}
	}

	if !foundRedirect {
		t.Error("Did not find expected DNAT redirect rule for eth1")
	}
	if !foundPassive {
		t.Error("Did not find expected log rule for eth2 (passive mode)")
	}
	if !foundMasq {
		t.Error("Did not find expected masquerade rule for DNS redirection")
	}
}

func TestGenerateDNSInspectRules_Wildcard(t *testing.T) {
	inspects := []config.DNSInspect{
		{
			Zone: "*",
			Mode: "redirect",
		},
	}

	zoneToIfaces := map[string][]string{
		"LAN":   {"eth1"},
		"Guest": {"eth2"},
		"WAN":   {"eth0"},
	}

	rules := GenerateDNSInspectRules(inspects, zoneToIfaces, "127.0.0.1", 53, nil)

	ifacesFound := make(map[string]bool)
	for _, r := range rules {
		if strings.Contains(r, "iifname \"eth1\"") && strings.Contains(r, "dnat") {
			ifacesFound["eth1"] = true
		}
		if strings.Contains(r, "iifname \"eth2\"") && strings.Contains(r, "dnat") {
			ifacesFound["eth2"] = true
		}
		if strings.Contains(r, "iifname \"eth0\"") && strings.Contains(r, "dnat") {
			ifacesFound["eth0"] = true
		}
	}

	if len(ifacesFound) != 3 {
		t.Errorf("Expected 3 interface redirect rules for wildcard, found %d", len(ifacesFound))
	}
}
