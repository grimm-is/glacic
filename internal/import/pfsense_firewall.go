package imports

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

// PfSenseFullConfig extends PfSenseConfig with firewall rules and other settings.
type PfSenseFullConfig struct {
	XMLName    xml.Name          `xml:"pfsense"`
	Version    string            `xml:"version"`
	System     PfSenseSystem     `xml:"system"`
	Interfaces PfSenseInterfaces `xml:"interfaces"`
	// DHCPD is parsed using the shared type
	DHCPD   PfSenseDHCPD   `xml:"dhcpd"`
	Aliases []PfSenseAlias `xml:"aliases>alias"`
	Filter  PfSenseFilter  `xml:"filter"`
	NAT     PfSenseNAT     `xml:"nat"`
	Unbound PfSenseUnbound `xml:"unbound"`
	DNSMasq PfSenseDNSMasq `xml:"dnsmasq"`

	// Raw XML for custom parsing
	RawDHCPD []byte `xml:"-"`
}

// PfSenseSystem contains system-level settings.
type PfSenseSystem struct {
	Hostname         string `xml:"hostname"`
	Domain           string `xml:"domain"`
	DNSServer        string `xml:"dnsserver"`
	DNSAllowOverride string `xml:"dnsallowoverride"`
	Timezone         string `xml:"timezone"`
}

// PfSenseInterfaces contains interface definitions.
type PfSenseInterfaces struct {
	WAN  PfSenseInterface `xml:"wan"`
	LAN  PfSenseInterface `xml:"lan"`
	OPT1 PfSenseInterface `xml:"opt1"`
	OPT2 PfSenseInterface `xml:"opt2"`
	OPT3 PfSenseInterface `xml:"opt3"`
	OPT4 PfSenseInterface `xml:"opt4"`
}

// PfSenseInterface represents an interface configuration.
type PfSenseInterface struct {
	Enable      string `xml:"enable"`
	Descr       string `xml:"descr"`
	If          string `xml:"if"`     // FreeBSD interface name (e.g., "em0", "igb0")
	IPAddr      string `xml:"ipaddr"` // "dhcp" or static IP
	Subnet      string `xml:"subnet"` // CIDR prefix length
	Gateway     string `xml:"gateway"`
	Spoofmac    string `xml:"spoofmac"`
	BlockPriv   string `xml:"blockpriv"`   // Block private networks
	BlockBogons string `xml:"blockbogons"` // Block bogon networks
}

// PfSenseFilter contains firewall filter rules.
type PfSenseFilter struct {
	Rules []PfSenseFilterRule `xml:"rule"`
}

// PfSenseFilterRule represents a firewall rule.
type PfSenseFilterRule struct {
	ID           string         `xml:"id,attr"`
	Tracker      string         `xml:"tracker"`
	Type         string         `xml:"type"`       // "pass", "block", "reject"
	Interface    string         `xml:"interface"`  // "wan", "lan", "opt1", etc.
	IPProtocol   string         `xml:"ipprotocol"` // "inet", "inet6", "inet46"
	Protocol     string         `xml:"protocol"`   // "tcp", "udp", "icmp", "any"
	Source       PfSenseAddress `xml:"source"`
	Destination  PfSenseAddress `xml:"destination"`
	Descr        string         `xml:"descr"`
	Log          string         `xml:"log"`
	Quick        string         `xml:"quick"`
	Disabled     string         `xml:"disabled"`
	Floating     string         `xml:"floating"`
	Direction    string         `xml:"direction"` // "in", "out", "any"
	Statetype    string         `xml:"statetype"` // "keep state", "sloppy state", etc.
	OS           string         `xml:"os"`        // OS fingerprint matching
	Tag          string         `xml:"tag"`
	Tagged       string         `xml:"tagged"`
	Max          string         `xml:"max"` // Max states
	MaxSrcNodes  string         `xml:"max-src-nodes"`
	MaxSrcConn   string         `xml:"max-src-conn"`
	MaxSrcStates string         `xml:"max-src-states"`
	Statetimeout string         `xml:"statetimeout"`
	Gateway      string         `xml:"gateway"` // Policy routing gateway
	Sched        string         `xml:"sched"`   // Schedule name
	Associated   string         `xml:"associated-rule-id"`
}

