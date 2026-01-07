package config

import (
	"fmt"
	"log"
	"net"
	"path/filepath"
	"regexp"
	"strings"
)

// isWildcardZone checks if a zone name is a wildcard pattern.
// Patterns include "*" (any zone) or glob patterns like "vpn*".
func isWildcardZone(zone string) bool {
	return zone == "*" || strings.ContainsAny(zone, "*?[]")
}

// matchesZone checks if a zone name matches a pattern (supports glob).
func matchesZone(pattern, zoneName string) bool {
	if pattern == "*" {
		return true
	}
	matched, err := filepath.Match(pattern, zoneName)
	if err != nil {
		// Malformed pattern - log and treat as no match
		log.Printf("[CONFIG] Warning: malformed zone pattern %q: %v", pattern, err)
		return false
	}
	return matched
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field    string
	Message  string
	Severity string // "error" (default), "warning"
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// HasErrors returns true if there are any validation errors.
func (e ValidationErrors) HasErrors() bool {
	return len(e) > 0
}

// Validate validates the entire configuration.
func (c *Config) Validate() ValidationErrors {
	var errs ValidationErrors

	// Validate zones
	errs = append(errs, c.validateZones()...)

	// Validate interfaces
	errs = append(errs, c.validateInterfaces()...)

	// Validate interface overlaps (The "Overlapping Subnets" Foot-Gun Fix)
	errs = append(errs, c.validateInterfaceOverlaps()...)

	// Validate IPSets
	errs = append(errs, c.validateIPSets()...)

	// Validate policies
	errs = append(errs, c.validatePolicies()...)

	// Validate NAT rules
	errs = append(errs, c.validateNAT()...)

	// Validate routes
	errs = append(errs, c.validateRoutes()...)

	return errs
}

func (c *Config) validateZones() ValidationErrors {
	var errs ValidationErrors

	for i, zone := range c.Zones {
		field := fmt.Sprintf("zones[%s]", zone.Name)
		if zone.Name == "" {
			field = fmt.Sprintf("zones[%d]", i)
		}

		// Validate top-level Src
		if zone.Src != "" && !isValidIPOrCIDR(zone.Src) {
			errs = append(errs, ValidationError{
				Field:   field + ".src",
				Message: fmt.Sprintf("invalid IP or CIDR: %s", zone.Src),
			})
		}

		// Validate top-level Dst
		if zone.Dst != "" && !isValidIPOrCIDR(zone.Dst) {
			errs = append(errs, ValidationError{
				Field:   field + ".dst",
				Message: fmt.Sprintf("invalid IP or CIDR: %s", zone.Dst),
			})
		}

		// Validate top-level VLAN (1-4094, 4095 is reserved)
		if zone.VLAN != 0 && (zone.VLAN < 1 || zone.VLAN > 4094) {
			errs = append(errs, ValidationError{
				Field:   field + ".vlan",
				Message: fmt.Sprintf("VLAN must be between 1 and 4094, got %d", zone.VLAN),
			})
		}

		// Validate match blocks
		for j, match := range zone.Matches {
			matchField := fmt.Sprintf("%s.match[%d]", field, j)

			if match.Src != "" && !isValidIPOrCIDR(match.Src) {
				errs = append(errs, ValidationError{
					Field:   matchField + ".src",
					Message: fmt.Sprintf("invalid IP or CIDR: %s", match.Src),
				})
			}

			if match.Dst != "" && !isValidIPOrCIDR(match.Dst) {
				errs = append(errs, ValidationError{
					Field:   matchField + ".dst",
					Message: fmt.Sprintf("invalid IP or CIDR: %s", match.Dst),
				})
			}

			if match.VLAN != 0 && (match.VLAN < 1 || match.VLAN > 4094) {
				errs = append(errs, ValidationError{
					Field:   matchField + ".vlan",
					Message: fmt.Sprintf("VLAN must be between 1 and 4094, got %d", match.VLAN),
				})
			}
		}
	}

	return errs
}

func (c *Config) validateInterfaces() ValidationErrors {
	var errs ValidationErrors
	seen := make(map[string]bool)

	for i, iface := range c.Interfaces {
		field := fmt.Sprintf("interfaces[%d]", i)

		// Check for duplicate names
		if seen[iface.Name] {
			errs = append(errs, ValidationError{
				Field:   field + ".name",
				Message: fmt.Sprintf("duplicate interface name: %s", iface.Name),
			})
		}
		seen[iface.Name] = true

		// Validate interface name format
		if !isValidInterfaceName(iface.Name) {
			errs = append(errs, ValidationError{
				Field:   field + ".name",
				Message: fmt.Sprintf("invalid interface name: %s", iface.Name),
			})
		}

		// Validate IPv4 addresses
		for j, ip := range iface.IPv4 {
			if !isValidCIDR(ip) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s.ipv4[%d]", field, j),
					Message: fmt.Sprintf("invalid IPv4 CIDR: %s", ip),
				})
			}
		}

		// Validate MTU
		if iface.MTU != 0 && (iface.MTU < 576 || iface.MTU > 65535) {
			errs = append(errs, ValidationError{
				Field:   field + ".mtu",
				Message: fmt.Sprintf("MTU must be between 576 and 65535, got %d", iface.MTU),
			})
		}

		// Validate VLANs
		for j, vlan := range iface.VLANs {
			vlanField := fmt.Sprintf("%s.vlans[%d]", field, j)
			vid := 0
			fmt.Sscanf(vlan.ID, "%d", &vid)
			if vid < 1 || vid > 4094 {
				errs = append(errs, ValidationError{
					Field:   vlanField + ".id",
					Message: fmt.Sprintf("VLAN ID must be between 1 and 4094, got %s", vlan.ID),
				})
			}
		}
	}

	return errs
}

