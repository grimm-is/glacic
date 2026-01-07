package config

import (
	"testing"
)

func TestZoneResolver_SimpleInterface(t *testing.T) {
	zones := []Zone{
		{Name: "wan", Interface: "eth0", DHCP: true},
		{Name: "lan", Interface: "eth1", IPv4: []string{"192.168.1.1/24"}},
	}

	resolver := NewZoneResolver(zones)

	// Test ResolveInterface
	if zone := resolver.ResolveInterface("eth0"); zone != "wan" {
		t.Errorf("ResolveInterface(eth0) = %q, want %q", zone, "wan")
	}
	if zone := resolver.ResolveInterface("eth1"); zone != "lan" {
		t.Errorf("ResolveInterface(eth1) = %q, want %q", zone, "lan")
	}
	if zone := resolver.ResolveInterface("eth2"); zone != "" {
		t.Errorf("ResolveInterface(eth2) = %q, want empty", zone)
	}
}

func TestZoneResolver_PrefixMatch(t *testing.T) {
	zones := []Zone{
		{Name: "vpn", Interface: "wg+"}, // + suffix = prefix match
	}

	resolver := NewZoneResolver(zones)

	// wg0, wg1, wg2 should all match
	for _, iface := range []string{"wg0", "wg1", "wg99"} {
		if zone := resolver.ResolveInterface(iface); zone != "vpn" {
			t.Errorf("ResolveInterface(%s) = %q, want %q", iface, zone, "vpn")
		}
	}

	// Non-matching interfaces
	if zone := resolver.ResolveInterface("eth0"); zone != "" {
		t.Errorf("ResolveInterface(eth0) = %q, want empty", zone)
	}
}

func TestZoneResolver_MatchBlocks(t *testing.T) {
	zones := []Zone{
		{
			Name: "dmz",
			Matches: []ZoneMatch{
				{Interface: "eth2"},
				{Interface: "eth3"},
			},
		},
	}

	resolver := NewZoneResolver(zones)

	matches := resolver.GetEffectiveMatches("dmz")
	if len(matches) != 2 {
		t.Fatalf("GetEffectiveMatches(dmz) = %d matches, want 2", len(matches))
	}
	if matches[0].Interface != "eth2" {
		t.Errorf("matches[0].Interface = %q, want %q", matches[0].Interface, "eth2")
	}
	if matches[1].Interface != "eth3" {
		t.Errorf("matches[1].Interface = %q, want %q", matches[1].Interface, "eth3")
	}
}

func TestZoneResolver_AdditiveInheritance(t *testing.T) {
	zones := []Zone{
		{
			Name: "guest",
			Src:  "192.168.10.0/24", // Global src
			Matches: []ZoneMatch{
				{Interface: "eth1"},
				{Interface: "wlan0"},
			},
		},
	}

	resolver := NewZoneResolver(zones)

	matches := resolver.GetEffectiveMatches("guest")
	if len(matches) != 2 {
		t.Fatalf("GetEffectiveMatches(guest) = %d matches, want 2", len(matches))
	}

	// Both matches should inherit the global Src
	for i, m := range matches {
		if m.Src != "192.168.10.0/24" {
			t.Errorf("matches[%d].Src = %q, want %q", i, m.Src, "192.168.10.0/24")
		}
	}
}

func TestZoneResolver_DeprecatedInterfaces(t *testing.T) {
	zones := []Zone{
		{
			Name:       "lan",
			Interfaces: []string{"eth1", "eth2"}, // Deprecated format
		},
	}

	resolver := NewZoneResolver(zones)

	// Should still resolve correctly
	if zone := resolver.ResolveInterface("eth1"); zone != "lan" {
		t.Errorf("ResolveInterface(eth1) = %q, want %q", zone, "lan")
	}
	if zone := resolver.ResolveInterface("eth2"); zone != "lan" {
		t.Errorf("ResolveInterface(eth2) = %q, want %q", zone, "lan")
	}

	// Should create two effective matches
	matches := resolver.GetEffectiveMatches("lan")
	if len(matches) != 2 {
		t.Fatalf("GetEffectiveMatches(lan) = %d matches, want 2", len(matches))
	}
}

func TestZoneResolver_NFTMatch_Simple(t *testing.T) {
	zones := []Zone{
		{Name: "wan", Interface: "eth0"},
	}

	resolver := NewZoneResolver(zones)

	nft := resolver.GetZoneNFTMatch("wan", "in")
	if nft != `iifname "eth0"` {
		t.Errorf("GetZoneNFTMatch(wan, in) = %q, want %q", nft, `iifname "eth0"`)
	}

	nft = resolver.GetZoneNFTMatch("wan", "out")
	if nft != `oifname "eth0"` {
		t.Errorf("GetZoneNFTMatch(wan, out) = %q, want %q", nft, `oifname "eth0"`)
	}
}

func TestZoneResolver_NFTMatch_Prefix(t *testing.T) {
	zones := []Zone{
		{Name: "vpn", Interface: "wg+"}, // + suffix = prefix match
	}

	resolver := NewZoneResolver(zones)

	nft := resolver.GetZoneNFTMatch("vpn", "in")
	if nft != `iifname "wg*"` {
		t.Errorf("GetZoneNFTMatch(vpn, in) = %q, want %q", nft, `iifname "wg*"`)
	}
}

func TestZoneResolver_NFTMatch_WithSrc(t *testing.T) {
	zones := []Zone{
		{Name: "guest", Interface: "eth1", Src: "192.168.10.0/24"},
	}

	resolver := NewZoneResolver(zones)

	nft := resolver.GetZoneNFTMatch("guest", "in")
	expected := `iifname "eth1" ip saddr 192.168.10.0/24`
	if nft != expected {
		t.Errorf("GetZoneNFTMatch(guest, in) = %q, want %q", nft, expected)
	}
}

func TestZoneResolver_NFTMatch_MultipleInterfaces(t *testing.T) {
	zones := []Zone{
		{
			Name: "dmz",
			Matches: []ZoneMatch{
				{Interface: "eth2"},
				{Interface: "eth3"},
			},
		},
	}

	resolver := NewZoneResolver(zones)

	nft := resolver.GetZoneNFTMatch("dmz", "in")
	expected := `iifname { "eth2", "eth3" }`
	if nft != expected {
		t.Errorf("GetZoneNFTMatch(dmz, in) = %q, want %q", nft, expected)
	}
}

func TestZoneResolver_GetZoneInterfaces(t *testing.T) {
	zones := []Zone{
		{
			Name: "dmz",
			Matches: []ZoneMatch{
				{Interface: "eth2"},
				{Interface: "eth3"},
			},
		},
		{Name: "vpn", Interface: "wg+"}, // + suffix = prefix match
	}

	resolver := NewZoneResolver(zones)

	// DMZ should return exact interfaces
	ifaces := resolver.GetZoneInterfaces("dmz")
	if len(ifaces) != 2 {
		t.Errorf("GetZoneInterfaces(dmz) = %v, want 2 interfaces", ifaces)
	}

	// VPN should return prefix with wildcard
	ifaces = resolver.GetZoneInterfaces("vpn")
	if len(ifaces) != 1 || ifaces[0] != "wg*" {
		t.Errorf("GetZoneInterfaces(vpn) = %v, want [wg*]", ifaces)
	}
}
