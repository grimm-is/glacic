package imports

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// MikroTikConfig holds parsed MikroTik RouterOS export.
type MikroTikConfig struct {
	Interfaces     []MikroTikInterface
	Addresses      []MikroTikAddress
	FirewallFilter []MikroTikFirewallRule
	FirewallNAT    []MikroTikNATRule
	DHCPServers    []MikroTikDHCPServer
	DHCPLeases     []MikroTikDHCPLease
	DNSStatic      []MikroTikDNSEntry
	Warnings       []string
}

// MikroTikInterface represents an interface.
type MikroTikInterface struct {
	Name     string
	Type     string // ether, bridge, vlan, etc.
	Disabled bool
	Comment  string
	MTU      int
	MAC      string
}

// MikroTikAddress represents an IP address assignment.
type MikroTikAddress struct {
	Address   string // CIDR format
	Interface string
	Network   string
	Disabled  bool
	Comment   string
}

// MikroTikFirewallRule represents a firewall filter rule.
type MikroTikFirewallRule struct {
	Chain           string
	Action          string // accept, drop, reject, jump, etc.
	Protocol        string
	SrcAddress      string
	DstAddress      string
	SrcPort         string
	DstPort         string
	InInterface     string
	OutInterface    string
	ConnectionState string
	Comment         string
	Disabled        bool
	Log             bool
	LogPrefix       string
}

// MikroTikNATRule represents a NAT rule.
type MikroTikNATRule struct {
	Chain        string // srcnat, dstnat
	Action       string // masquerade, src-nat, dst-nat
	Protocol     string
	SrcAddress   string
	DstAddress   string
	SrcPort      string
	DstPort      string
	ToAddresses  string
	ToPorts      string
	OutInterface string
	InInterface  string
	Comment      string
	Disabled     bool
}

// MikroTikDHCPServer represents a DHCP server config.
type MikroTikDHCPServer struct {
	Name        string
	Interface   string
	AddressPool string
	LeaseTime   string
	Disabled    bool
}

// MikroTikDHCPLease represents a DHCP lease/reservation.
type MikroTikDHCPLease struct {
	Address    string
	MACAddress string
	Server     string
	Hostname   string
	Comment    string
	Disabled   bool
}

// MikroTikDNSEntry represents a static DNS entry.
type MikroTikDNSEntry struct {
	Name     string
	Address  string
	TTL      string
	Disabled bool
	Comment  string
}

// ParseMikroTikExport parses a MikroTik RouterOS export file.
func ParseMikroTikExport(path string) (*MikroTikConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open MikroTik export: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	return parseMikroTikScanner(scanner)
}

// ParseMikroTikExportStr parses a MikroTik RouterOS export string.
func ParseMikroTikExportStr(content string) (*MikroTikConfig, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	return parseMikroTikScanner(scanner)
}

func parseMikroTikScanner(scanner *bufio.Scanner) (*MikroTikConfig, error) {
	cfg := &MikroTikConfig{}
	var currentSection string
	var lineBuffer string

	for scanner.Scan() {
		line := scanner.Text()

		// Handle line continuation (backslash at end)
		if strings.HasSuffix(strings.TrimSpace(line), "\\") {
			lineBuffer += strings.TrimSuffix(strings.TrimSpace(line), "\\") + " "
			continue
		}
		if lineBuffer != "" {
			line = lineBuffer + line
			lineBuffer = ""
		}

		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Section headers
		if strings.HasPrefix(line, "/") {
			currentSection = line
			continue
		}

		// Parse based on section
		switch {
		case strings.HasPrefix(currentSection, "/interface"):
			if iface := parseMikroTikInterface(line); iface != nil {
				cfg.Interfaces = append(cfg.Interfaces, *iface)
			}
		case strings.HasPrefix(currentSection, "/ip address"):
			if addr := parseMikroTikAddress(line); addr != nil {
				cfg.Addresses = append(cfg.Addresses, *addr)
			}
		case strings.HasPrefix(currentSection, "/ip firewall filter"):
			if rule := parseMikroTikFirewallRule(line); rule != nil {
				cfg.FirewallFilter = append(cfg.FirewallFilter, *rule)
			}
		case strings.HasPrefix(currentSection, "/ip firewall nat"):
			if rule := parseMikroTikNATRule(line); rule != nil {
				cfg.FirewallNAT = append(cfg.FirewallNAT, *rule)
			}
		case strings.HasPrefix(currentSection, "/ip dhcp-server lease"):
			if lease := parseMikroTikDHCPLease(line); lease != nil {
				cfg.DHCPLeases = append(cfg.DHCPLeases, *lease)
			}
		case strings.HasPrefix(currentSection, "/ip dhcp-server"):
			if !strings.Contains(currentSection, "lease") && !strings.Contains(currentSection, "network") {
				if srv := parseMikroTikDHCPServer(line); srv != nil {
					cfg.DHCPServers = append(cfg.DHCPServers, *srv)
				}
			}
		case strings.HasPrefix(currentSection, "/ip dns static"):
			if entry := parseMikroTikDNSEntry(line); entry != nil {
				cfg.DNSStatic = append(cfg.DNSStatic, *entry)
			}
		}
	}

	return cfg, scanner.Err()
}

