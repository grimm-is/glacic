package firewall

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

// Protocol is a bitmask of network protocols.
type Protocol uint8

const (
	ProtoTCP  Protocol = 1 << iota // TCP protocol
	ProtoUDP                       // UDP protocol
	ProtoICMP                      // ICMP protocol
)

// Common protocol combinations
const (
	ProtoTCPUDP = ProtoTCP | ProtoUDP // Both TCP and UDP (e.g., DNS)
)

// String returns a human-readable protocol name.
func (p Protocol) String() string {
	var parts []string
	if p&ProtoTCP != 0 {
		parts = append(parts, "tcp")
	}
	if p&ProtoUDP != 0 {
		parts = append(parts, "udp")
	}
	if p&ProtoICMP != 0 {
		parts = append(parts, "icmp")
	}
	if len(parts) == 0 {
		return "any"
	}
	return strings.Join(parts, "+")
}

// Service defines a network service that can be allowed through the firewall.
type Service struct {
	Name        string   // Human-readable name
	Description string   // What this service does
	Protocol    Protocol // Protocol flags (TCP, UDP, ICMP, or combinations)

	// Port specifications (0 means protocol-only, like ICMP)
	Port    int   // Single port or start of range
	EndPort int   // End of port range (0 = single port)
	Ports   []int // Alternative: multiple ports for same protocol

	// Direction: "in" (input), "out" (output), "both"
	Direction string

	// Module: kernel module name for connection tracking (e.g., "nf_conntrack_ftp")
	// Presence of this field implies a helper is required.
	Module string
}

// NFTMatches generates all nftables match expressions for this service.
// Returns one expression per protocol (e.g., DNS returns both TCP and UDP rules).
func (s *Service) NFTMatches() []string {
	var matches []string

	if s.Protocol&ProtoICMP != 0 {
		matches = append(matches, "meta l4proto icmp")
	}

	portExpr := s.portExpr()
	if s.Protocol&ProtoTCP != 0 && portExpr != "" {
		matches = append(matches, fmt.Sprintf("tcp %s", portExpr))
	}
	if s.Protocol&ProtoUDP != 0 && portExpr != "" {
		matches = append(matches, fmt.Sprintf("udp %s", portExpr))
	}

	return matches
}

