package learning

import (
	"fmt"
	"net"

	"grimm.is/glacic/internal/config"
)

// DNSRedirectRule represents a single DNS redirect rule for nftables.
type DNSRedirectRule struct {
	SourceInterface string
	Protocol        string // "tcp" or "udp"
	DestPort        uint16
	DNATAddr        net.IP
	DNATPort        uint16
	ExcludeIPs      []net.IP
}

// GenerateDNSInspectRules generates nftables rules for DNS inspection/redirection.
// This is called by the firewall script builder when dns.inspect blocks are configured.
func GenerateDNSInspectRules(inspects []config.DNSInspect, zoneToIfaces map[string][]string, listenAddr string, listenPort int, routerIPs []string) []string {
	if len(inspects) == 0 {
		return nil
	}

	var rules []string

	// Create the nat table if it doesn't exist
	rules = append(rules, "add table ip nat")

	// Create prerouting chain for DNAT
	rules = append(rules, "add chain ip nat prerouting { type nat hook prerouting priority dstnat; policy accept; }")

	// Create postrouting chain for masquerade
	rules = append(rules, "add chain ip nat postrouting { type nat hook postrouting priority srcnat; policy accept; }")

	// Default listen address/port
	if listenAddr == "" {
		listenAddr = "127.0.0.1"
	}
	if listenPort == 0 {
		listenPort = 53
	}

	for _, inspect := range inspects {
		if inspect.Mode == "" {
			continue
		}

		// Resolve zone to interfaces
		ifaces := zoneToIfaces[inspect.Zone]
		if len(ifaces) == 0 {
			// Try wildcard matching
			for zoneName, zoneIfaces := range zoneToIfaces {
				if matchZoneWildcard(inspect.Zone, zoneName) {
					ifaces = append(ifaces, zoneIfaces...)
				}
			}
		}

		// Generate rules for each interface
		for _, iface := range ifaces {
			if inspect.Mode == "redirect" {
				// UDP DNS redirect
				udpRule := fmt.Sprintf("add rule ip nat prerouting iifname %q udp dport 53", iface)
				if inspect.ExcludeRouter && len(routerIPs) > 0 {
					for _, ip := range routerIPs {
						udpRule += fmt.Sprintf(" ip saddr != %s", ip)
					}
				}
				udpRule += fmt.Sprintf(" dnat to %s:%d", listenAddr, listenPort)
				rules = append(rules, udpRule)

				// TCP DNS redirect
				tcpRule := fmt.Sprintf("add rule ip nat prerouting iifname %q tcp dport 53", iface)
				if inspect.ExcludeRouter && len(routerIPs) > 0 {
					for _, ip := range routerIPs {
						tcpRule += fmt.Sprintf(" ip saddr != %s", ip)
					}
				}
				tcpRule += fmt.Sprintf(" dnat to %s:%d", listenAddr, listenPort)
				rules = append(rules, tcpRule)
			} else if inspect.Mode == "passive" {
				// Passive inspection via nflog
				// Note: We use the filter table or nat table for logging?
				// Usually snooping is better in filter table, but for consistency here we add it.
				// However, if we want to snoop WITHOUT redirecting, we can use prerouting.
				rules = append(rules, fmt.Sprintf("add rule ip nat prerouting iifname %q udp dport 53 log group 100 prefix \"DNS_PASSIVE:\"", iface))
				rules = append(rules, fmt.Sprintf("add rule ip nat prerouting iifname %q tcp dport 53 log group 100 prefix \"DNS_PASSIVE:\"", iface))
			}
		}
	}

	// Add masquerade for redirected DNS traffic
	rules = append(rules, fmt.Sprintf("add rule ip nat postrouting ip daddr %s udp dport %d masquerade", listenAddr, listenPort))
	rules = append(rules, fmt.Sprintf("add rule ip nat postrouting ip daddr %s tcp dport %d masquerade", listenAddr, listenPort))

	return rules
}

