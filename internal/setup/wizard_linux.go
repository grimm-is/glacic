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

// AutoDetectWAN tries each interface to find one with DHCP
func (w *Wizard) AutoDetectWAN() (*InterfaceInfo, string, error) {
	if w.hardware == nil {
		if err := w.DetectHardware(); err != nil {
			return nil, "", err
		}
	}

	physical := w.hardware.GetPhysicalInterfaces()

	for i := range physical {
		iface := &physical[i]
		w.logger.Info("Probing for DHCP...", "interface", iface.Name)

		success, ip, err := w.ProbeWAN(iface.Name, 10*time.Second)
		if err != nil {
			w.logger.Error("DHCP probe error", "error", err)
			continue
		}

		if success {
			w.logger.Info("Found DHCP!", "ip", ip)
			iface.SuggestWAN = true
			return iface, ip, nil
		}

		w.logger.Info("No DHCP response")
	}

	return nil, "", fmt.Errorf("no WAN interface detected")
}

// RunAutoSetup performs automatic setup
func (w *Wizard) RunAutoSetup() (*WizardResult, error) {
	w.logger.Info("=== Firewall Setup Wizard ===")

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

	// Select LAN interface
	lanIface := w.hardware.FindBestLANCandidate(result.WANInterface)
	if lanIface != nil {
		result.LANInterface = lanIface.Name
		w.logger.Info("LAN Configured", "interface", result.LANInterface, "ip", result.LANIP)
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