// PfSenseAddress represents source or destination in a rule.
type PfSenseAddress struct {
	Any     string `xml:"any"`
	Network string `xml:"network"` // "lan", "wan", "(self)", or CIDR
	Address string `xml:"address"` // IP or alias name
	Port    string `xml:"port"`    // Single port or range "80" or "80-443"
	Not     string `xml:"not"`     // Negate the match
}

// PfSenseNAT contains NAT rules.
type PfSenseNAT struct {
	Outbound PfSenseOutboundNAT `xml:"outbound"`
	Rules    []PfSenseNATRule   `xml:"rule"`
}

// PfSenseOutboundNAT contains outbound NAT settings.
type PfSenseOutboundNAT struct {
	Mode  string                `xml:"mode"` // "automatic", "hybrid", "advanced", "disabled"
	Rules []PfSenseOutboundRule `xml:"rule"`
}

// PfSenseOutboundRule represents an outbound NAT rule.
type PfSenseOutboundRule struct {
	Interface     string         `xml:"interface"`
	Source        PfSenseAddress `xml:"source"`
	Destination   PfSenseAddress `xml:"destination"`
	Target        string         `xml:"target"` // NAT target address
	TargetIP      string         `xml:"targetip"`
	PoolOpts      string         `xml:"poolopts"`
	SourceHash    string         `xml:"source_hash_key"`
	Descr         string         `xml:"descr"`
	Disabled      string         `xml:"disabled"`
	NoNAT         string         `xml:"nonat"`
	StaticNatPort string         `xml:"staticnatport"`
}

// PfSenseNATRule represents a port forward (DNAT) rule.
type PfSenseNATRule struct {
	Interface   string         `xml:"interface"`
	Protocol    string         `xml:"protocol"`
	Source      PfSenseAddress `xml:"source"`
	Destination PfSenseAddress `xml:"destination"`
	Target      string         `xml:"target"`     // Internal IP
	LocalPort   string         `xml:"local-port"` // Internal port
	Descr       string         `xml:"descr"`
	Disabled    string         `xml:"disabled"`
	NoRDR       string         `xml:"nordr"`
	Associated  string         `xml:"associated-rule-id"`
}

// ParsePfSenseBackup parses a complete pfSense/OPNsense backup file.
func ParsePfSenseBackup(path string) (*ImportResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup file: %w", err)
	}

	var cfg PfSenseFullConfig
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	result := &ImportResult{
		Hostname: cfg.System.Hostname,
		Domain:   cfg.System.Domain,
	}

	// Extract interfaces
	result.Interfaces = extractInterfaces(&cfg)

	// Extract aliases
	result.Aliases = extractAliases(cfg.Aliases)

	// Extract filter rules
	result.FilterRules = extractFilterRules(cfg.Filter.Rules, result.Interfaces)

	// Extract NAT rules
	result.NATRules, result.PortForwards = extractNATRules(&cfg.NAT, result.Interfaces)

	// Extract DNS hosts
	result.StaticHosts = extractStaticHosts(&cfg)

	// Extract DHCP scopes
	result.DHCPScopes = extractDHCPScopes(&cfg, result.Interfaces)

	// Generate migration guidance
	generateMigrationGuidance(result)

	return result, nil
}

func extractInterfaces(cfg *PfSenseFullConfig) []ImportedInterface {
	var interfaces []ImportedInterface

	// Helper to add interface if enabled
	addIf := func(name string, iface PfSenseInterface) {
		if iface.If == "" {
			return
		}

		imp := ImportedInterface{
			OriginalName: name,
			OriginalIf:   iface.If,
			Description:  iface.Descr,
			IsDHCP:       iface.IPAddr == "dhcp",
			BlockPrivate: iface.BlockPriv != "",
			BlockBogons:  iface.BlockBogons != "",
			NeedsMapping: true,
		}

		if !imp.IsDHCP {
			imp.IPAddress = iface.IPAddr
			imp.Subnet = iface.Subnet
		}

		// Suggest zone based on name
		switch strings.ToLower(name) {
		case "wan":
			imp.Zone = "WAN"
			imp.SuggestedIf = "eth0"
		case "lan":
			imp.Zone = "LAN"
			imp.SuggestedIf = "eth1"
		default:
			if iface.Descr != "" {
				imp.Zone = iface.Descr
			} else {
				imp.Zone = strings.ToUpper(name)
			}
			imp.SuggestedIf = fmt.Sprintf("eth%d", len(interfaces))
		}

		interfaces = append(interfaces, imp)
	}

	addIf("wan", cfg.Interfaces.WAN)
	addIf("lan", cfg.Interfaces.LAN)
	addIf("opt1", cfg.Interfaces.OPT1)
	addIf("opt2", cfg.Interfaces.OPT2)
	addIf("opt3", cfg.Interfaces.OPT3)
	addIf("opt4", cfg.Interfaces.OPT4)

	return interfaces
}