// NFTMatch returns a single nftables match expression (first match).
// Use NFTMatches() for services with multiple protocols.
func (s *Service) NFTMatch() string {
	matches := s.NFTMatches()
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// portExpr generates the port match expression.
func (s *Service) portExpr() string {
	if len(s.Ports) > 0 {
		ports := make([]string, len(s.Ports))
		for i, p := range s.Ports {
			ports[i] = fmt.Sprintf("%d", p)
		}
		return fmt.Sprintf("dport { %s }", strings.Join(ports, ", "))
	}
	if s.Port > 0 {
		if s.EndPort > s.Port {
			return fmt.Sprintf("dport %d-%d", s.Port, s.EndPort)
		}
		return fmt.Sprintf("dport %d", s.Port)
	}
	return ""
}

// BuiltinServices is the registry of built-in services.
var BuiltinServices = map[string]*Service{
	"dns": {
		Name:        "dns",
		Description: "Domain Name System",
		Protocol:    ProtoTCPUDP,
		Port:        53,
	},
	"ntp": {
		Name:        "ntp",
		Description: "Network Time Protocol",
		Protocol:    ProtoUDP,
		Port:        123,
	},
	"ssh": {
		Name:        "ssh",
		Description: "Secure Shell",
		Protocol:    ProtoTCP,
		Port:        22,
	},
	"icmp": {
		Name:        "icmp",
		Description: "Internet Control Message Protocol (ping)",
		Protocol:    ProtoICMP,
	},
	"snmp": {
		Name:        "snmp",
		Description: "Simple Network Management Protocol",
		Protocol:    ProtoUDP,
		Port:        161,
	},
	"syslog": {
		Name:        "syslog",
		Description: "System Logging",
		Protocol:    ProtoUDP,
		Port:        514,
	},
	"http": {
		Name:        "http",
		Description: "HTTP Web Traffic",
		Protocol:    ProtoTCP,
		Port:        80,
	},
	"https": {
		Name:        "https",
		Description: "HTTPS Secure Web Traffic",
		Protocol:    ProtoTCP,
		Port:        443,
	},
	"web": {
		Name:        "web",
		Description: "Web (HTTP + HTTPS)",
		Protocol:    ProtoTCP,
		Ports:       []int{80, 443},
	},
	"dhcp": {
		Name:        "dhcp",
		Description: "Dynamic Host Configuration Protocol",
		Protocol:    ProtoUDP,
		Ports:       []int{67, 68},
	},
	"mdns": {
		Name:        "mdns",
		Description: "Multicast DNS",
		Protocol:    ProtoUDP,
		Port:        5353,
	},
	"ftp": {
		Name:        "ftp",
		Description: "File Transfer Protocol",
		Protocol:    ProtoTCP,
		Port:        21,
		Module:      "nf_conntrack_ftp",
	},
	"tftp": {
		Name:        "tftp",
		Description: "Trivial File Transfer Protocol",
		Protocol:    ProtoUDP,
		Port:        69,
		Module:      "nf_conntrack_tftp",
	},
	"sip": {
		Name:        "sip",
		Description: "Session Initiation Protocol (VoIP)",
		Protocol:    ProtoUDP,
		Port:        5060,
		Module:      "nf_conntrack_sip",
	},
	"h323": {
		Name:        "h323",
		Description: "H.323 VoIP Signaling",
		Protocol:    ProtoTCP,
		Port:        1720,
		Module:      "nf_conntrack_h323",
	},
	"pptp": {
		Name:        "pptp",
		Description: "Point-to-Point Tunneling Protocol",
		Protocol:    ProtoTCP,
		Port:        1723,
		Module:      "nf_conntrack_pptp",
	},
	"irc": {
		Name:        "irc",
		Description: "Internet Relay Chat DCC",
		Protocol:    ProtoTCP,
		Port:        6667,
		Module:      "nf_conntrack_irc",
	},
	"amanda": {
		Name:        "amanda",
		Description: "Amanda Backup",
		Protocol:    ProtoUDP,
		Port:        10080,
		Module:      "nf_conntrack_amanda",
	},
	"netbios-ns": {
		Name:        "netbios-ns",
		Description: "NetBIOS Name Service",
		Protocol:    ProtoUDP,
		Port:        137,
		Module:      "nf_conntrack_netbios_ns",
	},
}

// CustomService creates a custom service definition.
func CustomService(name string, protocol Protocol, port int, opts ...CustomServiceOption) *Service {
	s := &Service{
		Name:      name,
		Protocol:  protocol,
		Port:      port,
		Direction: "in",
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CustomServiceOption is a functional option for CustomService.
type CustomServiceOption func(*Service)

// WithDescription sets the service description.
func WithDescription(desc string) CustomServiceOption {
	return func(s *Service) { s.Description = desc }
}

// WithPortRange sets a port range.
func WithPortRange(start, end int) CustomServiceOption {
	return func(s *Service) {
		s.Port = start
		s.EndPort = end
	}
}

// GetService looks up a service by name.
// It checks:
// 1. Built-in services (helpers, complex definitions)
// 2. System services (/etc/services)
func GetService(name string) *Service {
	// 1. Check built-ins
	if svc, ok := BuiltinServices[name]; ok {
		return svc
	}

	// 2. Check system services
	// Try TCP first
	port, err := net.LookupPort("tcp", name)
	if err == nil {
		return &Service{
			Name:        name,
			Description: "System service (TCP)",
			Protocol:    ProtoTCP,
			Port:        port,
			Direction:   "both",
		}
	}

	// Try UDP
	port, err = net.LookupPort("udp", name)
	if err == nil {
		return &Service{
			Name:        name,
			Description: "System service (UDP)",
			Protocol:    ProtoUDP,
			Port:        port,
			Direction:   "both",
		}
	}

	return nil
}

// SearchServices searches for services matching the query string.
// It checks built-in services and parses /etc/services.
func SearchServices(query string) []*Service {
	var results []*Service
	query = strings.ToLower(query)

	// 1. Search built-ins
	for _, svc := range BuiltinServices {
		if strings.Contains(strings.ToLower(svc.Name), query) ||
			strings.Contains(strings.ToLower(svc.Description), query) {
			results = append(results, svc)
		}
	}

	// 2. Search /etc/services
	// Simple parser for /etc/services
	// format: service-name  port/proto  [aliases...]  # comment
	f, err := os.Open("/etc/services")
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		seen := make(map[string]bool)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			// Remove comment
			if idx := strings.Index(line, "#"); idx != -1 {
				line = line[:idx]
			}

			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}

			name := fields[0]
			portProto := fields[1]

			// Check if name matches query (or aliases)
			match := strings.Contains(strings.ToLower(name), query)
			if !match {
				// Check aliases
				for _, alias := range fields[2:] {
					if strings.Contains(strings.ToLower(alias), query) {
						match = true
						break
					}
				}
			}

			if match {
				// Avoid duplicates (tcp/udp often listed separately)
				key := name + "/" + portProto
				if seen[key] {
					continue
				}
				seen[key] = true

				// Don't add if already in built-ins
				if _, ok := BuiltinServices[name]; ok {
					continue
				}

				// Parse port/proto
				ppParts := strings.Split(portProto, "/")
				if len(ppParts) != 2 {
					continue
				}

				port := 0
				fmt.Sscanf(ppParts[0], "%d", &port)
				proto := ProtoTCP
				if ppParts[1] == "udp" {
					proto = ProtoUDP
				} else if ppParts[1] == "ddp" || ppParts[1] == "sctp" {
					// Skip unsupported protocols for now or map to generic?
					// net.LookupPort supports tcp/udp.
					continue
				}

				results = append(results, &Service{
					Name:        name,
					Description: fmt.Sprintf("System service (%s)", portProto),
					Protocol:    proto,
					Port:        port,
					Direction:   "both",
				})
			}
		}
	}

	return results
}

