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

// generateWebAccessRules generates firewall rules based on cfg.Web.Allow/Deny.
// Returns true if new rules were generated (indicating legacy logic should be skipped).
func generateWebAccessRules(cfg *Config, sb *ScriptBuilder) bool {
	if cfg.Web == nil {
		return false
	}

	// Determine ports
	var ports []string
	parsePort := func(addr string, defaultPort int) {
		if addr == "" {
			ports = append(ports, fmt.Sprintf("%d", defaultPort))
			return
		}
		parts := strings.Split(addr, ":")
		if len(parts) == 2 && parts[1] != "" {
			ports = append(ports, parts[1])
		} else {
			ports = append(ports, fmt.Sprintf("%d", defaultPort))
		}
	}

	parsePort(cfg.Web.Listen, 80)
	parsePort(cfg.Web.TLSListen, 443)

	// Removing duplicates
	uniquePorts := make(map[string]bool)
	var finalPorts []string
	for _, p := range ports {
		if !uniquePorts[p] {
			uniquePorts[p] = true
			finalPorts = append(finalPorts, p)
		}
	}
	portSet := strings.Join(finalPorts, ", ")
	
	hasRules := len(cfg.Web.Deny) > 0 || len(cfg.Web.Allow) > 0
	if !hasRules {
		return false
	}
	
	// Helper to build rule constraints
	buildConstraints := func(rule config.AccessRule) string {
		var parts []string
		
		// Interfaces
		var ifaces []string
		if rule.Interface != "" {
			ifaces = append(ifaces, forceQuote(rule.Interface))
		}
		for _, i := range rule.Interfaces {
			ifaces = append(ifaces, forceQuote(i))
		}
		if len(ifaces) > 0 {
			parts = append(parts, fmt.Sprintf("iifname { %s }", strings.Join(ifaces, ", ")))
		}
		
		// Sources
		var sources []string
		if rule.Source != "" {
			sources = append(sources, rule.Source)
		}
		sources = append(sources, rule.Sources...)
		
		if len(sources) > 0 {
			// Check if sources contain IPv6 or hostnames?
			// For simplicity assume IP/CIDR or resolveable?
			// nftables handles CIDR/IP directly. Hostnames are resolved by nft at load time.
			// Construct a set?
			// If mixed v4/v6, nftables "inet" set works if items are typed.
			// But inline rules with "ip saddr" vs "ip6 saddr" need separation usually?
			// "inet" tables support "ip saddr" and "ip6 saddr".
			// But a single list { 1.2.3.4, 2001::1 } passed to "ip saddr" will fail for the v6 address.
			// However, if we use "saddr { ... }" in inet table, it might work if typed correctly?
			// Actually, "inet" table still distinguishes L3 protocol in hooks, but rules are mixed.
			// Safe approach: separate v4 and v6? Or rely on nftables smarts?
			// For now, let's just join them in a set matching unnamed set syntax.
			// BUT: "ip saddr" matches IPv4. "ip6 saddr" IPv6.
			// If we put v6 addr in "ip saddr", it fails.
			
			// We'll rely on a heuristic: if any contain ":", assume v6 present?
			// Or better: Use "saddr" (which implies address family agostic? No, saddr isn't generic for inet without context).
			// Actually, we must use "ip saddr" or "ip6 saddr".
			// Complex.
			// For this iteration, let's just output `ip saddr { ... }` and `ip6 saddr { ... }` splitting the list.
			
			var v4 []string
			var v6 []string
			for _, s := range sources {
				if strings.Contains(s, ":") && !strings.Contains(s, ".") { // simple v6 check (mostly valid)
					v6 = append(v6, s)
				} else {
					v4 = append(v4, s) // Assume v4 or hostname (resolved to v4 typically?)
				}
			}
			
			if len(v4) > 0 {
				parts = append(parts, fmt.Sprintf("ip saddr { %s }", strings.Join(v4, ", ")))
			}
			if len(v6) > 0 {
				parts = append(parts, fmt.Sprintf("ip6 saddr { %s }", strings.Join(v6, ", ")))
			}
		}
		
		return strings.Join(parts, " ")
	}

	// Generate Deny Rules
	for _, rule := range cfg.Web.Deny {
		match := buildConstraints(rule)
		sb.AddRule("input", fmt.Sprintf("%s tcp dport { %s } drop", match, portSet), "[web] Explicit Deny")
	}

	// Generate Allow Rules
	for _, rule := range cfg.Web.Allow {
		match := buildConstraints(rule)
		sb.AddRule("input", fmt.Sprintf("%s tcp dport { %s } accept", match, portSet), "[web] Explicit Allow")
	}

	return true
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

// AddTableWithComment adds a table creation command with a metadata comment.
// The comment embeds version, apply count, and config hash for traceability.
func (b *ScriptBuilder) AddTableWithComment(comment string) {
	if comment == "" {
		b.AddTable()
		return
	}
	b.AddLine(fmt.Sprintf("add table %s %s { comment %q; }", b.family, b.tableName, comment))
}

// AddChain adds a chain creation command.
// comment is optional - pass empty string for no comment.
func (b *ScriptBuilder) AddChain(name, chainType string, hook string, priority int, policy string, comment ...string) {
	if !isValidIdentifier(name) {
		// Quoting is safer.
	}
	qName := quote(name)

	// Build comment clause if provided
	commentClause := ""
	if len(comment) > 0 && comment[0] != "" {
		commentClause = fmt.Sprintf(" comment %q;", comment[0])
	}

	if chainType != "" && hook != "" {
		policyStr := ""
		if policy != "" {
			policyStr = fmt.Sprintf("policy %s; ", policy)
		}
		b.AddLine(fmt.Sprintf("add chain %s %s %s { type %s hook %s priority %d; %s%s}",
			b.family, b.tableName, qName, chainType, hook, priority, policyStr, commentClause))
	} else {
		// Regular chain (no hook)
		if commentClause != "" {
			b.AddLine(fmt.Sprintf("add chain %s %s %s {%s }", b.family, b.tableName, qName, commentClause))
		} else {
			b.AddLine(fmt.Sprintf("add chain %s %s %s", b.family, b.tableName, qName))
		}
	}
	
	// Smart Flush: Ensure chain is empty before adding rules, but preserve table/sets.
	// We use "flush chain family table chain"
	b.AddLine(fmt.Sprintf("flush chain %s %s %s", b.family, b.tableName, qName))
}

// AddRule adds a rule to a chain.
// comment is optional - pass empty string for no comment.
// If the rule expression already contains a comment, the new comment is skipped.
func (b *ScriptBuilder) AddRule(chainName, ruleExpr string, comment ...string) {
	commentClause := ""
	// Only add comment if provided AND ruleExpr doesn't already have one
	if len(comment) > 0 && comment[0] != "" && !strings.Contains(ruleExpr, "comment ") {
		commentClause = fmt.Sprintf(" comment %q", comment[0])
	}
	b.AddLine(fmt.Sprintf("add rule %s %s %s %s%s", b.family, b.tableName, quote(chainName), ruleExpr, commentClause))
}

// AddSet adds a set creation command.
// comment is optional - pass empty string for no comment.
func (b *ScriptBuilder) AddSet(name, setType string, comment string, size int, flags ...string) {
	flagStr := ""
	if len(flags) > 0 {
		flagStr = " flags " + strings.Join(flags, ",") + ";"
	}
	sizeStr := ""
	if size > 0 {
		sizeStr = fmt.Sprintf(" size %d;", size)
	}
	commentClause := ""
	if comment != "" {
		commentClause = fmt.Sprintf(" comment %q;", comment)
	}
	b.AddLine(fmt.Sprintf("add set %s %s %s { type %s;%s%s%s }", b.family, b.tableName, quote(name), setType, flagStr, sizeStr, commentClause))
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
// comment is optional.
func (b *ScriptBuilder) AddFlowtable(name string, devices []string, comment ...string) {
	if len(devices) == 0 {
		return
	}
	devs := make([]string, len(devices))
	for i, d := range devices {
		devs[i] = quote(d)
	}
	commentClause := ""
	if len(comment) > 0 && comment[0] != "" {
		commentClause = fmt.Sprintf(" comment %q;", comment[0])
	}
	b.AddLine(fmt.Sprintf("add flowtable %s %s %s { hook ingress priority 0; devices = { %s };%s }",
		b.family, b.tableName, quote(name), strings.Join(devs, ", "), commentClause))
}

// AddMap adds a map definition.
// comment is optional.
func (b *ScriptBuilder) AddMap(name, typeKey, typeValue string, comment string, flags, elements []string) {
	flagStr := ""
	if len(flags) > 0 {
		flagStr = fmt.Sprintf("flags %s; ", strings.Join(flags, ", "))
	}
	commentClause := ""
	if comment != "" {
		commentClause = fmt.Sprintf(" comment %q;", comment)
	}
	b.AddLine(fmt.Sprintf("add map %s %s %s { type %s : %s; %s%s}",
		b.family, b.tableName, quote(name), typeKey, typeValue, flagStr, commentClause))

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
// configHash should be the first 8 hex chars of SHA256 of the config file.
func BuildFilterTableScript(cfg *Config, vpn *config.VPNConfig, tableName string, configHash string) (*ScriptBuilder, error) {
	sb := NewScriptBuilder(tableName, "inet")

	// Create table with metadata comment
	applyCount := GetNextApplyCount(tableName, "inet")
	comment := BuildMetadataComment(applyCount, configHash)
	sb.AddTableWithComment(comment)

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

	// Define GeoIP sets if referenced by any policy rule
	// We scan all policies to find used identifiers like "US", "CN", etc.
	usedCountries := make(map[string]bool)
	for _, pol := range cfg.Policies {
		if pol.Disabled {
			continue
		}
		for _, rule := range pol.Rules {
			if rule.Disabled {
				continue
			}
			if rule.SourceCountry != "" {
				usedCountries[strings.ToUpper(rule.SourceCountry)] = true
			}
			if rule.DestCountry != "" {
				usedCountries[strings.ToUpper(rule.DestCountry)] = true
			}
		}
	}

	for code := range usedCountries {
		if len(code) != 2 {
			return nil, fmt.Errorf("invalid country code: %s", code)
		}
		setName := fmt.Sprintf("geoip_country_%s", code)
		// Add set definition: type ipv4_addr; flags interval;
		// note: Currently only supporting IPv4 for GeoIP as per BuildRuleExpression
		sb.AddSet(setName, "ipv4_addr", fmt.Sprintf("[geoip] %s", code), 0, "interval")
	}

	// CRITICAL: Define IPSets BEFORE rules that reference them
	for _, ipset := range cfg.IPSets {
		setType := ipset.Type
		if setType == "" {
			setType = "ipv4_addr"
		}
		
		isDynamic := setType == "dns" || setType == "dynamic"

		// Infer interval flag: needed if entries contain CIDR notation
		var flags []string
		for _, entry := range ipset.Entries {
			if strings.Contains(entry, "/") {
				flags = append(flags, "interval")
				break
			}
		}

		// Handle DNS type sets
		size := ipset.Size
		if setType == "dns" {
			setType = "ipv4_addr"
			// Default size for Dynamic sets if not specified (optimization)
			if size == 0 {
				size = 65535
			}
		}

		if !isValidIdentifier(ipset.Name) {
			return nil, fmt.Errorf("invalid ipset name: %s", ipset.Name)
		}
		sb.AddSet(ipset.Name, setType, fmt.Sprintf("[ipset:%s]", ipset.Name), size, flags...)

		// Smart Flush: Flush static sets to match config, but preserve dynamic sets (persistence).
		if !isDynamic {
			sb.AddLine(fmt.Sprintf("flush set %s %s %s", sb.family, sb.tableName, quote(ipset.Name)))
		}

		// Add elements if specified inline
		if len(ipset.Entries) > 0 {
			sb.AddSetElements(ipset.Name, ipset.Entries)
		}
		if len(ipset.Entries) > 0 {
			sb.AddSetElements(ipset.Name, ipset.Entries)
		}
	}

	// Define DNS Egress Control Sets (Dynamic, Persistent)
	// We handle v4 and v6 separately because nftables sets are typed.
	if cfg.DNS != nil && cfg.DNS.EgressFilter {
		// size 65535, timeout 3600 (default fallback, individual elements override)
		// We add "timeout" flag to support element timeouts.
		sb.AddSet("dns_allowed_v4", "ipv4_addr", "[dns-wall] Allowed IPv4", 65535, "timeout")
		sb.AddSet("dns_allowed_v6", "ipv6_addr", "[dns-wall] Allowed IPv6", 65535, "timeout")
		// Do NOT flush these sets. They are persistent.
	}

	// Create base chains with default drop policy
	sb.AddChain("input", "filter", "input", 0, "drop", "[base] Incoming traffic")
	sb.AddChain("forward", "filter", "forward", 0, "drop", "[base] Routed traffic")
	sb.AddChain("output", "filter", "output", 0, "drop", "[base] Outgoing traffic")

	// Create mangle chain for UplinkManager policy routing marks
	// Priority -150 (mangle) ensures marks are set before routing decision
	sb.AddChain("mark_prerouting", "filter", "prerouting", -150, "accept", "[base] Policy routing marks")

	// Add base rules (loopback, established/related)
	sb.AddRule("input", "iifname \"lo\" accept", "[base] Loopback")
	sb.AddRule("output", "oifname \"lo\" accept", "[base] Loopback")
	sb.AddRule("input", "ct state established,related accept", "[base] Stateful")
	sb.AddRule("forward", "ct state established,related accept", "[base] Stateful")
	sb.AddRule("output", "ct state established,related accept", "[base] Stateful")

	// Drop invalid packets (malformed, out-of-window, or spoofed)
	// Rate-limit logging to prevent log spam
	sb.AddRule("input", "ct state invalid limit rate 10/minute log group 0 prefix \"DROP_INVALID: \" counter drop", "[base] Invalid drop")
	sb.AddRule("forward", "ct state invalid limit rate 10/minute log group 0 prefix \"DROP_INVALID: \" counter drop", "[base] Invalid drop")
	sb.AddRule("output", "ct state invalid drop", "[base] Invalid drop")

	// Device Discovery Logging (nflog group 100)
	// Log NEW connections early in the chain for device tracking.
	// Rate limited to prevent flooding the collector on busy networks.
	// This happens BEFORE accept/drop decisions so we see all devices.
	sb.AddRule("input", "ct state new limit rate 100/second burst 50 packets log group 100 prefix \"DISCOVER: \"", "[feature] Device discovery")
	sb.AddRule("forward", "ct state new limit rate 100/second burst 50 packets log group 100 prefix \"DISCOVER: \"", "[feature] Device discovery")

	// Explicitly log mDNS for device discovery (multicast often bypasses state checking)
	sb.AddRule("input", "udp dport 5353 limit rate 100/second burst 50 packets log group 100 prefix \"DISCOVER_MDNS: \"", "[feature] mDNS discovery")

	// Add essential service rules (DHCP, DNS for router itself)
	// DHCP Client (WAN) and Server (LAN) - Must be before DROP_INVALID
	sb.AddRule("input", "udp dport 67-68 accept", "[svc:dhcp] DHCP server/client")
	sb.AddRule("output", "udp dport 67-68 accept", "[svc:dhcp] DHCP client")

	// VPN Lockout Protection Rules (ManagementAccess = true)
	// These rules ensure VPN traffic is ALWAYS accepted, even if other rules fail
	// Added BEFORE invalid packet drops and other rules
	if vpn != nil {
		// Tailscale lockout protection
		for _, ts := range vpn.Tailscale {
			if ts.ManagementAccess {
				iface := "tailscale0"
				if ts.Interface != "" {
					iface = ts.Interface
				}
				sb.AddRule("input", fmt.Sprintf("iifname %q accept comment \"tailscale-lockout-protection\"", iface))
				sb.AddRule("output", fmt.Sprintf("oifname %q accept comment \"tailscale-lockout-protection\"", iface))
				sb.AddRule("forward", fmt.Sprintf("iifname %q accept comment \"tailscale-lockout-protection\"", iface))
				sb.AddRule("forward", fmt.Sprintf("oifname %q accept comment \"tailscale-lockout-protection\"", iface))
			}
		}

		// WireGuard lockout protection
		for _, wg := range vpn.WireGuard {
			if wg.Enabled && wg.ManagementAccess {
				iface := wg.Interface
				if iface == "" {
					iface = "wg0"
				}
				sb.AddRule("input", fmt.Sprintf("iifname %q accept comment \"wireguard-lockout-protection\"", iface))
				sb.AddRule("output", fmt.Sprintf("oifname %q accept comment \"wireguard-lockout-protection\"", iface))
				sb.AddRule("forward", fmt.Sprintf("iifname %q accept comment \"wireguard-lockout-protection\"", iface))
				sb.AddRule("forward", fmt.Sprintf("oifname %q accept comment \"wireguard-lockout-protection\"", iface))
			}
		}
	}

	// MSS Clamping to PMTU (Forward Chain)
	// Keeps TCP connections healthy across links with different MTUs (e.g. PPPoE, VPNs)
	if cfg.MSSClamping {
		sb.AddRule("forward", "tcp flags syn tcp option maxseg size set rt mtu", "[feature] MSS clamping")
	}

	// Flowtable Offload Rule (Forward Chain)
	// Bypasses the rest of the ruleset for established connections in the flowtable.
	if cfg.EnableFlowOffload {
		sb.AddRule("forward", "ip protocol { tcp, udp } flow add @ft", "[feature] Flow offload")
	}

	// Add ICMP accept rules
	sb.AddRule("input", "meta l4proto icmp accept", "[base] ICMP")
	sb.AddRule("input", "meta l4proto icmpv6 accept", "[base] ICMPv6")
	// IPv6 Neighbor Discovery (Vital for IPv6 connectivity)
	sb.AddRule("input", "icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-solicit, nd-router-advert } accept", "[base] IPv6 ND")
	sb.AddRule("output", "meta l4proto icmp accept", "[base] ICMP")
	sb.AddRule("output", "meta l4proto icmpv6 accept", "[base] ICMPv6")
	sb.AddRule("output", "icmpv6 type { nd-neighbor-solicit, nd-neighbor-advert, nd-router-solicit, nd-router-advert } accept", "[base] IPv6 ND")

	// mDNS Reflector Rules (Must be before generic drops)
	if cfg.MDNS != nil && cfg.MDNS.Enabled && len(cfg.MDNS.Interfaces) > 0 {
		var mdnsIfaces []string
		for _, iface := range cfg.MDNS.Interfaces {
			mdnsIfaces = append(mdnsIfaces, forceQuote(iface))
		}
		ifacesStr := strings.Join(mdnsIfaces, ", ")

		// Allow INPUT multicast (query/response) on enabled interfaces
		sb.AddRule("input", fmt.Sprintf("iifname { %s } ip daddr 224.0.0.251 udp dport 5353 accept", ifacesStr), "[svc:mdns] Reflector input")
		sb.AddRule("input", fmt.Sprintf("iifname { %s } ip6 daddr ff02::fb udp dport 5353 accept", ifacesStr), "[svc:mdns] Reflector input v6")

		// Allow OUTPUT multicast (reflector sending)
		sb.AddRule("output", fmt.Sprintf("oifname { %s } ip daddr 224.0.0.251 udp dport 5353 accept", ifacesStr), "[svc:mdns] Reflector output")
		sb.AddRule("output", fmt.Sprintf("oifname { %s } ip6 daddr ff02::fb udp dport 5353 accept", ifacesStr), "[svc:mdns] Reflector output v6")
	}

	// NTP Service Rules
	if cfg.NTP != nil && cfg.NTP.Enabled {
		// Allow OUTPUT (Client syncing from upstream)
		sb.AddRule("output", "udp dport 123 accept", "[svc:ntp] Client sync")

		// Allow INPUT on Internal Zones (LAN Clients)
		zoneMap := buildZoneMapForScript(cfg)
		for _, zone := range cfg.Zones {
			ifaces := resolveZoneInterfaces(zone.Name, zoneMap)
			if isZoneInternal(&zone, ifaces) {
				for _, ifname := range ifaces {
					sb.AddRule("input", fmt.Sprintf("iifname %s udp dport 123 accept", forceQuote(ifname)), fmt.Sprintf("[svc:ntp] zone:%s", zone.Name))
				}
			}
		}
	}


	// UPnP Service Rules
	if cfg.UPnP != nil && cfg.UPnP.Enabled {
		if len(cfg.UPnP.InternalIntfs) > 0 {
			var upnpIfaces []string
			for _, iface := range cfg.UPnP.InternalIntfs {
				upnpIfaces = append(upnpIfaces, forceQuote(iface))
			}
			ifacesStr := strings.Join(upnpIfaces, ", ")

			// Allow SSDP (UDP 1900) on internal interfaces
			// Standard multicast address 239.255.255.250, but we allow port generic for simplicity
			sb.AddRule("input", fmt.Sprintf("iifname { %s } udp dport 1900 accept", ifacesStr), "[svc:upnp] SSDP discovery")
		}
	}

	// Drop mDNS noise on VPN interfaces (unless explicitly enabled above?)
	sb.AddRule("input", "iifname \"wg*\" udp dport 5353 limit rate 5/minute log group 0 prefix \"DROP_MDNS: \" counter drop", "[vpn] mDNS noise drop")
	sb.AddRule("input", "iifname \"tun*\" udp dport 5353 limit rate 5/minute log group 0 prefix \"DROP_MDNS: \" counter drop", "[vpn] mDNS noise drop")

	// Network Learning Rules
	// Log TLS traffic for SNI inspection.
	sb.AddRule("forward", "tcp dport 443 ct state established limit rate 50/second burst 20 packets log group 100 prefix \"TLS_SNI: \"", "[feature] TLS SNI learning")

	// VPN Transport Rules (WireGuard)
	if vpn != nil {
		for _, wg := range vpn.WireGuard {
			if wg.Enabled {
				// Allow incoming handshake/data on ListenPort
				if wg.ListenPort > 0 {
					sb.AddRule("input", fmt.Sprintf("udp dport %d accept", wg.ListenPort), fmt.Sprintf("[vpn:wg] %s listen", wg.Interface))
				}
				// Allow outgoing handshake/data to Peers
				for _, peer := range wg.Peers {
					parts := strings.Split(peer.Endpoint, ":")
					if len(parts) == 2 {
						port := parts[1]
						sb.AddRule("output", fmt.Sprintf("udp dport %s accept", port), fmt.Sprintf("[vpn:wg] peer %s", peer.Endpoint))
					}
				}
			}
		}
	}

	// DNS and API Access (Zone-Aware / Interface-Aware)

	// Global Output enabled for DNS (router resolving)
	sb.AddRule("output", "udp dport 53 accept", "[svc:dns] Router DNS client")
	sb.AddRule("output", "tcp dport 53 accept", "[svc:dns] Router DNS client")

	// Allow DNS/API/SSH/etc based on Zone and Interface config
	
	// Web Access Control (New Config)
	useNewWebRules := generateWebAccessRules(cfg, sb)

	// DNS Egress Control (DNS Wall) - Forward Chain
	// Block forwarding to IPs not in the allowed sets.
	// Only affects Forward chain (LAN Clients). Router Output is unrestricted.
	if cfg.DNS != nil && cfg.DNS.EgressFilter {
		// Allow if in set.
		// We rely on "return" or "accept"? 
		// If we use "return", we continue to other checks (e.g. invalid drop, logging).
		// But this is a positive security model: "Allow only if resolved".
		// Actually, we usually want to BLOCK if NOT in set.
		// "ip daddr != @dns_allowed_v4 reject"
		// But we must permit non-IP traffic? (L2?) "inet" table sees IP.
		// We must also permit the DNS traffic itself (UDP/TCP 53) to the router/upstream.
		// The forward chain structure usually:
		// 1. Established/Related ACCEPT
		// 2. Invalid DROP
		// 3. DNS Wall Check
		// 4. Policy Checks
		
		// DNS Wall Logic:
		// If protocol is IP, and dst is NOT in dns_allowed_v4, AND it is a new connection... REJECT.
		// But we must allow traffic to the DNS server itself?
		// Forward chain does not handle traffic TO the router. Input does.
		// Forward chain handles traffic THROUGH the router.
		// If client queries 8.8.8.8 (Forward), it must be allowed.
		// If client queries Router (Input), it's allowed.
		// So checking @dns_allowed usually implies we should add the DNS servers to the allowed set OR explicit allow rules.
		// Assuming strict egress:
		// "ip daddr != @dns_allowed_v4 ct state new reject" is very harsh. It blocks direct IP access.
		// Exceptions:
		// - DNS traffic (unless we force local DNS).
		// - NTP?
		// For v1, we apply this rule. Admin must add exceptions via specific Policies/Rules if needed?
		// Or we insert it after explicit Policies?
		// Plan says: "Generate reject rule in Forward chain".
		
		// If we place this logic here (after base rules, before policy rules), it overrides policies?
		// Usually "Standard" firewall:
		// 1. ConnTrack (Accept Est)
		// 2. Drop Invalid
		// 3. DNS Wall (Drop Unknown)
		// 4. Accept Policy
		
		// Yes, DNS Wall filters everything.
		// We need to support IPv6 too.
		
		sb.AddRule("forward", "ip daddr != @dns_allowed_v4 ct state new reject with icmp type admin-prohibited", "[dns-wall] Block unknown IPv4")
		sb.AddRule("forward", "ip6 daddr != @dns_allowed_v6 ct state new reject with icmpv6 type no-route", "[dns-wall] Block unknown IPv6")
	}

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

		if !useNewWebRules && (allowWeb || allowAPI) {
			addService(qIface, "http")
			addService(qIface, "https")

			if allowAPI || allowWeb {
				// Explicitly allow default API port 8443 (WebUI/API)
				tcpElements = append(tcpElements, fmt.Sprintf("%s . %d", qIface, 8443))
			}

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
		sb.AddRule("input", fmt.Sprintf("iifname . tcp dport { %s } accept", strings.Join(tcpElements, ", ")), "[svc] Consolidated TCP services")
	}

	if len(udpElements) > 0 {
		sb.AddRule("input", fmt.Sprintf("iifname . udp dport { %s } accept", strings.Join(udpElements, ", ")), "[svc] Consolidated UDP services")
	}

	// ICMP (Keep separate as it doesn't match dport)
	if len(icmpElements) > 0 {
		sb.AddRule("input", fmt.Sprintf("iifname { %s } meta l4proto icmp accept", strings.Join(icmpElements, ", ")), "[svc] Consolidated ICMP")
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

		ipsetComment := fmt.Sprintf("[ipset:%s] %s", ipset.Name, action)
		if applyInput {
			if matchSource {
				sb.AddRule("input", fmt.Sprintf("%s saddr @%s limit rate 10/minute log group 0 prefix \"DROP_IPSET: \" counter %s", addrFamily, quote(ipset.Name), action), ipsetComment)
			}
			if matchDest {
				sb.AddRule("input", fmt.Sprintf("%s daddr @%s limit rate 10/minute log group 0 prefix \"DROP_IPSET: \" counter %s", addrFamily, quote(ipset.Name), action), ipsetComment)
			}
		}

		if applyForward {
			if matchSource {
				sb.AddRule("forward", fmt.Sprintf("%s saddr @%s limit rate 10/minute log group 0 prefix \"DROP_IPSET: \" counter %s", addrFamily, ipset.Name, action), ipsetComment)
			}
			if matchDest {
				sb.AddRule("forward", fmt.Sprintf("%s daddr @%s limit rate 10/minute log group 0 prefix \"DROP_IPSET: \" counter %s", addrFamily, ipset.Name, action), ipsetComment)
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
		chainComment := fmt.Sprintf("[policy:%s→%s]", pol.From, pol.To)
		sb.AddChain(chainName, "", "", 0, "", chainComment)

		// Add rules to policy chain
		for i, rule := range pol.Rules {
			if rule.Disabled {
				continue
			}
			ruleExpr, err := BuildRuleExpression(rule)
			if err != nil {
				return nil, err
			}
			if ruleExpr != "" {
				ruleComment := ""
				if rule.Name != "" {
					ruleComment = fmt.Sprintf("[policy:%s→%s] %s", pol.From, pol.To, rule.Name)
				} else {
					ruleComment = fmt.Sprintf("[policy:%s→%s] rule#%d", pol.From, pol.To, i+1)
				}
				sb.AddRule(chainName, ruleExpr, ruleComment)
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
			if len(prefix) > 28 {
				prefix = prefix[:28] + ": "
			}
			sb.AddRule(chainName, fmt.Sprintf("limit rate 10/minute log group 0 prefix %q counter %s", prefix, defaultAction), fmt.Sprintf("[policy:%s→%s] default", pol.From, pol.To))
		} else {
			sb.AddRule(chainName, "counter "+defaultAction, fmt.Sprintf("[policy:%s→%s] default", pol.From, pol.To))
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
		sb.AddMap("input_vmap", "ifname", "verdict", "[base] Input dispatch map", nil, elements)
		sb.AddRule("input", "iifname vmap @input_vmap", "[base] Policy dispatch")
	}

	// Forward Map
	if len(forwardMap) > 0 {
		var elements []string
		for key, verdict := range forwardMap {
			elements = append(elements, fmt.Sprintf("%s : %s", key, verdict))
		}
		sb.AddMap("forward_vmap", "ifname . ifname", "verdict", "[base] Forward dispatch map", nil, elements)
		sb.AddRule("forward", "meta iifname . meta oifname vmap @forward_vmap", "[base] Policy dispatch")
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
		sb.AddRule("input", fmt.Sprintf("queue num %d bypass", queueGroup), "[feature] Inline learning queue")
		sb.AddRule("forward", fmt.Sprintf("queue num %d bypass", queueGroup), "[feature] Inline learning queue")
	} else {
		// Standard async mode with nflog
		sb.AddRule("input", "limit rate 10/minute burst 5 packets log group 0 prefix \"DROP_INPUT: \" counter drop", "[base] Final drop")
		sb.AddRule("forward", "limit rate 10/minute burst 5 packets log group 0 prefix \"DROP_FWD: \" counter drop", "[base] Final drop")
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

	// GeoIP matching (uses country-specific sets like @geoip_country_CN)
	// These sets must be populated by the GeoIPManager when configuration is applied
	if rule.SourceCountry != "" {
		// Country codes should be uppercase ISO 3166-1 alpha-2 (e.g., "US", "CN", "RU")
		countryCode := strings.ToUpper(rule.SourceCountry)
		parts = append(parts, fmt.Sprintf("ip saddr @geoip_country_%s", countryCode))
	}
	if rule.DestCountry != "" {
		countryCode := strings.ToUpper(rule.DestCountry)
		parts = append(parts, fmt.Sprintf("ip daddr @geoip_country_%s", countryCode))
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

		// Resolve Interfaces (InInterface) for DNAT
		// If InInterface is empty, we imply all interfaces (empty string in slice)
		// If it matches a Zone, we expand to all interfaces in that zone.
		var inInterfaces []string
		if r.Type == "dnat" {
			if r.InInterface != "" {
				if z := findZone(cfg.Zones, r.InInterface); z != nil {
					// It's a zone
					inInterfaces = zoneMap[z.Name]
				} else {
					// Assumed to be an interface name
					inInterfaces = []string{r.InInterface}
				}
			} else {
				// Empty interface - match all
				inInterfaces = []string{""}
			}
		} else {
			// For SNAT/Masquerade, InInterface is rare/optional match.
			// Legacy used it as match filter.
			// Use simple value for now unless we want to support zone matching there too.
			// Let's support zone matching for consistency if specified.
			if r.InInterface != "" {
				if z := findZone(cfg.Zones, r.InInterface); z != nil {
					inInterfaces = zoneMap[z.Name]
				} else {
					inInterfaces = []string{r.InInterface}
				}
			} else {
				inInterfaces = []string{""}
			}
		}

		// Expand rule for each interface match
		for _, ifaceName := range inInterfaces {
			// Common matchers
			match := ""

			// Logic to avoid redundant protocol check
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

			commentSuffix := ""
			if r.Description != "" {
				commentSuffix = fmt.Sprintf(" comment %q", r.Description)
			}

			if r.Type == "masquerade" && r.OutInterface != "" {
				if !seenMasq[r.OutInterface] {
					combinedMatch := fmt.Sprintf("oifname \"%s\" %s", r.OutInterface, match)
					if ifaceName != "" {
						// Add InInterface match if specific (e.g. zone based masquerade restriction)
						combinedMatch += fmt.Sprintf("iifname \"%s\" ", ifaceName)
					}
					sb.AddRule("postrouting", fmt.Sprintf("%smasquerade%s", combinedMatch, commentSuffix))
					seenMasq[r.OutInterface] = true
				}

			} else if r.Type == "dnat" && (r.ToIP != "" || r.ToPort != "") {
				// DNAT
				matchExpr := match
				if ifaceName != "" {
					matchExpr = fmt.Sprintf("iifname \"%s\" %s", ifaceName, match)
				}

				// Append dport match if present
				if r.DestPort != "" {
					proto := r.Protocol
					if proto == "" || proto == "any" {
						proto = "tcp"
					}
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
				// SNAT
				currentMatch := match
				if ifaceName != "" {
					currentMatch = fmt.Sprintf("iifname \"%s\" %s", ifaceName, match)
				}
				combinedMatch := fmt.Sprintf("oifname \"%s\" %s", r.OutInterface, currentMatch)
				sb.AddRule("postrouting", fmt.Sprintf("%ssnat to %s%s", combinedMatch, r.SNATIP, commentSuffix))
			}

			// Hairpin NAT (Reflected)
			if r.Type == "dnat" && r.Hairpin && r.ToIP != "" {
				// Resolve WAN IPs
				var hairpinIPs []string
				if r.DestIP != "" {
					hairpinIPs = append(hairpinIPs, r.DestIP)
				} else if ifaceName != "" {
					// Use current iteration interface for resolution
					for _, iface := range cfg.Interfaces {
						if iface.Name == ifaceName {
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

						if ifaceName != "" {
							hairpinMatch = fmt.Sprintf("iifname != \"%s\" %s", ifaceName, hairpinMatch)
						}

						target := fmt.Sprintf("%s:%s", r.ToIP, r.ToPort)
						if r.ToPort == "" {
							target = r.ToIP
						}

						sb.AddRule("prerouting", fmt.Sprintf("%sdnat to %s comment \"hairpin: %s\"", hairpinMatch, target, r.Name))

						// 2. Hairpin SNAT (Masquerade)
						masqMatch := ""
						if ifaceName != "" {
							masqMatch += fmt.Sprintf("iifname != \"%s\" ", ifaceName)
						}

						masqMatch += fmt.Sprintf("ip daddr %s ", r.ToIP)
						if r.ToPort != "" {
							proto := r.Protocol
							if proto == "" || proto == "any" {
								proto = "tcp"
							}
							masqMatch += fmt.Sprintf("%s dport %s ", proto, r.ToPort)
						}

						sb.AddRule("postrouting", fmt.Sprintf("%smasquerade comment \"hairpin-masq: %s\"", masqMatch, r.Name))
					}
				}
			}
		} // end ifaceName loop
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
	// Sandbox mode (privilege separation via network namespace) is enabled by default.
	sandboxEnabled := false // FORCE DISABLED: Sandbox logic removed
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
		if ur.UID <= 0 {
			continue
		}

		// Resolve uplink name to mark
		// Search UplinkGroups for matching uplink name
		var mark uint32
		uplinkName := ur.Uplink
		if uplinkName == "" {
			uplinkName = ur.VPNLink // Fallback to VPNLink field
		}

		if uplinkName != "" {
			mark = resolveUplinkToMark(cfg, uplinkName)
		}

		if mark == 0 {
			// Could not resolve uplink - log and skip
			// (In a real scenario, log warning here)
			continue
		}

		// Generate nft rule: meta skuid <uid> mark set <mark> ct mark set meta mark
		sb.AddRule("output", fmt.Sprintf("meta skuid %d meta mark set 0x%x ct mark set meta mark",
			ur.UID, mark), fmt.Sprintf("[uid-route] %s", ur.Name))
		seenMarks[int(mark)] = true
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

// resolveUplinkToMark searches VPN config for an uplink by interface name and returns its routing mark.
// Returns 0 if the uplink cannot be found.
// Note: This is a simplified implementation that works with firewall.Config's available fields.
// Full UplinkGroup resolution would require extending firewall.Config.
func resolveUplinkToMark(cfg *Config, uplinkName string) uint32 {
	if cfg == nil {
		return 0
	}

	// Search VPN config for WireGuard tunnels matching the name
	if cfg.VPN != nil {
		for i, wg := range cfg.VPN.WireGuard {
			if wg.Interface == uplinkName || wg.Name == uplinkName {
				return uint32(0x0200 + i) // MarkWireGuardBase + index
			}
		}
		for i, ts := range cfg.VPN.Tailscale {
			iface := ts.Interface
			if iface == "" {
				iface = "tailscale0"
			}
			if iface == uplinkName {
				return uint32(0x0220 + i) // MarkTailscaleBase + index
			}
		}
	}

	// Search Interfaces for matching interface with a Table > 0 (Multi-WAN style)
	for i, iface := range cfg.Interfaces {
		if iface.Name == uplinkName && iface.Table > 0 {
			return uint32(0x0100 + i) // MarkWANBase + index
		}
	}

	return 0
}