// validateInterfaceOverlaps checks for overlapping subnets across interfaces
func (c *Config) validateInterfaceOverlaps() ValidationErrors {
	var errs ValidationErrors

	type ifaceNet struct {
		Name  string
		IPNet *net.IPNet
		CIDR  string
	}

	var networks []ifaceNet

	// Collect all networks
	for _, iface := range c.Interfaces {
		for _, addr := range iface.IPv4 {
			_, ipnet, err := net.ParseCIDR(addr)
			if err == nil {
				networks = append(networks, ifaceNet{
					Name:  iface.Name,
					IPNet: ipnet,
					CIDR:  addr,
				})
			}
		}
		for _, vlan := range iface.VLANs {
			for _, addr := range vlan.IPv4 {
				_, ipnet, err := net.ParseCIDR(addr)
				if err == nil {
					networks = append(networks, ifaceNet{
						Name:  fmt.Sprintf("%s.vlan%s", iface.Name, vlan.ID),
						IPNet: ipnet,
						CIDR:  addr,
					})
				}
			}
		}
	}

	// Compare all pairs
	for i := 0; i < len(networks); i++ {
		for j := i + 1; j < len(networks); j++ {
			n1 := networks[i]
			n2 := networks[j]

			// Skip if same interface (technically alias IPs are allowed, but usually same subnet on same iface is alias)
			// But if they are different subnets on same iface, it's advanced but allowed.
			// The user concern is "eth0 vs eth1".
			// So checks across different interfaces are primary.
			if n1.Name == n2.Name {
				// Same interface.
				if n1.IPNet.String() == n2.IPNet.String() {
					errs = append(errs, ValidationError{
						Field:   fmt.Sprintf("interfaces[%s]", n1.Name),
						Message: fmt.Sprintf("duplicate subnet %s on same interface", n1.CIDR),
					})
				}
				continue
			}

			// Check overlap
			if netsOverlap(n1.IPNet, n2.IPNet) {
				errs = append(errs, ValidationError{
					Field:    "interfaces",
					Message:  fmt.Sprintf("overlapping subnets detected: %s (%s) and %s (%s)", n1.Name, n1.CIDR, n2.Name, n2.CIDR),
					Severity: "error", // High risk
				})
			}
		}
	}

	return errs
}

// netsOverlap returns true if two subnets overlap
func netsOverlap(n1, n2 *net.IPNet) bool {
	// Check if n1 contains n2's network address OR n2 contains n1's network address
	// Note: This covers exact match, containment, etc.
	return n1.Contains(n2.IP) || n2.Contains(n1.IP)
}