func extractAliases(aliases []PfSenseAlias) []ImportedAlias {
	var result []ImportedAlias

	for _, a := range aliases {
		imp := ImportedAlias{
			Name:        a.Name,
			Type:        a.Type,
			Description: a.Descr,
			Values:      strings.Fields(strings.ReplaceAll(a.Address, " ", "\n")),
		}

		// Determine if we can import this
		switch a.Type {
		case "host", "network":
			imp.CanImport = true
			imp.ConvertType = "ipset"
		case "port":
			imp.CanImport = true
			imp.ConvertType = "service"
		case "urltable", "urltable_ports":
			imp.CanImport = true
			imp.ConvertType = "ipset" // Can use URL source
		default:
			imp.CanImport = false
		}

		result = append(result, imp)
	}

	return result
}

func extractFilterRules(rules []PfSenseFilterRule, interfaces []ImportedInterface) []ImportedFilterRule {
	var result []ImportedFilterRule

	// Build interface name map
	ifaceMap := make(map[string]string)
	for _, iface := range interfaces {
		ifaceMap[iface.OriginalName] = iface.Zone
	}

	for _, r := range rules {
		imp := ImportedFilterRule{
			Description: r.Descr,
			Interface:   r.Interface,
			Direction:   r.Direction,
			Protocol:    r.Protocol,
			Log:         r.Log != "",
			Disabled:    r.Disabled != "",
			CanImport:   true,
		}

		// Convert action
		switch r.Type {
		case "pass":
			imp.Action = "accept"
		case "block":
			imp.Action = "drop"
		case "reject":
			imp.Action = "reject"
		default:
			imp.Action = r.Type
		}

		// Convert source
		if r.Source.Any != "" {
			imp.Source = "any"
		} else if r.Source.Network != "" {
			imp.Source = r.Source.Network
		} else if r.Source.Address != "" {
			imp.Source = r.Source.Address
		}
		imp.SourcePort = r.Source.Port

		// Convert destination
		if r.Destination.Any != "" {
			imp.Destination = "any"
		} else if r.Destination.Network != "" {
			imp.Destination = r.Destination.Network
		} else if r.Destination.Address != "" {
			imp.Destination = r.Destination.Address
		}
		imp.DestPort = r.Destination.Port

		// Suggest policy name
		if zone, ok := ifaceMap[r.Interface]; ok {
			imp.SuggestedPolicy = fmt.Sprintf("%s_policy", strings.ToLower(zone))
		}

		// Check for features that need manual handling
		if r.Gateway != "" {
			imp.ImportNotes = append(imp.ImportNotes, "Policy routing gateway - requires manual configuration")
		}
		if r.Sched != "" {
			imp.ImportNotes = append(imp.ImportNotes, "Schedule-based rule - check scheduler configuration")
		}
		if r.OS != "" {
			imp.ImportNotes = append(imp.ImportNotes, "OS fingerprint matching not supported")
			imp.CanImport = false
		}
		if r.Floating != "" {
			imp.ImportNotes = append(imp.ImportNotes, "Floating rule - applies to multiple interfaces")
		}
		if r.MaxSrcConn != "" || r.MaxSrcNodes != "" {
			imp.ImportNotes = append(imp.ImportNotes, "Connection limits - use rate limiting")
		}

		result = append(result, imp)
	}

	return result
}

