package imports

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// OpenWrtFirewallConfig holds parsed OpenWrt firewall configuration.
type OpenWrtFirewallConfig struct {
	Zones     []OpenWrtZone
	Forwards  []OpenWrtForward
	Rules     []OpenWrtRule
	Redirects []OpenWrtRedirect
	Defaults  OpenWrtDefaults
	Warnings  []string
}

// OpenWrtDefaults represents the defaults section.
type OpenWrtDefaults struct {
	Input       string // accept, drop, reject
	Output      string
	Forward     string
	SynFlood    bool
	DropInvalid bool
}

// OpenWrtZone represents a firewall zone.
type OpenWrtZone struct {
	Name      string
	Network   []string // Interfaces/networks in this zone
	Input     string   // accept, drop, reject
	Output    string
	Forward   string
	Masq      bool     // Enable masquerading
	MasqSrc   []string // Masquerade source networks
	MasqDest  []string // Masquerade destination networks
	MTUFix    bool
	Conntrack bool
}

// OpenWrtForward represents inter-zone forwarding policy.
type OpenWrtForward struct {
	Src     string // Source zone
	Dest    string // Destination zone
	Enabled bool
}

// OpenWrtRule represents a firewall rule.
type OpenWrtRule struct {
	Name       string
	Src        string // Source zone
	SrcIP      string
	SrcMAC     string
	SrcPort    string
	Dest       string // Destination zone
	DestIP     string
	DestPort   string
	Proto      string // tcp, udp, icmp, all
	Target     string // ACCEPT, DROP, REJECT
	Family     string // ipv4, ipv6, any
	Enabled    bool
	Limit      string // Rate limit
	LimitBurst int
	Extra      string // Extra iptables options
}

// OpenWrtRedirect represents a port forward/redirect.
type OpenWrtRedirect struct {
	Name       string
	Src        string
	SrcIP      string
	SrcDPort   string // External port
	Dest       string
	DestIP     string // Internal IP
	DestPort   string // Internal port
	Proto      string
	Target     string // DNAT, SNAT
	Enabled    bool
	Reflection bool // NAT reflection
}

// ParseOpenWrtFirewall parses /etc/config/firewall.
func ParseOpenWrtFirewall(path string) (*OpenWrtFirewallConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open firewall config: %w", err)
	}
	defer file.Close()

	cfg := &OpenWrtFirewallConfig{}
	scanner := bufio.NewScanner(file)

	var currentSection string
	var currentZone *OpenWrtZone
	var currentForward *OpenWrtForward
	var currentRule *OpenWrtRule
	var currentRedirect *OpenWrtRedirect

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section headers
		if strings.HasPrefix(line, "config ") {
			// Save previous section
			saveSection(cfg, currentSection, currentZone, currentForward, currentRule, currentRedirect)
			currentZone = nil
			currentForward = nil
			currentRule = nil
			currentRedirect = nil

			parts := strings.Fields(line)
			if len(parts) >= 2 {
				currentSection = parts[1]
				switch currentSection {
				case "zone":
					currentZone = &OpenWrtZone{}
					if len(parts) >= 3 {
						currentZone.Name = strings.Trim(parts[2], "'\"")
					}
				case "forwarding":
					currentForward = &OpenWrtForward{Enabled: true}
				case "rule":
					currentRule = &OpenWrtRule{Enabled: true}
					if len(parts) >= 3 {
						currentRule.Name = strings.Trim(parts[2], "'\"")
					}
				case "redirect":
					currentRedirect = &OpenWrtRedirect{Enabled: true}
					if len(parts) >= 3 {
						currentRedirect.Name = strings.Trim(parts[2], "'\"")
					}
				case "defaults":
					// Will be parsed via options
				}
			}
			continue
		}

		// Parse options
		if strings.HasPrefix(line, "option ") || strings.HasPrefix(line, "list ") {
			parts := parseUCIOption(line)
			if len(parts) < 2 {
				continue
			}
			key, value := parts[0], parts[1]

			switch currentSection {
			case "defaults":
				parseDefaults(&cfg.Defaults, key, value)
			case "zone":
				if currentZone != nil {
					parseZone(currentZone, key, value, strings.HasPrefix(line, "list "))
				}
			case "forwarding":
				if currentForward != nil {
					parseForward(currentForward, key, value)
				}
			case "rule":
				if currentRule != nil {
					parseRule(currentRule, key, value)
				}
			case "redirect":
				if currentRedirect != nil {
					parseRedirect(currentRedirect, key, value)
				}
			}
		}
	}

	// Save last section
	saveSection(cfg, currentSection, currentZone, currentForward, currentRule, currentRedirect)

	return cfg, scanner.Err()
}

func saveSection(cfg *OpenWrtFirewallConfig, section string, zone *OpenWrtZone, fwd *OpenWrtForward, rule *OpenWrtRule, redir *OpenWrtRedirect) {
	switch section {
	case "zone":
		if zone != nil {
			cfg.Zones = append(cfg.Zones, *zone)
		}
	case "forwarding":
		if fwd != nil {
			cfg.Forwards = append(cfg.Forwards, *fwd)
		}
	case "rule":
		if rule != nil {
			cfg.Rules = append(cfg.Rules, *rule)
		}
	case "redirect":
		if redir != nil {
			cfg.Redirects = append(cfg.Redirects, *redir)
		}
	}
}

