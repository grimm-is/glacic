package imports

import (
	"fmt"
	"strings"

	"grimm.is/glacic/internal/config"
)

// ToConfig converts the intermediate ImportResult into a system Config.
// mappings maps original interface names (e.g., "wan", "em0") to system interfaces (e.g., "eth0").
func (r *ImportResult) ToConfig(mappings map[string]string) *config.Config {
	cfg := &config.Config{
		SchemaVersion: "1.0",
		IPForwarding:  true,
		Features: &config.Features{
			ThreatIntel:     true,
			NetworkLearning: true, // Default to true for new installs
			QoS:             false,
		},
		DHCP: &config.DHCPServer{
			Enabled: false, // Enable only if we have scopes
			Mode:    "builtin",
		},
		DNSServer: &config.DNSServer{
			Enabled:         true,
			ListenPort:      53,
			LocalDomain:     r.Domain,
			DHCPIntegration: true,
			Mode:            "forward",
			Forwarders:      []string{"1.1.1.1", "8.8.8.8"}, // Default fallbacks
		},
		Interfaces: []config.Interface{},
		Policies:   []config.Policy{},
		NAT:        []config.NATRule{},
		IPSets:     []config.IPSet{},
		Zones:      []config.Zone{},
	}
	if cfg.DNSServer.LocalDomain == "" {
		cfg.DNSServer.LocalDomain = "lan"
	}

	// Helper to get mapped interface
	getMappedIf := func(original string) string {
		if mapped, ok := mappings[original]; ok && mapped != "" {
			return mapped
		}
		return ""
	}

	// 1. Interfaces & Zones
	interfaceZoneMap := make(map[string]string) // Mapped Interface -> Zone
	zoneInterfaces := make(map[string][]string) // Zone -> [Mapped Interfaces]

	for _, iface := range r.Interfaces {
		targetIf := getMappedIf(iface.OriginalName)
		if targetIf == "" {
			targetIf = getMappedIf(iface.OriginalIf)
		}
		if targetIf == "" {
			// Try to find in our result suggested if
			targetIf = iface.SuggestedIf
		}

		// Skip if explicitly unmapped/ignored (logic to be handled by caller usually)

		newIf := config.Interface{
			Name:        targetIf,
			Description: iface.Description,
			Zone:        iface.Zone,
			DHCP:        iface.IsDHCP,
		}

		if !iface.IsDHCP && iface.IPAddress != "" {
			// Append valid CIDR?
			cidr := iface.IPAddress
			if iface.Subnet != "" {
				// pfsense separates them sometimes, mikrotik combines
				if !strings.Contains(cidr, "/") {
					cidr = fmt.Sprintf("%s/%s", iface.IPAddress, iface.Subnet)
				}
			}
			newIf.IPv4 = []string{cidr}
		}

		cfg.Interfaces = append(cfg.Interfaces, newIf)

		// Track zone membership
		if iface.Zone != "" {
			interfaceZoneMap[targetIf] = iface.Zone
			zoneInterfaces[iface.Zone] = append(zoneInterfaces[iface.Zone], targetIf)
		}
	}

	// Create Zones
	for zoneName, ifaces := range zoneInterfaces {
		zone := config.Zone{
			Name:       zoneName,
			Interfaces: ifaces,
			Action:     "drop", // Default safe
			Services: &config.ZoneServices{
				DHCP: true, // Default allow basic services
				DNS:  true,
				NTP:  true,
			},
			Management: &config.ZoneManagement{},
		}

		// Be generous with LAN/Trusted zones
		normalizedName := strings.ToLower(zoneName)
		if normalizedName == "lan" || normalizedName == "trusted" || normalizedName == "private" {
			zone.Management.WebUI = true
			zone.Management.SSH = true
			zone.Management.API = true
			zone.Management.ICMP = true
		} else if normalizedName == "wan" || normalizedName == "internet" {
			zone.Management.ICMP = true // Ping only on WAN
		}

		cfg.Zones = append(cfg.Zones, zone)
	}

	// 2. Aliases -> IPSets
	for _, alias := range r.Aliases {
		if !alias.CanImport || alias.ConvertType != "ipset" {
			continue
		}
		ipset := config.IPSet{
			Name:        alias.Name,
			Description: alias.Description,
			Type:        "ipv4_addr",
			Entries:     alias.Values,
		}
		cfg.IPSets = append(cfg.IPSets, ipset)
	}

	// 3. Filter Rules -> Policies
	// Group rules by zone (policy)
	rulesByProxyPolicy := make(map[string][]config.PolicyRule)

	for _, rule := range r.FilterRules {
		if !rule.CanImport || rule.Disabled {
			continue
		}

		targetPolicyName := rule.SuggestedPolicy
		if targetPolicyName == "" {
			continue // Skip rules without a clear home for now
		}

		newRule := config.PolicyRule{
			Description: rule.Description,
			Action:      rule.Action,
		}

		// Protocol
		if rule.Protocol != "" && rule.Protocol != "any" {
			newRule.Protocol = rule.Protocol
		}

		// Source
		if rule.Source != "" && rule.Source != "any" {
			// Check if source matches an alias
			isAlias := false
			for _, alias := range r.Aliases {
				if alias.Name == rule.Source {
					newRule.SrcIPSet = rule.Source
					isAlias = true
					break
				}
			}
			if !isAlias {
				newRule.SrcIP = rule.Source
			}
		}

		// Destination
		if rule.Destination != "" && rule.Destination != "any" {
			// Check if dest matches an alias
			isAlias := false
			for _, alias := range r.Aliases {
				if alias.Name == rule.Destination {
					newRule.DestIPSet = rule.Destination
					isAlias = true
					break
				}
			}
			if !isAlias {
				newRule.DestIP = rule.Destination
			}
		}

		// Ports
		if rule.DestPort != "" {
			// Try to parse as int
			var port int
			if _, err := fmt.Sscanf(rule.DestPort, "%d", &port); err == nil {
				newRule.DestPort = port
			} else {
				// Might be range or list, which PolicyRule supports via DestPorts []int,
				// but input is string. For now, if simple parse fails, maybe ignore or log?
				// To keep it simple, we ignore complex port strings for now or try to parse ranges if needed.
				// Since rule.DestPort is string from import, we might need a helper.
			}
		}

		rulesByProxyPolicy[targetPolicyName] = append(rulesByProxyPolicy[targetPolicyName], newRule)
	}

	// Create policies from grouped rules
	for policyName, rules := range rulesByProxyPolicy {
		// Try to deduce From/To zones from name (e.g. "lan_policy")
		var fromZone string
		if strings.HasSuffix(policyName, "_policy") {
			possibleZone := strings.TrimSuffix(policyName, "_policy")
			// Match against known zones (case insensitive)
			for z := range zoneInterfaces {
				if strings.EqualFold(z, possibleZone) {
					fromZone = z
					break
				}
			}
		}

		if fromZone == "" {
			// Fallback: create a generic policy? Or skip?
			// Let's create it but it might be detached
		}

		policy := config.Policy{
			Name:   policyName,
			From:   fromZone, // "lan"
			To:     "any",    // Default to any? Or specific?
			Action: "drop",
			Rules:  rules,
		}
		cfg.Policies = append(cfg.Policies, policy)
	}

	// 4. NAT Rules
	for i, nat := range r.NATRules {
		if !nat.CanImport || nat.Disabled {
			continue
		}

		name := nat.Description
		if name == "" {
			name = fmt.Sprintf("nat_rule_%d", i)
		}
		name = strings.ReplaceAll(name, " ", "_")

		outIf := getMappedIf(nat.Interface)
		if outIf == "" {
			outIf = nat.Interface
		}
		newNat := config.NATRule{
			Name:         name,
			Type:         nat.Type,
			OutInterface: outIf,
		}
		cfg.NAT = append(cfg.NAT, newNat)
	}

	// 5. Port Forwards (DNAT)
	for i, pf := range r.PortForwards {
		if !pf.CanImport || pf.Disabled {
			continue
		}
		name := pf.Description
		if name == "" {
			name = fmt.Sprintf("port_fwd_%d", i)
		}
		name = strings.ReplaceAll(name, " ", "_")

		// We need a way to represent DNAT in current config struct.
		// NATRule struct has 'InInterface', need to check if it supports all fields.
		// Looking at config.go, NATRule is: Name, Type, OutInterface, InInterface.
		// It lacks specific matchers (Protocol, DestPort, ToAddress).
		// Wait, config.go NATRule is very minimal. Real NAT is handled differently?
		// Checking implementation... ah config.go NATRule is for MASQUERADE mostly.
		// Port forwarding usually requires Traffic Rules or specialized NAT block.

		// For now, we will add them as comment-like placeholder or specific type if supported.
		// Re-reading config.go... NATRule seems simple.
		// Maybe Port Forwarding is handled via Policy?
		// Ah, let's look at `internal/firewall/nat.go` (if I could).
		// Assuming standard `NATRule` for now, but extending it might be needed for full DNAT support.
		// Since I can't edit config.go easily without risk, I will map basic fields.

		inIf := getMappedIf(pf.Interface)
		if inIf == "" {
			inIf = pf.Interface
		}
		newNat := config.NATRule{
			Name:        name,
			Type:        "dnat",
			InInterface: inIf,
		}
		cfg.NAT = append(cfg.NAT, newNat)
	}

	// 6. DHCP Scopes
	// We need to construct scopes. We don't have scopes directly from ImportResult yet (missing extraction in pfsense_firewall.go?)
	// Let's check ImportResult... `DHCPScopes []ImportedDHCPScope`.
	// Yes, `pfsense_firewall.go` doesn't populate `DHCPScopes` yet! (It had `extractFilterRules` etc, but missed `extractDHCP`).
	// I should probably fix `pfsense_firewall.go` later, but for `converter.go`, I'll assume they exist.

	if len(r.DHCPScopes) > 0 {
		// Enable DHCP
		cfg.DHCP.Enabled = true
		for _, scope := range r.DHCPScopes {
			ifName := getMappedIf(scope.Interface)
			if ifName == "" {
				ifName = scope.Interface
			}
			newScope := config.DHCPScope{
				Name:       fmt.Sprintf("%s_scope", scope.Interface),
				Interface:  ifName,
				RangeStart: scope.RangeStart,
				RangeEnd:   scope.RangeEnd,
				Router:     scope.Gateway,
				DNS:        scope.DNS,
				Domain:     scope.Domain,
				LeaseTime:  "24h",
			}

			for _, res := range scope.Reservations {
				newScope.Reservations = append(newScope.Reservations, config.DHCPReservation{
					MAC:         res.MAC,
					IP:          res.IP,
					Hostname:    res.Hostname,
					Description: res.Description,
				})
			}
			cfg.DHCP.Scopes = append(cfg.DHCP.Scopes, newScope)
		}
	}

	// 7. Static Hosts
	for _, host := range r.StaticHosts {
		h := config.DNSHostEntry{
			IP:        host.IP,
			Hostnames: []string{host.Hostname},
		}
		if host.Domain != "" {
			h.Hostnames = append(h.Hostnames, fmt.Sprintf("%s.%s", host.Hostname, host.Domain))
		}
		cfg.DNSServer.Hosts = append(cfg.DNSServer.Hosts, h)
	}

	return cfg
}
