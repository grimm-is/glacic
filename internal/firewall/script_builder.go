package firewall

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"grimm.is/glacic/internal/config"
)

var identifierRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// RFC1918 private address ranges (should not appear on WAN as source)
var protectionPrivateNetworks = []*net.IPNet{
	mustParseCIDR("10.0.0.0/8"),
	mustParseCIDR("172.16.0.0/12"),
	mustParseCIDR("192.168.0.0/16"),
}

// Bogon ranges - reserved/invalid addresses
var protectionBogonNetworks = []*net.IPNet{
	mustParseCIDR("0.0.0.0/8"),       // reserved
	mustParseCIDR("127.0.0.0/8"),     // loopback
	mustParseCIDR("169.254.0.0/16"),  // link-local
	mustParseCIDR("192.0.0.0/24"),    // IETF protocol
	mustParseCIDR("192.0.2.0/24"),    // TEST-NET-1
	mustParseCIDR("198.51.100.0/24"), // TEST-NET-2
	mustParseCIDR("203.0.113.0/24"),  // TEST-NET-3
	mustParseCIDR("224.0.0.0/4"),     // multicast
	mustParseCIDR("240.0.0.0/4"),     // reserved
}

func isValidIdentifier(s string) bool {
	return identifierRegex.MatchString(s)
}

func quote(s string) string {
	if isValidIdentifier(s) {
		return s
	}
	return fmt.Sprintf("%q", s)
}

// forceQuote always quotes a string - needed for interface names in concatenation sets
// where nftables requires quoted identifiers even for valid names.
func forceQuote(s string) string {
	return fmt.Sprintf("%q", s)
}

// ScriptBuilder builds nftables scripts for atomic application.
type ScriptBuilder struct {
	lines     []string
	tableName string
	family    string
}

// NewScriptBuilder creates a new script builder for the given table.
func NewScriptBuilder(tableName, family string) *ScriptBuilder {
	return &ScriptBuilder{
		tableName: tableName,
		family:    family,
		lines:     make([]string, 0, 100),
	}
}

// AddLine adds a raw nft command line to the script.
func (b *ScriptBuilder) AddLine(line string) {
	b.lines = append(b.lines, line)
}

// AddTable adds a table creation command.
func (b *ScriptBuilder) AddTable() {
	b.AddLine(fmt.Sprintf("add table %s %s", b.family, b.tableName))
}

// AddChain adds a chain creation command.
func (b *ScriptBuilder) AddChain(name, chainType string, hook string, priority int, policy string) {
	if !isValidIdentifier(name) {
		// Quoting is safer.
	}
	qName := quote(name)
	if chainType != "" && hook != "" {
		policyStr := ""
		if policy != "" {
			policyStr = fmt.Sprintf("policy %s; ", policy)
		}
		b.AddLine(fmt.Sprintf("add chain %s %s %s { type %s hook %s priority %d; %s}",
			b.family, b.tableName, qName, chainType, hook, priority, policyStr))
	} else {
		// Regular chain (no hook)
		b.AddLine(fmt.Sprintf("add chain %s %s %s", b.family, b.tableName, qName))
	}
}

// AddRule adds a rule to a chain.
func (b *ScriptBuilder) AddRule(chainName, ruleExpr string) {
	b.AddLine(fmt.Sprintf("add rule %s %s %s %s", b.family, b.tableName, quote(chainName), ruleExpr))
}

// AddSet adds a set creation command.
func (b *ScriptBuilder) AddSet(name, setType string, flags ...string) {
	flagStr := ""
	if len(flags) > 0 {
		flagStr = " flags " + strings.Join(flags, ",") + ";"
	}
	b.AddLine(fmt.Sprintf("add set %s %s %s { type %s;%s }", b.family, b.tableName, quote(name), setType, flagStr))
}

// AddSetElements adds elements to an existing set.
func (b *ScriptBuilder) AddSetElements(setName string, elements []string) {
	if len(elements) == 0 {
		return
	}
	quotedElements := make([]string, len(elements))
	for i, e := range elements {
		// Elements might be complex (e.g. intervals), but quoting usually safe?
		// nftables set elements generally don't need quotes unless they contain special chars.
		// We'll rely on our quote helper which only quotes if needed.
		quotedElements[i] = quote(e)
	}
	b.AddLine(fmt.Sprintf("add element %s %s %s { %s }", b.family, b.tableName, quote(setName), strings.Join(quotedElements, ", ")))
}

// AddFlowtable adds a flowtable definition.
func (b *ScriptBuilder) AddFlowtable(name string, devices []string) {
	if len(devices) == 0 {
		return
	}
	// flowtable ft { hook ingress priority 0; devices = { eth0, eth1 }; }
	devs := make([]string, len(devices))
	for i, d := range devices {
		devs[i] = quote(d)
	}
	b.AddLine(fmt.Sprintf("add flowtable %s %s %s { hook ingress priority 0; devices = { %s }; }",
		b.family, b.tableName, quote(name), strings.Join(devs, ", ")))
}

// AddMap adds a map definition.
func (b *ScriptBuilder) AddMap(name, typeKey, typeValue string, flags, elements []string) {
	// add map inet filter zone_map { type ifname : verdict; }
	flagStr := ""
	if len(flags) > 0 {
		flagStr = fmt.Sprintf("flags %s; ", strings.Join(flags, ", "))
	}
	b.AddLine(fmt.Sprintf("add map %s %s %s { type %s : %s; %s}",
		b.family, b.tableName, quote(name), typeKey, typeValue, flagStr))

	if len(elements) > 0 {
		b.AddLine(fmt.Sprintf("add element %s %s %s { %s }",
			b.family, b.tableName, quote(name), strings.Join(elements, ", ")))
	}
}

// Build returns the complete script as a string.
func (b *ScriptBuilder) Build() string {
	return strings.Join(b.lines, "\n") + "\n"
}

// String returns the script for debugging.
func (b *ScriptBuilder) String() string {
	return b.Build()
}

