package imports

import (
	"encoding/xml"
	"fmt"
	"net"
	"os"

	"grimm.is/glacic/internal/config"
)

// PfSenseConfig represents the relevant parts of a pfSense/OPNsense config.xml.
type PfSenseConfig struct {
	XMLName xml.Name       `xml:"pfsense"`
	Version string         `xml:"version"`
	DHCPD   PfSenseDHCPD   `xml:"dhcpd"`
	DNSMasq PfSenseDNSMasq `xml:"dnsmasq"`
	Unbound PfSenseUnbound `xml:"unbound"`
	Hosts   []PfSenseHost  `xml:"system>hosts>host"`
	Aliases []PfSenseAlias `xml:"aliases>alias"`
}

// PfSenseDHCPD represents the dhcpd section.
type PfSenseDHCPD struct {
	Interfaces map[string]PfSenseDHCPInterface
}

// UnmarshalXML implements custom parsing for dynamic interface names in dhcpd.
func (d *PfSenseDHCPD) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	d.Interfaces = make(map[string]PfSenseDHCPInterface)
	for {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		switch val := t.(type) {
		case xml.StartElement:
			var iface PfSenseDHCPInterface
			if err := dec.DecodeElement(&iface, &val); err != nil {
				return err
			}
			d.Interfaces[val.Name.Local] = iface
		case xml.EndElement:
			if val == start.End() {
				return nil
			}
		}
	}
}

// PfSenseDHCPInterface represents DHCP config for an interface.
type PfSenseDHCPInterface struct {
	XMLName    xml.Name           `xml:""`
	Enable     string             `xml:"enable"`
	RangeFrom  string             `xml:"range>from"`
	RangeTo    string             `xml:"range>to"`
	Gateway    string             `xml:"gateway"`
	Domain     string             `xml:"domain"`
	DNSServer1 string             `xml:"dnsserver"`
	DNSServer2 string             `xml:"dnsserver2"`
	StaticMaps []PfSenseStaticMap `xml:"staticmap"`
}

// PfSenseStaticMap represents a static DHCP mapping.
type PfSenseStaticMap struct {
	MAC         string `xml:"mac"`
	CID         string `xml:"cid"` // Client ID
	IPAddr      string `xml:"ipaddr"`
	Hostname    string `xml:"hostname"`
	Description string `xml:"descr"`
	ARP         string `xml:"arp_table_static_entry"`
	WINSServer  string `xml:"winsserver"`
	DNSServer   string `xml:"dnsserver"`
	Gateway     string `xml:"gateway"`
	Domain      string `xml:"domain"`
}

// PfSenseDNSMasq represents dnsmasq configuration.
type PfSenseDNSMasq struct {
	Enable          string                  `xml:"enable"`
	RegDHCP         string                  `xml:"regdhcp"`
	RegDHCPStatic   string                  `xml:"regdhcpstatic"`
	DHCPFirst       string                  `xml:"dhcpfirst"`
	Hosts           []PfSenseDNSHost        `xml:"hosts"`
	DomainOverrides []PfSenseDomainOverride `xml:"domainoverrides"`
}

// PfSenseUnbound represents Unbound DNS configuration.
type PfSenseUnbound struct {
	Enable        string        `xml:"enable"`
	DNSSEC        string        `xml:"dnssec"`
	Forwarding    string        `xml:"forwarding"`
	RegDHCP       string        `xml:"regdhcp"`
	RegDHCPStatic string        `xml:"regdhcpstatic"`
	Hosts         []PfSenseHost `xml:"hosts"`
}

// PfSenseHost represents a static host entry.
type PfSenseHost struct {
	Host    string `xml:"host"`
	Domain  string `xml:"domain"`
	IP      string `xml:"ip"`
	Descr   string `xml:"descr"`
	Aliases string `xml:"aliases"` // Comma-separated aliases
}

// PfSenseDNSHost represents a dnsmasq host override.
type PfSenseDNSHost struct {
	Host   string `xml:"host"`
	Domain string `xml:"domain"`
	IP     string `xml:"ip"`
	Descr  string `xml:"descr"`
}

// PfSenseDomainOverride represents a domain override (conditional forwarding).
type PfSenseDomainOverride struct {
	Domain string `xml:"domain"`
	IP     string `xml:"ip"`
	Descr  string `xml:"descr"`
}

// PfSenseAlias represents a firewall alias (can contain hosts).
type PfSenseAlias struct {
	Name    string `xml:"name"`
	Type    string `xml:"type"` // host, network, port, etc.
	Address string `xml:"address"`
	Descr   string `xml:"descr"`
	Detail  string `xml:"detail"`
}

// ParsePfSenseConfig parses a pfSense or OPNsense config.xml file.
func ParsePfSenseConfig(path string) (*PfSenseConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open pfSense config: %w", err)
	}
	defer file.Close()

	// Read the entire file for custom parsing
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read pfSense config: %w", err)
	}

	cfg := &PfSenseConfig{}
	if err := xml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse pfSense XML: %w", err)
	}

	return cfg, nil
}

