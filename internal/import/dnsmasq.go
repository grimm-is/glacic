// Package imports provides parsers for importing configuration from external services.
package imports

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"grimm.is/glacic/internal/config"
)

// DnsmasqConfig holds parsed dnsmasq configuration.
type DnsmasqConfig struct {
	// DHCP configuration
	DHCPRanges    []DHCPRange
	DHCPHosts     []DHCPHost // Static reservations
	DHCPOptions   map[string]string
	Domain        string
	DHCPLeaseTime time.Duration

	// DNS configuration
	Servers      []string // Upstream DNS servers
	LocalDomains []string // Domains to resolve locally
	Addresses    []DNSAddress
	CnameRecords []CnameRecord
	HostsFiles   []string
	NoResolv     bool // Don't read /etc/resolv.conf
	NoHosts      bool // Don't read /etc/hosts

	// Errors encountered during parsing
	Warnings []string
}

// DHCPRange represents a dnsmasq dhcp-range directive.
type DHCPRange struct {
	Interface  string
	Start      net.IP
	End        net.IP
	Netmask    net.IPMask
	LeaseTime  time.Duration
	Tag        string
	IsStatic   bool // dhcp-range=...,static
	IsDisabled bool // dhcp-range=...,ignore
}

// DHCPHost represents a dnsmasq dhcp-host directive (static reservation).
type DHCPHost struct {
	MAC         string
	IP          net.IP
	Hostname    string
	LeaseTime   time.Duration
	Tags        []string
	IgnoreMAC   bool // id:* syntax
	SetHostname bool // set:hostname
}

// DNSAddress represents a dnsmasq address directive.
type DNSAddress struct {
	Domain string
	IP     net.IP
}

// CnameRecord represents a dnsmasq cname directive.
type CnameRecord struct {
	Alias  string
	Target string
}

// ParseDnsmasqConfig parses a dnsmasq configuration file.
func ParseDnsmasqConfig(path string) (*DnsmasqConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open dnsmasq config: %w", err)
	}
	defer file.Close()

	cfg := &DnsmasqConfig{
		DHCPOptions: make(map[string]string),
	}

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key=value or key (boolean)
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := ""
		if len(parts) > 1 {
			value = strings.TrimSpace(parts[1])
		}

		if err := cfg.parseDirective(key, value); err != nil {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("line %d: %v", lineNum, err))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading dnsmasq config: %w", err)
	}

	return cfg, nil
}

func (c *DnsmasqConfig) parseDirective(key, value string) error {
	switch key {
	case "dhcp-range":
		return c.parseDHCPRange(value)
	case "dhcp-host":
		return c.parseDHCPHost(value)
	case "dhcp-option":
		return c.parseDHCPOption(value)
	case "domain":
		c.Domain = value
	case "dhcp-leasetime":
		dur, err := parseDnsmasqDuration(value)
		if err != nil {
			return err
		}
		c.DHCPLeaseTime = dur
	case "server":
		c.Servers = append(c.Servers, value)
	case "local":
		// local=/domain/ means resolve locally
		domain := strings.Trim(value, "/")
		c.LocalDomains = append(c.LocalDomains, domain)
	case "address":
		return c.parseAddress(value)
	case "cname":
		return c.parseCname(value)
	case "addn-hosts":
		c.HostsFiles = append(c.HostsFiles, value)
	case "no-resolv":
		c.NoResolv = true
	case "no-hosts":
		c.NoHosts = true
	default:
		// Ignore unknown directives
	}
	return nil
}

func (c *DnsmasqConfig) parseDHCPRange(value string) error {
	// Format: [tag:<tag>,][set:<tag>,]<start>,<end>[,<netmask>][,<lease-time>]
	// Or: <interface>,<start>,<end>,...
	parts := strings.Split(value, ",")
	if len(parts) < 2 {
		return fmt.Errorf("invalid dhcp-range: %s", value)
	}

	r := DHCPRange{}
	idx := 0

	// Check for interface name (starts with letter, not a tag)
	if len(parts[0]) > 0 && !strings.HasPrefix(parts[0], "tag:") && !strings.HasPrefix(parts[0], "set:") {
		// Could be interface or IP - check if it's an IP
		if net.ParseIP(parts[0]) == nil {
			r.Interface = parts[0]
			idx++
		}
	}

	// Parse start IP
	if idx >= len(parts) {
		return fmt.Errorf("missing start IP in dhcp-range")
	}
	r.Start = net.ParseIP(parts[idx])
	if r.Start == nil {
		return fmt.Errorf("invalid start IP: %s", parts[idx])
	}
	idx++

	// Parse end IP
	if idx >= len(parts) {
		return fmt.Errorf("missing end IP in dhcp-range")
	}
	r.End = net.ParseIP(parts[idx])
	if r.End == nil {
		return fmt.Errorf("invalid end IP: %s", parts[idx])
	}
	idx++

	// Parse optional netmask and lease time
	for ; idx < len(parts); idx++ {
		part := strings.TrimSpace(parts[idx])
		if part == "static" {
			r.IsStatic = true
		} else if part == "ignore" {
			r.IsDisabled = true
		} else if ip := net.ParseIP(part); ip != nil {
			// It's a netmask
			r.Netmask = net.IPMask(ip.To4())
		} else if dur, err := parseDnsmasqDuration(part); err == nil {
			r.LeaseTime = dur
		}
	}

	c.DHCPRanges = append(c.DHCPRanges, r)
	return nil
}

