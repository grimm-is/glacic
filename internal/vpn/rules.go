//go:build linux

package vpn

import (
	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

// GenerateLockoutProtectionRules generates nftables rules that ensure
// Tailscale traffic is ALWAYS accepted, regardless of other firewall rules.
// These rules should be added FIRST in the input chain.
func GenerateLockoutProtectionRules(table *nftables.Table, inputChain *nftables.Chain, iface string) []*nftables.Rule {
	if iface == "" {
		iface = "tailscale0"
	}

	// Pad interface name to 16 bytes (IFNAMSIZ)
	ifaceBytes := make([]byte, 16)
	copy(ifaceBytes, iface)

	rules := []*nftables.Rule{
		// Accept all input from Tailscale interface
		{
			Table: table,
			Chain: inputChain,
			Exprs: []expr.Any{
				// Match input interface
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     ifaceBytes,
				},
				// Accept
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
			UserData: []byte("tailscale-lockout-protection"),
		},
	}

	return rules
}

// GenerateForwardRules generates rules for Tailscale subnet routing.
// These allow traffic to flow between Tailscale and local networks.
func GenerateForwardRules(table *nftables.Table, forwardChain *nftables.Chain, iface string, localInterfaces []string) []*nftables.Rule {
	if iface == "" {
		iface = "tailscale0"
	}

	ifaceBytes := make([]byte, 16)
	copy(ifaceBytes, iface)

	var rules []*nftables.Rule

	// Allow forwarding FROM Tailscale to local interfaces
	for _, localIface := range localInterfaces {
		localIfaceBytes := make([]byte, 16)
		copy(localIfaceBytes, localIface)

		rules = append(rules, &nftables.Rule{
			Table: table,
			Chain: forwardChain,
			Exprs: []expr.Any{
				// Match input interface (tailscale)
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     ifaceBytes,
				},
				// Match output interface (local)
				&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     localIfaceBytes,
				},
				// Accept
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
			UserData: []byte("tailscale-forward-to-" + localIface),
		})
	}

	// Allow forwarding TO Tailscale from local interfaces (return traffic)
	for _, localIface := range localInterfaces {
		localIfaceBytes := make([]byte, 16)
		copy(localIfaceBytes, localIface)

		rules = append(rules, &nftables.Rule{
			Table: table,
			Chain: forwardChain,
			Exprs: []expr.Any{
				// Match input interface (local)
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     localIfaceBytes,
				},
				// Match output interface (tailscale)
				&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     ifaceBytes,
				},
				// Accept
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
			UserData: []byte("tailscale-forward-from-" + localIface),
		})
	}

	return rules
}

// GenerateOutputRules generates rules allowing output to Tailscale.
func GenerateOutputRules(table *nftables.Table, outputChain *nftables.Chain, iface string) []*nftables.Rule {
	if iface == "" {
		iface = "tailscale0"
	}

	ifaceBytes := make([]byte, 16)
	copy(ifaceBytes, iface)

	return []*nftables.Rule{
		// Accept all output to Tailscale interface
		{
			Table: table,
			Chain: outputChain,
			Exprs: []expr.Any{
				// Match output interface
				&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
				&expr.Cmp{
					Op:       expr.CmpOpEq,
					Register: 1,
					Data:     ifaceBytes,
				},
				// Accept
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
			UserData: []byte("tailscale-output"),
		},
	}
}

// RuleComment returns a comment for Tailscale rules.
func RuleComment(description string) []byte {
	return []byte("tailscale: " + description)
}
