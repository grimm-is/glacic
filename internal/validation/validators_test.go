package validation

import (
	"strings"
	"testing"
)

func TestValidateInterfaceName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Happy paths
		{"simple", "eth0", false},
		{"with dash", "eth-0", false},
		{"with underscore", "eth_0", false},
		{"with dot (vlan)", "eth0.100", false},
		{"max length", "eth0123456789ab", false}, // 15 chars

		// Sad paths
		{"empty", "", true},
		{"too long", "eth01234567890123", true}, // 17 chars
		{"space", "eth 0", true},
		{"semicolon injection", "eth0;rm", true},
		{"pipe injection", "eth0|cat", true},
		{"ampersand", "eth0&", true},
		{"dollar sign", "eth0$USER", true},
		{"backtick", "eth0`whoami`", true},
		{"parentheses", "eth0()", true},
		{"redirect", "eth0>file", true},
		{"backslash", "eth0\\n", true},
		{"newline", "eth0\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInterfaceName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInterfaceName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Happy paths
		{"simple", "my-policy", false},
		{"underscore", "zone_lan", false},
		{"alphanumeric", "policy123", false},

		// Sad paths
		{"empty", "", true},
		{"space", "my policy", true},
		{"dot", "my.policy", true},
		{"semicolon", "policy;drop", true},
		{"long", strings.Repeat("a", 256), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIdentifier(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIdentifier(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	allowedDirs := []string{"/etc/glacic", "/var/lib/glacic"}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		// Happy paths
		{"relative", "config.hcl", false},
		{"allowed absolute", "/etc/glacic/firewall.hcl", false},
		{"allowed subdir", "/var/lib/glacic/state/db", false},

		// Sad paths
		{"empty", "", true},
		{"path traversal", "../../../etc/passwd", true},
		{"absolute not allowed", "/etc/passwd", true},
		{"null byte", "/etc/glacic/config\x00.hcl", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path, allowedDirs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestValidateIPOrCIDR(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Happy paths - IPs
		{"ipv4", "192.168.1.1", false},
		{"ipv6", "2001:db8::1", false},
		{"ipv4 loopback", "127.0.0.1", false},

		// Happy paths - CIDRs
		{"ipv4 cidr", "192.168.1.0/24", false},
		{"ipv6 cidr", "2001:db8::/32", false},

		// Sad paths
		{"empty", "", true},
		{"invalid ip", "999.999.999.999", true},
		{"invalid cidr", "192.168.1.0/99", true},
		{"text", "not-an-ip", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIPOrCIDR(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIPOrCIDR(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAllowlist(t *testing.T) {
	allowed := []string{"tcp", "udp", "icmp"}

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"in list", "tcp", false},
		{"in list 2", "udp", false},
		{"not in list", "sctp", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAllowlist(tt.value, allowed)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAllowlist(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePortNumber(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{"min valid", 1, false},
		{"http", 80, false},
		{"https", 443, false},
		{"max valid", 65535, false},

		{"zero", 0, true},
		{"negative", -1, true},
		{"too high", 65536, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePortNumber(tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePortNumber(%d) error = %v, wantErr %v", tt.port, err, tt.wantErr)
			}
		})
	}
}

func TestValidateProtocol(t *testing.T) {
	tests := []struct {
		name    string
		proto   string
		wantErr bool
	}{
		{"tcp", "tcp", false},
		{"udp", "udp", false},
		{"icmp", "icmp", false},
		{"icmpv6", "icmpv6", false},
		{"uppercase", "TCP", false}, // Should be case-insensitive

		{"invalid", "xyz", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProtocol(tt.proto)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProtocol(%q) error = %v, wantErr %v", tt.proto, err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"clean", "hello", "hello"},
		{"semicolon", "hello;world", "helloworld"},
		{"pipe", "a|b", "ab"},
		{"multiple", "a;b|c&d", "abcd"},
		{"quotes", "a\"b'c", "abc"},
		{"newlines", "a\nb\rc", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeString(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
