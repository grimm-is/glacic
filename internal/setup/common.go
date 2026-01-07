package setup

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"grimm.is/glacic/internal/logging"
)

// DefaultConfigDir is the default configuration directory
const DefaultConfigDir = "/etc/glacic"

// DefaultConfigFile is the default config file path
const DefaultConfigFile = "/etc/glacic/glacic.hcl"

// InterfaceInfo contains detected information about a network interface
type InterfaceInfo struct {
	Name       string   `json:"name"`
	MAC        string   `json:"mac"`
	Driver     string   `json:"driver"`
	LinkUp     bool     `json:"link_up"`
	Speed      string   `json:"speed,omitempty"`
	IPs        []string `json:"ips,omitempty"`
	IsVirtual  bool     `json:"is_virtual"`
	IsBridge   bool     `json:"is_bridge"`
	IsBond     bool     `json:"is_bond"`
	IsVLAN     bool     `json:"is_vlan"`
	SuggestWAN bool     `json:"suggest_wan,omitempty"`
}

// DetectedHardware contains all detected network hardware
type DetectedHardware struct {
	Interfaces []InterfaceInfo `json:"interfaces"`
}

// WizardResult contains the result of the setup wizard
type WizardResult struct {
	WANInterface   string            `json:"wan_interface"`
	WANMethod      string            `json:"wan_method"` // "dhcp" or "static"
	WANIP          string            `json:"wan_ip,omitempty"`
	WANGateway     string            `json:"wan_gateway,omitempty"`
	LANInterface   string            `json:"lan_interface"`   // Primary LAN interface (for DHCP scope)
	LANInterfaces  []string          `json:"lan_interfaces"`  // All LAN interfaces (for zone)
	DHCPInterfaces map[string]string `json:"dhcp_interfaces"` // Interface -> IP (interfaces with existing DHCP)
	LANIP          string            `json:"lan_ip"`
	LANSubnet      string            `json:"lan_subnet"`
	ConfigPath     string            `json:"config_path"`
}

// Wizard handles the setup process
type Wizard struct {
	configDir  string
	configFile string
	hardware   *DetectedHardware
	logger     *logging.Logger
}

// NewWizard creates a new setup wizard
func NewWizard(configDir string) *Wizard {
	if configDir == "" {
		configDir = DefaultConfigDir
	}
	return &Wizard{
		configDir:  configDir,
		configFile: filepath.Join(configDir, "glacic.hcl"),
		logger:     logging.New(logging.DefaultConfig()),
	}
}

// NeedsSetup returns true if no config exists
func (w *Wizard) NeedsSetup() bool {
	_, err := os.Stat(w.configFile)
	return os.IsNotExist(err)
}

// GetHardware returns detected hardware
func (w *Wizard) GetHardware() *DetectedHardware {
	return w.hardware
}

// GenerateConfig creates a config file from wizard results
func (w *Wizard) GenerateConfig(result *WizardResult) error {
	// Ensure config directory exists
	if err := os.MkdirAll(w.configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Generate config from template with custom functions
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}
	tmpl := template.Must(template.New("config").Funcs(funcMap).Parse(configTemplate))

	f, err := os.Create(w.configFile)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, result); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	result.ConfigPath = w.configFile
	return nil
}

// excludedPrefixes are interface name prefixes to ignore
var excludedPrefixes = []string{
	"lo",     // Loopback
	"docker", // Docker
	"br-",    // Docker bridges
	"veth",   // Virtual ethernet (containers)
	"virbr",  // Libvirt bridges
	"vnet",   // Libvirt VMs
	"tun",    // VPN tunnels
	"tap",    // TAP devices
	"dummy",  // Dummy interfaces
	"sit",    // IPv6-in-IPv4 tunnels
	"ip6tnl", // IPv6 tunnels
}