func (c *Config) validateIPSets() ValidationErrors {
	var errs ValidationErrors
	seen := make(map[string]bool)

	for i, ipset := range c.IPSets {
		field := fmt.Sprintf("ipsets[%d]", i)

		// Check for duplicate names
		if seen[ipset.Name] {
			errs = append(errs, ValidationError{
				Field:   field + ".name",
				Message: fmt.Sprintf("duplicate IPSet name: %s", ipset.Name),
			})
		}
		seen[ipset.Name] = true

		// Validate name format (alphanumeric, underscore, hyphen)
		if !isValidSetName(ipset.Name) {
			errs = append(errs, ValidationError{
				Field:   field + ".name",
				Message: fmt.Sprintf("invalid IPSet name (use alphanumeric, underscore, hyphen): %s", ipset.Name),
			})
		}

		// Validate type
		validTypes := map[string]bool{
			"":             true, // default
			"ipv4_addr":    true,
			"ipv6_addr":    true,
			"inet_service": true,
		}
		if !validTypes[ipset.Type] {
			errs = append(errs, ValidationError{
				Field:   field + ".type",
				Message: fmt.Sprintf("invalid IPSet type: %s (use ipv4_addr, ipv6_addr, or inet_service)", ipset.Type),
			})
		}

		// Warn if both static entries and external source
		if len(ipset.Entries) > 0 && (ipset.FireHOLList != "" || ipset.URL != "") {
			// This is allowed but worth noting - static entries are merged with downloaded
		}

		// Validate static entries
		for j, entry := range ipset.Entries {
			if !isValidIPOrCIDR(entry) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("%s.entries[%d]", field, j),
					Message: fmt.Sprintf("invalid IP or CIDR: %s", entry),
				})
			}
		}

		// Validate FireHOL list name
		if ipset.FireHOLList != "" {
			validLists := map[string]bool{
				"firehol_level1": true, "firehol_level2": true, "firehol_level3": true,
				"spamhaus_drop": true, "spamhaus_edrop": true,
				"dshield": true, "blocklist_de": true,
				"feodo": true, "tor_exits": true, "fullbogons": true,
			}
			if !validLists[ipset.FireHOLList] {
				errs = append(errs, ValidationError{
					Field:   field + ".firehol_list",
					Message: fmt.Sprintf("unknown FireHOL list: %s", ipset.FireHOLList),
				})
			}
		}

		// Validate URL format
		if ipset.URL != "" && !isValidURL(ipset.URL) {
			errs = append(errs, ValidationError{
				Field:   field + ".url",
				Message: fmt.Sprintf("invalid URL: %s", ipset.URL),
			})
		}

		// Validate refresh hours
		if ipset.RefreshHours < 0 {
			errs = append(errs, ValidationError{
				Field:   field + ".refresh_hours",
				Message: "refresh_hours cannot be negative",
			})
		}

		// Validate action
		validActions := map[string]bool{"": true, "drop": true, "accept": true, "reject": true, "log": true}
		if !validActions[ipset.Action] {
			errs = append(errs, ValidationError{
				Field:   field + ".action",
				Message: fmt.Sprintf("invalid action: %s (use drop, accept, reject, or log)", ipset.Action),
			})
		}

		// Validate apply_to
		validApplyTo := map[string]bool{"": true, "input": true, "forward": true, "both": true}
		if !validApplyTo[ipset.ApplyTo] {
			errs = append(errs, ValidationError{
				Field:   field + ".apply_to",
				Message: fmt.Sprintf("invalid apply_to: %s (use input, forward, or both)", ipset.ApplyTo),
			})
		}
	}

	return errs
}

