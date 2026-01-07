package config

// Policy defines traffic rules between zones.
// Rules are evaluated in order - first match wins.
type Policy struct {
	From        string `hcl:"from,label" json:"from"`    // Source zone name
	To          string `hcl:"to,label" json:"to"`        // Destination zone name
	Name        string `hcl:"name,optional" json:"name"` // Optional descriptive name
	Description string `hcl:"description,optional" json:"description,omitempty"`
	Priority    int    `hcl:"priority,optional" json:"priority,omitempty"` // Policy priority (lower = evaluated first)
	Disabled    bool   `hcl:"disabled,optional" json:"disabled,omitempty"` // Temporarily disable this policy

	// Action for traffic matching this policy (when no specific rule matches)
	// Values: "accept", "drop", "reject" (default: drop)
	Action string `hcl:"action,optional" json:"action,omitempty"`

	// Masquerade controls NAT for outbound traffic through this policy
	// nil = auto (enable when RFC1918 source → non-RFC1918 dest)
	// true = always masquerade
	// false = never masquerade
	Masquerade *bool `hcl:"masquerade,optional" json:"masquerade,omitempty"`

	Log       bool         `hcl:"log,optional" json:"log,omitempty"`               // Log packets matching default action
	LogPrefix string       `hcl:"log_prefix,optional" json:"log_prefix,omitempty"` // Prefix for log messages
	Rules     []PolicyRule `hcl:"rule,block" json:"rules"`

	// Inheritance - allows policies to inherit rules from a parent policy
	// Child policies get all parent rules first, then their own additional rules
	Inherits string `hcl:"inherits,optional" json:"inherits,omitempty"` // Name of parent policy to inherit from
}

// GetEffectiveRules returns the policy's rules including any inherited rules.
// Inherited rules come first, followed by the policy's own rules.
func (p *Policy) GetEffectiveRules(allPolicies []Policy) []PolicyRule {
	if p.Inherits == "" {
		return p.Rules
	}

	// Find parent policy
	var parent *Policy
	for i := range allPolicies {
		if allPolicies[i].Name == p.Inherits {
			parent = &allPolicies[i]
			break
		}
	}

	if parent == nil {
		return p.Rules
	}

	// Get parent's effective rules (supports chained inheritance)
	parentRules := parent.GetEffectiveRules(allPolicies)

	// Combine: parent rules first, then child's own rules
	effective := make([]PolicyRule, 0, len(parentRules)+len(p.Rules))
	effective = append(effective, parentRules...)
	effective = append(effective, p.Rules...)
	return effective
}

// IsChildOf checks if this policy inherits from the given policy name.
func (p *Policy) IsChildOf(parentName string) bool {
	return p.Inherits == parentName
}

// GetInheritanceChain returns the chain of policy names this policy inherits from.
func (p *Policy) GetInheritanceChain(allPolicies []Policy) []string {
	if p.Inherits == "" {
		return nil
	}

	chain := []string{p.Inherits}

	// Find parent and get its chain
	for i := range allPolicies {
		if allPolicies[i].Name == p.Inherits {
			parentChain := allPolicies[i].GetInheritanceChain(allPolicies)
			chain = append(chain, parentChain...)
			break
		}
	}

	return chain
}

