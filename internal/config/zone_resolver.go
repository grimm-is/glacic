package config

import (
	"fmt"
	"strings"
)

// ZoneResolver provides methods to resolve zone membership and generate
// nftables match expressions from zone configurations.
type ZoneResolver struct {
	zones []Zone
}

// NewZoneResolver creates a new resolver from config zones
func NewZoneResolver(zones []Zone) *ZoneResolver {
	return &ZoneResolver{zones: zones}
}

// EffectiveMatch represents a fully resolved match with all fields populated
// after applying global zone defaults to per-match overrides.
type EffectiveMatch struct {
	Interface string // Interface name or pattern (e.g., "eth0" or "wg*")
	IsPrefix  bool   // True if Interface is a wildcard pattern
	Src       string
	Dst       string
	VLAN      int
}

// IsInterfaceWildcard checks if an interface name has a wildcard suffix (+ or *)
func IsInterfaceWildcard(iface string) bool {
	return strings.HasSuffix(iface, "+") || strings.HasSuffix(iface, "*")
}

// InterfaceBase returns the interface without wildcard suffix
func InterfaceBase(iface string) string {
	if strings.HasSuffix(iface, "+") || strings.HasSuffix(iface, "*") {
		return iface[:len(iface)-1]
	}
	return iface
}

// InterfaceToNFT converts an interface pattern to nftables format
// e.g., "wg+" -> "wg*", "eth0" -> "eth0"
func InterfaceToNFT(iface string) string {
	if strings.HasSuffix(iface, "+") {
		return iface[:len(iface)-1] + "*"
	}
	return iface
}

// GetEffectiveMatches returns all effective matches for a zone.
// This merges global zone criteria with per-match overrides.
// If no explicit matches are defined, uses top-level zone criteria as a single match.
// If deprecated 'interfaces' is used, converts to interface matches.
func (r *ZoneResolver) GetEffectiveMatches(zoneName string) []EffectiveMatch {
	zone := r.findZone(zoneName)
	if zone == nil {
		return nil
	}

	// Case 1: New format with explicit match blocks
	if len(zone.Matches) > 0 {
		var matches []EffectiveMatch
		for _, m := range zone.Matches {
			iface := orDefault(m.Interface, zone.Interface)
			matches = append(matches, EffectiveMatch{
				Interface: InterfaceToNFT(iface),
				IsPrefix:  IsInterfaceWildcard(iface),
				Src:       orDefault(m.Src, zone.Src),
				Dst:       orDefault(m.Dst, zone.Dst),
				VLAN:      orDefaultInt(m.VLAN, zone.VLAN),
			})
		}
		return matches
	}

	// Case 2: Simple format with top-level criteria
	if zone.Interface != "" || zone.Src != "" {
		return []EffectiveMatch{{
			Interface: InterfaceToNFT(zone.Interface),
			IsPrefix:  IsInterfaceWildcard(zone.Interface),
			Src:       zone.Src,
			Dst:       zone.Dst,
			VLAN:      zone.VLAN,
		}}
	}

	// Case 3: Deprecated 'interfaces' field - convert to matches
	if len(zone.Interfaces) > 0 {
		var matches []EffectiveMatch
		for _, iface := range zone.Interfaces {
			matches = append(matches, EffectiveMatch{
				Interface: InterfaceToNFT(iface),
				IsPrefix:  IsInterfaceWildcard(iface),
				Src:       zone.Src,
				Dst:       zone.Dst,
				VLAN:      zone.VLAN,
			})
		}
		return matches
	}

	// Case 4: Networks/IPSets only (legacy)
	if len(zone.Networks) > 0 {
		return []EffectiveMatch{{Src: strings.Join(zone.Networks, ",")}}
	}

	return nil
}

// ResolveInterface returns the zone name for a given interface.
// Returns empty string if no zone matches.
func (r *ZoneResolver) ResolveInterface(ifaceName string) string {
	for _, zone := range r.zones {
		matches := r.GetEffectiveMatches(zone.Name)
		for _, m := range matches {
			if m.IsPrefix {
				// Remove * suffix and check prefix
				prefix := strings.TrimSuffix(m.Interface, "*")
				if strings.HasPrefix(ifaceName, prefix) {
					return zone.Name
				}
			} else if m.Interface == ifaceName {
				return zone.Name
			}
		}
	}
	return ""
}

// GetZoneInterfaces returns all interfaces that belong to a zone.
// For prefix zones (like "wg*"), returns the pattern as-is.
func (r *ZoneResolver) GetZoneInterfaces(zoneName string) []string {
	matches := r.GetEffectiveMatches(zoneName)
	var result []string
	seen := make(map[string]bool)

	for _, m := range matches {
		if m.Interface != "" && !seen[m.Interface] {
			result = append(result, m.Interface)
			seen[m.Interface] = true
		}
	}
	return result
}

// ToNFTMatch converts an EffectiveMatch to nftables match expressions.
// Returns a slice of match clauses that should be ANDed together.
func (r *ZoneResolver) ToNFTMatch(m EffectiveMatch, direction string) []string {
	var clauses []string

	// Interface match (already in nft format with * suffix if needed)
	if m.Interface != "" {
		if direction == "in" || direction == "" {
			clauses = append(clauses, fmt.Sprintf(`iifname "%s"`, m.Interface))
		} else {
			clauses = append(clauses, fmt.Sprintf(`oifname "%s"`, m.Interface))
		}
	}

	// Source IP/network match
	if m.Src != "" {
		clauses = append(clauses, fmt.Sprintf(`ip saddr %s`, m.Src))
	}

	// Destination IP/network match
	if m.Dst != "" {
		clauses = append(clauses, fmt.Sprintf(`ip daddr %s`, m.Dst))
	}

	// VLAN match
	if m.VLAN > 0 {
		clauses = append(clauses, fmt.Sprintf(`vlan id %d`, m.VLAN))
	}

	return clauses
}

// GetZoneNFTMatch returns the nftables match expression for a zone.
// For zones with multiple matches (OR logic), returns a set of clauses.
func (r *ZoneResolver) GetZoneNFTMatch(zoneName, direction string) string {
	matches := r.GetEffectiveMatches(zoneName)
	if len(matches) == 0 {
		return ""
	}

	// Single match case - simple AND of clauses
	if len(matches) == 1 {
		clauses := r.ToNFTMatch(matches[0], direction)
		return strings.Join(clauses, " ")
	}

	// Multiple matches - need OR logic
	// If all matches are just interface-only, use iifname { "eth0", "eth1" }
	allInterfaceOnly := true
	var interfaces []string
	for _, m := range matches {
		if m.Src != "" || m.Dst != "" || m.VLAN > 0 {
			allInterfaceOnly = false
			break
		}
		if m.Interface != "" {
			interfaces = append(interfaces, fmt.Sprintf(`"%s"`, m.Interface))
		}
	}

	if allInterfaceOnly && len(interfaces) > 0 {
		if direction == "in" || direction == "" {
			return fmt.Sprintf(`iifname { %s }`, strings.Join(interfaces, ", "))
		}
		return fmt.Sprintf(`oifname { %s }`, strings.Join(interfaces, ", "))
	}

	// Complex case with mixed criteria - caller needs to handle OR separately
	// Return first match for now (limitation to be addressed)
	return strings.Join(r.ToNFTMatch(matches[0], direction), " ")
}

// findZone finds a zone by name
func (r *ZoneResolver) findZone(name string) *Zone {
	for i := range r.zones {
		if strings.EqualFold(r.zones[i].Name, name) {
			return &r.zones[i]
		}
	}
	return nil
}

// Helper functions
func orDefault(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func orDefaultInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