func (c *Config) validatePolicies() ValidationErrors {
	var errs ValidationErrors
	zones := c.getDefinedZones()

	for i, policy := range c.Policies {
		field := fmt.Sprintf("policies[%d]", i)

		// Validate from zone exists (allow wildcards)
		if policy.From != "" && !isWildcardZone(policy.From) && !zones[policy.From] {
			errs = append(errs, ValidationError{
				Field:   field + ".from",
				Message: fmt.Sprintf("unknown zone: %s", policy.From),
			})
		}

		// Validate to zone exists (allow "firewall"/"self" and wildcards)
		if policy.To != "" && !isWildcardZone(policy.To) && policy.To != "firewall" && policy.To != "Firewall" && policy.To != "self" && !zones[policy.To] {
			errs = append(errs, ValidationError{
				Field:   field + ".to",
				Message: fmt.Sprintf("unknown zone: %s", policy.To),
			})
		}

		// Validate rules
		for j, rule := range policy.Rules {
			ruleField := fmt.Sprintf("%s.rules[%d]", field, j)

			// Validate action
			validActions := map[string]bool{"accept": true, "drop": true, "reject": true}
			if !validActions[strings.ToLower(rule.Action)] {
				errs = append(errs, ValidationError{
					Field:   ruleField + ".action",
					Message: fmt.Sprintf("invalid action: %s", rule.Action),
				})
			}

			// Validate protocol
			if rule.Protocol != "" {
				validProtos := map[string]bool{"tcp": true, "udp": true, "icmp": true, "any": true}
				if !validProtos[strings.ToLower(rule.Protocol)] {
					errs = append(errs, ValidationError{
						Field:   ruleField + ".proto",
						Message: fmt.Sprintf("invalid protocol: %s", rule.Protocol),
					})
				}
			}

			// Validate ports
			if rule.DestPort != 0 && (rule.DestPort < 1 || rule.DestPort > 65535) {
				errs = append(errs, ValidationError{
					Field:   ruleField + ".dest_port",
					Message: fmt.Sprintf("port must be between 1 and 65535, got %d", rule.DestPort),
				})
			}

			// Validate IPSet references
			if rule.SrcIPSet != "" && !c.hasIPSet(rule.SrcIPSet) {
				errs = append(errs, ValidationError{
					Field:   ruleField + ".src_ipset",
					Message: fmt.Sprintf("unknown IPSet: %s", rule.SrcIPSet),
				})
			}
			if rule.DestIPSet != "" && !c.hasIPSet(rule.DestIPSet) {
				errs = append(errs, ValidationError{
					Field:   ruleField + ".dest_ipset",
					Message: fmt.Sprintf("unknown IPSet: %s", rule.DestIPSet),
				})
			}
		}

		// Validate inheritance
		if policy.Inherits != "" {
			// Check that parent policy exists
			parentFound := false
			for j, p := range c.Policies {
				if p.Name == policy.Inherits {
					parentFound = true
					// Prevent self-inheritance
					if i == j {
						errs = append(errs, ValidationError{
							Field:   field + ".inherits",
							Message: "policy cannot inherit from itself",
						})
					}
					break
				}
			}
			if !parentFound {
				errs = append(errs, ValidationError{
					Field:   field + ".inherits",
					Message: fmt.Sprintf("unknown parent policy: %s", policy.Inherits),
				})
			}

			// Check for circular inheritance
			visited := make(map[string]bool)
			current := policy.Inherits
			for current != "" {
				if visited[current] {
					errs = append(errs, ValidationError{
						Field:   field + ".inherits",
						Message: fmt.Sprintf("circular inheritance detected involving: %s", current),
					})
					break
				}
				visited[current] = true

				// Find next parent
				var nextParent string
				for _, p := range c.Policies {
					if p.Name == current {
						nextParent = p.Inherits
						break
					}
				}
				current = nextParent
			}
		}
	}

	// Check for duplicate zone combinations (excluding inherited policies with same zones)
	zoneCombos := make(map[string][]int) // "from->to" -> list of policy indices
	for i, policy := range c.Policies {
		key := policy.From + "->" + policy.To
		zoneCombos[key] = append(zoneCombos[key], i)
	}
	for combo, indices := range zoneCombos {
		if len(indices) > 1 {
			// Multiple policies for same zone combo - check if they're related by inheritance
			for _, idx := range indices {
				policy := c.Policies[idx]
				// If this policy doesn't inherit from another in the same combo, it's a conflict
				hasInheritanceRelation := false
				for _, otherIdx := range indices {
					if idx == otherIdx {
						continue
					}
					otherPolicy := c.Policies[otherIdx]
					if policy.Inherits == otherPolicy.Name || otherPolicy.Inherits == policy.Name {
						hasInheritanceRelation = true
						break
					}
				}
				if !hasInheritanceRelation && policy.Inherits == "" {
					// Only warn for non-inheriting policies - inheriting is intentional
					errs = append(errs, ValidationError{
						Field:    fmt.Sprintf("policies[%d]", idx),
						Message:  fmt.Sprintf("duplicate zone combination %s without inheritance relationship", combo),
						Severity: "warning",
					})
				}
			}
		}
	}

	return errs
}

func (c *Config) validateNAT() ValidationErrors {
	var errs ValidationErrors

	for i, nat := range c.NAT {
		field := fmt.Sprintf("nat[%d]", i)

		validTypes := map[string]bool{"masquerade": true, "snat": true, "dnat": true}
		if !validTypes[strings.ToLower(nat.Type)] {
			errs = append(errs, ValidationError{
				Field:   field + ".type",
				Message: fmt.Sprintf("invalid NAT type: %s", nat.Type),
			})
		}
	}

	return errs
}

func (c *Config) validateRoutes() ValidationErrors {
	var errs ValidationErrors

	for i, route := range c.Routes {
		field := fmt.Sprintf("routes[%d]", i)

		if route.Destination != "" && !isValidCIDR(route.Destination) {
			errs = append(errs, ValidationError{
				Field:   field + ".destination",
				Message: fmt.Sprintf("invalid destination CIDR: %s", route.Destination),
			})
		}

		if route.Gateway != "" && net.ParseIP(route.Gateway) == nil {
			errs = append(errs, ValidationError{
				Field:   field + ".gateway",
				Message: fmt.Sprintf("invalid gateway IP: %s", route.Gateway),
			})
		}
	}

	return errs
}

