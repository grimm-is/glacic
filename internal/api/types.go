package api

import (
	"grimm.is/glacic/internal/config"
)

// RuleWithStats enriches the static config with runtime data.
// This is the primary response object for the ClearPath Policy Editor.
type RuleWithStats struct {
	config.PolicyRule

	// Policy context (for grouping)
	PolicyFrom string `json:"policy_from"`
	PolicyTo   string `json:"policy_to"`

	// Runtime Observability
	Stats RuleStats `json:"stats"`

	// UI Helpers (The "Pills")
	ResolvedSrc  *ResolvedAddress `json:"resolved_src,omitempty"`
	ResolvedDest *ResolvedAddress `json:"resolved_dest,omitempty"`

	// Power User Info
	GeneratedSyntax string `json:"nft_syntax,omitempty"`
}

// RuleStats holds runtime performance data for sparklines.
type RuleStats struct {
	// Cumulative counters (from nftables)
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`

	// Live Rate (Bytes/sec) for Sparklines
	// 60 points = 120 seconds of history
	SparklineData []float64 `json:"sparkline_data"`
}

// ResolvedAddress drives the "Pill" UI components.
// Used to display human-readable names for IPs, aliases, and IPSets.
type ResolvedAddress struct {
	DisplayName string `json:"display_name"` // e.g., "LAN_Nets" or "My iPhone"
	Type        string `json:"type"`         // "alias", "ipset", "host", "ip", "cidr", "any"

	// Details for the popover
	Description string   `json:"description,omitempty"`
	Count       int      `json:"count"`        // Total items in list
	IsTruncated bool     `json:"is_truncated"` // True if > 20 items
	Preview     []string `json:"preview"`      // The first 20 items
}

// PolicyWithStats enriches a Policy with runtime stats for all its rules.
type PolicyWithStats struct {
	config.Policy
	Rules []RuleWithStats `json:"rules"`
}
