package imports

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"grimm.is/glacic/internal/config"
)

// OpenWrtConfig holds parsed OpenWrt UCI configuration.
type OpenWrtConfig struct {
	// DHCP configuration
	DHCPPools    []OpenWrtDHCPPool
	StaticLeases []OpenWrtStaticLease

	// DNS configuration
	DNSServers   []string
	LocalDomains []string
	Hosts        []OpenWrtHost

	// Errors encountered during parsing
	Warnings []string
}

// OpenWrtDHCPPool represents an OpenWrt dhcp section.
type OpenWrtDHCPPool struct {
	Name      string // Section name (e.g., "lan")
	Interface string
	Start     int
	Limit     int
	LeaseTime string
	Ignore    bool
}

// OpenWrtStaticLease represents an OpenWrt host section (static lease).
type OpenWrtStaticLease struct {
	IP        net.IP
	MAC       string
	Name      string
	DNS       bool // Register in DNS
	LeaseTime string
}

// OpenWrtHost represents a static DNS host entry.
type OpenWrtHost struct {
	IP   net.IP
	Name string
}

// ParseOpenWrtDHCP parses an OpenWrt /etc/config/dhcp file.
// UCI format: config <type> '<name>' followed by option/list lines
func ParseOpenWrtDHCP(path string) (*OpenWrtConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open OpenWrt config: %w", err)
	}
	defer file.Close()

	cfg := &OpenWrtConfig{}
	scanner := bufio.NewScanner(file)

	var currentSection string
	var currentName string
	var currentPool *OpenWrtDHCPPool
	var currentLease *OpenWrtStaticLease

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse section headers: config <type> '<name>' or config <type> <name>
		if strings.HasPrefix(line, "config ") {
			// Save previous section
			if currentPool != nil {
				cfg.DHCPPools = append(cfg.DHCPPools, *currentPool)
				currentPool = nil
			}
			if currentLease != nil {
				cfg.StaticLeases = append(cfg.StaticLeases, *currentLease)
				currentLease = nil
			}

			parts := strings.Fields(line)
			if len(parts) >= 2 {
				currentSection = parts[1]
				if len(parts) >= 3 {
					currentName = strings.Trim(parts[2], "'\"")
				} else {
					currentName = ""
				}

				switch currentSection {
				case "dhcp":
					currentPool = &OpenWrtDHCPPool{Name: currentName}
				case "host":
					currentLease = &OpenWrtStaticLease{}
				}
			}
			continue
		}

		// Parse options: option <key> '<value>' or option <key> <value>
		if strings.HasPrefix(line, "option ") {
			parts := parseUCIOption(line)
			if len(parts) < 2 {
				continue
			}
			key := parts[0]
			value := parts[1]

			switch currentSection {
			case "dhcp":
				if currentPool != nil {
					switch key {
					case "interface":
						currentPool.Interface = value
					case "start":
						fmt.Sscanf(value, "%d", &currentPool.Start)
					case "limit":
						fmt.Sscanf(value, "%d", &currentPool.Limit)
					case "leasetime":
						currentPool.LeaseTime = value
					case "ignore":
						currentPool.Ignore = value == "1"
					}
				}
			case "host":
				if currentLease != nil {
					switch key {
					case "ip":
						currentLease.IP = net.ParseIP(value)
					case "mac":
						currentLease.MAC = normalizeMACAddress(value)
					case "name":
						currentLease.Name = value
					case "dns":
						currentLease.DNS = value == "1"
					case "leasetime":
						currentLease.LeaseTime = value
					}
				}
			case "dnsmasq":
				switch key {
				case "domain":
					cfg.LocalDomains = append(cfg.LocalDomains, value)
				}
			}
			continue
		}

		// Parse lists: list <key> '<value>'
		if strings.HasPrefix(line, "list ") {
			parts := parseUCIOption(line)
			if len(parts) < 2 {
				continue
			}
			key := parts[0]
			value := parts[1]

			switch currentSection {
			case "dnsmasq":
				switch key {
				case "server":
					cfg.DNSServers = append(cfg.DNSServers, value)
				case "address":
					// Format: /domain/ip
					addrParts := strings.Split(strings.Trim(value, "/"), "/")
					if len(addrParts) >= 2 {
						cfg.Hosts = append(cfg.Hosts, OpenWrtHost{
							Name: addrParts[0],
							IP:   net.ParseIP(addrParts[1]),
						})
					}
				}
			}
		}
	}

	// Save last section
	if currentPool != nil {
		cfg.DHCPPools = append(cfg.DHCPPools, *currentPool)
	}
	if currentLease != nil {
		cfg.StaticLeases = append(cfg.StaticLeases, *currentLease)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading OpenWrt config: %w", err)
	}

	return cfg, nil
}