// BuildFilterTableScript builds the complete filter table script from config.
func BuildFilterTableScript(cfg *Config, vpn *config.VPNConfig, tableName string) (*ScriptBuilder, error) {
	sb := NewScriptBuilder(tableName, "inet")

	// Create table
	sb.AddTable()

	// Flowtables (Performance Optimization)
	// Hardware/Software Offload for established connections.
	if cfg.EnableFlowOffload {
		var flowDevices []string
		for _, iface := range cfg.Interfaces {
			// Only add interfaces that exist and are managed.
			// Ideally we verify they support offload, but we can just try adding them.
			flowDevices = append(flowDevices, iface.Name)
		}
		if len(flowDevices) > 0 {
			sb.AddFlowtable("ft", flowDevices)
		}
	}

	// Add Protection Chain (Raw Prerouting)
	addProtectionRules(cfg, sb)

	// CRITICAL: Define IPSets BEFORE rules that reference them
	for _, ipset := range cfg.IPSets {
		setType := ipset.Type
		if setType == "" {
			setType = "ipv4_addr"
		}

		// Infer interval flag: needed if entries contain CIDR notation
		var flags []string
		for _, entry := range ipset.Entries {
			if strings.Contains(entry, "/") {
				flags = append(flags, "interval")
				break
			}
		}

		if !isValidIdentifier(ipset.Name) {
			return nil, fmt.Errorf("invalid ipset name: %s", ipset.Name)
		}
		sb.AddSet(ipset.Name, setType, flags...)

		// Add elements if specified inline
		if len(ipset.Entries) > 0 {
			sb.AddSetElements(ipset.Name, ipset.Entries)
		}
	}

	// Create base chains with default drop policy
	sb.AddChain("input", "filter", "input", 0, "drop")
	sb.AddChain("forward", "filter", "forward", 0, "drop")
	sb.AddChain("output", "filter", "output", 0, "drop")

	// Add base rules (loopback, established/related)
	// Loopback handled explicitly in services section to be safe, but can stay here too
	sb.AddRule("input", "ct state established,related accept")
	sb.AddRule("forward", "ct state established,related accept")
	sb.AddRule("output", "ct state established,related accept")

	// MSS Clamping to PMTU (Forward Chain)
	// Keeps TCP connections healthy across links with different MTUs (e.g. PPPoE, VPNs)
	if cfg.MSSClamping {
		// iptables -A FORWARD -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
		// nft: tcp flags syn / syn,rst tcp option maxseg size set rt mtu
		sb.AddRule("forward", "tcp flags syn tcp option maxseg size set rt mtu")
	}

	// Flowtable Offload Rule (Forward Chain)
	// Bypasses the rest of the ruleset for established connections in the flowtable.
	if cfg.EnableFlowOffload {
		sb.AddRule("forward", "ip protocol { tcp, udp } flow add @ft")
	}

	// Drop invalid packets early with logging
	sb.AddRule("input", "ct state invalid limit rate 10/minute burst 5 packets log group 0 prefix \"DROP_INVALID: \" counter drop")
	sb.AddRule("forward", "ct state invalid limit rate 10/minute burst 5 packets log group 0 prefix \"DROP_INVALID: \" counter drop")
	sb.AddRule("output", "ct state invalid limit rate 10/minute burst 5 packets log group 0 prefix \"DROP_INVALID: \" counter drop")

	// Add ICMP accept rules
	sb.AddRule("input", "meta l4proto icmp accept")
	sb.AddRule("input", "meta l4proto icmpv6 accept")
	// IPv6 Neighbor Discovery (Vital for IPv6 connectivity)
	sb.AddRule("input", "icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-solicit, nd-router-advert } accept")
	sb.AddRule("output", "meta l4proto icmp accept")
	sb.AddRule("output", "meta l4proto icmpv6 accept")
	sb.AddRule("output", "icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-solicit, nd-router-advert } accept")

	// mDNS Reflector Rules (Must be before generic drops)
	if cfg.MDNS != nil && cfg.MDNS.Enabled && len(cfg.MDNS.Interfaces) > 0 {
		var mdnsIfaces []string
		for _, iface := range cfg.MDNS.Interfaces {
			mdnsIfaces = append(mdnsIfaces, forceQuote(iface))
		}
		ifacesStr := strings.Join(mdnsIfaces, ", ")

		// Allow INPUT multicast (query/response) on enabled interfaces
		// Match both specific multicast IP and port ensuring we only accept mDNS traffic
		sb.AddRule("input", fmt.Sprintf("iifname { %s } ip daddr 224.0.0.251 udp dport 5353 accept", ifacesStr))
		sb.AddRule("input", fmt.Sprintf("iifname { %s } ip6 daddr ff02::fb udp dport 5353 accept", ifacesStr))

		// Allow OUTPUT multicast (reflector sending)
		sb.AddRule("output", fmt.Sprintf("oifname { %s } ip daddr 224.0.0.251 udp dport 5353 accept", ifacesStr))
		sb.AddRule("output", fmt.Sprintf("oifname { %s } ip6 daddr ff02::fb udp dport 5353 accept", ifacesStr))
	}

	// Drop mDNS noise on VPN interfaces (unless explicitly enabled above?)
	// Note: If a VPN interface was in MDNS.Interfaces, the rule above would have accepted it first.
	sb.AddRule("input", "iifname \"wg*\" udp dport 5353 limit rate 5/minute log group 0 prefix \"DROP_MDNS: \" counter drop")
	sb.AddRule("input", "iifname \"tun*\" udp dport 5353 limit rate 5/minute log group 0 prefix \"DROP_MDNS: \" counter drop")

	// Allow forwarded traffic to API Sandbox (Global Allow, bypasses Zone Policy)
	// This makes the API accessible even if the zone policy is strict, provided
	// the interface has AccessWebUI enabled (which handles the DNAT).
	// But we need to allow the *forwarded* packet to pass the filter.
	// 169.254.255.2 is the fixed IP of the API namespace.
	sb.AddRule("forward", "ip daddr 169.254.255.2 tcp dport {8080, 8443} counter accept")

	// Allow LOCAL traffic to API Sandbox (Output Chain)
	// Needed for 'curl localhost:8080' on the box itself.
	sb.AddRule("output", "ip daddr 169.254.255.2 tcp dport {8080, 8443} counter accept")

	// Network Learning Rules
	// Log TLS traffic (heuristic: established, port 443) for SNI inspection.
	// The nflog reader will extract the Client Hello.
	// We rate limit to prevent flooding the control plane.
	sb.AddRule("forward", "tcp dport 443 ct state established limit rate 50/second burst 20 packets log group 100 prefix \"TLS_SNI: \"")

	// Add essential service rules (DHCP, DNS for router itself)
	// DHCP Client (WAN) and Server (LAN)
	sb.AddRule("input", "udp dport 67-68 accept")
	sb.AddRule("output", "udp dport 67-68 accept")

	// VPN Transport Rules (WireGuard)
	if vpn != nil {
		for _, wg := range vpn.WireGuard {
			if wg.Enabled {
				// Allow incoming handshake/data on ListenPort
				if wg.ListenPort > 0 {
					sb.AddRule("input", fmt.Sprintf("udp dport %d accept", wg.ListenPort))
				}
				// Allow outgoing handshake/data to Peers
				for _, peer := range wg.Peers {
					// Extract port from Endpoint (host:port)
					parts := strings.Split(peer.Endpoint, ":")
					if len(parts) == 2 {
						port := parts[1] // Simple extraction, validation assumes config is checked
						// We don't validate port here, just pass to rule. If invalid, nft might fail?
						// Better to validate if possible, but safe strings are okay.
						// Add rule allowing output to this port
						sb.AddRule("output", fmt.Sprintf("udp dport %s accept", port))
					}
				}
			}
		}
	}

	// DNS and API Access (Zone-Aware / Interface-Aware)
	// Loopback always allowed
	sb.AddRule("input", "iifname \"lo\" accept")
	sb.AddRule("output", "oifname \"lo\" accept")

	// Global Output enabled for DNS (router resolving)
	sb.AddRule("output", "udp dport 53 accept")
	sb.AddRule("output", "tcp dport 53 accept")

	// Allow DNS/API/SSH/etc based on Zone and Interface config

	// Consolidate services into TCP and UDP sets for concatenation
	// Format: "iifname . port"
	var tcpElements []string
	var udpElements []string
	var icmpElements []string

	// Helper to add a service for an interface
	addService := func(ifaceName, serviceName string) {
		svc, ok := BuiltinServices[serviceName]
		if !ok {
			return // Should not happen for known builtins
		}

		// Handle TCP
		if svc.Protocol&ProtoTCP != 0 {
			if len(svc.Ports) > 0 {
				for _, p := range svc.Ports {
					tcpElements = append(tcpElements, fmt.Sprintf("%s . %d", ifaceName, p))
				}
			} else if svc.Port > 0 {
				tcpElements = append(tcpElements, fmt.Sprintf("%s . %d", ifaceName, svc.Port))
			} else if svc.Ports == nil && svc.Port == 0 && svc.EndPort == 0 {
				// No ports defined (rare for TCP service, maybe fully custom?)
			}
		}

		// Handle UDP
		if svc.Protocol&ProtoUDP != 0 {
			if len(svc.Ports) > 0 {
				for _, p := range svc.Ports {
					udpElements = append(udpElements, fmt.Sprintf("%s . %d", ifaceName, p))
				}
			} else if svc.Port > 0 {
				udpElements = append(udpElements, fmt.Sprintf("%s . %d", ifaceName, svc.Port))
			}
		}

		// Handle ICMP
		if svc.Protocol&ProtoICMP != 0 {
			icmpElements = append(icmpElements, ifaceName)
		}
	}

	// Build map of Interface Name -> Zone Pointer for fast lookup
	ifaceToZone := make(map[string]*config.Zone)
	for i := range cfg.Zones {
		z := &cfg.Zones[i]
		// Canonical matches
		for _, m := range z.Matches {
			if m.Interface != "" {
				ifaceToZone[m.Interface] = z
			}
		}
		// Legacy interfaces list (backup)
		for _, ifName := range z.Interfaces {
			ifaceToZone[ifName] = z
		}
	}

	// Iterate interfaces to collect allowed services
	for _, iface := range cfg.Interfaces {
		// 1. Zone Config (Default)
		var zone *config.Zone
		
		if iface.Zone != "" {
			// Try to find zone by explicit name first
			for i := range cfg.Zones {
				if strings.EqualFold(cfg.Zones[i].Name, iface.Zone) {
					zone = &cfg.Zones[i]
					break
				}
			}
		}
		
		// Fallback: Look up by interface map (if zone not found or not specified on interface)
		if zone == nil {
			zone = ifaceToZone[iface.Name]
		}

		// Defaults
		allowDNS := false
		allowNTP := false
		allowSSH := false
		allowWeb := false
		allowAPI := false
		allowICMP := false
		allowSNMP := false
		allowSyslog := false

		// Apply Zone Defaults
		if zone != nil {
			// Services
			if zone.Services != nil {
				allowDNS = zone.Services.DNS
				allowNTP = zone.Services.NTP

				// Custom Ports (Zone only)
				for _, cp := range zone.Services.CustomPorts {
					endPort := cp.EndPort
					if endPort == 0 {
						endPort = cp.Port
					}
					// Inline rule for custom ports (ranges hard to putting in sets with single ports)
					sb.AddRule("input", fmt.Sprintf("iifname %q %s dport %d-%d accept", iface.Name, cp.Protocol, cp.Port, endPort))
				}
			} else {
				// Legacy heuristic
				isLikelyExternal := strings.EqualFold(zone.Name, "WAN") || strings.EqualFold(zone.Name, "Internet")
				allowDNS = !isLikelyExternal
			}

			// Management Defaults
			if zone.Management != nil {
				allowSSH = zone.Management.SSH
				allowWeb = zone.Management.Web || zone.Management.WebUI
				allowAPI = zone.Management.API
				allowICMP = zone.Management.ICMP
				allowSNMP = zone.Management.SNMP
				allowSyslog = zone.Management.Syslog
			}
		}

		// 2. Interface Overrides (Priority)
		if iface.Management != nil {
			allowSSH = iface.Management.SSH
			allowWeb = iface.Management.Web || iface.Management.WebUI
			allowAPI = iface.Management.API
			allowICMP = iface.Management.ICMP
			allowSNMP = iface.Management.SNMP
			allowSyslog = iface.Management.Syslog
		} else {
			// Legacy Interface-level fallback
			if iface.AccessWebUI {
				allowWeb = true
				allowAPI = true
			}
		}


		// Quote interface name for nftables
		qIface := forceQuote(iface.Name)

		// Collect services
		if allowDNS {
			addService(qIface, "dns")
		}
		if allowNTP {
			addService(qIface, "ntp")
		}
		if allowSSH {
			addService(qIface, "ssh")
		}
		if allowICMP {
			addService(qIface, "icmp")
		}
		if allowSNMP {
			addService(qIface, "snmp")
		}
		if allowSyslog {
			addService(qIface, "syslog")
		}


			if allowWeb || allowAPI {
				addService(qIface, "http")
				addService(qIface, "https")

				if allowAPI || allowWeb {
					// Explicitly allow default API port 8443 (WebUI/API)
					tcpElements = append(tcpElements, fmt.Sprintf("%s . %d", qIface, 8443))
				}

				// Allow forwarding to sandbox for web/api
				// This is critical for DNATed traffic to reach the API namespace
			// 169.254.255.2 is the hardcoded sandbox IP
			sb.AddRule("forward", fmt.Sprintf("iifname %s ip daddr 169.254.255.2 tcp dport { 8080, 8443 } accept", qIface))

			// Custom port if legacy field used
			if iface.WebUIPort > 0 && iface.WebUIPort != 80 && iface.WebUIPort != 443 {
				tcpElements = append(tcpElements, fmt.Sprintf("%s . %d", qIface, iface.WebUIPort))
			}
		}
	}

	// Process zones directly (for zones using match blocks instead of interface.zone)
	// This ensures zones with management/services blocks are processed even without explicit interface config
	zoneMap := buildZoneMapForScript(cfg)
	for _, zone := range cfg.Zones {
		if zone.Management == nil && zone.Services == nil {
			continue // No rules to generate
		}

		// Get interfaces for this zone
		zoneIfaces, ok := zoneMap[zone.Name]
		if !ok || len(zoneIfaces) == 0 {
			continue
		}

		for _, ifaceName := range zoneIfaces {
			qIface := forceQuote(ifaceName)

			// Zone Services
			if zone.Services != nil {
				if zone.Services.DNS {
					addService(qIface, "dns")
				}
				if zone.Services.NTP {
					addService(qIface, "ntp")
				}
				if zone.Services.DHCP {
					// DHCP is already globally allowed, but if we want interface-specific:
					// addService(qIface, "dhcp")
				}
			}

			// Zone Management
			if zone.Management != nil {
				if zone.Management.SSH {
					addService(qIface, "ssh")
				}
				if zone.Management.Web || zone.Management.WebUI || zone.Management.API {
					addService(qIface, "http")
					addService(qIface, "https")

					if zone.Management.API || zone.Management.Web || zone.Management.WebUI {
						tcpElements = append(tcpElements, fmt.Sprintf("%s . %d", qIface, 8443))
					}

					// Allow forwarding to sandbox for web/api
					sb.AddRule("forward", fmt.Sprintf("iifname %s ip daddr 169.254.255.2 tcp dport { 8080, 8443 } accept", qIface))
				}
				if zone.Management.ICMP {
					addService(qIface, "icmp")
				}
				if zone.Management.SNMP {
					addService(qIface, "snmp")
				}
				if zone.Management.Syslog {
					addService(qIface, "syslog")
				}
			}
		}
	}

	// Apply Consolidated Rules
	if len(tcpElements) > 0 {
		sb.AddRule("input", fmt.Sprintf("iifname . tcp dport { %s } accept", strings.Join(tcpElements, ", ")))
	}

	if len(udpElements) > 0 {
		sb.AddRule("input", fmt.Sprintf("iifname . udp dport { %s } accept", strings.Join(udpElements, ", ")))
	}

	// ICMP (Keep separate as it doesn't match dport)
	if len(icmpElements) > 0 {
		sb.AddRule("input", fmt.Sprintf("iifname { %s } meta l4proto icmp accept", strings.Join(icmpElements, ", ")))
	}

	// Add auto-generated IPSet block rules
	for _, ipset := range cfg.IPSets {
		if ipset.Action == "" {
			continue
		}

		// Determine action
		action := "drop"
		switch strings.ToLower(ipset.Action) {
		case "accept":
			action = "accept"
		case "reject":
			action = "reject"
		}

		// Determine chains
		applyInput := ipset.ApplyTo == "input" || ipset.ApplyTo == "both" || ipset.ApplyTo == ""
		applyForward := ipset.ApplyTo == "forward" || ipset.ApplyTo == "both"

		// Default match on source if not specified
		matchSource := ipset.MatchOnSource || (!ipset.MatchOnSource && !ipset.MatchOnDest)
		matchDest := ipset.MatchOnDest

		// Determine address family
		addrFamily := "ip"
		if ipset.Type == "ipv6_addr" {
			addrFamily = "ip6"
		}

		if applyInput {
			if matchSource {
				sb.AddRule("input", fmt.Sprintf("%s saddr @%s limit rate 10/minute log group 0 prefix \"DROP_IPSET: \" counter %s", addrFamily, quote(ipset.Name), action))
			}
			if matchDest {
				sb.AddRule("input", fmt.Sprintf("%s daddr @%s limit rate 10/minute log group 0 prefix \"DROP_IPSET: \" counter %s", addrFamily, quote(ipset.Name), action))
			}
		}

		if applyForward {
			if matchSource {
				sb.AddRule("forward", fmt.Sprintf("%s saddr @%s limit rate 10/minute log group 0 prefix \"DROP_IPSET: \" counter %s", addrFamily, ipset.Name, action))
			}
			if matchDest {
				sb.AddRule("forward", fmt.Sprintf("%s daddr @%s limit rate 10/minute log group 0 prefix \"DROP_IPSET: \" counter %s", addrFamily, ipset.Name, action))
			}
		}
	}

	// Build zone map for policy rules
	zoneMap = buildZoneMapForScript(cfg)

	// Add policy chains and rules
	// Initialize maps for O(1) dispatch
	inputMap := make(map[string]string)   // iifname -> verdict
	forwardMap := make(map[string]string) // iifname . oifname -> verdict

	for _, pol := range cfg.Policies {
		if pol.Disabled {
			continue
		}

		chainName := fmt.Sprintf("policy_%s_%s", pol.From, pol.To)
		if !isValidIdentifier(chainName) {
			// This shouldn't happen if zones are valid, but check
			return nil, fmt.Errorf("invalid policy chain name derived from zones: %s", chainName)
		}
		sb.AddChain(chainName, "", "", 0, "") // Regular chain, no hook

		// Add rules to policy chain
		for _, rule := range pol.Rules {
			if rule.Disabled {
				continue
			}
			ruleExpr, err := BuildRuleExpression(rule)
			if err != nil {
				return nil, err
			}
			if ruleExpr != "" {
				sb.AddRule(chainName, ruleExpr)
			}
		}

		// Add default action
		defaultAction := "drop"
		if strings.ToLower(pol.Action) == "accept" {
			defaultAction = "accept"
		}

		// If default is drop/reject, log it
		if defaultAction != "accept" {
			prefix := fmt.Sprintf("DROP_POL_%s_%s: ", pol.From, pol.To)
			// Truncate prefix if too long (nft restrict < 128 usually, but prefix itself should be short)
			if len(prefix) > 28 {
				prefix = prefix[:28] + ": "
			}
			sb.AddRule(chainName, fmt.Sprintf("limit rate 10/minute log group 0 prefix %q counter %s", prefix, defaultAction))
		} else {
			sb.AddRule(chainName, "counter "+defaultAction)
		}

		// Map Collection for Verdict Maps (Optimization)
		fromIfaces := zoneMap[pol.From]
		toIfaces := zoneMap[pol.To]

		if strings.EqualFold(pol.To, "firewall") || strings.EqualFold(pol.To, "self") {
			// Input Chain Dispatch
			for _, iface := range fromIfaces {
				// Map key: iifname
				// Check for duplicates? First match wins (linear behavior simulation)
				if _, exists := inputMap[iface]; !exists {
					inputMap[iface] = fmt.Sprintf("jump %s", chainName)
				}
			}
		} else {
			// Forward Chain Dispatch
			for _, src := range fromIfaces {
				for _, dst := range toIfaces {
					// Map key: iifname . oifname
					key := fmt.Sprintf("%s . %s", quote(src), quote(dst))
					if _, exists := forwardMap[key]; !exists {
						forwardMap[key] = fmt.Sprintf("jump %s", chainName)
					}
				}
			}
		}
	}

	// Generate Verdict Maps and Dispatch Rules
	// Input Map
	if len(inputMap) > 0 {
		var elements []string
		for iface, verdict := range inputMap {
			elements = append(elements, fmt.Sprintf("%s : %s", quote(iface), verdict))
		}
		sb.AddMap("input_vmap", "ifname", "verdict", nil, elements)
		sb.AddRule("input", "iifname vmap @input_vmap")
	}

	// Forward Map
	if len(forwardMap) > 0 {
		var elements []string
		for key, verdict := range forwardMap {
			elements = append(elements, fmt.Sprintf("%s : %s", key, verdict))
		}
		sb.AddMap("forward_vmap", "ifname . ifname", "verdict", nil, elements)
		sb.AddRule("forward", "meta iifname . meta oifname vmap @forward_vmap")
	}

	// Add final drop rules (already in chain policy, but explicit for logging)
	// Add final drop rules with rate-limited logging to prevent "Log Spam Death Spiral"
	//
	// When inline learning mode is enabled, we use nfqueue instead of drop.
	// This holds packets until the learning engine returns a verdict, fixing the
	// "first packet" problem where the first packet of a new flow would be dropped
	// before the allow rule could be added.
	if cfg.RuleLearning != nil && cfg.RuleLearning.Enabled && cfg.RuleLearning.InlineMode {
		// Use nfqueue for inline mode
		// 'bypass' flag = accept if queue full (fail-open)
		queueGroup := cfg.RuleLearning.LogGroup
		if queueGroup == 0 {
			queueGroup = 100
		}
		sb.AddRule("input", fmt.Sprintf("queue num %d bypass", queueGroup))
		sb.AddRule("forward", fmt.Sprintf("queue num %d bypass", queueGroup))
	} else {
		// Standard async mode with nflog
		sb.AddRule("input", "limit rate 10/minute burst 5 packets log group 0 prefix \"DROP_INPUT: \" counter drop")
		sb.AddRule("forward", "limit rate 10/minute burst 5 packets log group 0 prefix \"DROP_FWD: \" counter drop")
	}


	return sb, nil
}

