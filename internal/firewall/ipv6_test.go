//go:build linux
// +build linux

package firewall

import (
	"github.com/google/nftables/expr"
	"testing"
)

func TestBuildIPMatchIPv6(t *testing.T) {
	// Since buildIPMatch is a method on Manager, and it uses unexported fields/methods,
	// we are testing it via a Manager instance.
	// However, buildIPMatch doesn't use m.conn, so we can pass a dummy Manager.
	m := &Manager{}

	tests := []struct {
		name     string
		input    string
		isSrc    bool
		wantIPv6 bool
	}{
		{"IPv4", "192.168.1.1", true, false},
		{"IPv6", "2001:db8::1", true, true},
		{"IPv6 CIDR", "2001:db8::/64", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exprs := m.buildIPMatch(tt.input, tt.isSrc)
			// Check if we have the correct protocol match
			foundV6 := false
			for _, e := range exprs {
				if cmp, ok := e.(*expr.Cmp); ok {
					// Check for NFPROTO_IPV6 (10) vs NFPROTO_IPV4 (2)
					if len(cmp.Data) == 1 {
						if cmp.Data[0] == 10 { // unix.NFPROTO_IPV6
							foundV6 = true
						}
					}
				}
			}
			if foundV6 != tt.wantIPv6 {
				t.Errorf("buildIPMatch(%q) IPv6 match = %v, want %v", tt.input, foundV6, tt.wantIPv6)
			}
		})
	}
}

func TestBuildSetMatchIPv6(t *testing.T) {
	m := &Manager{}

	ipsetTypes := map[string]string{
		"v4set": "ipv4_addr",
		"v6set": "ipv6_addr",
	}

	tests := []struct {
		name    string
		setName string
		wantLen uint32 // Length of payload to load
	}{
		{"IPv4 Set", "v4set", 4},
		{"IPv6 Set", "v6set", 16},
		{"Default Set", "unknown", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exprs := m.buildSetMatch(tt.setName, true, ipsetTypes)
			// First expression should be Payload load
			if len(exprs) < 1 {
				t.Fatal("No expressions returned")
			}
			payload, ok := exprs[0].(*expr.Payload)
			if !ok {
				t.Fatal("First expression is not a Payload")
			}
			if payload.Len != tt.wantLen {
				t.Errorf("Payload length = %d, want %d", payload.Len, tt.wantLen)
			}
		})
	}
}