// shouldExclude checks if an interface should be excluded
func shouldExclude(name string) bool {
	for _, prefix := range excludedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// IsPrivateIP checks if an IP is in private ranges
func IsPrivateIP(ip net.IP) bool {
	private := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	for _, cidr := range private {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// IsPrivateOrBogon checks if an IP string is in private, CGNAT, or bogon ranges.
// Returns true for IPs that should NOT be considered as public WAN IPs.
func IsPrivateOrBogon(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return true // Invalid IP, treat as bogon
	}

	// Check if it's IPv4
	ip4 := ip.To4()
	if ip4 == nil {
		// IPv6 - for now just check link-local and ULA
		return ip.IsLinkLocalUnicast() || ip.IsPrivate()
	}

	// Bogon/reserved ranges
	bogonRanges := []string{
		"0.0.0.0/8",       // This network
		"10.0.0.0/8",      // Private RFC1918
		"100.64.0.0/10",   // CGNAT (RFC6598)
		"127.0.0.0/8",     // Loopback
		"169.254.0.0/16",  // Link-local
		"172.16.0.0/12",   // Private RFC1918
		"192.0.0.0/24",    // IETF Protocol Assignments
		"192.0.2.0/24",    // TEST-NET-1
		"192.168.0.0/16",  // Private RFC1918
		"198.18.0.0/15",   // Benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"224.0.0.0/4",     // Multicast
		"240.0.0.0/4",     // Reserved
	}

	for _, cidr := range bogonRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil && network.Contains(ip4) {
			return true
		}
	}
	return false
}

// GetPhysicalInterfaces returns only physical (non-virtual) interfaces
func (hw *DetectedHardware) GetPhysicalInterfaces() []InterfaceInfo {
	physical := make([]InterfaceInfo, 0)
	for _, iface := range hw.Interfaces {
		if !iface.IsVirtual && !iface.IsBridge && !iface.IsBond {
			physical = append(physical, iface)
		}
	}
	return physical
}

// GetActiveInterfaces returns interfaces with link up
func (hw *DetectedHardware) GetActiveInterfaces() []InterfaceInfo {
	active := make([]InterfaceInfo, 0)
	for _, iface := range hw.Interfaces {
		if iface.LinkUp {
			active = append(active, iface)
		}
	}
	return active
}

// FindBestWANCandidate returns the best interface for WAN
// Prefers: physical, link up, no existing IP
func (hw *DetectedHardware) FindBestWANCandidate() *InterfaceInfo {
	candidates := hw.GetPhysicalInterfaces()

	// First pass: interface with link up and no IP (likely connected to ISP)
	for i := range candidates {
		if candidates[i].LinkUp && len(candidates[i].IPs) == 0 {
			return &candidates[i]
		}
	}

	// Second pass: any interface with link up
	for i := range candidates {
		if candidates[i].LinkUp {
			return &candidates[i]
		}
	}

	// Fallback: first physical interface
	if len(candidates) > 0 {
		return &candidates[0]
	}

	return nil
}

// FindBestLANCandidate returns the best interface for LAN (excluding WAN)
func (hw *DetectedHardware) FindBestLANCandidate(wanName string) *InterfaceInfo {
	candidates := hw.GetPhysicalInterfaces()

	for i := range candidates {
		if candidates[i].Name != wanName {
			return &candidates[i]
		}
	}

	return nil
}

// configTemplate is the HCL config template
const configTemplate = `# Firewall Configuration
# Generated by setup wizard

schema_version = "1.0"

# SECURITY: IP forwarding disabled by default
# Enable this only after configuring firewall policies
ip_forwarding = false

# Interface Definitions
interface "{{.WANInterface}}" {
{{- if eq .WANMethod "dhcp"}}
  dhcp = true
{{- else}}
  ipv4 = ["{{.WANIP}}"]
{{- end}}
}
{{- if .LANInterfaces}}
{{- range $i, $iface := .LANInterfaces}}

interface "{{$iface}}" {
{{- if index $.DHCPInterfaces $iface}}
  # Existing DHCP server detected - use as client
  dhcp = true
{{- else if eq $iface $.LANInterface}}
  ipv4 = ["{{$.LANIP}}/24"]
{{- end}}
}
{{- end}}
{{- end}}

# Zone Definitions
zone "WAN" {
  interface = "{{.WANInterface}}"
  description = "WAN - Internet Connection"
{{- if eq .WANMethod "dhcp"}}
  dhcp = true
{{- else}}
  ipv4 = ["{{.WANIP}}"]
{{- end}}
}
{{- if .LANInterfaces}}
{{- $lanIndex := 1}}
{{- range $i, $iface := .LANInterfaces}}

zone "LAN{{$lanIndex}}" {
  interface = "{{$iface}}"
  description = "LAN{{$lanIndex}} - {{$iface}}"
{{- if eq $iface $.LANInterface}}
  ipv4 = ["{{$.LANIP}}/24"]
{{- end}}

  # Services provided to LAN clients (auto-generates firewall rules)
  services {
    dhcp = true
    dns  = true
  }

  # Allow management access from LAN
  management {
    web  = true
    icmp = true
  }
}
{{- $lanIndex = add $lanIndex 1}}
{{- end}}
{{- end}}

# API Server Configuration
# TLS certificates are auto-generated in /var/lib/glacic/certs/
api {
  enabled    = true
  listen     = ":8080"
  tls_listen = ":8443"
  tls_cert   = "/var/lib/glacic/certs/server.crt"
  tls_key    = "/var/lib/glacic/certs/server.key"
}
{{- if .LANInterface}}

# DHCP Server for LAN
dhcp {
  enabled = true
  scope "lan" {
    interface   = "{{.LANInterface}}"
    range_start = "192.168.1.100"
    range_end   = "192.168.1.200"
    router      = "{{.LANIP}}"
    dns         = ["{{.LANIP}}", "8.8.8.8"]
  }
}

# DNS Server
dns_server {
  enabled    = true
  listen_on  = ["{{.LANIP}}"]
  forwarders = ["8.8.8.8", "1.1.1.1"]
}
{{- end}}

# Firewall Policies
# NOTE: No policies = no traffic forwarding (safe default)
# Add policies via web UI to allow traffic flow

# To enable internet access for LAN, add:
# policy "LAN" "WAN" {
#   action = "accept"
# }
#
# nat "outbound" {
#   type = "masquerade"
#   out_zone = "WAN"
# }
`
