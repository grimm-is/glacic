package api

import (
	"net"
	"sort"

	"grimm.is/glacic/internal/config"
)

// DeviceLookup abstracts device identity and discovery systems.
// This is the "Brain" of the network - merging user-defined names with auto-discovered info.
type DeviceLookup interface {
	// FindByIP returns the best available name for an IP.
	// matchType values: "identity" (user override), "hostname" (DHCP), "vendor" (OUI), "reverse" (DNS)
	FindByIP(ip string) (name string, matchType string, found bool)
}

// AliasResolver translates raw policy fields to human-readable UI pills.
// It merges static config (IPSets, Zones) with runtime device discovery.
type AliasResolver struct {
	config       *config.Config
	deviceLookup DeviceLookup
}

// NewAliasResolver creates a resolver with the given config and optional device lookup.
func NewAliasResolver(cfg *config.Config, lookup DeviceLookup) *AliasResolver {
	return &AliasResolver{
		config:       cfg,
		deviceLookup: lookup,
	}
}

// ResolveSource resolves the source field of a rule to a UI pill.
func (r *AliasResolver) ResolveSource(rule config.PolicyRule) *ResolvedAddress {
	// Priority: SrcIPSet > SrcIP > SrcZone > Any
	if rule.SrcIPSet != "" {
		return r.resolveIPSet(rule.SrcIPSet)
	}
	if rule.SrcIP != "" {
		return r.Resolve(rule.SrcIP)
	}
	if rule.SrcZone != "" {
		return r.resolveZone(rule.SrcZone)
	}
	return &ResolvedAddress{DisplayName: "Any", Type: "any", Count: 0}
}

// ResolveDest resolves the destination field of a rule to a UI pill.
func (r *AliasResolver) ResolveDest(rule config.PolicyRule) *ResolvedAddress {
	// Priority: DestIPSet > DestIP > Service > DestZone > Any
	if rule.DestIPSet != "" {
		return r.resolveIPSet(rule.DestIPSet)
	}
	if rule.DestIP != "" {
		return r.Resolve(rule.DestIP)
	}
	if len(rule.Services) > 0 {
		return r.resolveServices(rule.Services)
	}
	if rule.DestPort > 0 {
		return r.resolvePort(rule.DestPort, rule.Protocol)
	}
	if rule.DestZone != "" {
		return r.resolveZone(rule.DestZone)
	}
	return &ResolvedAddress{DisplayName: "Any", Type: "any", Count: 0}
}

// Resolve maps a raw string (IP, CIDR, zone name, etc.) to a UI pill.
func (r *AliasResolver) Resolve(input string) *ResolvedAddress {
	// Case 0: Empty or "any"
	if input == "" || input == "any" {
		return &ResolvedAddress{DisplayName: "Any", Type: "any", Count: 0}
	}

	// Case 1: Reference to a configured IPSet
	if r.config != nil {
		for _, ipset := range r.config.IPSets {
			if ipset.Name == input {
				return r.formatSet(input, "ipset", ipset.Description, ipset.Entries)
			}
		}
	}

	// Case 2: Reference to a Zone
	if r.config != nil {
		for _, zone := range r.config.Zones {
			if zone.Name == input {
				return &ResolvedAddress{
					DisplayName: input,
					Type:        "zone",
					Description: zone.Description,
					Count:       1,
				}
			}
		}
	}

	// Case 3: Raw CIDR (e.g., 10.0.0.0/24)
	if _, _, err := net.ParseCIDR(input); err == nil {
		return &ResolvedAddress{
			DisplayName: input,
			Type:        "cidr",
			Count:       1,
			Preview:     []string{input},
		}
	}

	// Case 4: Single IP → Device Lookup
	if ip := net.ParseIP(input); ip != nil {
		return r.resolveSingleIP(input)
	}

	// Case 5: Fallback (hostname or unknown string)
	return &ResolvedAddress{DisplayName: input, Type: "raw", Count: 1}
}