// ParsePfSenseStaticMaps extracts static DHCP mappings from config.xml.
// This is a simpler parser that just extracts staticmap entries.
func ParsePfSenseStaticMaps(path string) ([]PfSenseStaticMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Simple struct to extract just staticmaps
	type dhcpdWrapper struct {
		StaticMaps []PfSenseStaticMap `xml:"dhcpd>lan>staticmap"`
	}

	var wrapper dhcpdWrapper
	if err := xml.Unmarshal(data, &wrapper); err != nil {
		// Try alternative paths for different interfaces
		type altWrapper struct {
			LAN  []PfSenseStaticMap `xml:"dhcpd>lan>staticmap"`
			OPT1 []PfSenseStaticMap `xml:"dhcpd>opt1>staticmap"`
			OPT2 []PfSenseStaticMap `xml:"dhcpd>opt2>staticmap"`
			WAN  []PfSenseStaticMap `xml:"dhcpd>wan>staticmap"`
		}
		var alt altWrapper
		if err := xml.Unmarshal(data, &alt); err != nil {
			return nil, fmt.Errorf("failed to parse staticmaps: %w", err)
		}
		var all []PfSenseStaticMap
		all = append(all, alt.LAN...)
		all = append(all, alt.OPT1...)
		all = append(all, alt.OPT2...)
		all = append(all, alt.WAN...)
		return all, nil
	}

	return wrapper.StaticMaps, nil
}

// ParsePfSenseHosts extracts static host entries from config.xml.
func ParsePfSenseHosts(path string) ([]PfSenseHost, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Try Unbound hosts first (more common in recent versions)
	type unboundWrapper struct {
		Hosts []PfSenseHost `xml:"unbound>hosts"`
	}
	var unbound unboundWrapper
	xml.Unmarshal(data, &unbound)

	// Also try dnsmasq hosts
	type dnsmasqWrapper struct {
		Hosts []PfSenseDNSHost `xml:"dnsmasq>hosts"`
	}
	var dnsmasq dnsmasqWrapper
	xml.Unmarshal(data, &dnsmasq)

	// Also try system hosts
	type systemWrapper struct {
		Hosts []PfSenseHost `xml:"system>hosts>host"`
	}
	var system systemWrapper
	xml.Unmarshal(data, &system)

	// Combine all sources
	var all []PfSenseHost
	all = append(all, unbound.Hosts...)
	all = append(all, system.Hosts...)

	// Convert dnsmasq hosts
	for _, h := range dnsmasq.Hosts {
		all = append(all, PfSenseHost{
			Host:   h.Host,
			Domain: h.Domain,
			IP:     h.IP,
			Descr:  h.Descr,
		})
	}

	return all, nil
}

// PfSenseStaticMapsToReservations converts pfSense static maps to DHCP reservations.
func PfSenseStaticMapsToReservations(maps []PfSenseStaticMap) []config.DHCPReservation {
	var reservations []config.DHCPReservation
	for _, m := range maps {

		if m.MAC == "" || m.IPAddr == "" {
			continue
		}

		res := config.DHCPReservation{
			MAC:         normalizeMACAddress(m.MAC),
			IP:          m.IPAddr,
			Hostname:    m.Hostname,
			Description: m.Description,
		}

		reservations = append(reservations, res)
	}

	return reservations
}

// PfSenseHostsToHostEntries converts pfSense host entries to DNS host entries.
func PfSenseHostsToHostEntries(hosts []PfSenseHost) []config.DNSHostEntry {
	var entries []config.DNSHostEntry

	for _, h := range hosts {
		if h.IP == "" {
			continue
		}

		ip := net.ParseIP(h.IP)
		if ip == nil {
			continue
		}

		// Build hostname with domain
		var hostnames []string
		if h.Host != "" {
			if h.Domain != "" {
				hostnames = append(hostnames, h.Host+"."+h.Domain)
			}
			hostnames = append(hostnames, h.Host)
		}

		if len(hostnames) == 0 {
			continue
		}

		entries = append(entries, config.DNSHostEntry{
			IP:        h.IP,
			Hostnames: hostnames,
		})
	}

	return entries
}

// PfSenseStaticMapsToLeases converts static maps to Lease format for unified handling.
func PfSenseStaticMapsToLeases(maps []PfSenseStaticMap) []Lease {
	var leases []Lease

	for _, m := range maps {
		if m.MAC == "" || m.IPAddr == "" {
			continue
		}

		leases = append(leases, Lease{
			MAC:      normalizeMACAddress(m.MAC),
			IP:       net.ParseIP(m.IPAddr),
			Hostname: m.Hostname,
			// Static mappings don't expire
		})
	}

	return leases
}

// OPNsenseConfig is an alias for PfSenseConfig as they share the same format.
type OPNsenseConfig = PfSenseConfig

// ParseOPNsenseConfig parses an OPNsense config.xml file.
// OPNsense uses the same basic XML structure as pfSense.
func ParseOPNsenseConfig(path string) (*OPNsenseConfig, error) {
	return ParsePfSenseConfig(path)
}