func extractNATRules(nat *PfSenseNAT, interfaces []ImportedInterface) ([]ImportedNATRule, []ImportedPortForward) {
	var natRules []ImportedNATRule
	var portForwards []ImportedPortForward

	// Outbound NAT rules
	if nat.Outbound.Mode == "automatic" || nat.Outbound.Mode == "hybrid" {
		natRules = append(natRules, ImportedNATRule{
			Type:        "masquerade",
			Interface:   "wan",
			Description: "Automatic outbound NAT",
			CanImport:   true,
		})
	}

	for _, r := range nat.Outbound.Rules {
		imp := ImportedNATRule{
			Type:        "snat",
			Interface:   r.Interface,
			Description: r.Descr,
			Disabled:    r.Disabled != "",
			CanImport:   true,
		}

		if r.Source.Network != "" {
			imp.Source = r.Source.Network
		} else if r.Source.Address != "" {
			imp.Source = r.Source.Address
		}

		if r.NoNAT != "" {
			imp.ImportNotes = append(imp.ImportNotes, "No-NAT rule - excludes traffic from NAT")
		}

		natRules = append(natRules, imp)
	}

	// Port forwards (DNAT)
	for _, r := range nat.Rules {
		imp := ImportedPortForward{
			Interface:    r.Interface,
			Protocol:     r.Protocol,
			InternalIP:   r.Target,
			InternalPort: r.LocalPort,
			Description:  r.Descr,
			Disabled:     r.Disabled != "",
			CanImport:    true,
		}

		if r.Destination.Port != "" {
			imp.ExternalPort = r.Destination.Port
		}

		if r.NoRDR != "" {
			imp.ImportNotes = append(imp.ImportNotes, "No-redirect rule")
			imp.CanImport = false
		}

		portForwards = append(portForwards, imp)
	}

	return natRules, portForwards
}

func extractStaticHosts(cfg *PfSenseFullConfig) []ImportedStaticHost {
	var hosts []ImportedStaticHost

	// From Unbound
	for _, h := range cfg.Unbound.Hosts {
		hosts = append(hosts, ImportedStaticHost{
			Hostname: h.Host,
			Domain:   h.Domain,
			IP:       h.IP,
		})
	}

	// From dnsmasq
	for _, h := range cfg.DNSMasq.Hosts {
		hosts = append(hosts, ImportedStaticHost{
			Hostname: h.Host,
			Domain:   h.Domain,
			IP:       h.IP,
		})
	}

	return hosts
}

func extractDHCPScopes(cfg *PfSenseFullConfig, interfaces []ImportedInterface) []ImportedDHCPScope {
	var scopes []ImportedDHCPScope

	// Map interface names (e.g., "lan") to zones/interfaces
	ifaceMap := make(map[string]ImportedInterface)
	for _, iface := range interfaces {
		ifaceMap[iface.OriginalName] = iface
	}

	for name, dhcpIf := range cfg.DHCPD.Interfaces {
		if dhcpIf.RangeFrom == "" {
			continue
		}

		iface, ok := ifaceMap[name]
		if !ok {
			continue
		}

		scope := ImportedDHCPScope{
			Interface:  iface.SuggestedIf,
			RangeStart: dhcpIf.RangeFrom,
			RangeEnd:   dhcpIf.RangeTo,
			Gateway:    dhcpIf.Gateway,
			Domain:     dhcpIf.Domain,
		}

		if dhcpIf.DNSServer1 != "" {
			scope.DNS = append(scope.DNS, dhcpIf.DNSServer1)
		}
		if dhcpIf.DNSServer2 != "" {
			scope.DNS = append(scope.DNS, dhcpIf.DNSServer2)
		}

		// Static mappings
		for _, m := range dhcpIf.StaticMaps {
			if m.MAC == "" || m.IPAddr == "" {
				continue
			}
			scope.Reservations = append(scope.Reservations, ImportedDHCPReservation{
				MAC:         m.MAC,
				IP:          m.IPAddr,
				Hostname:    m.Hostname,
				Description: m.Description,
			})
		}

		scopes = append(scopes, scope)
	}

	return scopes
}

func generateMigrationGuidance(result *ImportResult) {
	// Interface mapping warnings
	for _, iface := range result.Interfaces {
		result.ManualSteps = append(result.ManualSteps,
			fmt.Sprintf("Map pfSense interface '%s' (%s) to Linux interface (suggested: %s)",
				iface.OriginalName, iface.OriginalIf, iface.SuggestedIf))
	}

	// FreeBSD vs Linux differences
	result.Warnings = append(result.Warnings,
		"Interface names differ between FreeBSD (em0, igb0) and Linux (eth0, enp0s3)",
		"Review all interface references in rules after mapping",
	)

	// Check for incompatible features
	for _, rule := range result.FilterRules {
		if !rule.CanImport {
			result.Incompatible = append(result.Incompatible,
				fmt.Sprintf("Rule '%s': %s", rule.Description, strings.Join(rule.ImportNotes, "; ")))
		}
	}

	// General guidance
	result.ManualSteps = append(result.ManualSteps,
		"Review and test all firewall rules after import",
		"Verify NAT rules work correctly with Linux interface names",
		"Check DHCP scopes are assigned to correct interfaces",
		"Test DNS resolution after migration",
	)

	// Alias migration
	hasAliases := false
	for _, a := range result.Aliases {
		if a.CanImport {
			hasAliases = true
			break
		}
	}
	if hasAliases {
		result.ManualSteps = append(result.ManualSteps,
			"Import aliases as IPSets or service definitions")
	}
}

