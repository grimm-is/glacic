package imports

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// IPTablesConfig holds parsed iptables-save output.
type IPTablesConfig struct {
	Tables   map[string]*IPTablesTable
	Warnings []string
}

// IPTablesTable represents an iptables table (filter, nat, mangle, raw).
type IPTablesTable struct {
	Name   string
	Chains map[string]*IPTablesChain
}

// IPTablesChain represents an iptables chain.
type IPTablesChain struct {
	Name    string
	Policy  string // ACCEPT, DROP, REJECT (for built-in chains)
	Rules   []IPTablesRule
	Packets uint64
	Bytes   uint64
}

// IPTablesRule represents an iptables rule.
type IPTablesRule struct {
	Chain        string
	Protocol     string
	Source       string
	Destination  string
	InInterface  string
	OutInterface string
	SrcPort      string
	DstPort      string
	Match        string   // -m match modules
	Target       string   // -j target
	TargetOpts   string   // Options for target
	States       []string // Connection states
	Comment      string
	Extra        []string // Unparsed options
}

// ParseIPTablesSave parses output from 'iptables-save' command.
func ParseIPTablesSave(path string) (*IPTablesConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open iptables-save output: %w", err)
	}
	defer file.Close()

	cfg := &IPTablesConfig{
		Tables: make(map[string]*IPTablesTable),
	}

	scanner := bufio.NewScanner(file)
	var currentTable *IPTablesTable

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Table declaration: *filter, *nat, *mangle, *raw
		if strings.HasPrefix(line, "*") {
			tableName := strings.TrimPrefix(line, "*")
			currentTable = &IPTablesTable{
				Name:   tableName,
				Chains: make(map[string]*IPTablesChain),
			}
			cfg.Tables[tableName] = currentTable
			continue
		}

		// Chain declaration: :INPUT ACCEPT [0:0]
		if strings.HasPrefix(line, ":") && currentTable != nil {
			chain := parseChainDeclaration(line)
			if chain != nil {
				currentTable.Chains[chain.Name] = chain
			}
			continue
		}

		// Rule: -A INPUT -p tcp --dport 22 -j ACCEPT
		if strings.HasPrefix(line, "-A ") && currentTable != nil {
			rule := parseIPTablesRuleLine(line)
			if rule != nil {
				if chain, ok := currentTable.Chains[rule.Chain]; ok {
					chain.Rules = append(chain.Rules, *rule)
				} else {
					// Create chain if it doesn't exist (custom chains)
					currentTable.Chains[rule.Chain] = &IPTablesChain{
						Name:  rule.Chain,
						Rules: []IPTablesRule{*rule},
					}
				}
			}
			continue
		}

		// COMMIT ends table
		if line == "COMMIT" {
			currentTable = nil
		}
	}

	return cfg, scanner.Err()
}

func parseChainDeclaration(line string) *IPTablesChain {
	// Format: :CHAINNAME POLICY [packets:bytes]
	// Example: :INPUT ACCEPT [100:5000]
	line = strings.TrimPrefix(line, ":")

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil
	}

	chain := &IPTablesChain{
		Name:   parts[0],
		Policy: parts[1],
	}

	// Parse packet/byte counters if present
	if len(parts) >= 3 {
		counters := strings.Trim(parts[2], "[]")
		fmt.Sscanf(counters, "%d:%d", &chain.Packets, &chain.Bytes)
	}

	return chain
}

