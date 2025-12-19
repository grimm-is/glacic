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

// isVirtualInterface checks if an interface is virtual
// Returns false for VM NICs (virtio, e1000, etc.) which appear in /sys/devices/virtual
// but should be treated as physical interfaces for setup purposes
func isVirtualInterface(name string) bool {
	// Check if it's in /sys/devices/virtual/net/
	virtualPath := filepath.Join("/sys/devices/virtual/net", name)
	_, err := os.Stat(virtualPath)
	if err != nil {
		return false // Not virtual
	}

	// It's in virtual path, but check if it has a driver that makes it a real NIC
	// VM network adapters have drivers even though they're "virtual"
	driverPath := filepath.Join("/sys/class/net", name, "device/driver/module")
	_, err = os.Stat(driverPath)
	if err == nil {
		// Has a device driver - treat as physical (VM NIC like virtio_net, e1000)
		return false
	}

	// No driver - truly virtual (loopback, veth, tap, etc.)
	return true
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
