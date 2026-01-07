package firewall

import (
	"testing"
)

func TestGetService_Builtin(t *testing.T) {
	svc := GetService("ssh")
	if svc == nil {
		t.Fatal("GetService(ssh) returned nil")
	}
	if svc.Port != 22 {
		t.Errorf("ssh port = %d, want 22", svc.Port)
	}
}

func TestGetService_System(t *testing.T) {
	// "postgres" is typically in /etc/services as 5432/tcp
	// We assume the test environment has /etc/services or net.LookupPort works against it.
	// CI environments usually have this.
	svc := GetService("postgres")
	if svc == nil {
		t.Skip("postgres service not found in system (possibly missing /etc/services)")
	}

	if svc.Port != 5432 {
		t.Errorf("postgres port = %d, want 5432", svc.Port)
	}
	if svc.Protocol != ProtoTCP {
		t.Errorf("postgres proto = %v, want TCP", svc.Protocol)
	}
}

func TestSearchServices(t *testing.T) {
	// Should find builtin "ssh"
	results := SearchServices("ssh")
	foundSSH := false
	for _, s := range results {
		if s.Name == "ssh" {
			foundSSH = true
			break
		}
	}
	if !foundSSH {
		t.Error("SearchServices(ssh) did not return builtin ssh service")
	}

	// Should find system service "domain" (DNS 53)
	// Or some other common service. "http" is often aliased as "www".
	results = SearchServices("www")
	foundWWW := false
	for _, s := range results {
		if s.Name == "http" || s.Name == "www" { // /etc/services might name it http
			foundWWW = true
		}
	}
	if !foundWWW {
		// Don't fail hard because /etc/services might vary
		t.Log("SearchServices(www) did not return expected service (might be missing in /etc/services)")
	}
	// "http" is builtin, so check if search finds it when querying "http"
	results = SearchServices("http")
	foundHTTP := false
	for _, s := range results {
		if s.Name == "http" {
			foundHTTP = true
			break
		}
	}
	if !foundHTTP {
		t.Error("SearchServices(http) did not return builtin http service")
	}
}
