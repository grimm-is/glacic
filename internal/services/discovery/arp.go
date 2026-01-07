package discovery

import (
	"bufio"
	"os"
	"strings"
)

// getMACFromARP attempts to resolve an IP to a MAC address using the system ARP table
func getMACFromARP(ip string) string {
	// Linux /proc/net/arp format:
	// IP address       HW type     Flags       HW address            Mask     Device
	// 192.168.1.1      0x1         0x2         00:11:22:33:44:55     *        eth0

	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Skip header
	if scanner.Scan() {
		// checks header
	}

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 {
			if fields[0] == ip {
				// Check flags? 0x2 is valid (complete), 0x0 is incomplete
				// but we just return what we have if it looks like a MAC
				mac := fields[3]
				if mac != "00:00:00:00:00:00" && len(mac) == 17 {
					return mac
				}
			}
		}
	}
	return ""
}
