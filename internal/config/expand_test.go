package config

import (
	"testing"
)

func TestExpandPolicies(t *testing.T) {
	zones := []Zone{
		{Name: "wan"},
		{Name: "lan"},
		{Name: "vpn_office"},
		{Name: "vpn_home"},
		{Name: "guest"},
	}

	tests := []struct {
		name     string
		policies []Policy
		expected int // expected number of expanded policies
	}{
		{
			name: "no wildcards",
			policies: []Policy{
				{From: "lan", To: "wan"},
			},
			expected: 1,
		},
		{
			name: "star to specific zone",
			policies: []Policy{
				{From: "*", To: "wan"},
			},
			// Expands to: lan->wan, vpn_office->wan, vpn_home->wan, guest->wan, self->wan
			// (wan->wan is skipped)
			expected: 5,
		},
		{
			name: "specific zone to star",
			policies: []Policy{
				{From: "wan", To: "*"},
			},
			// Expands to: wan->lan, wan->vpn_office, wan->vpn_home, wan->guest, wan->self
			// (wan->wan is skipped)
			expected: 5,
		},
		{
			name: "glob pattern",
			policies: []Policy{
				{From: "vpn*", To: "wan"},
			},
			// Expands to: vpn_office->wan, vpn_home->wan
			expected: 2,
		},
		{
			name: "star to star (all pairs)",
			policies: []Policy{
				{From: "*", To: "*"},
			},
			// All pairs except same-zone (6 zones * 5 = 30)
			expected: 30,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expanded := ExpandPolicies(tc.policies, zones)
			if len(expanded) != tc.expected {
				t.Errorf("expected %d policies, got %d", tc.expected, len(expanded))
				for _, p := range expanded {
					t.Logf("  %s -> %s", p.From, p.To)
				}
			}
		})
	}
}

func TestMatchesZone(t *testing.T) {
	tests := []struct {
		pattern  string
		zoneName string
		expected bool
	}{
		{"*", "wan", true},
		{"*", "anything", true},
		{"vpn*", "vpn_office", true},
		{"vpn*", "vpn_home", true},
		{"vpn*", "lan", false},
		{"*_office", "vpn_office", true},
		{"*_office", "lan_office", true},
		{"*_office", "vpn_home", false},
		{"lan", "lan", true},
		{"lan", "wan", false},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+"_"+tc.zoneName, func(t *testing.T) {
			result := matchesZone(tc.pattern, tc.zoneName)
			if result != tc.expected {
				t.Errorf("matchesZone(%q, %q) = %v, want %v", tc.pattern, tc.zoneName, result, tc.expected)
			}
		})
	}
}
