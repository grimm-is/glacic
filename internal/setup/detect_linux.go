//go:build linux
// +build linux

// Package setup provides the first-run setup wizard functionality.
package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// DetectHardware scans for available network interfaces
func DetectHardware() (*DetectedHardware, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %w", err)
	}

	hw := &DetectedHardware{
		Interfaces: make([]InterfaceInfo, 0),
	}

	for _, link := range links {
		name := link.Attrs().Name

		// Skip excluded interfaces
		if shouldExclude(name) {
			continue
		}

		info := InterfaceInfo{
			Name:      name,
			MAC:       link.Attrs().HardwareAddr.String(),
			LinkUp:    link.Attrs().OperState == netlink.OperUp,
			IsVirtual: isVirtualInterface(name),
		}

		// Detect interface type
		switch link.Type() {
		case "bridge":
			info.IsBridge = true
		case "bond":
			info.IsBond = true
		case "vlan":
			info.IsVLAN = true
		}

		// Get driver info
		info.Driver = getDriverInfo(name)

		// Get current IPs
		addrs, err := netlink.AddrList(link, unix.AF_INET)
		if err == nil {
			for _, addr := range addrs {
				info.IPs = append(info.IPs, addr.IPNet.String())
			}
		}

		// Get link speed if available
		info.Speed = getLinkSpeed(name)

		hw.Interfaces = append(hw.Interfaces, info)
	}

	return hw, nil
}

// getLinkSpeed reads the link speed for an interface
func getLinkSpeed(name string) string {
	speedPath := filepath.Join("/sys/class/net", name, "speed")
	data, err := os.ReadFile(speedPath)
	if err != nil {
		return ""
	}
	speed := strings.TrimSpace(string(data))
	if speed == "" || speed == "-1" {
		return ""
	}
	return speed + " Mbps"
}

// isVirtualInterface checks if an interface is virtual (and thus not suitable for WAN/LAN setup)
// We use a permissive approach: if an interface has a MAC address and isn't in the
// excluded list (lo, veth, tun, tap, etc.), it's considered "physical" for setup purposes.
// This works in VMs, containers, and bare metal where interface paths vary.
func isVirtualInterface(name string) bool {
	// Check if interface has a valid MAC address
	// Interfaces without MACs (like loopback) are truly virtual
	macPath := filepath.Join("/sys/class/net", name, "address")
	data, err := os.ReadFile(macPath)
	if err != nil {
		return true // Can't read MAC - treat as virtual
	}

	mac := strings.TrimSpace(string(data))
	// Loopback and some virtual interfaces have all-zeros MAC
	if mac == "" || mac == "00:00:00:00:00:00" {
		return true
	}

	// Has a real MAC address - usable for networking
	return false
}

// getDriverInfo tries to find the driver name
func getDriverInfo(name string) string {
	driverPath := filepath.Join("/sys/class/net", name, "device/driver/module")
	target, err := os.Readlink(driverPath)
	if err == nil {
		return filepath.Base(target)
	}
	return ""
}