// resolveSingleIP looks up a single IP in the device registry.
func (r *AliasResolver) resolveSingleIP(ip string) *ResolvedAddress {
	res := &ResolvedAddress{
		DisplayName: ip,
		Type:        "ip",
		Count:       1,
		Preview:     []string{ip},
	}

	if r.deviceLookup == nil {
		return res
	}

	// Query unified device store
	name, matchType, found := r.deviceLookup.FindByIP(ip)
	if found {
		res.DisplayName = name
		res.Description = ip // Keep IP visible in tooltip

		// Map match types to UI pill types for styling
		switch matchType {
		case "identity":
			res.Type = "device_named" // User-defined name (gold star)
		case "hostname":
			res.Type = "device_auto" // DHCP discovered (standard)
		case "vendor":
			res.Type = "device_vendor" // OUI lookup only
		default:
			res.Type = "host"
		}
	}

	return res
}

// resolveIPSet looks up an IPSet by name.
func (r *AliasResolver) resolveIPSet(name string) *ResolvedAddress {
	if r.config == nil {
		return &ResolvedAddress{DisplayName: name, Type: "ipset", Count: 0}
	}

	for _, ipset := range r.config.IPSets {
		if ipset.Name == name {
			return r.formatSet(name, "ipset", ipset.Description, ipset.Entries)
		}
	}

	return &ResolvedAddress{
		DisplayName: name,
		Type:        "ipset",
		Description: "Unknown IPSet",
		Count:       0,
	}
}

// resolveZone looks up a zone by name.
func (r *AliasResolver) resolveZone(name string) *ResolvedAddress {
	if r.config != nil {
		for _, zone := range r.config.Zones {
			if zone.Name == name {
				return &ResolvedAddress{
					DisplayName: name,
					Type:        "zone",
					Description: zone.Description,
					Count:       len(zone.Interfaces),
				}
			}
		}
	}

	return &ResolvedAddress{DisplayName: name, Type: "zone", Count: 1}
}

// resolveServices formats a list of service names.
func (r *AliasResolver) resolveServices(services []string) *ResolvedAddress {
	if len(services) == 1 {
		return &ResolvedAddress{
			DisplayName: services[0],
			Type:        "service",
			Count:       1,
		}
	}

	return &ResolvedAddress{
		DisplayName: services[0] + "...",
		Type:        "service",
		Count:       len(services),
		IsTruncated: len(services) > 1,
		Preview:     services,
	}
}

// resolvePort converts a port number to a service name.
func (r *AliasResolver) resolvePort(port int, proto string) *ResolvedAddress {
	name := portToServiceName(port, proto)
	return &ResolvedAddress{
		DisplayName: name,
		Type:        "port",
		Count:       1,
	}
}

// formatSet formats a list of items with truncation for large sets.
func (r *AliasResolver) formatSet(name, typeStr, desc string, entries []string) *ResolvedAddress {
	// Sort for consistent UI rendering
	sorted := make([]string, len(entries))
	copy(sorted, entries)
	sort.Strings(sorted)

	const limit = 20
	truncated := false
	preview := sorted

	if len(sorted) > limit {
		preview = sorted[:limit]
		truncated = true
	}

	return &ResolvedAddress{
		DisplayName: name,
		Type:        typeStr,
		Description: desc,
		Count:       len(entries),
		IsTruncated: truncated,
		Preview:     preview,
	}
}

// portToServiceName converts a port number to a well-known service name.
func portToServiceName(port int, proto string) string {
	services := map[int]string{
		22:    "SSH",
		53:    "DNS",
		80:    "HTTP",
		443:   "HTTPS",
		25:    "SMTP",
		110:   "POP3",
		143:   "IMAP",
		993:   "IMAPS",
		465:   "SMTPS",
		587:   "Submission",
		21:    "FTP",
		3306:  "MySQL",
		5432:  "PostgreSQL",
		6379:  "Redis",
		27017: "MongoDB",
		8080:  "HTTP-Alt",
		8443:  "HTTPS-Alt",
		5353:  "mDNS",
		123:   "NTP",
		161:   "SNMP",
		514:   "Syslog",
	}

	if name, ok := services[port]; ok {
		return name
	}

	// Format as "Proto/Port"
	if proto != "" {
		return proto + "/" + formatPort(port)
	}
	return "Port " + formatPort(port)
}

// formatPort safely formats a port number as string.
func formatPort(port int) string {
	return string(rune('0'+port/10000%10)) +
		string(rune('0'+port/1000%10)) +
		string(rune('0'+port/100%10)) +
		string(rune('0'+port/10%10)) +
		string(rune('0'+port%10))
}