func (c *DnsmasqConfig) parseDHCPHost(value string) error {
	// Format: <mac>,<ip>[,<hostname>][,<lease-time>]
	// Or: <mac>,set:<tag>,<ip>
	// Or: <hostname>,<ip>
	parts := strings.Split(value, ",")
	if len(parts) < 2 {
		return fmt.Errorf("invalid dhcp-host: %s", value)
	}

	h := DHCPHost{}

	// First part is usually MAC or hostname
	first := parts[0]
	if isMACAddress(first) {
		h.MAC = normalizeMACAddress(first)
	} else if first == "id:*" {
		h.IgnoreMAC = true
	} else {
		// Assume it's a hostname
		h.Hostname = first
	}

	// Parse remaining parts
	for i := 1; i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])
		if strings.HasPrefix(part, "set:") {
			h.Tags = append(h.Tags, strings.TrimPrefix(part, "set:"))
		} else if ip := net.ParseIP(part); ip != nil {
			h.IP = ip
		} else if dur, err := parseDnsmasqDuration(part); err == nil {
			h.LeaseTime = dur
		} else if part != "" && h.Hostname == "" {
			h.Hostname = part
		}
	}

	c.DHCPHosts = append(c.DHCPHosts, h)
	return nil
}

func (c *DnsmasqConfig) parseDHCPOption(value string) error {
	// Format: <option-number>,<value> or <option-name>,<value>
	parts := strings.SplitN(value, ",", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid dhcp-option: %s", value)
	}
	c.DHCPOptions[parts[0]] = parts[1]
	return nil
}

func (c *DnsmasqConfig) parseAddress(value string) error {
	// Format: /<domain>/<ip> or /<domain>/
	parts := strings.Split(strings.Trim(value, "/"), "/")
	if len(parts) < 1 {
		return fmt.Errorf("invalid address: %s", value)
	}

	addr := DNSAddress{Domain: parts[0]}
	if len(parts) > 1 && parts[1] != "" {
		addr.IP = net.ParseIP(parts[1])
	}
	c.Addresses = append(c.Addresses, addr)
	return nil
}

func (c *DnsmasqConfig) parseCname(value string) error {
	// Format: <alias>,<target>
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid cname: %s", value)
	}
	c.CnameRecords = append(c.CnameRecords, CnameRecord{
		Alias:  strings.TrimSpace(parts[0]),
		Target: strings.TrimSpace(parts[1]),
	})
	return nil
}

// ToConfig converts parsed dnsmasq config to native config structures.
func (c *DnsmasqConfig) ToConfig() (*config.DHCPServer, *config.DNSServer) {
	dhcp := &config.DHCPServer{
		Enabled: len(c.DHCPRanges) > 0,
	}

	// Convert DHCP ranges to scopes
	for i, r := range c.DHCPRanges {
		if r.IsDisabled {
			continue
		}

		scope := config.DHCPScope{
			Name:       fmt.Sprintf("imported_%d", i+1),
			Interface:  r.Interface,
			RangeStart: r.Start.String(),
			RangeEnd:   r.End.String(),
		}

		if r.LeaseTime > 0 {
			scope.LeaseTime = r.LeaseTime.String()
		} else if c.DHCPLeaseTime > 0 {
			scope.LeaseTime = c.DHCPLeaseTime.String()
		}

		if c.Domain != "" {
			scope.Domain = c.Domain
		}

		dhcp.Scopes = append(dhcp.Scopes, scope)
	}

	// Convert static hosts to reservations
	if len(dhcp.Scopes) > 0 {
		for _, h := range c.DHCPHosts {
			if h.MAC == "" || h.IP == nil {
				continue
			}
			res := config.DHCPReservation{
				MAC:      h.MAC,
				IP:       h.IP.String(),
				Hostname: h.Hostname,
			}
			// Add to first scope (could be smarter about matching)
			dhcp.Scopes[0].Reservations = append(dhcp.Scopes[0].Reservations, res)
		}
	}

	// Convert DNS config
	dns := &config.DNSServer{
		Enabled:    true,
		Forwarders: c.Servers,
	}

	if c.Domain != "" {
		dns.LocalDomain = c.Domain
	}

	// Convert static addresses to hosts
	for _, addr := range c.Addresses {
		if addr.IP != nil {
			dns.Hosts = append(dns.Hosts, config.DNSHostEntry{
				IP:        addr.IP.String(),
				Hostnames: []string{addr.Domain},
			})
		}
	}

	return dhcp, dns
}

// parseDnsmasqDuration parses dnsmasq duration format (e.g., "12h", "1d", "infinite").
func parseDnsmasqDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))

	if s == "infinite" {
		return 0, nil // 0 means infinite in our config
	}

	// Try standard Go duration first
	if dur, err := time.ParseDuration(s); err == nil {
		return dur, nil
	}

	// Parse dnsmasq format: number followed by s/m/h/d/w
	re := regexp.MustCompile(`^(\d+)([smhdw]?)$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}

	num, _ := strconv.Atoi(matches[1])
	unit := matches[2]
	if unit == "" {
		unit = "s" // Default to seconds
	}

	switch unit {
	case "s":
		return time.Duration(num) * time.Second, nil
	case "m":
		return time.Duration(num) * time.Minute, nil
	case "h":
		return time.Duration(num) * time.Hour, nil
	case "d":
		return time.Duration(num) * 24 * time.Hour, nil
	case "w":
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration unit: %s", unit)
	}
}

func isMACAddress(s string) bool {
	// Check for common MAC formats: XX:XX:XX:XX:XX:XX or XX-XX-XX-XX-XX-XX
	s = strings.ToLower(s)
	macRegex := regexp.MustCompile(`^([0-9a-f]{2}[:-]){5}[0-9a-f]{2}$`)
	return macRegex.MatchString(s)
}

func normalizeMACAddress(s string) string {
	// Convert to lowercase with colons
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", ":")
	return s
}
