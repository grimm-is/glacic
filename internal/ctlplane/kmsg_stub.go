//go:build !linux

package ctlplane

import (
	"fmt"
)

// readKmsg is a stub for non-Linux platforms.
// Kernel message reading is only supported on Linux via /dev/kmsg.
func readKmsg(limit int) ([]LogEntry, error) {
	return nil, fmt.Errorf("kernel messages not available on this platform")
}

// readLastLines is a stub for non-Linux platforms.
func readLastLines(path string, n int, source LogSource) ([]LogEntry, error) {
	return nil, fmt.Errorf("log reading not implemented on this platform")
}
