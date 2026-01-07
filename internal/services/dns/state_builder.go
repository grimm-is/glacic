package dns

import (
	"log"
	"net"
	"strings"

	"github.com/miekg/dns"
	"grimm.is/glacic/internal/config"
)

type serverState struct {
	listenAddrs    []string
	records        map[string]config.DNSRecord
	blockedDomains map[string]bool
	forwarders     []string
}

// buildServerState constructs the DNS server state from the new config format.
func (s *Service) buildServerState(cfg *config.Config) *serverState {
	dnsCfg := cfg.DNS
	state := &serverState{
		records:        make(map[string]config.DNSRecord),
		blockedDomains: make(map[string]bool),
		forwarders:     dnsCfg.Forwarders,
	}

	// 1. Gather all listeners and records
	var listenAddrs []string

	// Map interfaces to zones for IP resolution
	zoneIfaceMap := make(map[string][]string) // zone -> [ip1, ip2]

	// Build interface IP map
	for _, iface := range cfg.Interfaces {
		// Extract IPs
		var ips []string
		for _, cidr := range iface.IPv4 {
			ip, _, err := net.ParseCIDR(cidr)
			if err == nil {
				ips = append(ips, ip.String())
			}
		}
		// IPv6
		for _, cidr := range iface.IPv6 {
			ip, _, err := net.ParseCIDR(cidr)
			if err == nil {
				ips = append(ips, ip.String())
			}
		}

		// Associate with zones
		// 1. Explicit interface zone
		if iface.Zone != "" {
			zoneIfaceMap[strings.ToLower(iface.Zone)] = append(zoneIfaceMap[strings.ToLower(iface.Zone)], ips...)
		}

		// 2. Referenced in zone block (canonical)
		for _, z := range cfg.Zones {
			// Legacy interfaces list
			for _, matchIf := range z.Interfaces {
				if matchIf == iface.Name {
					zoneIfaceMap[strings.ToLower(z.Name)] = append(zoneIfaceMap[strings.ToLower(z.Name)], ips...)
				}
			}
			// Match list
			for _, m := range z.Matches {
				if m.Interface == iface.Name {
					zoneIfaceMap[strings.ToLower(z.Name)] = append(zoneIfaceMap[strings.ToLower(z.Name)], ips...)
				}
			}
		}
	}

	// Process Serve blocks
	for _, serve := range dnsCfg.Serve {
		// Determine Listen IPs
		zoneName := strings.ToLower(serve.Zone)
		if zoneName == "*" || zoneName == "any" {
			// Listen on both IPv4 and IPv6 wildcards for full dual-stack support
			listenAddrs = append(listenAddrs, "0.0.0.0", "::")
		} else {
			if ips, ok := zoneIfaceMap[zoneName]; ok {
				listenAddrs = append(listenAddrs, ips...)
			} else {
				log.Printf("[DNS] Warning: serve zone %q has no associated interfaces/IPs", serve.Zone)
			}
		}

		// Hosts
		for _, host := range serve.Hosts {
			for _, hostname := range host.Hostnames {
				fqdn := dns.Fqdn(hostname)
				recType := "A"
				if ip := net.ParseIP(host.IP); ip != nil && ip.To4() == nil {
					recType = "AAAA"
				}
				state.records[strings.ToLower(fqdn)] = config.DNSRecord{
					Name:  hostname,
					Type:  recType,
					Value: host.IP,
					TTL:   3600,
				}
			}
		}

		// Blocklists
		if len(serve.Blocklists) > 0 {
			bl, _ := loadBlocklistsFromConfig(serve.Blocklists)
			for k, v := range bl {
				state.blockedDomains[k] = v
			}
		}
	}

	// Dedup listeners
	state.listenAddrs = uniqueStrings(listenAddrs)

	return state
}