// buildZoneMapForScript builds a zone-to-interfaces map.
// Uses ZoneResolver to handle new match-based zone format and backwards compat.
func buildZoneMapForScript(cfg *Config) map[string][]string {
	zoneMap := make(map[string][]string)

	// Use ZoneResolver for new-style zone definitions
	resolver := config.NewZoneResolver(cfg.Zones)

	// 1. Process zones using ZoneResolver
	for _, zone := range cfg.Zones {
		ifaces := resolver.GetZoneInterfaces(zone.Name)
		if len(ifaces) > 0 {
			zoneMap[zone.Name] = ifaces
		} else {
			zoneMap[zone.Name] = []string{}
		}
	}

	// 2. Add interfaces that reference a Zone (interface-level zone assignment)
	// This is the legacy way: interface "eth0" { zone = "WAN" }
	for _, iface := range cfg.Interfaces {
		// Implicit Interface Zones: Every interface is also a zone of its own name
		if _, exists := zoneMap[iface.Name]; !exists {
			zoneMap[iface.Name] = []string{iface.Name}
		}

		if iface.Zone != "" {
			// Check if already in list (avoid duplicates)
			found := false
			for _, existing := range zoneMap[iface.Zone] {
				if existing == iface.Name {
					found = true
					break
				}
			}
			if !found {
				zoneMap[iface.Zone] = append(zoneMap[iface.Zone], iface.Name)
			}
		}
	}

	return zoneMap
}

