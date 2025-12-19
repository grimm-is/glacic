package network

import (
	"fmt"
	"log"
	"os/exec"
)

// StartDHCPv6Client starts a DHCPv6 client on the interface.
// Currently wraps 'dhclient -6'.
func StartDHCPv6Client(iface string) error {
	// Check if already running
	if isProcessRunning(fmt.Sprintf("dhclient -6 -P %s", iface)) {
		return nil
	}

	cmd := exec.Command("dhclient", "-6", "-P", "-v", iface)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start dhclient -6 on %s: %w", iface, err)
	}

	// We don't wait for it, it daemonizes or runs in background.
	// Actually dhclient by default daemonizes.
	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("[DHCPv6] dhclient on %s exited with: %v", iface, err)
		}
	}()

	return nil
}

func isProcessRunning(name string) bool {
	// naive check using pgrep
	// TODO: implement better process tracking
	return false
}

// EnableIPv6Forwarding enables global IPv6 forwarding.
func EnableIPv6Forwarding() error {
	return DefaultSystemController.WriteSysctl("/proc/sys/net/ipv6/conf/all/forwarding", "1")
}