// Helper functions

func (c *Config) getDefinedZones() map[string]bool {
	zones := make(map[string]bool)

	// 1. Explicit zone definitions
	for _, zone := range c.Zones {
		zones[zone.Name] = true
	}

	// 2. Interface-referenced zones (legacy style: interface has zone = "wan")
	for _, iface := range c.Interfaces {
		if iface.Zone != "" {
			zones[iface.Zone] = true
		}
		for _, vlan := range iface.VLANs {
			if vlan.Zone != "" {
				zones[vlan.Zone] = true
			}
		}
	}
	return zones
}

// NormalizeZoneMappings unifies zone-interface mappings from both syntaxes:
//   - Zone-centric: zone "wan" { interfaces = ["eth0"] }
//   - Interface-centric (legacy): interface "eth0" { zone = "wan" }
//
// After normalization, all zone.Interfaces arrays are populated AND all iface.Zone fields are set.
func (c *Config) NormalizeZoneMappings() {
	// Build map of zone name -> zone INDEX (not pointer, to survive slice reallocation)
	zoneIndex := make(map[string]int)
	for i := range c.Zones {
		zoneIndex[c.Zones[i].Name] = i
	}

	// Helper to get zone by name, returning nil if not found
	getZone := func(name string) *Zone {
		if idx, ok := zoneIndex[name]; ok {
			return &c.Zones[idx]
		}
		return nil
	}

	// Helper to add a new zone and update the index
	addZone := func(z Zone) *Zone {
		c.Zones = append(c.Zones, z)
		idx := len(c.Zones) - 1
		zoneIndex[z.Name] = idx
		return &c.Zones[idx]
	}

	// 1. Process zone-centric config: zone "name" { interfaces = [...] }
	// We need to populate iface.Zone for these interfaces if not already set
	ifaceMap := make(map[string]*Interface)
	for i := range c.Interfaces {
		ifaceMap[c.Interfaces[i].Name] = &c.Interfaces[i]
	}

	for _, zone := range c.Zones {
		for _, ifaceName := range zone.Interfaces {
			if iface, exists := ifaceMap[ifaceName]; exists {
				// Set interface zone if empty (prefer zone definition in new syntax)
				if iface.Zone == "" {
					iface.Zone = zone.Name
				}
			}
		}
	}

	// 2. Process interface-centric (legacy): interface "name" { zone = "..." }
	// We need to add these interfaces to the zone's interface list
	for _, iface := range c.Interfaces {
		if iface.Zone != "" {
			zone := getZone(iface.Zone)
			if zone == nil {
				// Create implicit zone for this interface-referenced zone
				zone = addZone(Zone{
					Name:       iface.Zone,
					Interfaces: []string{iface.Name},
				})
			} else {
				// Add interface to existing zone if not already present
				if !containsString(zone.Interfaces, iface.Name) {
					zone.Interfaces = append(zone.Interfaces, iface.Name)
				}
			}
		}

		// Process VLAN zones
		for _, vlan := range iface.VLANs {
			if vlan.Zone != "" {
				vlanIfaceName := fmt.Sprintf("%s.%s", iface.Name, vlan.ID)
				zone := getZone(vlan.Zone)
				if zone == nil {
					addZone(Zone{
						Name:       vlan.Zone,
						Interfaces: []string{vlanIfaceName},
					})
				} else {
					if !containsString(zone.Interfaces, vlanIfaceName) {
						zone.Interfaces = append(zone.Interfaces, vlanIfaceName)
					}
				}
			}
		}
	}
}


func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func (c *Config) hasIPSet(name string) bool {
	for _, ipset := range c.IPSets {
		if ipset.Name == name {
			return true
		}
	}
	return false
}

func isValidInterfaceName(name string) bool {
	if name == "" || len(name) > 15 {
		return false
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z][a-zA-Z0-9._-]*$`, name)
	return matched
}

func isValidSetName(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z][a-zA-Z0-9_-]*$`, name)
	return matched
}

func isValidCIDR(s string) bool {
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

func isValidIPOrCIDR(s string) bool {
	if strings.Contains(s, "/") {
		_, _, err := net.ParseCIDR(s)
		return err == nil
	}
	return net.ParseIP(s) != nil
}

func isValidURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// NormalizePolicies sets default names for policies if not specified.
func (c *Config) NormalizePolicies() {
	for i := range c.Policies {
		p := &c.Policies[i]
		if p.Name == "" {
			p.Name = fmt.Sprintf("%s-to-%s", p.From, p.To)
		}
	}
}
