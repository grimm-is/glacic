//go:build linux
// +build linux

package setup

import (
	"context"
	"fmt"
	"time"

	"grimm.is/glacic/internal/services/dhcp"

	"github.com/vishvananda/netlink"
)

// DetectHardware scans for network interfaces
func (w *Wizard) DetectHardware() error {
	hw, err := DetectHardware()
	if err != nil {
		return err
	}
	w.hardware = hw
	return nil
}

// ProbeWAN attempts DHCP on an interface to detect if it's connected to a network
func (w *Wizard) ProbeWAN(ifaceName string, timeout time.Duration) (bool, string, error) {
	// Bring interface up
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return false, "", fmt.Errorf("interface not found: %w", err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return false, "", fmt.Errorf("failed to bring up interface: %w", err)
	}

	// Create a temporary DHCP client
	cm := dhcp.NewClientManager(nil)
	defer cm.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Start DHCP client
	if err := cm.StartClient(ifaceName); err != nil {
		return false, "", fmt.Errorf("failed to start DHCP: %w", err)
	}

	// Wait for lease or timeout
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, "", nil // Timeout, no DHCP server found
		case <-ticker.C:
			lease, err := cm.GetLease(ifaceName)
			if err == nil && lease != nil {
				return true, lease.IPAddress.String(), nil
			}
		}
	}
}

// AutoDetectWAN probes all interfaces for DHCP and selects the one with a public IP as WAN.
// If no public IP is found, falls back to the first interface that got any DHCP lease.
func (w *Wizard) AutoDetectWAN() (*InterfaceInfo, string, error) {
	if w.hardware == nil {
		if err := w.DetectHardware(); err != nil {
			return nil, "", err
		}
	}

	physical := w.hardware.GetPhysicalInterfaces()

	type dhcpResult struct {
		iface *InterfaceInfo
		ip    string
		err   error
	}

	// Probe all interfaces for DHCP
	results := make([]dhcpResult, 0)
	for i := range physical {
		iface := &physical[i]
		w.logger.Info("Probing for DHCP...", "interface", iface.Name)

		success, ip, err := w.ProbeWAN(iface.Name, 10*time.Second)
		if err != nil {
			w.logger.Error("DHCP probe error", "error", err)
			continue
		}

		if success {
			w.logger.Info("Found DHCP!", "interface", iface.Name, "ip", ip)
			results = append(results, dhcpResult{iface: iface, ip: ip})
		} else {
			w.logger.Info("No DHCP response", "interface", iface.Name)
		}
	}

	if len(results) == 0 {
		return nil, "", fmt.Errorf("no WAN interface detected (no DHCP response)")
	}

	// Prefer interface with public IP (not RFC1918 private, not bogon)
	for _, r := range results {
		if !IsPrivateOrBogon(r.ip) {
			w.logger.Info("Selected WAN (public IP)", "interface", r.iface.Name, "ip", r.ip)
			r.iface.SuggestWAN = true
			return r.iface, r.ip, nil
		}
	}

	// All IPs are private - use the first one (likely double-NAT or CGNAT)
	result := results[0]
	w.logger.Info("Selected WAN (private IP, possible CGNAT/double-NAT)", "interface", result.iface.Name, "ip", result.ip)
	result.iface.SuggestWAN = true
	return result.iface, result.ip, nil
}

// RunAutoSetup performs automatic setup
func (w *Wizard) RunAutoSetup() (*WizardResult, error) {
	w.logger.Info(fmt.Sprintf("=== %s Setup Wizard ===", "Glacic"))

	// Detect hardware
	w.logger.Info("Detecting network interfaces...")
	if err := w.DetectHardware(); err != nil {
		return nil, fmt.Errorf("hardware detection failed: %w", err)
	}

	physical := w.hardware.GetPhysicalInterfaces()
	w.logger.Info("Physical interfaces detected", "count", len(physical))
	for _, iface := range physical {
		status := "down"
		if iface.LinkUp {
			status = "up"
		}
		w.logger.Info("Interface details", "name", iface.Name, "mac", iface.MAC, "status", status)
	}

	if len(physical) < 1 {
		return nil, fmt.Errorf("at least one network interface is required")
	}

	result := &WizardResult{
		LANIP:     "192.168.1.1",
		LANSubnet: "192.168.1.0/24",
	}

	// Detect WAN
	w.logger.Info("Detecting WAN interface (looking for DHCP)...")
	wanIface, wanIP, err := w.AutoDetectWAN()
	if err != nil {
		w.logger.Warn("Failed to auto-detect WAN", "error", err)
		// Fall back to first interface as WAN
		if len(physical) > 0 {
			result.WANInterface = physical[0].Name
			result.WANMethod = "dhcp"
			w.logger.Info("Using fallback WAN (no DHCP detected)", "interface", result.WANInterface)
		}
	} else {
		result.WANInterface = wanIface.Name
		result.WANMethod = "dhcp"
		result.WANIP = wanIP
		w.logger.Info("WAN Configured", "interface", result.WANInterface, "ip", wanIP)
	}

	// Collect ALL non-WAN interfaces as LAN
	// Also probe each for existing DHCP servers
	var lanInterfaces []string
	dhcpInterfaces := make(map[string]string)
	var firstNonDHCPLAN string

	for _, iface := range physical {
		if iface.Name == result.WANInterface {
			continue
		}
		lanInterfaces = append(lanInterfaces, iface.Name)

		// Probe for DHCP on this interface (short timeout)
		w.logger.Info("Probing LAN for existing DHCP...", "interface", iface.Name)
		success, ip, _ := w.ProbeWAN(iface.Name, 5*time.Second)
		if success {
			w.logger.Info("Found DHCP on LAN interface", "interface", iface.Name, "ip", ip)
			dhcpInterfaces[iface.Name] = ip
		} else {
			w.logger.Info("No DHCP on LAN interface (will serve)", "interface", iface.Name)
			if firstNonDHCPLAN == "" {
				firstNonDHCPLAN = iface.Name
			}
		}
	}
	result.LANInterfaces = lanInterfaces
	result.DHCPInterfaces = dhcpInterfaces

	if len(lanInterfaces) > 0 {
		// Primary LAN interface: prefer one without DHCP (so we can serve)
		if firstNonDHCPLAN != "" {
			result.LANInterface = firstNonDHCPLAN
		} else {
			result.LANInterface = lanInterfaces[0]
		}
		w.logger.Info("LAN Configured", "interfaces", lanInterfaces, "primary", result.LANInterface, "dhcp_clients", len(dhcpInterfaces))
	} else if len(physical) == 1 {
		// Single interface mode - WAN only, no LAN
		w.logger.Info("Single interface mode - no LAN available")
		result.LANInterface = ""
	}

	// Generate config
	w.logger.Info("Generating configuration...")
	if err := w.GenerateConfig(result); err != nil {
		return nil, fmt.Errorf("failed to generate config: %w", err)
	}
	w.logger.Info("Config written", "path", w.configFile)

	w.logger.Info("=== Setup Complete ===")
	w.logger.Info("IMPORTANT: IP forwarding and NAT are DISABLED by default.")
	w.logger.Info("Use the web UI to enable internet access for LAN devices.")

	return result, nil
}