// parseUCIOption parses "option key 'value'" or "list key 'value'" format.
func parseUCIOption(line string) []string {
	// Remove "option " or "list " prefix
	line = strings.TrimPrefix(line, "option ")
	line = strings.TrimPrefix(line, "list ")

	// Split on first space
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		return parts
	}

	// Clean up value (remove quotes)
	parts[1] = strings.Trim(parts[1], "'\"")
	return parts
}

// ParseOpenWrtEthers parses an /etc/ethers file (MAC to hostname mapping).
// Format: <mac> <hostname>
func ParseOpenWrtEthers(path string) ([]OpenWrtStaticLease, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open ethers file: %w", err)
	}
	defer file.Close()

	var leases []OpenWrtStaticLease
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 2 {
			leases = append(leases, OpenWrtStaticLease{
				MAC:  normalizeMACAddress(fields[0]),
				Name: fields[1],
			})
		}
	}

	return leases, scanner.Err()
}

// ParseHostsFile parses a standard /etc/hosts file.
// Format: <ip> <hostname> [<alias>...]
func ParseHostsFile(path string) ([]config.DNSHostEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open hosts file: %w", err)
	}
	defer file.Close()

	var hosts []config.DNSHostEntry
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Remove comments
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		ip := net.ParseIP(fields[0])
		if ip == nil {
			continue
		}

		// Skip localhost entries
		if ip.IsLoopback() {
			continue
		}

		hosts = append(hosts, config.DNSHostEntry{
			IP:        fields[0],
			Hostnames: fields[1:],
		})
	}

	return hosts, scanner.Err()
}

// ToConfig converts parsed OpenWrt config to native config structures.
func (c *OpenWrtConfig) ToConfig(networkBase string) (*config.DHCPServer, *config.DNSServer) {
	dhcp := &config.DHCPServer{
		Enabled: len(c.DHCPPools) > 0,
	}

	// Convert DHCP pools to scopes
	// Note: OpenWrt uses start offset + limit, we need actual IPs
	// networkBase should be like "192.168.1" for a /24 network
	for _, pool := range c.DHCPPools {
		if pool.Ignore {
			continue
		}

		scope := config.DHCPScope{
			Name:      pool.Name,
			Interface: pool.Interface,
		}

		if networkBase != "" {
			scope.RangeStart = fmt.Sprintf("%s.%d", networkBase, pool.Start)
			scope.RangeEnd = fmt.Sprintf("%s.%d", networkBase, pool.Start+pool.Limit-1)
		}

		if pool.LeaseTime != "" {
			scope.LeaseTime = pool.LeaseTime
		}

		dhcp.Scopes = append(dhcp.Scopes, scope)
	}

	// Convert static leases to reservations
	if len(dhcp.Scopes) > 0 {
		for _, lease := range c.StaticLeases {
			if lease.MAC == "" || lease.IP == nil {
				continue
			}
			res := config.DHCPReservation{
				MAC:         lease.MAC,
				IP:          lease.IP.String(),
				Hostname:    lease.Name,
				RegisterDNS: lease.DNS,
			}
			dhcp.Scopes[0].Reservations = append(dhcp.Scopes[0].Reservations, res)
		}
	}

	// Convert DNS config
	dns := &config.DNSServer{
		Enabled:    true,
		Forwarders: c.DNSServers,
	}

	if len(c.LocalDomains) > 0 {
		dns.LocalDomain = c.LocalDomains[0]
	}

	// Convert static hosts
	for _, host := range c.Hosts {
		if host.IP != nil {
			dns.Hosts = append(dns.Hosts, config.DNSHostEntry{
				IP:        host.IP.String(),
				Hostnames: []string{host.Name},
			})
		}
	}

	return dhcp, dns
}

// ParseOpenWrtLeases parses OpenWrt's /var/dhcp.leases or /tmp/dhcp.leases file.
// Format is same as dnsmasq: <expiry> <mac> <ip> <hostname> [<client-id>]
func ParseOpenWrtLeases(path string) ([]Lease, error) {
	return ParseDnsmasqLeases(path)
}

// ConvertOpenWrtLeasetime converts OpenWrt leasetime format to Go duration.
// Formats: "12h", "30m", "1d", "infinite"
func ConvertOpenWrtLeasetime(s string) time.Duration {
	dur, err := parseDnsmasqDuration(s)
	if err != nil {
		return 12 * time.Hour // Default
	}
	return dur
}