// BuildRuleExpression converts a PolicyRule to an nft rule expression string.
// Exported for use by the API layer to show generated syntax.
func BuildRuleExpression(rule config.PolicyRule) (string, error) {
	var parts []string

	// Protocol
	if rule.Protocol != "" && rule.Protocol != "any" {
		parts = append(parts, fmt.Sprintf("meta l4proto %s", rule.Protocol))
	}

	// Source IP
	if rule.SrcIP != "" {
		parts = append(parts, fmt.Sprintf("ip saddr %s", rule.SrcIP))
	}

	// Source IPSet - CRITICAL: Must not be ignored!
	if rule.SrcIPSet != "" {
		if !isValidIdentifier(rule.SrcIPSet) {
			return "", fmt.Errorf("invalid source ipset name: %s", rule.SrcIPSet)
		}
		parts = append(parts, fmt.Sprintf("ip saddr @%s", quote(rule.SrcIPSet)))
	}

	// Destination IP
	if rule.DestIP != "" {
		parts = append(parts, fmt.Sprintf("ip daddr %s", rule.DestIP))
	}

	// Destination IPSet - CRITICAL: Must not be ignored!
	if rule.DestIPSet != "" {
		if !isValidIdentifier(rule.DestIPSet) {
			return "", fmt.Errorf("invalid dest ipset name: %s", rule.DestIPSet)
		}
		parts = append(parts, fmt.Sprintf("ip daddr @%s", quote(rule.DestIPSet)))
	}

	// Connection state (ct state) - CRITICAL for stateful filtering!
	if rule.ConnState != "" {
		// Validate conn_state values: new, established, related, invalid
		validStates := map[string]bool{
			"new":         true,
			"established": true,
			"related":     true,
			"invalid":     true,
		}
		// conn_state can be comma-separated: "established,related"
		stateList := strings.Split(rule.ConnState, ",")
		for _, s := range stateList {
			s = strings.TrimSpace(strings.ToLower(s))
			if !validStates[s] {
				return "", fmt.Errorf("invalid conn_state value: %s (valid: new, established, related, invalid)", s)
			}
		}
		parts = append(parts, fmt.Sprintf("ct state %s", rule.ConnState))
	}

	// Time-of-day matching (meta hour/day, requires kernel 5.4+)
	if rule.TimeStart != "" && rule.TimeEnd != "" {
		// nftables uses: meta hour >= 09:00:00 meta hour < 17:00:00
		// Time format must be HH:MM:SS without quotes
		start := rule.TimeStart
		if len(start) == 5 {
			start += ":00"
		}
		end := rule.TimeEnd
		if len(end) == 5 {
			end += ":00"
		}
		parts = append(parts, fmt.Sprintf("meta hour >= %s meta hour < %s", start, end))
	}

	// Days of week matching
	if len(rule.Days) > 0 {
		// nftables uses lowercase day names
		days := make([]string, len(rule.Days))
		for i, d := range rule.Days {
			days[i] = strings.ToLower(d)
		}
		parts = append(parts, fmt.Sprintf("meta day { %s }", strings.Join(days, ", ")))
	}

	// Destination port - only if L4 protocol
	if rule.DestPort > 0 {
		proto := rule.Protocol
		if proto == "" || proto == "any" {
			proto = "tcp" // Default to TCP for port rules
		}
		// Only add port for L4 protocols
		if proto == "tcp" || proto == "udp" {
			parts = append(parts, fmt.Sprintf("%s dport %d", proto, rule.DestPort))
		}
	}

	// Action
	action := "accept"
	switch strings.ToLower(rule.Action) {
	case "drop":
		action = "drop"
	case "reject":
		action = "reject"
	}

	// Add logging if action is drop/reject
	if action != "accept" {
		// Log rule
		parts = append(parts, "limit rate 10/minute log group 0 prefix \"DROP_RULE: \"")
	}

	// Add counter for observability (required for sparklines)
	// Named counter if specified, anonymous otherwise
	if rule.Counter != "" {
		parts = append(parts, fmt.Sprintf("counter name %q", rule.Counter))
	} else {
		parts = append(parts, "counter")
	}

	// Add verdict
	parts = append(parts, action)

	// Add rule ID comment for stats collector correlation
	// This allows mapping nft counters back to config rule IDs
	if rule.ID != "" {
		parts = append(parts, fmt.Sprintf(`comment "rule:%s"`, rule.ID))
	} else if rule.Name != "" {
		// Fallback for rules without explicit IDs
		parts = append(parts, fmt.Sprintf(`comment "rule:%s"`, rule.Name))
	}

	return strings.Join(parts, " "), nil
}

