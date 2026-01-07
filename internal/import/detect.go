package imports

import (
	"fmt"
	"grimm.is/glacic/internal/brand"
	"net"
	"os"
	"os/exec"
	"strings"
)

// ExternalService represents a detected external network service.
type ExternalService struct {
	Name       string   // e.g., "dnsmasq", "isc-dhcp-server", "bind9"
	Type       string   // "dns", "dhcp", or "both"
	Running    bool     // Is the service currently running?
	ConfigFile string   // Path to config file if found
	LeaseFile  string   // Path to lease file if found (DHCP only)
	Ports      []int    // Ports the service is listening on
	PIDs       []int    // Process IDs
	Conflicts  []string // Specific conflicts with built-in services
}

// DetectionResult contains all detected external services.
type DetectionResult struct {
	DNSServices  []ExternalService
	DHCPServices []ExternalService
	Warnings     []string
}

// DetectExternalServices scans for running DNS and DHCP services.
func DetectExternalServices() *DetectionResult {
	result := &DetectionResult{}

	// Check for common DNS servers
	dnsServices := []struct {
		name       string
		process    string
		configPath string
	}{
		{"dnsmasq", "dnsmasq", "/etc/dnsmasq.conf"},
		{"bind9", "named", "/etc/bind/named.conf"},
		{"unbound", "unbound", "/etc/unbound/unbound.conf"},
		{"systemd-resolved", "systemd-resolved", "/etc/systemd/resolved.conf"},
		{"coredns", "coredns", "/etc/coredns/Corefile"},
	}

	for _, svc := range dnsServices {
		if detected := detectService(svc.name, svc.process, svc.configPath, "dns"); detected != nil {
			result.DNSServices = append(result.DNSServices, *detected)
		}
	}

	// Check for common DHCP servers
	dhcpServices := []struct {
		name       string
		process    string
		configPath string
		leasePath  string
	}{
		{"dnsmasq", "dnsmasq", "/etc/dnsmasq.conf", "/var/lib/misc/dnsmasq.leases"},
		{"isc-dhcp-server", "dhcpd", "/etc/dhcp/dhcpd.conf", "/var/lib/dhcp/dhcpd.leases"},
		{"kea-dhcp4", "kea-dhcp4", "/etc/kea/kea-dhcp4.conf", ""},
		{"dnsmasq", "dnsmasq", "/etc/dnsmasq.d/", "/var/lib/dnsmasq/dnsmasq.leases"},
	}

	for _, svc := range dhcpServices {
		if detected := detectDHCPService(svc.name, svc.process, svc.configPath, svc.leasePath); detected != nil {
			result.DHCPServices = append(result.DHCPServices, *detected)
		}
	}

	// Check for port conflicts
	if isPortInUse(53) {
		result.Warnings = append(result.Warnings, "Port 53 (DNS) is already in use")
	}
	if isPortInUse(67) {
		result.Warnings = append(result.Warnings, "Port 67 (DHCP) is already in use")
	}

	// Deduplicate (dnsmasq appears in both lists)
	result.DNSServices = deduplicateServices(result.DNSServices)
	result.DHCPServices = deduplicateServices(result.DHCPServices)

	return result
}

func detectService(name, process, configPath, serviceType string) *ExternalService {
	svc := &ExternalService{
		Name: name,
		Type: serviceType,
	}

	// Check if process is running
	pids := findProcessByName(process)
	if len(pids) > 0 {
		svc.Running = true
		svc.PIDs = pids
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); err == nil {
		svc.ConfigFile = configPath
	}

	// Only return if we found something
	if !svc.Running && svc.ConfigFile == "" {
		return nil
	}

	// Add conflict warnings
	svc.Conflicts = getConflicts(name, serviceType)

	return svc
}

func detectDHCPService(name, process, configPath, leasePath string) *ExternalService {
	svc := detectService(name, process, configPath, "dhcp")
	if svc == nil {
		return nil
	}

	// Check for lease file
	if leasePath != "" {
		if _, err := os.Stat(leasePath); err == nil {
			svc.LeaseFile = leasePath
		}
	}

	return svc
}

func findProcessByName(name string) []int {
	// Use pgrep to find processes
	cmd := exec.Command("pgrep", "-x", name)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(line, "%d", &pid); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

func isPortInUse(port int) bool {
	// Try to listen on the port
	addr := fmt.Sprintf(":%d", port)

	// Try UDP (for DNS/DHCP)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return true // Port is in use
	}
	conn.Close()

	// Try TCP (for DNS)
	if port == 53 {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return true
		}
		ln.Close()
	}

	return false
}

func getConflicts(name, serviceType string) []string {
	var conflicts []string

	switch name {
	case "dnsmasq":
		if serviceType == "dns" || serviceType == "both" {
			conflicts = append(conflicts, fmt.Sprintf("Will conflict with %s DNS on port 53", brand.Name))
		}
		if serviceType == "dhcp" || serviceType == "both" {
			conflicts = append(conflicts, fmt.Sprintf("Will conflict with %s DHCP on port 67", brand.Name))
		}
	case "bind9", "unbound", "coredns":
		conflicts = append(conflicts, fmt.Sprintf("Will conflict with %s DNS on port 53", brand.Name))
	case "isc-dhcp-server", "kea-dhcp4":
		conflicts = append(conflicts, fmt.Sprintf("Will conflict with %s DHCP on port 67", brand.Name))
	case "systemd-resolved":
		conflicts = append(conflicts,
			"systemd-resolved uses port 53 on 127.0.0.53",
			"Consider disabling DNSStubListener in resolved.conf",
		)
	}

	return conflicts
}

func deduplicateServices(services []ExternalService) []ExternalService {
	seen := make(map[string]bool)
	var result []ExternalService

	for _, svc := range services {
		key := svc.Name + "|" + svc.Type
		if !seen[key] {
			seen[key] = true
			result = append(result, svc)
		}
	}

	return result
}

// GetFeatureDegradationWarnings returns warnings about features that won't work
// when using external services.
func GetFeatureDegradationWarnings(externalDNS, externalDHCP bool) []string {
	var warnings []string

	if externalDNS && externalDHCP {
		warnings = append(warnings,
			"⚠️ Using external DNS and DHCP servers",
			"",
			"The following features are unavailable:",
			"• Automatic hostname registration from DHCP leases",
			"• Per-client DNS query statistics",
			"• DNS blocking and filtering",
			"• DHCP lease visibility in dashboard",
			"• Learning firewall client identification",
			"• Per-client bandwidth tracking",
			"",
			fmt.Sprintf("Consider migrating to %s's built-in services for full functionality.", brand.Name),
		)
	} else if externalDNS {
		warnings = append(warnings,
			"⚠️ Using external DNS server",
			"",
			"The following features are unavailable:",
			"• Automatic hostname registration from DHCP leases",
			"• Per-client DNS query statistics",
			"• DNS blocking and filtering",
			"• Learning firewall DNS-based identification",
			"",
			"DHCP integration with DNS will not work.",
		)
	} else if externalDHCP {
		warnings = append(warnings,
			"⚠️ Using external DHCP server",
			"",
			"The following features are unavailable:",
			"• DHCP lease visibility in dashboard",
			"• Automatic hostname registration in DNS",
			"• Per-client bandwidth tracking",
			"• Learning firewall MAC-based identification",
			"",
			"Consider importing leases for partial visibility.",
		)
	}

	return warnings
}
