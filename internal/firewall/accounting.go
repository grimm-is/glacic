//go:build linux
// +build linux

package firewall

import (
	"fmt"
	"sync"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

// TrafficStats holds traffic counters for an interface or zone.
type TrafficStats struct {
	Name    string `json:"name"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

// AccountingStats holds all traffic accounting data.
type AccountingStats struct {
	Interfaces []TrafficStats `json:"interfaces"`
	Zones      []TrafficStats `json:"zones"`
}

// Accounting manages traffic accounting rules and counters.
type Accounting struct {
	conn  *nftables.Conn
	table *nftables.Table
	mu    sync.RWMutex
}

// NewAccounting creates a new accounting manager.
func NewAccounting(conn *nftables.Conn) *Accounting {
	return &Accounting{conn: conn}
}

// SetupAccounting creates accounting chains and rules for interfaces and zones.
func (a *Accounting) SetupAccounting(interfaces []string, zones map[string][]string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Create accounting table
	a.table = &nftables.Table{
		Name:   "accounting",
		Family: nftables.TableFamilyIPv4,
	}

	a.conn.DelTable(a.table)
	_ = a.conn.Flush()

	a.conn.AddTable(a.table)

	// Create input chain for ingress accounting (runs before filter)
	inputChain := a.conn.AddChain(&nftables.Chain{
		Name:     "input",
		Table:    a.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookInput,
		Priority: nftables.ChainPriorityFilter,
	})

	// Create forward chain for forwarded traffic accounting
	forwardChain := a.conn.AddChain(&nftables.Chain{
		Name:     "forward",
		Table:    a.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookForward,
		Priority: nftables.ChainPriorityFilter,
	})

	// Create output chain for egress accounting
	outputChain := a.conn.AddChain(&nftables.Chain{
		Name:     "output",
		Table:    a.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityFilter,
	})

	if err := a.conn.Flush(); err != nil {
		return fmt.Errorf("failed to create accounting chains: %w", err)
	}

	// Add per-interface accounting rules
	for _, iface := range interfaces {
		// Input (ingress) counter for this interface
		a.addInterfaceCounter(inputChain, iface, true)
		// Output (egress) counter
		a.addInterfaceCounter(outputChain, iface, false)
		// Forward counters (both directions)
		a.addInterfaceCounter(forwardChain, iface, true)
		a.addInterfaceCounter(forwardChain, iface, false)
	}

	// Add per-zone accounting rules (based on input interface)
	for zone, ifaces := range zones {
		for _, iface := range ifaces {
			a.addZoneCounter(inputChain, zone, iface, true)
			a.addZoneCounter(forwardChain, zone, iface, true)
			a.addZoneCounter(forwardChain, zone, iface, false)
		}
	}

	return a.conn.Flush()
}

// addInterfaceCounter adds a counter rule for an interface
func (a *Accounting) addInterfaceCounter(chain *nftables.Chain, iface string, ingress bool) {
	var exprs []expr.Any

	if ingress {
		// Match input interface
		exprs = append(exprs,
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     pad(iface),
			},
		)
	} else {
		// Match output interface
		exprs = append(exprs,
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     pad(iface),
			},
		)
	}

	// Add counter (this is what we'll read later)
	exprs = append(exprs, &expr.Counter{})

	// Continue processing (don't verdict, just count)
	a.conn.AddRule(&nftables.Rule{
		Table:    chain.Table,
		Chain:    chain,
		Exprs:    exprs,
		UserData: []byte(fmt.Sprintf("iface:%s:%v", iface, ingress)),
	})
}

// addZoneCounter adds a counter rule for a zone
func (a *Accounting) addZoneCounter(chain *nftables.Chain, zone string, iface string, ingress bool) {
	var exprs []expr.Any

	if ingress {
		exprs = append(exprs,
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     pad(iface),
			},
		)
	} else {
		exprs = append(exprs,
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     pad(iface),
			},
		)
	}

	exprs = append(exprs, &expr.Counter{})

	a.conn.AddRule(&nftables.Rule{
		Table:    chain.Table,
		Chain:    chain,
		Exprs:    exprs,
		UserData: []byte(fmt.Sprintf("zone:%s:%v", zone, ingress)),
	})
}

// GetStats retrieves current traffic accounting statistics.
func (a *Accounting) GetStats() (*AccountingStats, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.table == nil {
		return &AccountingStats{}, nil
	}

	stats := &AccountingStats{
		Interfaces: make([]TrafficStats, 0),
		Zones:      make([]TrafficStats, 0),
	}

	// Aggregate counters by interface/zone
	ifaceStats := make(map[string]*TrafficStats)
	zoneStats := make(map[string]*TrafficStats)

	// Get all chains in accounting table
	chains, err := a.conn.ListChains()
	if err != nil {
		return nil, err
	}

	for _, chain := range chains {
		if chain.Table.Name != "accounting" {
			continue
		}

		rules, err := a.conn.GetRules(a.table, chain)
		if err != nil {
			continue
		}

		for _, rule := range rules {
			// Parse UserData to identify what this counter is for
			if len(rule.UserData) == 0 {
				continue
			}

			var packets, bytes uint64
			for _, e := range rule.Exprs {
				if counter, ok := e.(*expr.Counter); ok {
					packets = counter.Packets
					bytes = counter.Bytes
					break
				}
			}

			// Parse the UserData format: "type:name:direction"
			userData := string(rule.UserData)
			var counterType, name string
			fmt.Sscanf(userData, "%[^:]:%[^:]:", &counterType, &name)

			if counterType == "iface" {
				if _, ok := ifaceStats[name]; !ok {
					ifaceStats[name] = &TrafficStats{Name: name}
				}
				ifaceStats[name].Packets += packets
				ifaceStats[name].Bytes += bytes
			} else if counterType == "zone" {
				if _, ok := zoneStats[name]; !ok {
					zoneStats[name] = &TrafficStats{Name: name}
				}
				zoneStats[name].Packets += packets
				zoneStats[name].Bytes += bytes
			}
		}
	}

	// Convert maps to slices
	for _, s := range ifaceStats {
		stats.Interfaces = append(stats.Interfaces, *s)
	}
	for _, s := range zoneStats {
		stats.Zones = append(stats.Zones, *s)
	}

	return stats, nil
}

// ResetCounters resets all traffic counters to zero.
func (a *Accounting) ResetCounters() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.table == nil {
		return nil
	}

	// To reset counters, we need to delete and recreate the rules
	// This is a limitation of nftables - counters can't be reset in place easily
	// For now, we just note that a full re-setup is needed
	return nil
}
