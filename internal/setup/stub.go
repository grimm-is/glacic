//go:build !linux
// +build !linux

// Package setup provides the first-run setup wizard functionality.
package setup

import (
	"fmt"
	"runtime"
	"time"
)

// ErrNotSupported is returned when setup operations are attempted on non-Linux systems.
var ErrNotSupported = fmt.Errorf("setup wizard not supported on %s", runtime.GOOS)

// DetectHardware scans for network interfaces (stub for non-Linux)
func (w *Wizard) DetectHardware() error {
	return ErrNotSupported
}

// ProbeWAN attempts DHCP on an interface (stub for non-Linux)
func (w *Wizard) ProbeWAN(ifaceName string, timeout time.Duration) (bool, string, error) {
	return false, "", ErrNotSupported
}

// AutoDetectWAN tries each interface to find one with DHCP (stub for non-Linux)
func (w *Wizard) AutoDetectWAN() (*InterfaceInfo, string, error) {
	return nil, "", ErrNotSupported
}

// RunAutoSetup runs the automatic setup process (stub for non-Linux)
func (w *Wizard) RunAutoSetup() (*WizardResult, error) {
	return nil, ErrNotSupported
}

// DetectHardware detects network hardware (stub for non-Linux)
func DetectHardware() (*DetectedHardware, error) {
	return nil, ErrNotSupported
}