func parseIPTablesRuleLine(line string) *IPTablesRule {
	rule := &IPTablesRule{}

	// Extract chain name
	chainRe := regexp.MustCompile(`^-A\s+(\S+)`)
	if m := chainRe.FindStringSubmatch(line); len(m) > 1 {
		rule.Chain = m[1]
	} else {
		return nil
	}

	// Protocol
	protoRe := regexp.MustCompile(`-p\s+(\w+)`)
	if m := protoRe.FindStringSubmatch(line); len(m) > 1 {
		rule.Protocol = m[1]
	}

	// Source
	srcRe := regexp.MustCompile(`-s\s+(\S+)`)
	if m := srcRe.FindStringSubmatch(line); len(m) > 1 {
		rule.Source = m[1]
	}

	// Destination
	dstRe := regexp.MustCompile(`-d\s+(\S+)`)
	if m := dstRe.FindStringSubmatch(line); len(m) > 1 {
		rule.Destination = m[1]
	}

	// Input interface
	iifRe := regexp.MustCompile(`-i\s+(\S+)`)
	if m := iifRe.FindStringSubmatch(line); len(m) > 1 {
		rule.InInterface = m[1]
	}

	// Output interface
	oifRe := regexp.MustCompile(`-o\s+(\S+)`)
	if m := oifRe.FindStringSubmatch(line); len(m) > 1 {
		rule.OutInterface = m[1]
	}

	// Source port
	sportRe := regexp.MustCompile(`--sport\s+(\S+)`)
	if m := sportRe.FindStringSubmatch(line); len(m) > 1 {
		rule.SrcPort = m[1]
	}

	// Destination port
	dportRe := regexp.MustCompile(`--dport\s+(\S+)`)
	if m := dportRe.FindStringSubmatch(line); len(m) > 1 {
		rule.DstPort = m[1]
	}

	// Multiport
	mportRe := regexp.MustCompile(`--dports\s+(\S+)`)
	if m := mportRe.FindStringSubmatch(line); len(m) > 1 {
		rule.DstPort = m[1]
	}

	// Match module
	matchRe := regexp.MustCompile(`-m\s+(\w+)`)
	matches := matchRe.FindAllStringSubmatch(line, -1)
	for _, m := range matches {
		if len(m) > 1 {
			rule.Match += m[1] + " "
		}
	}
	rule.Match = strings.TrimSpace(rule.Match)

	// Connection state
	stateRe := regexp.MustCompile(`--state\s+(\S+)`)
	if m := stateRe.FindStringSubmatch(line); len(m) > 1 {
		rule.States = strings.Split(m[1], ",")
	}
	// Also check for conntrack match
	ctRe := regexp.MustCompile(`--ctstate\s+(\S+)`)
	if m := ctRe.FindStringSubmatch(line); len(m) > 1 {
		rule.States = strings.Split(m[1], ",")
	}

	// Target
	targetRe := regexp.MustCompile(`-j\s+(\S+)`)
	if m := targetRe.FindStringSubmatch(line); len(m) > 1 {
		rule.Target = m[1]
	}

	// Comment
	commentRe := regexp.MustCompile(`--comment\s+"([^"]+)"`)
	if m := commentRe.FindStringSubmatch(line); len(m) > 1 {
		rule.Comment = m[1]
	}

	return rule
}