// addProtectionRules adds protection rules to the script
func addProtectionRules(cfg *Config, sb *ScriptBuilder) {
	if len(cfg.Protections) == 0 {
		return
	}

	// Create protection chain (hooks into prerouting, priority raw -300)
	sb.AddChain("protection", "filter", "prerouting", -300, "accept")

	for _, p := range cfg.Protections {
		// Enable logic: If block exists, we assume it's enabled unless explicitly disabled?
		if !p.Enabled && false {
			// Skipping check as per discussion
		}

		ifaceMatch := ""
		if p.Interface != "" && p.Interface != "*" {
			ifaceMatch = fmt.Sprintf("iifname %q ", p.Interface)
		}

		// Invalid Packets
		if p.InvalidPackets {
			// ct state invalid drop
			sb.AddRule("protection", fmt.Sprintf("%sct state invalid limit rate 10/minute log group 0 prefix \"DROP_INVALID: \" counter drop", ifaceMatch))
		}

		// Anti-Spoofing (RFC1918)
		if p.AntiSpoofing {
			for _, cidr := range protectionPrivateNetworks {
				// Use CIDR notation directly with nft
				sb.AddRule("protection", fmt.Sprintf("%sip saddr %s limit rate 10/minute log group 0 prefix \"SPOOFED-SRC: \" counter drop", ifaceMatch, cidr.String()))
			}
		}

		// Bogon Filtering
		if p.BogonFiltering {
			for _, cidr := range protectionBogonNetworks {
				sb.AddRule("protection", fmt.Sprintf("%sip saddr %s counter drop", ifaceMatch, cidr.String()))
			}
		}

		// SYN Flood Protection
		if p.SynFloodProtection {
			rate := p.SynFloodRate
			if rate == 0 {
				rate = 25
			}
			burst := p.SynFloodBurst
			if burst == 0 {
				burst = 50
			}
			// tcp flags & (fin|syn|rst|ack) == syn
			sb.AddRule("protection", fmt.Sprintf("%stcp flags & (fin|syn|rst|ack) == syn limit rate %d/second burst %d packets return", ifaceMatch, rate, burst))
			sb.AddRule("protection", fmt.Sprintf("%stcp flags & (fin|syn|rst|ack) == syn limit rate 10/minute log group 0 prefix \"DROP_SYNFLOOD: \" counter drop", ifaceMatch))
		}

		// ICMP Rate Limit
		if p.ICMPRateLimit {
			rate := p.ICMPRate
			if rate == 0 {
				rate = 10
			}
			// burst = 2 * rate
			sb.AddRule("protection", fmt.Sprintf("%smeta l4proto icmp limit rate %d/second burst %d packets return", ifaceMatch, rate, rate*2))
			sb.AddRule("protection", fmt.Sprintf("%smeta l4proto icmp limit rate 10/minute log group 0 prefix \"DROP_ICMPFLOOD: \" counter drop", ifaceMatch))
		}
	}
}

