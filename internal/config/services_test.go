package config

import (
	"testing"
)

func TestLookupService(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		wantFound   bool
		wantPorts   int
	}{
		{"http exists", "http", true, 1},
		{"https exists", "https", true, 1},
		{"ssh exists", "ssh", true, 1},
		{"dns exists with multiple ports", "dns", true, 2}, // TCP and UDP
		{"unknown service", "nonexistent", false, 0},
		{"case sensitivity", "HTTP", false, 0}, // Should be lowercase
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, found := LookupService(tt.serviceName)
			if found != tt.wantFound {
				t.Errorf("LookupService(%q) found = %v, want %v", tt.serviceName, found, tt.wantFound)
			}
			if found && len(svc.Ports) != tt.wantPorts {
				t.Errorf("LookupService(%q) ports = %d, want %d", tt.serviceName, len(svc.Ports), tt.wantPorts)
			}
		})
	}
}

func TestLookupServicePorts(t *testing.T) {
	// Test specific port values
	tests := []struct {
		service  string
		port     int
		protocol string
	}{
		{"http", 80, "tcp"},
		{"https", 443, "tcp"},
		{"ssh", 22, "tcp"},
		{"dhcp", 67, "udp"},
		{"ntp", 123, "udp"},
		{"mysql", 3306, "tcp"},
		{"postgres", 5432, "tcp"},
		{"wireguard", 51820, "udp"},
	}

	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			svc, found := LookupService(tt.service)
			if !found {
				t.Fatalf("Service %q not found", tt.service)
			}

			var foundPort bool
			for _, p := range svc.Ports {
				if p.Port == tt.port && p.Protocol == tt.protocol {
					foundPort = true
					break
				}
			}
			if !foundPort {
				t.Errorf("Service %q missing port %d/%s", tt.service, tt.port, tt.protocol)
			}
		})
	}
}

func TestListServices(t *testing.T) {
	services := ListServices()
	if len(services) == 0 {
		t.Error("ListServices() returned empty list")
	}

	// Check that all listed services can be looked up
	for _, name := range services {
		if _, found := LookupService(name); !found {
			t.Errorf("Listed service %q not found in lookup", name)
		}
	}

	// Check for expected services
	expectedServices := []string{"http", "https", "ssh", "dns", "dhcp", "smtp", "ftp"}
	for _, expected := range expectedServices {
		found := false
		for _, svc := range services {
			if svc == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected service %q not in list", expected)
		}
	}
}

func TestServicePortRanges(t *testing.T) {
	// Test services with port ranges
	svc, found := LookupService("ftp-data")
	if !found {
		t.Skip("ftp-data service not defined")
	}

	for _, p := range svc.Ports {
		if p.EndPort > 0 && p.EndPort <= p.Port {
			t.Errorf("Invalid port range: %d-%d", p.Port, p.EndPort)
		}
	}
}

func TestBuiltinServicesConsistency(t *testing.T) {
	// Verify all builtin services have required fields
	for name, svc := range BuiltinServices {
		if svc.Description == "" {
			t.Errorf("Service %q has no description", name)
		}
		if len(svc.Ports) == 0 {
			t.Errorf("Service %q has no ports defined", name)
		}
		for i, p := range svc.Ports {
			// Port 0 is valid for ICMP and "any" protocol
			if p.Protocol != "icmp" && p.Protocol != "any" {
				if p.Port <= 0 || p.Port > 65535 {
					t.Errorf("Service %q port[%d] has invalid port: %d", name, i, p.Port)
				}
			}
			validProtocols := map[string]bool{"tcp": true, "udp": true, "icmp": true, "any": true}
			if !validProtocols[p.Protocol] {
				t.Errorf("Service %q port[%d] has invalid protocol: %s", name, i, p.Protocol)
			}
		}
	}
}
