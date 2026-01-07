//go:build !linux

package firewall

import (
	"fmt"
)

// GetConntrackEntries is a stub for non-Linux platforms.
// Connection tracking is only supported on Linux via netlink.
func GetConntrackEntries() ([]ConntrackEntry, error) {
	return nil, fmt.Errorf("conntrack not supported on this platform")
}
