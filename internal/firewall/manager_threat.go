//go:build linux

package firewall

import (
	"fmt"

	"github.com/google/nftables"
)

// applyThreatIntelRules ensures threat sets exist and adds blocking rules.
func (m *Manager) applyThreatIntelRules(ipsetMgr *IPSetManager, inputChain, forwardChain *nftables.Chain) error {
	sets := []struct {
		Name string
		Type SetType
	}{
		{"threat_v4", SetTypeIPv4Addr},
		{"threat_v6", SetTypeIPv6Addr},
	}

	for _, s := range sets {
		// Ensure set exists (ignore error if it does)
		_ = ipsetMgr.CreateSet(s.Name, s.Type, "interval")

		// Add DROP rule to INPUT
		if err := ipsetMgr.CreateBlockingRule(s.Name, s.Type, inputChain.Name, "drop", true, false); err != nil {
			return fmt.Errorf("failed to create input drop rule for %s: %w", s.Name, err)
		}

		// Add DROP rule to FORWARD
		if err := ipsetMgr.CreateBlockingRule(s.Name, s.Type, forwardChain.Name, "drop", true, true); err != nil {
			return fmt.Errorf("failed to create forward drop rule for %s: %w", s.Name, err)
		}
	}
	return nil
}
