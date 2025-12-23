package stats

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// NFTFetcher implements CounterFetcher using nft CLI.
type NFTFetcher struct{}

// FetchCounters executes `nft -j list ruleset` and extracts rule counters.
func (f *NFTFetcher) FetchCounters() (map[string]uint64, error) {
	cmd := exec.Command("nft", "-j", "list", "ruleset")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nft command failed: %w", err)
	}
	return ParseNFTCounters(output)
}

// nftRuleset represents the top-level nft JSON output.
type nftRuleset struct {
	Nftables []nftElement `json:"nftables"`
}

// nftElement represents a single element in the nftables array.
// Only one of these fields will be non-nil per element.
type nftElement struct {
	Metainfo *json.RawMessage `json:"metainfo,omitempty"`
	Table    *json.RawMessage `json:"table,omitempty"`
	Chain    *json.RawMessage `json:"chain,omitempty"`
	Rule     *nftRule         `json:"rule,omitempty"`
	Set      *json.RawMessage `json:"set,omitempty"`
	Map      *json.RawMessage `json:"map,omitempty"`
}

// nftRule represents a rule in nft JSON.
type nftRule struct {
	Family string          `json:"family"`
	Table  string          `json:"table"`
	Chain  string          `json:"chain"`
	Handle int             `json:"handle"`
	Expr   []nftExpression `json:"expr"`
}

// nftExpression represents an expression in a rule.
// We only care about counter and comment.
type nftExpression struct {
	Counter *nftCounter `json:"counter,omitempty"`
	Comment *string     `json:"comment,omitempty"`
}

// nftCounter holds counter values.
type nftCounter struct {
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

// ParseNFTCounters parses nft JSON output and extracts rule ID -> bytes mapping.
// It looks for rules with comments matching "rule:{id}" pattern.
func ParseNFTCounters(jsonData []byte) (map[string]uint64, error) {
	var ruleset nftRuleset
	if err := json.Unmarshal(jsonData, &ruleset); err != nil {
		return nil, fmt.Errorf("failed to parse nft JSON: %w", err)
	}

	result := make(map[string]uint64)

	for _, elem := range ruleset.Nftables {
		if elem.Rule == nil {
			continue
		}
		rule := elem.Rule

		// Extract counter and comment from expressions
		var bytes uint64
		var ruleID string
		hasCounter := false

		for _, expr := range rule.Expr {
			if expr.Counter != nil {
				bytes = expr.Counter.Bytes
				hasCounter = true
			}
			if expr.Comment != nil {
				// Parse comment for "rule:xxx" pattern
				if id := extractRuleID(*expr.Comment); id != "" {
					ruleID = id
				}
			}
		}

		// Only include if we have both counter and rule ID
		if hasCounter && ruleID != "" {
			result[ruleID] = bytes
		}
	}

	return result, nil
}

// extractRuleID extracts the rule ID from a comment like "rule:uuid-here".
func extractRuleID(comment string) string {
	const prefix = "rule:"
	if strings.HasPrefix(comment, prefix) {
		return strings.TrimPrefix(comment, prefix)
	}
	return ""
}