// WithPorts sets multiple ports.
func WithPorts(ports ...int) CustomServiceOption {
	return func(s *Service) { s.Ports = ports }
}

// WithDirection sets the traffic direction.
func WithDirection(dir string) CustomServiceOption {
	return func(s *Service) { s.Direction = dir }
}

// ConntrackHelper represents a connection tracking helper configuration.
type ConntrackHelper struct {
	Name        string   // Helper name (ftp, sip, tftp, etc.)
	Description string   // Human-readable description
	Module      string   // Kernel module name
	Ports       []int    // Default ports
	Protocol    Protocol // tcp, udp, or both
	Enabled     bool     // Whether this helper is enabled
}

// ConntrackManager manages connection tracking helpers.
type ConntrackManager struct {
	enabledHelpers map[string]bool
}

// NewConntrackManager creates a new connection tracking manager.
func NewConntrackManager() *ConntrackManager {
	return &ConntrackManager{
		enabledHelpers: make(map[string]bool),
	}
}

// EnableHelper loads and enables a connection tracking helper.
// EnableHelper loads and enables a connection tracking helper.
func (m *ConntrackManager) EnableHelper(name string) error {
	svc, ok := BuiltinServices[name]
	if !ok {
		return fmt.Errorf("unknown helper/service: %s", name)
	}
	if svc.Module == "" {
		return fmt.Errorf("service %s has no helper module", name)
	}

	// Load the kernel module
	if err := m.loadModule(svc.Module); err != nil {
		return fmt.Errorf("failed to load module %s: %w", svc.Module, err)
	}

	m.enabledHelpers[name] = true
	return nil
}

