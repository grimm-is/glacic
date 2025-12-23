package firewall

import (
	"fmt"
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
}

// GetService returns a service by name, or nil if not found.
func GetService(name string) *Service {
	return BuiltinServices[strings.ToLower(name)]
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

// WithPorts sets multiple ports.
func WithPorts(ports ...int) CustomServiceOption {
	return func(s *Service) { s.Ports = ports }
}

// WithDirection sets the traffic direction.
func WithDirection(dir string) CustomServiceOption {
	return func(s *Service) { s.Direction = dir }
}