// parseMikroTikProps parses key=value properties from a line.
func parseMikroTikProps(line string) map[string]string {
	props := make(map[string]string)

	// Remove "add" or "set" prefix
	line = strings.TrimPrefix(line, "add ")
	line = strings.TrimPrefix(line, "set ")

	// Parse key=value pairs
	// Handle quoted values
	re := regexp.MustCompile(`([\w-]+)=(?:"([^"]+)"|(\S+))`)
	matches := re.FindAllStringSubmatch(line, -1)
	for _, m := range matches {
		key := m[1]
		value := m[2]
		if value == "" {
			value = m[3]
		}
		props[key] = value
	}

	return props
}

func parseMikroTikInterface(line string) *MikroTikInterface {
	if !strings.HasPrefix(line, "add ") && !strings.HasPrefix(line, "set ") {
		return nil
	}

	props := parseMikroTikProps(line)
	if len(props) == 0 {
		return nil
	}

	iface := &MikroTikInterface{
		Name:     props["name"],
		Type:     props["type"],
		Comment:  props["comment"],
		Disabled: props["disabled"] == "yes",
	}

	if mtu, ok := props["mtu"]; ok {
		fmt.Sscanf(mtu, "%d", &iface.MTU)
	}

	return iface
}

func parseMikroTikAddress(line string) *MikroTikAddress {
	if !strings.HasPrefix(line, "add ") {
		return nil
	}

	props := parseMikroTikProps(line)
	return &MikroTikAddress{
		Address:   props["address"],
		Interface: props["interface"],
		Network:   props["network"],
		Disabled:  props["disabled"] == "yes",
		Comment:   props["comment"],
	}
}

func parseMikroTikFirewallRule(line string) *MikroTikFirewallRule {
	if !strings.HasPrefix(line, "add ") {
		return nil
	}

	props := parseMikroTikProps(line)
	return &MikroTikFirewallRule{
		Chain:           props["chain"],
		Action:          props["action"],
		Protocol:        props["protocol"],
		SrcAddress:      props["src-address"],
		DstAddress:      props["dst-address"],
		SrcPort:         props["src-port"],
		DstPort:         props["dst-port"],
		InInterface:     props["in-interface"],
		OutInterface:    props["out-interface"],
		ConnectionState: props["connection-state"],
		Comment:         props["comment"],
		Disabled:        props["disabled"] == "yes",
		Log:             props["log"] == "yes",
		LogPrefix:       props["log-prefix"],
	}
}

func parseMikroTikNATRule(line string) *MikroTikNATRule {
	if !strings.HasPrefix(line, "add ") {
		return nil
	}

	props := parseMikroTikProps(line)
	return &MikroTikNATRule{
		Chain:        props["chain"],
		Action:       props["action"],
		Protocol:     props["protocol"],
		SrcAddress:   props["src-address"],
		DstAddress:   props["dst-address"],
		SrcPort:      props["src-port"],
		DstPort:      props["dst-port"],
		ToAddresses:  props["to-addresses"],
		ToPorts:      props["to-ports"],
		OutInterface: props["out-interface"],
		InInterface:  props["in-interface"],
		Comment:      props["comment"],
		Disabled:     props["disabled"] == "yes",
	}
}

func parseMikroTikDHCPServer(line string) *MikroTikDHCPServer {
	if !strings.HasPrefix(line, "add ") {
		return nil
	}

	props := parseMikroTikProps(line)
	return &MikroTikDHCPServer{
		Name:        props["name"],
		Interface:   props["interface"],
		AddressPool: props["address-pool"],
		LeaseTime:   props["lease-time"],
		Disabled:    props["disabled"] == "yes",
	}
}

func parseMikroTikDHCPLease(line string) *MikroTikDHCPLease {
	if !strings.HasPrefix(line, "add ") {
		return nil
	}

	props := parseMikroTikProps(line)
	return &MikroTikDHCPLease{
		Address:    props["address"],
		MACAddress: props["mac-address"],
		Server:     props["server"],
		Hostname:   props["host-name"],
		Comment:    props["comment"],
		Disabled:   props["disabled"] == "yes",
	}
}

func parseMikroTikDNSEntry(line string) *MikroTikDNSEntry {
	if !strings.HasPrefix(line, "add ") {
		return nil
	}

	props := parseMikroTikProps(line)
	return &MikroTikDNSEntry{
		Name:     props["name"],
		Address:  props["address"],
		TTL:      props["ttl"],
		Disabled: props["disabled"] == "yes",
		Comment:  props["comment"],
	}
}

