package firewall

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ConntrackHelper represents a connection tracking helper configuration.
type ConntrackHelper struct {
	Name        string // Helper name (ftp, sip, tftp, etc.)
	Description string // Human-readable description
	Module      string // Kernel module name
	Ports       []int  // Default ports
	Protocol    string // tcp, udp, or both
	Enabled     bool   // Whether this helper is enabled
}

// BuiltinHelpers contains predefined connection tracking helpers.
var BuiltinHelpers = map[string]ConntrackHelper{
	"ftp": {
		Name:        "ftp",
		Description: "FTP active mode - tracks PORT/PASV commands for data connections",
		Module:      "nf_conntrack_ftp",
		Ports:       []int{21},
		Protocol:    "tcp",
	},
	"tftp": {
		Name:        "tftp",
		Description: "TFTP - tracks TFTP connections",
		Module:      "nf_conntrack_tftp",
		Ports:       []int{69},
		Protocol:    "udp",
	},
	"sip": {
		Name:        "sip",
		Description: "SIP VoIP - tracks SIP signaling for RTP media streams",
		Module:      "nf_conntrack_sip",
		Ports:       []int{5060},
		Protocol:    "udp",
	},
	"h323": {
		Name:        "h323",
		Description: "H.323 VoIP - tracks H.323 signaling",
		Module:      "nf_conntrack_h323",
		Ports:       []int{1720},
		Protocol:    "tcp",
	},
	"pptp": {
		Name:        "pptp",
		Description: "PPTP VPN - tracks GRE tunnels for PPTP",
		Module:      "nf_conntrack_pptp",
		Ports:       []int{1723},
		Protocol:    "tcp",
	},
	"irc": {
		Name:        "irc",
		Description: "IRC DCC - tracks DCC file transfers",
		Module:      "nf_conntrack_irc",
		Ports:       []int{6667},
		Protocol:    "tcp",
	},
	"amanda": {
		Name:        "amanda",
		Description: "Amanda backup - tracks Amanda backup connections",
		Module:      "nf_conntrack_amanda",
		Ports:       []int{10080},
		Protocol:    "udp",
	},
	"netbios-ns": {
		Name:        "netbios-ns",
		Description: "NetBIOS name service",
		Module:      "nf_conntrack_netbios_ns",
		Ports:       []int{137},
		Protocol:    "udp",
	},
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
func (m *ConntrackManager) EnableHelper(name string) error {
	helper, ok := BuiltinHelpers[name]
	if !ok {
		return fmt.Errorf("unknown helper: %s", name)
	}

	// Load the kernel module
	if err := m.loadModule(helper.Module); err != nil {
		return fmt.Errorf("failed to load module %s: %w", helper.Module, err)
	}

	m.enabledHelpers[name] = true
	return nil
}

// DisableHelper unloads a connection tracking helper.
func (m *ConntrackManager) DisableHelper(name string) error {
	helper, ok := BuiltinHelpers[name]
	if !ok {
		return fmt.Errorf("unknown helper: %s", name)
	}

	// Unload the kernel module
	if err := m.unloadModule(helper.Module); err != nil {
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
func (m *ConntrackManager) ListAvailableHelpers() []ConntrackHelper {
	helpers := make([]ConntrackHelper, 0, len(BuiltinHelpers))
	for _, h := range BuiltinHelpers {
		h.Enabled = m.enabledHelpers[h.Name]
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

// GetConntrackEntries returns active connection tracking entries.
// Uses the conntrack CLI tool if available.
func GetConntrackEntries() ([]ConntrackEntry, error) {
	cmd := exec.Command("conntrack", "-L", "-o", "extended")
	output, err := cmd.Output()
	if err != nil {
		// conntrack tool might not be installed
		return nil, fmt.Errorf("conntrack -L failed: %w", err)
	}

	var entries []ConntrackEntry
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		entry := parseConntrackLine(line)
		if entry.Protocol != "" {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// parseConntrackLine parses a single line of conntrack -L output.
// Example: "tcp      6 431999 ESTABLISHED src=192.168.1.5 dst=93.184.216.34 sport=54321 dport=443"
func parseConntrackLine(line string) ConntrackEntry {
	var entry ConntrackEntry

	fields := strings.Fields(line)
	if len(fields) < 4 {
		return entry
	}

	entry.Protocol = fields[0]

	for _, field := range fields {
		if strings.HasPrefix(field, "src=") {
			entry.SrcIP = strings.TrimPrefix(field, "src=")
		} else if strings.HasPrefix(field, "dst=") {
			entry.DstIP = strings.TrimPrefix(field, "dst=")
		} else if strings.HasPrefix(field, "sport=") {
			fmt.Sscanf(strings.TrimPrefix(field, "sport="), "%d", &entry.SrcPort)
		} else if strings.HasPrefix(field, "dport=") {
			fmt.Sscanf(strings.TrimPrefix(field, "dport="), "%d", &entry.DstPort)
		}
	}

	// State is usually the 4th field for TCP
	if entry.Protocol == "tcp" && len(fields) > 3 {
		// Check if field looks like a state (all caps, not key=value)
		for _, f := range fields[2:5] {
			if !strings.Contains(f, "=") && f == strings.ToUpper(f) && len(f) > 2 {
				entry.State = f
				break
			}
		}
	}

	return entry
}
