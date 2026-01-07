package network

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
)

// StartDHCPClient starts a DHCP client on the interface for IPv4.
// It wraps udhcpc (standard in Alpine) or dhclient (Debian/others).
func StartDHCPClient(iface string) error {
	// First, check for udhcpc (Alpine/BusyBox)
	if _, err := exec.LookPath("udhcpc"); err == nil {
		return startUDHCPC(iface)
	}

	// Fallback to dhclient
	if _, err := exec.LookPath("dhclient"); err == nil {
		return startDhclient(iface)
	}

	return fmt.Errorf("no DHCP client found (tried udhcpc, dhclient)")
}

// StopDHCPClient stops any running DHCP client for the interface.
func StopDHCPClient(iface string) error {
	safeIface := regexp.QuoteMeta(iface)
	// Kill udhcpc
	_ = exec.Command("pkill", "-f", fmt.Sprintf("udhcpc .* -i %s", safeIface)).Run()
	// Kill dhclient
	_ = exec.Command("pkill", "-f", fmt.Sprintf("dhclient .* %s", safeIface)).Run()
	return nil
}

func startUDHCPC(iface string) error {
	// Kill existing instance for this interface
	// Pattern to match: udhcpc -i <iface>
	// Escape interface name for regex (e.g. "." -> "\.")
	safeIface := regexp.QuoteMeta(iface)
	_ = exec.Command("pkill", "-f", fmt.Sprintf("udhcpc .* -i %s", safeIface)).Run()

	args := []string{"-i", iface, "-b"} // -b: Background if lease not obtained immediately

	// Check for default script
	scriptPath := "/usr/share/udhcpc/default.script"
	if _, err := os.Stat(scriptPath); err == nil {
		args = append(args, "-s", scriptPath)
	}

	cmd := exec.Command("udhcpc", args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start udhcpc on %s: %w", iface, err)
	}

	// udhcpc with -b might fork or stay running. We don't wait for it here
	// because we want the supervisor/manager to continue.
	// We release the resources.
	go func() {
		_ = cmd.Wait()
	}()

	log.Printf("[network] Started IPv4 DHCP client (udhcpc) on %s", iface)
	return nil
}

func startDhclient(iface string) error {
	// Kill any existing dhclient for this interface before starting a new one.
	// This is the "check" - we ensure no duplicate instances by killing first.
	safeIface := regexp.QuoteMeta(iface)
	_ = exec.Command("pkill", "-f", fmt.Sprintf("dhclient .* %s", safeIface)).Run()

	cmd := exec.Command("dhclient", "-4", "-v", iface)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start dhclient on %s: %w", iface, err)
	}

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("[network] dhclient on %s exited: %v", iface, err)
		}
	}()

	log.Printf("[network] Started IPv4 DHCP client (dhclient) on %s", iface)
	return nil
}