// GenerateHCLConfig generates an HCL configuration from import result.
// This is a starting point that requires manual review.
func (r *ImportResult) GenerateHCLConfig() string {
	var sb strings.Builder

	sb.WriteString("# Configuration - Imported from pfSense/OPNsense\n")
	sb.WriteString("# ⚠️  REQUIRES MANUAL REVIEW AND INTERFACE MAPPING\n")
	sb.WriteString("#\n")
	sb.WriteString("# Original hostname: " + r.Hostname + "\n")
	sb.WriteString("# Original domain: " + r.Domain + "\n")
	sb.WriteString("#\n\n")

	sb.WriteString("schema_version = \"1.0\"\n")
	sb.WriteString("ip_forwarding = true\n\n")

	// Interfaces
	sb.WriteString("# ============================================\n")
	sb.WriteString("# INTERFACES - UPDATE NAMES FOR YOUR SYSTEM\n")
	sb.WriteString("# ============================================\n\n")

	for _, iface := range r.Interfaces {
		sb.WriteString(fmt.Sprintf("# Original: %s (%s)\n", iface.OriginalName, iface.OriginalIf))
		sb.WriteString(fmt.Sprintf("interface \"%s\" {\n", iface.SuggestedIf))
		sb.WriteString(fmt.Sprintf("  description = \"%s\"\n", iface.Description))
		sb.WriteString(fmt.Sprintf("  zone = \"%s\"\n", iface.Zone))
		if iface.IsDHCP {
			sb.WriteString("  dhcp = true\n")
		} else if iface.IPAddress != "" {
			sb.WriteString(fmt.Sprintf("  ipv4 = [\"%s/%s\"]\n", iface.IPAddress, iface.Subnet))
		}
		sb.WriteString("}\n\n")
	}

	// Zones
	sb.WriteString("# ============================================\n")
	sb.WriteString("# ZONES\n")
	sb.WriteString("# ============================================\n\n")

	seenZones := make(map[string]bool)
	for _, iface := range r.Interfaces {
		if !seenZones[iface.Zone] {
			seenZones[iface.Zone] = true
			sb.WriteString(fmt.Sprintf("zone \"%s\" {\n", iface.Zone))
			sb.WriteString(fmt.Sprintf("  interfaces = [\"%s\"]\n", iface.SuggestedIf))
			sb.WriteString("}\n\n")
		}
	}

	// Sample policies from rules
	sb.WriteString("# ============================================\n")
	sb.WriteString("# POLICIES - REVIEW AND ADJUST\n")
	sb.WriteString("# ============================================\n\n")

	// Group rules by interface/zone
	rulesByZone := make(map[string][]ImportedFilterRule)
	for _, rule := range r.FilterRules {
		zone := rule.SuggestedPolicy
		if zone == "" {
			zone = "default_policy"
		}
		rulesByZone[zone] = append(rulesByZone[zone], rule)
	}

	for policyName, rules := range rulesByZone {
		sb.WriteString(fmt.Sprintf("policy \"%s\" {\n", policyName))
		sb.WriteString("  # from = \"ZONE\"\n")
		sb.WriteString("  # to = \"ZONE\"\n")
		sb.WriteString("  default_action = \"drop\"\n\n")

		for i, rule := range rules {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("  # ... and %d more rules\n", len(rules)-10))
				break
			}

			name := rule.Description
			if name == "" {
				name = fmt.Sprintf("rule_%d", i+1)
			}
			name = strings.ReplaceAll(name, " ", "_")
			name = strings.ReplaceAll(name, "\"", "")

			sb.WriteString(fmt.Sprintf("  rule \"%s\" {\n", name))
			if rule.Protocol != "" && rule.Protocol != "any" {
				sb.WriteString(fmt.Sprintf("    proto = \"%s\"\n", rule.Protocol))
			}
			if rule.DestPort != "" {
				sb.WriteString(fmt.Sprintf("    dest_port = %s\n", rule.DestPort))
			}
			sb.WriteString(fmt.Sprintf("    action = \"%s\"\n", rule.Action))
			if rule.Disabled {
				sb.WriteString("    disabled = true\n")
			}
			sb.WriteString("  }\n\n")
		}
		sb.WriteString("}\n\n")
	}

	// Port forwards
	if len(r.PortForwards) > 0 {
		sb.WriteString("# ============================================\n")
		sb.WriteString("# PORT FORWARDS (DNAT)\n")
		sb.WriteString("# ============================================\n\n")

		for i, pf := range r.PortForwards {
			name := pf.Description
			if name == "" {
				name = fmt.Sprintf("portfwd_%d", i+1)
			}
			name = strings.ReplaceAll(name, " ", "_")

			sb.WriteString(fmt.Sprintf("# %s\n", pf.Description))
			sb.WriteString(fmt.Sprintf("nat \"%s\" {\n", name))
			sb.WriteString("  type = \"dnat\"\n")
			sb.WriteString(fmt.Sprintf("  in_interface = \"%s\"  # UPDATE\n", pf.Interface))
			if pf.Protocol != "" {
				sb.WriteString(fmt.Sprintf("  protocol = \"%s\"\n", pf.Protocol))
			}
			if pf.ExternalPort != "" {
				sb.WriteString(fmt.Sprintf("  dest_port = %s\n", pf.ExternalPort))
			}
			sb.WriteString(fmt.Sprintf("  to_address = \"%s\"\n", pf.InternalIP))
			if pf.InternalPort != "" {
				sb.WriteString(fmt.Sprintf("  to_port = %s\n", pf.InternalPort))
			}
			sb.WriteString("}\n\n")
		}
	}

	// Static hosts
	if len(r.StaticHosts) > 0 {
		sb.WriteString("# ============================================\n")
		sb.WriteString("# DNS STATIC HOSTS\n")
		sb.WriteString("# ============================================\n\n")
		sb.WriteString("dns_server {\n")
		sb.WriteString("  enabled = true\n\n")

		for _, h := range r.StaticHosts {
			hostname := h.Hostname
			if h.Domain != "" {
				hostname = h.Hostname + "." + h.Domain
			}
			sb.WriteString(fmt.Sprintf("  host \"%s\" {\n", h.IP))
			sb.WriteString(fmt.Sprintf("    hostnames = [\"%s\"]\n", hostname))
			sb.WriteString("  }\n")
		}
		sb.WriteString("}\n\n")
	}

	// DHCP Server
	if len(r.DHCPScopes) > 0 {
		sb.WriteString("# ============================================\n")
		sb.WriteString("# DHCP SERVER\n")
		sb.WriteString("# ============================================\n\n")
		sb.WriteString("dhcp {\n")
		sb.WriteString("  enabled = true\n\n")

		for _, scope := range r.DHCPScopes {
			sb.WriteString(fmt.Sprintf("  scope \"%s\" {\n", scope.Interface))
			sb.WriteString(fmt.Sprintf("    interface = \"%s\"\n", scope.Interface))
			sb.WriteString(fmt.Sprintf("    range_start = \"%s\"\n", scope.RangeStart))
			sb.WriteString(fmt.Sprintf("    range_end = \"%s\"\n", scope.RangeEnd))

			if len(scope.DNS) > 0 {
				sb.WriteString(fmt.Sprintf("    dns_servers = [\"%s\"]\n", strings.Join(scope.DNS, "\", \"")))
			}
			if scope.Gateway != "" {
				sb.WriteString(fmt.Sprintf("    gateway = \"%s\"\n", scope.Gateway))
			}

			// Reservations
			for _, res := range scope.Reservations {
				sb.WriteString("    static {\n")
				sb.WriteString(fmt.Sprintf("      mac = \"%s\"\n", res.MAC))
				sb.WriteString(fmt.Sprintf("      ip = \"%s\"\n", res.IP))
				if res.Hostname != "" {
					sb.WriteString(fmt.Sprintf("      hostname = \"%s\"\n", res.Hostname))
				}
				sb.WriteString("    }\n")
			}
			sb.WriteString("  }\n\n")
		}
		sb.WriteString("}\n\n")
	}

	return sb.String()
}