// BuildNATTableScript builds the NAT table script from config.
func BuildNATTableScript(cfg *Config) (*ScriptBuilder, error) {
	if len(cfg.NAT) == 0 && cfg.Interfaces == nil {
		return nil, nil
	}

	sb := NewScriptBuilder("nat", "ip")
	sb.AddTable()

	// Prerouting chain for DNAT (policy accept)
	sb.AddChain("prerouting", "nat", "prerouting", -100, "accept")

	// Postrouting chain for SNAT/Masquerade (policy accept)
	sb.AddChain("postrouting", "nat", "postrouting", 100, "accept")

	// Pre-calculate zone map for correct interface resolution (supports match blocks)
	zoneMap := buildZoneMapForScript(cfg)

	// Track interfaces with masquerade enabled to prevent duplicates
	seenMasq := make(map[string]bool)

	// Add NAT rules
	for _, r := range cfg.NAT {
		// Common matchers
		match := ""
		
		// Logic to avoid redundant protocol check:
		// If DNAT with DestPort, we (later) append "tcp dport X", which implies tcp.
		// Adding "meta l4proto tcp" + "tcp dport X" causes syntax error.
		// So we suppress meta l4proto here if we know we'll add a port match.
		suppressProto := r.Type == "dnat" && r.DestPort != ""

		if r.Protocol != "" && r.Protocol != "any" && !suppressProto {
			match += fmt.Sprintf("meta l4proto %s ", r.Protocol)
		}
		if r.SrcIP != "" {
			match += fmt.Sprintf("ip saddr %s ", r.SrcIP)
		}
		if r.DestIP != "" {
			match += fmt.Sprintf("ip daddr %s ", r.DestIP)
		}
		if r.Mark != 0 {
			match += fmt.Sprintf("mark 0x%x ", r.Mark)
		}
		// Comment handling:
		// We process comment separately to append it at the end of the rule (after matches/statements)
		commentSuffix := ""
		if r.Description != "" {
			commentSuffix = fmt.Sprintf(" comment %q", r.Description)
		}

		if r.Type == "masquerade" && r.OutInterface != "" {
			if !seenMasq[r.OutInterface] {
				// We don't use 'match' here for standard interface masquerade as it's just oifname
				// But advanced masquerade (src ip restricted) should use it.
				// However, legacy config just uses OutInterface.
				// Let's support both.
				combinedMatch := fmt.Sprintf("oifname \"%s\" %s", r.OutInterface, match)
				sb.AddRule("postrouting", fmt.Sprintf("%smasquerade%s", combinedMatch, commentSuffix))
				seenMasq[r.OutInterface] = true
			}

		} else if r.Type == "dnat" && r.InInterface != "" && (r.ToIP != "" || r.ToPort != "") {
			// DNAT
			matchExpr := fmt.Sprintf("iifname \"%s\" %s", r.InInterface, match)

			// Append dport match if present
			if r.DestPort != "" {
				// We need to know protocol for dport match
				proto := r.Protocol
				if proto == "" || proto == "any" {
					proto = "tcp" // Default to TCP if logic requires port match? Or error?
					// Nftables requires protocol context for dport.
					// If user didn't specify protocol but specified port, we assume TCP or UDP?
					// Let's default to tcp if unspecified, but ideally user specifies it.
					// If they specified "udp", we use "udp".
				}
				// Note: match string already includes "meta l4proto ...".
				// Nftables allows "meta l4proto tcp tcp dport 80".
				matchExpr += fmt.Sprintf(" %s dport %s", proto, r.DestPort)
			}

			target := ""
			if r.ToIP != "" && r.ToPort != "" {
				target = fmt.Sprintf("%s:%s", r.ToIP, r.ToPort)
			} else if r.ToIP != "" {
				target = r.ToIP
			} else if r.ToPort != "" {
				target = fmt.Sprintf(":%s", r.ToPort)
			}

			sb.AddRule("prerouting", fmt.Sprintf("%s dnat to %s%s", matchExpr, target, commentSuffix))
		} else if r.Type == "snat" && r.OutInterface != "" && r.SNATIP != "" {
			// SNAT requires postrouting
			combinedMatch := fmt.Sprintf("oifname \"%s\" %s", r.OutInterface, match)
			sb.AddRule("postrouting", fmt.Sprintf("%ssnat to %s%s", combinedMatch, r.SNATIP, commentSuffix))
		}

		// Hairpin NAT (NAT Reflection)
		if r.Type == "dnat" && r.Hairpin && r.ToIP != "" {
			// Resolve WAN IPs to match against
			var hairpinIPs []string
			if r.DestIP != "" {
				hairpinIPs = append(hairpinIPs, r.DestIP)
			} else if r.InInterface != "" {
				// Try to resolve interface IP
				for _, iface := range cfg.Interfaces {
					if iface.Name == r.InInterface {
						for _, ipCIDR := range iface.IPv4 {
							ip, _, err := net.ParseCIDR(ipCIDR)
							if err == nil && ip != nil {
								hairpinIPs = append(hairpinIPs, ip.String())
							}
						}
						break
					}
				}
			}

			if len(hairpinIPs) > 0 {
				for _, ipAddr := range hairpinIPs {
					// 1. Reflected DNAT
					hairpinMatch := ""
					if r.Protocol != "" && r.Protocol != "any" && !suppressProto {
						hairpinMatch += fmt.Sprintf("meta l4proto %s ", r.Protocol)
					}
					hairpinMatch += fmt.Sprintf("ip daddr %s ", ipAddr)
					
					if r.DestPort != "" {
						proto := r.Protocol
						if proto == "" || proto == "any" {
							proto = "tcp"
						}
						hairpinMatch += fmt.Sprintf("%s dport %s ", proto, r.DestPort)
					}

					if r.InInterface != "" {
						hairpinMatch = fmt.Sprintf("iifname != \"%s\" %s", r.InInterface, hairpinMatch)
					}

					target := fmt.Sprintf("%s:%s", r.ToIP, r.ToPort)
					if r.ToPort == "" {
						target = r.ToIP
					}
					
					sb.AddRule("prerouting", fmt.Sprintf("%sdnat to %s comment \"hairpin: %s\"", hairpinMatch, target, r.Name))

					// 2. Hairpin SNAT (Masquerade)
					masqMatch := ""
					if r.InInterface != "" {
						masqMatch += fmt.Sprintf("iifname != \"%s\" ", r.InInterface)
					}
					
					masqMatch += fmt.Sprintf("ip daddr %s ", r.ToIP)
					if r.ToPort != "" {
						proto := r.Protocol
						if proto == "" || proto == "any" {
							blockProto := "tcp"
							masqMatch += fmt.Sprintf("%s dport %s ", blockProto, r.ToPort)
						} else {
							masqMatch += fmt.Sprintf("%s dport %s ", proto, r.ToPort)
						}
					}
					
					sb.AddRule("postrouting", fmt.Sprintf("%smasquerade comment \"hairpin-masq: %s\"", masqMatch, r.Name))
				}
			}
		}
	}

	// 1b. Policy-based auto-masquerade
	// Generate masquerade rules from policies with Masquerade=true or auto-detect
	for _, pol := range cfg.Policies {
		if pol.Disabled {
			continue
		}

		// Determine if masquerade should be enabled for this policy
		shouldMasquerade := false
		if pol.Masquerade != nil {
			// Explicit setting
			shouldMasquerade = *pol.Masquerade
		} else {
			// Auto-detect: masquerade when source is internal (RFC1918) and dest is external
			srcZone := findZone(cfg.Zones, pol.From)
			dstZone := findZone(cfg.Zones, pol.To)
			if srcZone != nil && dstZone != nil {
				// Use resolved interfaces for zone checks
				srcIfaces := zoneMap[srcZone.Name]
				dstIfaces := zoneMap[dstZone.Name]
				srcIsInternal := isZoneInternal(srcZone, srcIfaces)
				dstIsExternal := isZoneExternal(dstZone, dstIfaces, cfg.Interfaces)
				shouldMasquerade = srcIsInternal && dstIsExternal
			}
		}

		if shouldMasquerade {
			// Get outbound interfaces for the destination zone
			// Use resolved interfaces from zoneMap
			dstIfaces, ok := zoneMap[pol.To]
			if ok {
				for _, ifName := range dstIfaces {
					if !seenMasq[ifName] {
						comment := fmt.Sprintf("auto-masq %s->%s", pol.From, pol.To)
						sb.AddRule("postrouting", fmt.Sprintf("oifname \"%s\" masquerade comment %q", ifName, comment))
						seenMasq[ifName] = true
					}
				}
			}
		}
	}

	// 2. Auto-generated Web UI Access rules
	// Sandbox mode (privilege separation via network namespace) is enabled by default.
	sandboxEnabled := true
	if cfg.API != nil && cfg.API.DisableSandbox {
		sandboxEnabled = false
	}

	for _, iface := range cfg.Interfaces {
		if iface.AccessWebUI {
			// Determine external port
			extPort := iface.WebUIPort
			if extPort == 0 {
				extPort = 443 // Default to HTTPS
			}

			// If sandbox is enabled, DNAT to namespace API.
			// If disabled, REDIRECT to local port (API listening on host)
			if sandboxEnabled {
				target := "169.254.255.2:8443"
				sb.AddRule("prerouting", fmt.Sprintf(
					"iifname \"%s\" tcp dport %d dnat to %s",
					iface.Name, extPort, target))
			} else {
				// Redirect to 8080 (assumed host API port)
				// dnat to 127.0.0.1 not supported for output? No, this is PREROUTING.
				// redirect to :port
				sb.AddRule("prerouting", fmt.Sprintf(
					"iifname \"%s\" tcp dport %d redirect to :8080",
					iface.Name, extPort))
			}
		}
	}

	// Zone-based Web UI DNAT (for zones using management { web = true })
	// This redirects standard ports 80/443 to the sandbox high ports
	for _, zone := range cfg.Zones {
		if zone.Management == nil || (!zone.Management.Web && !zone.Management.WebUI && !zone.Management.API) {
			continue
		}

		// Get interfaces for this zone
		zoneIfaces, ok := zoneMap[zone.Name]
		if !ok || len(zoneIfaces) == 0 {
			continue
		}

		for _, ifaceName := range zoneIfaces {
			if sandboxEnabled {
				// HTTPS: 443 -> sandbox:8443
				sb.AddRule("prerouting", fmt.Sprintf(
					"iifname \"%s\" tcp dport 443 dnat to 169.254.255.2:8443",
					ifaceName))
				// HTTP: 80 -> sandbox:8080 (for redirect to HTTPS)
				sb.AddRule("prerouting", fmt.Sprintf(
					"iifname \"%s\" tcp dport 80 dnat to 169.254.255.2:8080",
					ifaceName))

				// Direct Access: 8443 -> sandbox:8443
				// Required because HTTP redirect sends clients to :8443
				sb.AddRule("prerouting", fmt.Sprintf(
					"iifname \"%s\" tcp dport 8443 dnat to 169.254.255.2:8443",
					ifaceName))
				// Direct Access: 8080 -> sandbox:8080
				sb.AddRule("prerouting", fmt.Sprintf(
					"iifname \"%s\" tcp dport 8080 dnat to 169.254.255.2:8080",
					ifaceName))
			} else {
				// Sandbox disabled - redirect to host ports
				sb.AddRule("prerouting", fmt.Sprintf(
					"iifname \"%s\" tcp dport 443 redirect to :8443",
					ifaceName))
				sb.AddRule("prerouting", fmt.Sprintf(
					"iifname \"%s\" tcp dport 80 redirect to :8080",
					ifaceName))
				// Direct 8443/8080 (no redirect needed if already listening there)
			}
		}
	}

	// 3. DNS Inspect Rules - Transparent DNS redirection
	// Add DNAT rules for zones configured with dns.inspect mode="redirect"
	if cfg.DNS != nil && len(cfg.DNS.Inspect) > 0 {
		// Determine DNS server listen address
		listenAddr := "127.0.0.1"
		listenPort := 53

		// Check if we have a serve block to get the listen port
		for _, serve := range cfg.DNS.Serve {
			if serve.ListenPort > 0 {
				listenPort = serve.ListenPort
				break
			}
		}

		// Get zone to interface mapping
		zoneIfaces := buildZoneMapForScript(cfg)

		// Collect router IPs to potentially exclude
		var routerIPs []string
		for _, iface := range cfg.Interfaces {
			// Add IPv4 addresses
			for _, ip := range iface.IPv4 {
				// Parse CIDR to get just the IP
				if idx := strings.Index(ip, "/"); idx > 0 {
					routerIPs = append(routerIPs, ip[:idx])
				} else {
					routerIPs = append(routerIPs, ip)
				}
			}
			// Add IPv6 addresses
			for _, ip := range iface.IPv6 {
				if idx := strings.Index(ip, "/"); idx > 0 {
					routerIPs = append(routerIPs, ip[:idx])
				} else {
					routerIPs = append(routerIPs, ip)
				}
			}
		}

		for _, inspect := range cfg.DNS.Inspect {
			// Skip passive mode - that's just for visibility/logging, not DNAT
			if inspect.Mode == "passive" {
				continue
			}

			// Resolve zone to interfaces (supports wildcards)
			ifaces := resolveZoneInterfaces(inspect.Zone, zoneIfaces)

			for _, iface := range ifaces {
				// UDP DNS DNAT
				udpRule := fmt.Sprintf("iifname \"%s\" udp dport 53", iface)
				if inspect.ExcludeRouter && len(routerIPs) > 0 {
					for _, ip := range routerIPs {
						udpRule += fmt.Sprintf(" ip saddr != %s", ip)
					}
				}
				udpRule += fmt.Sprintf(" dnat to %s:%d comment \"dns-inspect-%s\"", listenAddr, listenPort, inspect.Zone)
				sb.AddRule("prerouting", udpRule)

				// TCP DNS DNAT
				tcpRule := fmt.Sprintf("iifname \"%s\" tcp dport 53", iface)
				if inspect.ExcludeRouter && len(routerIPs) > 0 {
					for _, ip := range routerIPs {
						tcpRule += fmt.Sprintf(" ip saddr != %s", ip)
					}
				}
				tcpRule += fmt.Sprintf(" dnat to %s:%d comment \"dns-inspect-%s\"", listenAddr, listenPort, inspect.Zone)
				sb.AddRule("prerouting", tcpRule)
			}
		}

		// Add masquerade for redirected DNS traffic
		sb.AddRule("postrouting", fmt.Sprintf("ip daddr %s udp dport %d masquerade comment \"dns-inspect-masq\"", listenAddr, listenPort))
		sb.AddRule("postrouting", fmt.Sprintf("ip daddr %s tcp dport %d masquerade comment \"dns-inspect-masq\"", listenAddr, listenPort))
	}

	// Always masquerade traffic to the sandbox to ensure return traffic works
	// and to solve routing issues with Link-Local addresses from external interfaces.
	if sandboxEnabled {
		sb.AddRule("postrouting", "ip daddr 169.254.255.2 masquerade comment \"sandbox-masq\"")
	}

	return sb, nil
}