// PolicyRule defines a specific rule within a policy.
// Rules are matched in the order they appear - first match wins.
type PolicyRule struct {
	// Identity
	ID          string `hcl:"id,optional" json:"id,omitempty"` // Unique ID for referencing in reorder operations
	Name        string `hcl:"name,label" json:"name"`
	Description string `hcl:"description,optional" json:"description,omitempty"`
	Disabled    bool   `hcl:"disabled,optional" json:"disabled,omitempty"` // Temporarily disable this rule

	// Ordering - rules are processed in order, these help with insertion
	Order       int    `hcl:"order,optional" json:"order,omitempty"`               // Explicit order (0 = use array position)
	InsertAfter string `hcl:"insert_after,optional" json:"insert_after,omitempty"` // Insert after rule with this ID/name

	// Match conditions
	Protocol  string   `hcl:"proto,optional" json:"proto,omitempty"`
	DestPort  int      `hcl:"dest_port,optional" json:"dest_port,omitempty"`
	DestPorts []int    `hcl:"dest_ports,optional" json:"dest_ports,omitempty"` // Multiple ports
	SrcPort   int      `hcl:"src_port,optional" json:"src_port,omitempty"`
	SrcPorts  []int    `hcl:"src_ports,optional" json:"src_ports,omitempty"`
	Services  []string `hcl:"services,optional" json:"services,omitempty"`     // Service names like "http", "ssh"
	SrcIP     string   `hcl:"src_ip,optional" json:"src_ip,omitempty"`         // Source IP/CIDR
	SrcIPSet  string   `hcl:"src_ipset,optional" json:"src_ipset,omitempty"`   // Source IPSet name
	DestIP    string   `hcl:"dest_ip,optional" json:"dest_ip,omitempty"`       // Destination IP/CIDR
	DestIPSet string   `hcl:"dest_ipset,optional" json:"dest_ipset,omitempty"` // Destination IPSet name

	// Additional match conditions
	SrcZone      string `hcl:"src_zone,optional" json:"src_zone,omitempty"`           // Override policy's From zone
	DestZone     string `hcl:"dest_zone,optional" json:"dest_zone,omitempty"`         // Override policy's To zone
	InInterface  string `hcl:"in_interface,optional" json:"in_interface,omitempty"`   // Match specific input interface
	OutInterface string `hcl:"out_interface,optional" json:"out_interface,omitempty"` // Match specific output interface
	ConnState    string `hcl:"conn_state,optional" json:"conn_state,omitempty"`       // "new", "established", "related", "invalid"

	// GeoIP matching (requires MaxMind database)
	SourceCountry string `hcl:"source_country,optional" json:"source_country,omitempty"` // ISO 3166-1 alpha-2 country code (e.g., "US", "CN")
	DestCountry   string `hcl:"dest_country,optional" json:"dest_country,omitempty"`   // ISO 3166-1 alpha-2 country code

	// Invert matching (match everything EXCEPT the specified value)
	InvertSrc  bool `hcl:"invert_src,optional" json:"invert_src,omitempty"`   // Negate source IP/IPSet match
	InvertDest bool `hcl:"invert_dest,optional" json:"invert_dest,omitempty"` // Negate destination IP/IPSet match

	// TCP Flags matching (for SYN flood protection, connection state filtering)
	// Values: "syn", "syn,!ack", "ack", "fin", "rst", "psh", "urg" or combinations
	// Common presets: "syn" (new only), "ack" (established), "fin,psh,urg" (xmas scan)
	TCPFlags string `hcl:"tcp_flags,optional" json:"tcp_flags,omitempty"`

	// Connection limiting (prevent abuse/DoS)
	MaxConnections int `hcl:"max_connections,optional" json:"max_connections,omitempty"` // Max concurrent connections per source

	// Time-of-day matching (uses nftables meta hour/day, requires kernel 5.4+)
	TimeStart string   `hcl:"time_start,optional" json:"time_start,omitempty"` // Start time "HH:MM" (24h format)
	TimeEnd   string   `hcl:"time_end,optional" json:"time_end,omitempty"`     // End time "HH:MM" (24h format)
	Days      []string `hcl:"days,optional" json:"days,omitempty"`             // Days of week: "Monday", "Tuesday", etc.

	// Action
	Action     string `hcl:"action" json:"action"`                              // accept, drop, reject, jump, return, log
	JumpTarget string `hcl:"jump_target,optional" json:"jump_target,omitempty"` // Target chain for jump action

	// Logging & accounting
	Log       bool   `hcl:"log,optional" json:"log,omitempty"`
	LogPrefix string `hcl:"log_prefix,optional" json:"log_prefix,omitempty"`
	LogLevel  string `hcl:"log_level,optional" json:"log_level,omitempty"` // "debug", "info", "notice", "warning", "error"
	Limit     string `hcl:"limit,optional" json:"limit,omitempty"`         // Rate limit e.g. "10/second"
	Counter   string `hcl:"counter,optional" json:"counter,omitempty"`     // Named counter for accounting

	// Metadata
	Comment string   `hcl:"comment,optional" json:"comment,omitempty"`
	Tags    []string `hcl:"tags,optional" json:"tags,omitempty"` // For grouping/filtering in UI

	// UI Organization
	GroupTag string `hcl:"group,optional" json:"group,omitempty"` // Section grouping: "User Access", "IoT Isolation"
}

// NATRule defines Network Address Translation rules.
type NATRule struct {
	Name         string `hcl:"name,label" json:"name"`
	Description  string `hcl:"description,optional" json:"description,omitempty"`
	Type         string `hcl:"type" json:"type"`                                      // masquerade, dnat, snat, redirect
	Protocol     string `hcl:"proto,optional" json:"proto,omitempty"`                 // tcp, udp
	OutInterface string `hcl:"out_interface,optional" json:"out_interface,omitempty"` // for masquerade/snat
	InInterface  string `hcl:"in_interface,optional" json:"in_interface,omitempty"`   // for dnat
	SrcIP        string `hcl:"src_ip,optional" json:"src_ip,omitempty"`               // Source IP match
	DestIP       string `hcl:"dest_ip,optional" json:"dest_ip,omitempty"`             // Dest IP match
	Mark         int    `hcl:"mark,optional" json:"mark,omitempty"`                   // FWMark match
	DestPort     string `hcl:"dest_port,optional" json:"dest_port,omitempty"`         // Dest Port match (supports ranges "80-90")
	ToIP         string `hcl:"to_ip,optional" json:"to_ip,omitempty"`                 // Target IP for DNAT
	ToPort       string `hcl:"to_port,optional" json:"to_port,omitempty"`             // Target Port for DNAT
	SNATIP       string `hcl:"snat_ip,optional" json:"snat_ip,omitempty"`             // for snat (Target IP)
	Hairpin      bool   `hcl:"hairpin,optional" json:"hairpin,omitempty"`             // Enable Hairpin NAT (NAT Reflection)
}