// matchZoneWildcard checks if a zone pattern matches a zone name.
// Supports trailing wildcard (e.g., "internal-*" matches "internal-lan", "internal-guest").
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

// --- Legacy types for backwards compatibility ---
// These will be removed once all consumers migrate to config.DNSInspect

// DNSRedirectConfig configures transparent DNS redirection
// Deprecated: Use config.DNSInspect instead
type DNSRedirectConfig struct {
	Enabled         bool     `hcl:"enabled,optional" json:"enabled"`
	ListenAddr      string   `hcl:"listen_addr,optional" json:"listen_addr"`
	ListenPort      int      `hcl:"listen_port,optional" json:"listen_port"`
	SourceIfaces    []string `hcl:"source_interfaces,optional" json:"source_interfaces"`
	ExcludeRouterIP bool     `hcl:"exclude_router,optional" json:"exclude_router"`
}

// DefaultDNSRedirectConfig returns sensible defaults
// Deprecated: Use config.DNSInspect instead
func DefaultDNSRedirectConfig() *DNSRedirectConfig {
	return &DNSRedirectConfig{
		Enabled:         true,
		ListenAddr:      "127.0.0.1",
		ListenPort:      53,
		SourceIfaces:    []string{"br-lan"},
		ExcludeRouterIP: true,
	}
}

// GenerateNftablesRules generates nftables rules for transparent DNS redirection
// Deprecated: Use GenerateDNSInspectRules instead
func (c *DNSRedirectConfig) GenerateNftablesRules(routerIPs []string) []string {
	if !c.Enabled {
		return nil
	}

	var rules []string

	rules = append(rules, "add table ip nat")
	rules = append(rules, "add chain ip nat prerouting { type nat hook prerouting priority dstnat; policy accept; }")
	rules = append(rules, "add chain ip nat postrouting { type nat hook postrouting priority srcnat; policy accept; }")

	for _, iface := range c.SourceIfaces {
		udpRule := fmt.Sprintf("add rule ip nat prerouting iifname %q udp dport 53", iface)
		if c.ExcludeRouterIP && len(routerIPs) > 0 {
			for _, ip := range routerIPs {
				udpRule += fmt.Sprintf(" ip saddr != %s", ip)
			}
		}
		udpRule += fmt.Sprintf(" dnat to %s:%d", c.ListenAddr, c.ListenPort)
		rules = append(rules, udpRule)

		tcpRule := fmt.Sprintf("add rule ip nat prerouting iifname %q tcp dport 53", iface)
		if c.ExcludeRouterIP && len(routerIPs) > 0 {
			for _, ip := range routerIPs {
				tcpRule += fmt.Sprintf(" ip saddr != %s", ip)
			}
		}
		tcpRule += fmt.Sprintf(" dnat to %s:%d", c.ListenAddr, c.ListenPort)
		rules = append(rules, tcpRule)
	}

	rules = append(rules, fmt.Sprintf("add rule ip nat postrouting ip daddr %s udp dport %d masquerade", c.ListenAddr, c.ListenPort))
	rules = append(rules, fmt.Sprintf("add rule ip nat postrouting ip daddr %s tcp dport %d masquerade", c.ListenAddr, c.ListenPort))

	return rules
}

// Validate checks the configuration for errors
// Deprecated: Validation now done at config layer
func (c *DNSRedirectConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.ListenAddr != "" {
		ip := net.ParseIP(c.ListenAddr)
		if ip == nil {
			return fmt.Errorf("invalid listen address: %s", c.ListenAddr)
		}
	}

	if c.ListenPort < 0 || c.ListenPort > 65535 {
		return fmt.Errorf("invalid listen port: %d", c.ListenPort)
	}

	if len(c.SourceIfaces) == 0 {
		return fmt.Errorf("no source interfaces specified for DNS redirection")
	}

	return nil
}
