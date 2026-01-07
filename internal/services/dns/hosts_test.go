package dns

import (
	"grimm.is/glacic/internal/config"
	"os"
	"testing"
)

func TestService_LoadHostsFile(t *testing.T) {
	// Create a temporary hosts file
	tmpFile, err := os.CreateTemp("", "hosts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	content := `
127.0.0.1	localhost
192.168.1.50	test-server.lan test-server
# This is a comment
::1		localhost ip6-localhost ip6-loopback
`
	if _, err := tmpFile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatal(err)
	}

	// Setup service
	cfg := &config.DNSServer{Enabled: true}
	s, _ := newTestService(cfg)

	// Load the temporary hosts file
	s.loadHostsFile(tmpFile.Name())

	// Verify records
	tests := []struct {
		name string
		ip   string
		typ  string
	}{
		{"localhost.", "127.0.0.1", "A"},
		{"test-server.lan.", "192.168.1.50", "A"},
		{"test-server.", "192.168.1.50", "A"},
		{"ip6-localhost.", "::1", "AAAA"},
	}

	for _, tt := range tests {
		rec, ok := s.records[tt.name]
		if !ok {
			t.Errorf("Record %s not found", tt.name)
			continue
		}
		if rec.Value != tt.ip {
			t.Errorf("Record %s IP mismatch: got %s, want %s", tt.name, rec.Value, tt.ip)
		}
		if rec.Type != tt.typ {
			t.Errorf("Record %s Type mismatch: got %s, want %s", tt.name, rec.Type, tt.typ)
		}
	}
}