// DisableHelper unloads a connection tracking helper.
// DisableHelper unloads a connection tracking helper.
func (m *ConntrackManager) DisableHelper(name string) error {
	svc, ok := BuiltinServices[name]
	if !ok {
		return fmt.Errorf("unknown helper/service: %s", name)
	}
	if svc.Module == "" {
		return fmt.Errorf("service %s has no helper module", name)
	}

	// Unload the kernel module
	if err := m.unloadModule(svc.Module); err != nil {
		// Module might be in use, just log warning
		return nil
	}

	delete(m.enabledHelpers, name)
	return nil
}

// EnableHelpers enables multiple helpers at once.
func (m *ConntrackManager) EnableHelpers(names []string) error {
	for _, name := range names {
		if err := m.EnableHelper(name); err != nil {
			return err
		}
	}
	return nil
}

// GetEnabledHelpers returns list of enabled helper names.
func (m *ConntrackManager) GetEnabledHelpers() []string {
	helpers := make([]string, 0, len(m.enabledHelpers))
	for name := range m.enabledHelpers {
		helpers = append(helpers, name)
	}
	return helpers
}

// ListAvailableHelpers returns all available helpers with their status.
// ListAvailableHelpers returns all available helpers with their status.
func (m *ConntrackManager) ListAvailableHelpers() []ConntrackHelper {
	var helpers []ConntrackHelper
	for _, svc := range BuiltinServices {
		if svc.Module == "" {
			continue
		}

		h := ConntrackHelper{
			Name:        svc.Name,
			Description: svc.Description,
			Module:      svc.Module,
			Protocol:    svc.Protocol,
			Enabled:     m.enabledHelpers[svc.Name],
		}

		// Map service ports to helper ports
		if len(svc.Ports) > 0 {
			h.Ports = svc.Ports
		} else if svc.Port > 0 {
			h.Ports = []int{svc.Port}
		}

		helpers = append(helpers, h)
	}
	return helpers
}

// loadModule loads a kernel module using modprobe.
func (m *ConntrackManager) loadModule(module string) error {
	// Check if module is already loaded
	data, err := os.ReadFile("/proc/modules")
	if err == nil {
		moduleName := strings.Replace(module, "-", "_", -1)
		if strings.Contains(string(data), moduleName) {
			return nil // Already loaded
		}
	}

	cmd := exec.Command("modprobe", module)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("modprobe %s failed: %s: %w", module, string(output), err)
	}
	return nil
}

// unloadModule unloads a kernel module using modprobe -r.
func (m *ConntrackManager) unloadModule(module string) error {
	cmd := exec.Command("modprobe", "-r", module)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("modprobe -r %s failed: %s: %w", module, string(output), err)
	}
	return nil
}

// SetupDefaultHelpers enables commonly needed helpers (FTP, SIP).
func (m *ConntrackManager) SetupDefaultHelpers() error {
	defaults := []string{"ftp", "sip", "tftp"}
	for _, name := range defaults {
		// Don't fail if a helper can't be loaded (module might not be available)
		_ = m.EnableHelper(name)
	}
	return nil
}

// GetConntrackStats returns connection tracking statistics.
func GetConntrackStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Read conntrack count
	if data, err := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_count"); err == nil {
		stats["count"] = strings.TrimSpace(string(data))
	}

	// Read conntrack max
	if data, err := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_max"); err == nil {
		stats["max"] = strings.TrimSpace(string(data))
	}

	// Read expect count (for helpers)
	if data, err := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_expect_max"); err == nil {
		stats["expect_max"] = strings.TrimSpace(string(data))
	}

	return stats, nil
}

// SetConntrackMax sets the maximum number of tracked connections.
func SetConntrackMax(max int) error {
	return os.WriteFile(
		"/proc/sys/net/netfilter/nf_conntrack_max",
		[]byte(fmt.Sprintf("%d", max)),
		0644,
	)
}

// ConntrackEntry represents a single connection tracking entry.
type ConntrackEntry struct {
	Protocol string
	SrcIP    string
	DstIP    string
	SrcPort  uint16
	DstPort  uint16
	State    string
	Timeout  uint32
}

// GetConntrackEntries is implemented in platform-specific files:
// - conntrack_linux.go: uses ti-mo/conntrack netlink library
// - conntrack_stub.go: stub for non-Linux platforms