func parseDefaults(d *OpenWrtDefaults, key, value string) {
	switch key {
	case "input":
		d.Input = value
	case "output":
		d.Output = value
	case "forward":
		d.Forward = value
	case "syn_flood":
		d.SynFlood = value == "1"
	case "drop_invalid":
		d.DropInvalid = value == "1"
	}
}

func parseZone(z *OpenWrtZone, key, value string, isList bool) {
	switch key {
	case "name":
		z.Name = value
	case "network":
		if isList {
			z.Network = append(z.Network, value)
		} else {
			z.Network = strings.Fields(value)
		}
	case "input":
		z.Input = value
	case "output":
		z.Output = value
	case "forward":
		z.Forward = value
	case "masq":
		z.Masq = value == "1"
	case "mtu_fix":
		z.MTUFix = value == "1"
	case "conntrack":
		z.Conntrack = value == "1"
	}
}

func parseForward(f *OpenWrtForward, key, value string) {
	switch key {
	case "src":
		f.Src = value
	case "dest":
		f.Dest = value
	case "enabled":
		f.Enabled = value != "0"
	}
}

func parseRule(r *OpenWrtRule, key, value string) {
	switch key {
	case "name":
		r.Name = value
	case "src":
		r.Src = value
	case "src_ip":
		r.SrcIP = value
	case "src_mac":
		r.SrcMAC = value
	case "src_port":
		r.SrcPort = value
	case "dest":
		r.Dest = value
	case "dest_ip":
		r.DestIP = value
	case "dest_port":
		r.DestPort = value
	case "proto":
		r.Proto = value
	case "target":
		r.Target = value
	case "family":
		r.Family = value
	case "enabled":
		r.Enabled = value != "0"
	case "limit":
		r.Limit = value
	case "extra":
		r.Extra = value
	}
}

func parseRedirect(r *OpenWrtRedirect, key, value string) {
	switch key {
	case "name":
		r.Name = value
	case "src":
		r.Src = value
	case "src_ip":
		r.SrcIP = value
	case "src_dport":
		r.SrcDPort = value
	case "dest":
		r.Dest = value
	case "dest_ip":
		r.DestIP = value
	case "dest_port":
		r.DestPort = value
	case "proto":
		r.Proto = value
	case "target":
		r.Target = value
	case "enabled":
		r.Enabled = value != "0"
	case "reflection":
		r.Reflection = value == "1"
	}
}

// ToImportResult converts OpenWrt firewall config to ImportResult.
func (c *OpenWrtFirewallConfig) ToImportResult() *ImportResult {
	result := &ImportResult{}

	// Convert zones to interfaces (conceptually)
	for _, z := range c.Zones {
		for i, net := range z.Network {
			iface := ImportedInterface{
				OriginalName: net,
				Zone:         strings.ToUpper(z.Name),
				NeedsMapping: true,
				SuggestedIf:  fmt.Sprintf("eth%d", i),
			}
			result.Interfaces = append(result.Interfaces, iface)
		}
	}

	// Convert rules
	for _, r := range c.Rules {
		if !r.Enabled {
			continue
		}

		rule := ImportedFilterRule{
			Description: r.Name,
			Interface:   r.Src,
			Protocol:    r.Proto,
			DestPort:    r.DestPort,
			SourcePort:  r.SrcPort,
			Source:      r.SrcIP,
			Destination: r.DestIP,
			CanImport:   true,
		}

		switch strings.ToUpper(r.Target) {
		case "ACCEPT":
			rule.Action = "accept"
		case "DROP":
			rule.Action = "drop"
		case "REJECT":
			rule.Action = "reject"
		default:
			rule.Action = strings.ToLower(r.Target)
		}

		if r.Src != "" && r.Dest != "" {
			rule.SuggestedPolicy = fmt.Sprintf("%s_to_%s", strings.ToLower(r.Src), strings.ToLower(r.Dest))
		}

		if r.Extra != "" {
			rule.ImportNotes = append(rule.ImportNotes, "Has extra iptables options: "+r.Extra)
		}

		result.FilterRules = append(result.FilterRules, rule)
	}

	// Convert redirects to port forwards
	for _, r := range c.Redirects {
		if !r.Enabled {
			continue
		}

		pf := ImportedPortForward{
			Interface:    r.Src,
			Protocol:     r.Proto,
			ExternalPort: r.SrcDPort,
			InternalIP:   r.DestIP,
			InternalPort: r.DestPort,
			Description:  r.Name,
			CanImport:    true,
		}

		if r.Target == "SNAT" {
			pf.ImportNotes = append(pf.ImportNotes, "SNAT redirect - may need manual review")
		}

		result.PortForwards = append(result.PortForwards, pf)
	}

	// Add zone-based NAT (masquerade)
	for _, z := range c.Zones {
		if z.Masq {
			result.NATRules = append(result.NATRules, ImportedNATRule{
				Type:        "masquerade",
				Interface:   z.Name,
				Description: fmt.Sprintf("Masquerade for zone %s", z.Name),
				CanImport:   true,
			})
		}
	}

	// Generate guidance
	result.ManualSteps = append(result.ManualSteps,
		"Map OpenWrt network names to Linux interfaces",
		"Review zone policies and convert to policies",
		"Check forwarding rules between zones",
	)

	if c.Defaults.SynFlood {
		result.ManualSteps = append(result.ManualSteps,
			"SYN flood protection was enabled - configure in protection settings")
	}

	return result
}