// ToImportResult converts MikroTik config to ImportResult.
func (c *MikroTikConfig) ToImportResult() *ImportResult {
	result := &ImportResult{}

	// Convert interfaces
	for _, iface := range c.Interfaces {
		imp := ImportedInterface{
			OriginalName: iface.Name,
			Description:  iface.Comment,
			NeedsMapping: true,
		}
		// Suggest Linux interface based on type
		if strings.HasPrefix(iface.Name, "ether") {
			imp.SuggestedIf = strings.Replace(iface.Name, "ether", "eth", 1)
		} else if strings.HasPrefix(iface.Name, "bridge") {
			imp.SuggestedIf = strings.Replace(iface.Name, "bridge", "br", 1)
		} else {
			imp.SuggestedIf = iface.Name
		}
		result.Interfaces = append(result.Interfaces, imp)
	}

	// Convert addresses to interface IPs
	for _, addr := range c.Addresses {
		for i := range result.Interfaces {
			if result.Interfaces[i].OriginalName == addr.Interface {
				result.Interfaces[i].IPAddress = addr.Address
				break
			}
		}
	}

	// Convert firewall rules
	for _, rule := range c.FirewallFilter {
		if rule.Disabled {
			continue
		}

		imp := ImportedFilterRule{
			Description: rule.Comment,
			Protocol:    rule.Protocol,
			DestPort:    rule.DstPort,
			SourcePort:  rule.SrcPort,
			Source:      rule.SrcAddress,
			Destination: rule.DstAddress,
			Interface:   rule.InInterface,
			Log:         rule.Log,
			CanImport:   true,
		}

		switch rule.Action {
		case "accept":
			imp.Action = "accept"
		case "drop":
			imp.Action = "drop"
		case "reject":
			imp.Action = "reject"
		case "jump":
			imp.ImportNotes = append(imp.ImportNotes, "Jump action - chain reference")
		case "passthrough":
			imp.ImportNotes = append(imp.ImportNotes, "Passthrough - logging/counting rule")
		default:
			imp.Action = rule.Action
		}

		// Map chain to policy
		switch rule.Chain {
		case "input":
			imp.SuggestedPolicy = "input_policy"
		case "output":
			imp.SuggestedPolicy = "output_policy"
		case "forward":
			imp.SuggestedPolicy = "forward_policy"
		default:
			imp.SuggestedPolicy = rule.Chain + "_policy"
		}

		if rule.ConnectionState != "" {
			imp.ImportNotes = append(imp.ImportNotes, "Connection state: "+rule.ConnectionState)
		}

		result.FilterRules = append(result.FilterRules, imp)
	}

	// Convert NAT rules
	for _, rule := range c.FirewallNAT {
		if rule.Disabled {
			continue
		}

		if rule.Chain == "srcnat" {
			natRule := ImportedNATRule{
				Interface:   rule.OutInterface,
				Source:      rule.SrcAddress,
				Description: rule.Comment,
				CanImport:   true,
			}
			switch rule.Action {
			case "masquerade":
				natRule.Type = "masquerade"
			case "src-nat":
				natRule.Type = "snat"
				natRule.Target = rule.ToAddresses
			}
			result.NATRules = append(result.NATRules, natRule)
		} else if rule.Chain == "dstnat" {
			pf := ImportedPortForward{
				Interface:    rule.InInterface,
				Protocol:     rule.Protocol,
				ExternalPort: rule.DstPort,
				InternalIP:   rule.ToAddresses,
				InternalPort: rule.ToPorts,
				Description:  rule.Comment,
				CanImport:    true,
			}
			result.PortForwards = append(result.PortForwards, pf)
		}
	}

	// Convert DHCP leases to reservations
	for _, lease := range c.DHCPLeases {
		if lease.Disabled {
			continue
		}
		// These would go into DHCP scopes
		result.ManualSteps = append(result.ManualSteps,
			fmt.Sprintf("DHCP reservation: %s -> %s (%s)", lease.MACAddress, lease.Address, lease.Hostname))
	}

	// Convert DNS entries
	for _, entry := range c.DNSStatic {
		if entry.Disabled {
			continue
		}
		result.StaticHosts = append(result.StaticHosts, ImportedStaticHost{
			Hostname: entry.Name,
			IP:       entry.Address,
		})
	}

	// Add guidance
	result.ManualSteps = append(result.ManualSteps,
		"Map MikroTik interface names (ether1, bridge1) to Linux names",
		"Review chain structure - MikroTik uses input/forward/output chains",
		"Check for address lists - may need to convert to IPSets",
	)

	result.Warnings = append(result.Warnings,
		"MikroTik uses different interface naming conventions",
		"Some advanced features (mangle, queues) not imported",
	)

	return result
}