// BuildNAT6TableScript builds the IPv6 NAT table (NAT66) for masquerade support.
// This enables private IPv6 networks (e.g., ULA, fd00::/8) to route to the public internet.
func BuildNAT6TableScript(cfg *Config) (*ScriptBuilder, error) {
	if cfg.Interfaces == nil {
		return nil, nil
	}

	// Only build NAT6 if there's at least one policy with masquerade potential
	// or an explicit IPv6 NAT rule
	hasIPv6Masq := false
	for _, pol := range cfg.Policies {
		if pol.Disabled {
			continue
		}
		// Check if policy might need masquerade (from internal to external)
		if pol.Masquerade != nil && *pol.Masquerade {
			hasIPv6Masq = true
			break
		}
	}
	// Also check for auto-masquerade (internal->external zones)
	if !hasIPv6Masq {
		for _, pol := range cfg.Policies {
			if pol.Disabled {
				continue
			}
			srcZone := findZone(cfg.Zones, pol.From)
			dstZone := findZone(cfg.Zones, pol.To)
			if srcZone != nil && dstZone != nil {
				zoneMap := buildZoneMapForScript(cfg)
				srcIfaces := zoneMap[srcZone.Name]
				dstIfaces := zoneMap[dstZone.Name]
				srcIsInternal := isZoneInternal(srcZone, srcIfaces)
				dstIsExternal := isZoneExternal(dstZone, dstIfaces, cfg.Interfaces)
				if srcIsInternal && dstIsExternal {
					hasIPv6Masq = true
					break
				}
			}
		}
	}

	if !hasIPv6Masq {
		return nil, nil
	}

	sb := NewScriptBuilder("nat6", "ip6")
	sb.AddTable()

	// Postrouting chain for IPv6 masquerade (policy accept)
	sb.AddChain("postrouting", "nat", "postrouting", 100, "accept")

	// Build zone map for interface resolution
	zoneMap := buildZoneMapForScript(cfg)
	seenMasq := make(map[string]bool)

	// Add masquerade rules for internal->external policies
	for _, pol := range cfg.Policies {
		if pol.Disabled {
			continue
		}

		shouldMasquerade := pol.Masquerade != nil && *pol.Masquerade
		if !shouldMasquerade {
			// Auto-detect: internal zone -> external zone
			srcZone := findZone(cfg.Zones, pol.From)
			dstZone := findZone(cfg.Zones, pol.To)
			if srcZone != nil && dstZone != nil {
				srcIfaces := zoneMap[srcZone.Name]
				dstIfaces := zoneMap[dstZone.Name]
				srcIsInternal := isZoneInternal(srcZone, srcIfaces)
				dstIsExternal := isZoneExternal(dstZone, dstIfaces, cfg.Interfaces)
				shouldMasquerade = srcIsInternal && dstIsExternal
			}
		}

		if shouldMasquerade {
			dstIfaces, ok := zoneMap[pol.To]
			if ok {
				for _, ifName := range dstIfaces {
					if !seenMasq[ifName] {
						comment := fmt.Sprintf("ipv6-masq %s->%s", pol.From, pol.To)
						sb.AddRule("postrouting", fmt.Sprintf("oifname \"%s\" masquerade comment %q", ifName, comment))
						seenMasq[ifName] = true
					}
				}
			}
		}
	}

	return sb, nil
}

