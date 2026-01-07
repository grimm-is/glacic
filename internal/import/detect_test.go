package imports

import (
	"testing"
)

func TestGetFeatureDegradationWarnings_BothExternal(t *testing.T) {
	warnings := GetFeatureDegradationWarnings(true, true)
	if len(warnings) == 0 {
		t.Error("expected warnings for both external services")
	}

	// Should mention both DNS and DHCP
	found := false
	for _, w := range warnings {
		if w == "⚠️ Using external DNS and DHCP servers" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning about both external services")
	}
}

func TestGetFeatureDegradationWarnings_ExternalDNS(t *testing.T) {
	warnings := GetFeatureDegradationWarnings(true, false)
	if len(warnings) == 0 {
		t.Error("expected warnings for external DNS")
	}

	found := false
	for _, w := range warnings {
		if w == "⚠️ Using external DNS server" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning about external DNS")
	}
}

func TestGetFeatureDegradationWarnings_ExternalDHCP(t *testing.T) {
	warnings := GetFeatureDegradationWarnings(false, true)
	if len(warnings) == 0 {
		t.Error("expected warnings for external DHCP")
	}

	found := false
	for _, w := range warnings {
		if w == "⚠️ Using external DHCP server" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning about external DHCP")
	}
}

func TestGetFeatureDegradationWarnings_NoExternal(t *testing.T) {
	warnings := GetFeatureDegradationWarnings(false, false)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %d", len(warnings))
	}
}

func TestDeduplicateServices(t *testing.T) {
	services := []ExternalService{
		{Name: "dnsmasq", Type: "dns"},
		{Name: "dnsmasq", Type: "dns"}, // Duplicate
		{Name: "bind9", Type: "dns"},
		{Name: "dnsmasq", Type: "dhcp"}, // Different type, not duplicate
	}

	result := deduplicateServices(services)
	if len(result) != 3 {
		t.Errorf("expected 3 services after deduplication, got %d", len(result))
	}
}

func TestGetConflicts_DNSMasq(t *testing.T) {
	conflicts := getConflicts("dnsmasq", "dns")
	if len(conflicts) == 0 {
		t.Error("expected conflicts for dnsmasq dns")
	}

	conflicts = getConflicts("dnsmasq", "dhcp")
	if len(conflicts) == 0 {
		t.Error("expected conflicts for dnsmasq dhcp")
	}

	conflicts = getConflicts("dnsmasq", "both")
	if len(conflicts) < 2 {
		t.Error("expected multiple conflicts for dnsmasq both")
	}
}

func TestGetConflicts_Bind(t *testing.T) {
	conflicts := getConflicts("bind9", "dns")
	if len(conflicts) == 0 {
		t.Error("expected conflicts for bind9")
	}
}

func TestGetConflicts_ISCDhcp(t *testing.T) {
	conflicts := getConflicts("isc-dhcp-server", "dhcp")
	if len(conflicts) == 0 {
		t.Error("expected conflicts for isc-dhcp-server")
	}
}

func TestGetConflicts_SystemdResolved(t *testing.T) {
	conflicts := getConflicts("systemd-resolved", "dns")
	if len(conflicts) < 2 {
		t.Error("expected multiple advice for systemd-resolved")
	}
}

func TestExternalService_Struct(t *testing.T) {
	svc := ExternalService{
		Name:       "test-service",
		Type:       "dns",
		Running:    true,
		ConfigFile: "/etc/test.conf",
		Ports:      []int{53},
		PIDs:       []int{1234},
		Conflicts:  []string{"test conflict"},
	}

	if svc.Name != "test-service" {
		t.Error("Name not set correctly")
	}
	if !svc.Running {
		t.Error("Running not set correctly")
	}
	if len(svc.PIDs) != 1 || svc.PIDs[0] != 1234 {
		t.Error("PIDs not set correctly")
	}
}

func TestDetectionResult_Struct(t *testing.T) {
	result := &DetectionResult{
		DNSServices: []ExternalService{
			{Name: "dnsmasq", Type: "dns"},
		},
		DHCPServices: []ExternalService{
			{Name: "isc-dhcp-server", Type: "dhcp"},
		},
		Warnings: []string{"Port 53 is in use"},
	}

	if len(result.DNSServices) != 1 {
		t.Error("DNSServices not set correctly")
	}
	if len(result.DHCPServices) != 1 {
		t.Error("DHCPServices not set correctly")
	}
	if len(result.Warnings) != 1 {
		t.Error("Warnings not set correctly")
	}
}