// ToImportResult converts iptables config to ImportResult.
func (c *IPTablesConfig) ToImportResult() *ImportResult {
	result := &ImportResult{}

	// Collect interfaces from rules
	interfaces := make(map[string]bool)

	// Process filter table
	if filter, ok := c.Tables["filter"]; ok {
		for _, chain := range filter.Chains {
			for _, rule := range chain.Rules {
				if rule.InInterface != "" {
					interfaces[rule.InInterface] = true
				}
				if rule.OutInterface != "" {
					interfaces[rule.OutInterface] = true
				}

				// Convert to ImportedFilterRule
				impRule := IPTablesRuleToImported(rule, chain.Name)
				result.FilterRules = append(result.FilterRules, impRule)
			}
		}
	}

	// Process nat table
	if nat, ok := c.Tables["nat"]; ok {
		for _, chain := range nat.Chains {
			for _, rule := range chain.Rules {
				if chain.Name == "POSTROUTING" {
					// SNAT/MASQUERADE
					natRule := ImportedNATRule{
						Interface:   rule.OutInterface,
						Source:      rule.Source,
						Destination: rule.Destination,
						Description: rule.Comment,
						CanImport:   true,
					}
					switch rule.Target {
					case "MASQUERADE":
						natRule.Type = "masquerade"
					case "SNAT":
						natRule.Type = "snat"
						natRule.Target = rule.TargetOpts
					}
					result.NATRules = append(result.NATRules, natRule)
				} else if chain.Name == "PREROUTING" {
					// DNAT
					if rule.Target == "DNAT" {
						pf := ImportedPortForward{
							Interface:    rule.InInterface,
							Protocol:     rule.Protocol,
							ExternalPort: rule.DstPort,
							Description:  rule.Comment,
							CanImport:    true,
						}
						// Parse --to-destination
						toDestRe := regexp.MustCompile(`--to-destination\s+(\S+)`)
						if m := toDestRe.FindStringSubmatch(rule.TargetOpts); len(m) > 1 {
							parts := strings.Split(m[1], ":")
							pf.InternalIP = parts[0]
							if len(parts) > 1 {
								pf.InternalPort = parts[1]
							}
						}
						result.PortForwards = append(result.PortForwards, pf)
					}
				}
			}
		}
	}

	// Add discovered interfaces
	for iface := range interfaces {
		result.Interfaces = append(result.Interfaces, ImportedInterface{
			OriginalName: iface,
			OriginalIf:   iface,
			NeedsMapping: false, // Already Linux interface names
			SuggestedIf:  iface,
		})
	}

	// Add guidance
	result.ManualSteps = append(result.ManualSteps,
		"Review chain policies and convert to zone defaults",
		"Custom chains may need to be converted to policies",
		"Check for match modules that need special handling",
	)

	return result
}

// IPTablesRuleToImported converts an iptables rule to ImportedFilterRule.
func IPTablesRuleToImported(rule IPTablesRule, chainName string) ImportedFilterRule {
	imp := ImportedFilterRule{
		Description: rule.Comment,
		Protocol:    rule.Protocol,
		DestPort:    rule.DstPort,
		SourcePort:  rule.SrcPort,
		Source:      rule.Source,
		Destination: rule.Destination,
		Interface:   rule.InInterface,
		CanImport:   true,
	}

	if imp.Description == "" {
		imp.Description = fmt.Sprintf("%s rule", chainName)
	}

	// Convert target to action
	switch rule.Target {
	case "ACCEPT":
		imp.Action = "accept"
	case "DROP":
		imp.Action = "drop"
	case "REJECT":
		imp.Action = "reject"
	case "LOG":
		imp.Action = "accept" // Log is usually followed by another rule
		imp.Log = true
		imp.ImportNotes = append(imp.ImportNotes, "LOG target - enable logging on rule")
	case "RETURN":
		imp.ImportNotes = append(imp.ImportNotes, "RETURN target - chain flow control")
		imp.CanImport = false
	default:
		// Custom chain jump
		imp.ImportNotes = append(imp.ImportNotes, "Jumps to chain: "+rule.Target)
	}

	// Map chain to policy
	switch chainName {
	case "INPUT":
		imp.SuggestedPolicy = "input_policy"
	case "OUTPUT":
		imp.SuggestedPolicy = "output_policy"
	case "FORWARD":
		imp.SuggestedPolicy = "forward_policy"
	default:
		imp.SuggestedPolicy = strings.ToLower(chainName) + "_policy"
	}

	// Handle connection states
	if len(rule.States) > 0 {
		stateStr := strings.Join(rule.States, ",")
		// Check for common patterns
		if strings.Contains(stateStr, "ESTABLISHED") || strings.Contains(stateStr, "RELATED") {
			imp.ImportNotes = append(imp.ImportNotes, "Stateful rule: "+stateStr)
		}
	}

	// Note complex matches
	if rule.Match != "" {
		if strings.Contains(rule.Match, "limit") {
			imp.ImportNotes = append(imp.ImportNotes, "Rate limiting - configure separately")
		}
		if strings.Contains(rule.Match, "recent") {
			imp.ImportNotes = append(imp.ImportNotes, "Recent match - may need IPSet")
		}
		if strings.Contains(rule.Match, "geoip") {
			imp.ImportNotes = append(imp.ImportNotes, "GeoIP match - configure geo blocking")
		}
	}

	return imp
}