// resolveZoneInterfaces resolves a zone name or wildcard to a list of interface names.
func resolveZoneInterfaces(pattern string, zoneMap map[string][]string) []string {
	if interfaces, ok := zoneMap[pattern]; ok {
		return interfaces
	}

	// Handle wildcard matching
	var results []string
	for zoneName, interfaces := range zoneMap {
		if matchZoneWildcard(pattern, zoneName) {
			results = append(results, interfaces...)
		}
	}
	return results
}

// matchZoneWildcard checks if a zone pattern matches a zone name.
func matchZoneWildcard(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(name) >= len(prefix) && name[:len(prefix)] == prefix
	}
	return pattern == name
}

// BuildMangleTableScript builds the mangle table script configuration.
// It implements policy routing marking for split-routing interfaces (CONNMARK for return traffic).
func BuildMangleTableScript(cfg *Config) (*ScriptBuilder, error) {
	// Check if any interface needs split routing
	hasSplit := false
	for _, iface := range cfg.Interfaces {
		if iface.Table > 0 && iface.Table != 254 {
			hasSplit = true
			break
		}
	}

	hasMarkRules := len(cfg.MarkRules) > 0
	hasUIDRouting := len(cfg.UIDRouting) > 0

	if !hasSplit && !hasMarkRules && !hasUIDRouting {
		return nil, nil // No table needed
	}

	sb := NewScriptBuilder("mangle", "ip")
	sb.AddTable()

	// Prerouting chain (Ingress marking)
	// Use type 'filter' with priority -150 (mangle) because 'type mangle' is deprecated/unsupported
	// in some environments for the inet/ip families in modern nftables.
	sb.AddChain("prerouting", "filter", "prerouting", -150, "accept")

	// Output chain (Egress restoration)
	sb.AddChain("output", "filter", "output", -150, "accept")

	// 1. Ingress: Mark connections coming from split-routing interfaces
	// Collect all marks used to restore them in Output
	seenMarks := make(map[int]bool)

	for _, iface := range cfg.Interfaces {
		if iface.Table > 0 && iface.Table != 254 {
			mark := iface.Table // Use Table ID as mark
			seenMarks[mark] = true

			// Mark connection as Table ID
			// ct mark set <ID>
			sb.AddRule("prerouting", fmt.Sprintf("iifname %q ct mark set 0x%x", iface.Name, mark))
		}
	}

	// 2. Custom Mark Rules
	for _, mr := range cfg.MarkRules {
		if !mr.Enabled {
			continue
		}
		// Build match expression
		match := ""
		if mr.InInterface != "" {
			match += fmt.Sprintf("iifname %q ", mr.InInterface)
		}
		if mr.OutInterface != "" {
			// OutInterface only valid in Forward/Output/Postrouting
			// For general marking, we usually use Prerouting (no output iface known yet) or Output (known)
			match += fmt.Sprintf("oifname %q ", mr.OutInterface)
		}
		if mr.Protocol != "" && mr.Protocol != "all" {
			match += fmt.Sprintf("meta l4proto %s ", mr.Protocol)
		}
		if mr.SrcIP != "" {
			match += fmt.Sprintf("ip saddr %s ", mr.SrcIP)
		}
		if mr.DstIP != "" {
			match += fmt.Sprintf("ip daddr %s ", mr.DstIP)
		}
		if mr.DstPort > 0 {
			// Assume TCP/UDP if not specified, or just add dport
			match += fmt.Sprintf("dport %d ", mr.DstPort)
		}

		chain := "prerouting"
		// If rule matches UID or Output Interface, must be in Output chain?
		// But MarkRules struct doesn't have UID, UIDRouting does.
		// If OutInterface is set, use Output chain?
		if mr.OutInterface != "" {
			chain = "output"
		}

		// Apply Mark
		// meta mark set <value>
		// ct mark set <value> (if SaveMark)
		markAction := fmt.Sprintf("meta mark set %s", mr.Mark)
		if mr.Mask != "" {
			// Masked mark not easily supported with simple set, ignoring mask for now or assume simple mark
			// nft supports: meta mark set meta mark and 0xffff0000 xor 0x10
			// Keep it simple: overwrite
		}

		sb.AddRule(chain, fmt.Sprintf("%s%s", match, markAction))
		if mr.SaveMark {
			sb.AddRule(chain, fmt.Sprintf("%sct mark set %s", match, mr.Mark))
		}
	}

	// 3. User ID Routing
	for _, ur := range cfg.UIDRouting {
		if !ur.Enabled {
			continue
		}
		// meta skuid <uid> mark set <mark>
		// We need to know the mark for the uplink.
		// Uplink name -> Table ID mapping?
		// For now, assume user knows what they are doing or we need a lookup.
		// The FireHOL script marks based on UID then routes.
		// in config: Uplink string (name).
		// We'll need to resolve Uplink name to a Mark/Table ID.
		// This requires more context than just the struct.
		// For now, let's implement the generic mechanism if Mark is provided directly or standard lookup.
		// If we can't resolve, skip?
		// Actually, FireHOL example: iptables ... -m owner --uid-owner wg10 -j MARK --set-mark 210
		// We need to implement this.
		if ur.UID > 0 {
			// Find mark from Uplink?
			// Simplification: We need 'Mark' in UIDRouting or lookup logic.
			// Config struct for UIDRouting has Uplink field.
			// Let's assume Uplink corresponds to a Table ID or Mark.
			// TODO: Resolve uplink to mark.
		}
	}

	// 2. Output: Restore mark from connection to packet
	// This ensures reply packets (which belong to the marked connection) get the fwmark,
	// allowing `ip rule fwmark <ID>` to route them via Table <ID>.
	for mark := range seenMarks {
		sb.AddRule("output", fmt.Sprintf("ct mark 0x%x meta mark set 0x%x", mark, mark))
	}

	return sb, nil
}

// findZone finds a zone by name in the zones list
func findZone(zones []config.Zone, name string) *config.Zone {
	for i := range zones {
		if zones[i].Name == name {
			return &zones[i]
		}
	}
	return nil
}

// isZoneInternal returns true if the zone contains RFC1918 (internal) networks
func isZoneInternal(zone *config.Zone, zoneIfaces []string) bool {
	// Check zone's explicitly defined networks
	for _, network := range zone.Networks {
		if isRFC1918Network(network) {
			return true
		}
	}
	// Zones with only interfaces are typically internal (LAN)
	// But we can't determine this without checking interface IPs
	// Default: if zone has interfaces but no networks, assume internal
	if len(zoneIfaces) > 0 && len(zone.Networks) == 0 {
		return true
	}
	return false
}

// isZoneExternal returns true if the zone is external (WAN) based on heuristics
func isZoneExternal(zone *config.Zone, zoneIfaces []string, interfaces []config.Interface) bool {
	// 1. Explicit setting
	if zone.External != nil {
		return *zone.External
	}

	// 2. Zone name contains wan/external
	lowerName := strings.ToLower(zone.Name)
	if strings.Contains(lowerName, "wan") || strings.Contains(lowerName, "external") {
		return true
	}

	// 3. Interface has DHCP client enabled (getting address from upstream)
	for _, ifName := range zoneIfaces {
		for _, iface := range interfaces {
			if iface.Name == ifName && iface.DHCP {
				return true
			}
		}
	}

	return false
}

func mustParseCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// isRFC1918Network checks if a CIDR is in RFC1918 private ranges
func isRFC1918Network(cidr string) bool {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		// Try parsing as plain IP
		ip := net.ParseIP(cidr)
		if ip == nil {
			return false
		}
		for _, rfc1918 := range protectionPrivateNetworks {
			if rfc1918.Contains(ip) {
				return true
			}
		}
		return false
	}

	// Check if the network's first IP falls within RFC1918 ranges
	for _, rfc1918 := range protectionPrivateNetworks {
		if rfc1918.Contains(network.IP) {
			return true
		}
	}
	return false
}
