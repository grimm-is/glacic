//go:build linux

package firewall

import (
	"testing"

	"github.com/google/nftables/expr"
)

func TestBuildIPMatch(t *testing.T) {
	m := &Manager{} // Mock manager

	tests := []struct {
		name           string
		cidr           string
		isSrc          bool
		isIPv6         bool
		expectedOffset uint32
	}{
		{"IPv4 Src", "192.168.1.1", true, false, 12},
		{"IPv4 Dst", "10.0.0.0/24", false, false, 16},
		{"IPv6 Src", "2001:db8::1", true, true, 8},
		{"IPv6 Dst", "2001:db8::/32", false, true, 24},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exprs := m.buildIPMatch(tc.cidr, tc.isSrc)
			if len(exprs) < 3 {
				t.Fatalf("Expected at least 3 expressions (meta, payload, cmp), got %d", len(exprs))
			}

			// Check for Payload expression to verify offset
			foundPayload := false
			for _, e := range exprs {
				if p, ok := e.(*expr.Payload); ok {
					foundPayload = true
					if p.Offset != tc.expectedOffset {
						t.Errorf("Expected offset %d, got %d", tc.expectedOffset, p.Offset)
					}
					// Basic check for base
					if p.Base != expr.PayloadBaseNetworkHeader {
						t.Error("Expected NetworkHeader base")
					}
				}
			}
			if !foundPayload {
				t.Error("No payload expression found")
			}
		})
	}
}

func TestBuildLimitExpr(t *testing.T) {
	m := &Manager{}

	tests := []struct {
		input        string
		valid        bool
		expectedRate uint64
	}{
		{"10/second", true, 10},
		{"100/m", true, 100},
		{"invalid", false, 0},
		{"10/year", false, 0},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			exprs := m.buildLimitExpr(tc.input)
			if tc.valid {
				if len(exprs) != 1 {
					t.Errorf("Expected 1 expression, got %d", len(exprs))
					return
				}
				lim, ok := exprs[0].(*expr.Limit)
				if !ok {
					t.Error("Expected Limit expression")
					return
				}
				if lim.Rate != tc.expectedRate {
					t.Errorf("Expected rate %d, got %d", tc.expectedRate, lim.Rate)
				}
			} else {
				if exprs != nil {
					t.Error("Expected nil for invalid input")
				}
			}
		})
	}
}

func TestBuildSetMatch(t *testing.T) {
	m := &Manager{}
	types := map[string]string{
		"v4set": "ipv4_addr",
		"v6set": "ipv6_addr",
	}

	exprs := m.buildSetMatch("v4set", true, types)
	if len(exprs) != 2 {
		t.Fatalf("Expected 2 exprs, got %d", len(exprs))
	}

	// Verify it uses IPv4 source offset (12)
	p, ok := exprs[0].(*expr.Payload)
	if !ok || p.Offset != 12 {
		t.Error("Expected IPv4 source payload")
	}

	l, ok := exprs[1].(*expr.Lookup)
	if !ok || l.SetName != "v4set" {
		t.Error("Expected lookup for v4set")
	}

	// Test v6
	exprs = m.buildSetMatch("v6set", false, types)
	p, ok = exprs[0].(*expr.Payload)
	if !ok || p.Offset != 24 { // IPv6 dest offset
		t.Error("Expected IPv6 dst payload offset")
	}
}
