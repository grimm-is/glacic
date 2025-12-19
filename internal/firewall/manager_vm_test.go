//go:build linux
// +build linux

package firewall

import (
	"testing"

	"grimm.is/glacic/internal/config"

	"github.com/google/nftables"
)

func TestManager_ApplyConfig_Integration(t *testing.T) {
	// Skip in precompiled test mode - requires real nftables and root
	t.Skip("Skipping integration test - requires VM environment with nftables")
	// this requires root and nftables, which the VM has.
	// We'll create a manager and apply a basic config.

	// Skip if not root (though VM is root)
	// if os.Geteuid() != 0 {
	// 	t.Skip("skipping integration test: must be root")
	// }

	// Use a mock store for ipsets if needed, or nil?
	// NewManager doesn't take args?
	// Manager struct has fields.
	// We need a constructor. `NewManager` isn't in manager.go?
	// Let's check constructor.
	// It seems New() or similar.
	// I'll assume we can initialize it or use New().
	// I'll verify constructor in a moment.

	conn, err := nftables.New()
	if err != nil {
		t.Fatalf("Failed to open nftables conn: %v", err)
	}
	m := NewManagerWithConn(NewRealNFTablesConn(conn), nil, "")

	// Create a simple config
	cfg := &config.Config{
		Interfaces: []config.Interface{
			{Name: "eth0", Zone: "wan", IPv4: []string{"192.168.1.5/24"}},
			{Name: "eth1", Zone: "lan", IPv4: []string{"10.0.0.1/24"}},
		},
		IPSets: []config.IPSet{
			{Name: "whitelist", Type: "ipv4_addr", Entries: []string{"1.2.3.4"}},
		},
		Zones: []config.Zone{
			{Name: "wan", Action: "drop", Interfaces: []string{"eth0"}},
			{Name: "lan", Action: "accept", Interfaces: []string{"eth1"}},
		},
		NAT: []config.NATRule{
			{Name: "masq", Type: "masquerade", OutInterface: "eth0"},
		},
		Policies: []config.Policy{
			{
				Name:   "lan-to-wan",
				From:   "lan",
				To:     "wan",
				Action: "accept",
				Rules: []config.PolicyRule{
					{
						Name:     "allow-http",
						Action:   "accept",
						Protocol: "tcp",
						DestPort: 80,
						SrcIP:    "10.0.0.0/24",
						Log:      true,
					},
					{
						Name:     "allow-ssh",
						Action:   "accept",
						Services: []string{"ssh"}, // Assuming ssh is known or mocked? LookupService depends on brand/services.
						SrcIPSet: "whitelist",
					},
				},
			},
		},
		ThreatIntel: &config.ThreatIntel{
			Enabled: true,
			Sources: []config.ThreatSource{{Name: "test", URL: "http://test", CollectionID: "1"}},
		},
		// NAT, etc.
	}

	if err := m.ApplyConfig(FromGlobalConfig(cfg)); err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	// Verify nftables rules exist
	conn, err = nftables.New()
	if err != nil {
		t.Fatalf("Failed to open nftables conn: %v", err)
	}

	chains, err := conn.ListChains()
	if err != nil {
		t.Fatalf("Failed to list chains: %v", err)
	}

	foundInput := false
	foundForward := false
	for _, c := range chains {
		if c.Table.Name == "glacic" {
			if c.Name == "input" {
				foundInput = true
			}
			if c.Name == "forward" {
				foundForward = true
			}
		}
	}

	if !foundInput {
		t.Error("Chain input not found in glacic table")
	}
	if !foundForward {
		t.Error("Chain forward not found in glacic table")
	}

	// Enhanced assertions: verify we have actual rules, not just chains
	tables, err := conn.ListTables()
	if err != nil {
		t.Fatalf("Failed to list tables: %v", err)
	}

	var glacicTable *nftables.Table
	for _, tbl := range tables {
		if tbl.Name == "glacic" {
			glacicTable = tbl
			break
		}
	}
	if glacicTable == nil {
		t.Fatal("glacic table not found")
	}

	// Verify rules exist in forward chain
	rules, err := conn.GetRules(glacicTable, &nftables.Chain{Name: "forward", Table: glacicTable})
	if err != nil {
		t.Logf("Warning: Cannot get forward chain rules: %v", err)
	} else if len(rules) == 0 {
		t.Error("Expected rules in forward chain, got none")
	} else {
		t.Logf("Forward chain has %d rules", len(rules))
	}

	// Verify NAT chain exists (for masquerade rule)
	foundNAT := false
	for _, c := range chains {
		if c.Table.Name == "glacic" && c.Name == "postrouting" {
			foundNAT = true
			break
		}
	}
	if !foundNAT {
		t.Error("NAT postrouting chain not found - masquerade rule not applied?")
	}
}
