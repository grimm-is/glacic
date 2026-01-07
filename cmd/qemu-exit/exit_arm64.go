//go:build linux && arm64

package main

import (
	"os"
	"os/exec"
)

// qemuExit on ARM64 - semihosting is complex and unreliable
// Just use poweroff and rely on TAP output parsing for test results
func qemuExit(exitCode int) {
	// Write exit code to a file that can be checked by the host
	// This is a simple fallback since semihosting is tricky
	if exitCode != 0 {
		os.WriteFile("/tmp/test_failed", []byte("1"), 0644)
	}

	// Poweroff - QEMU will exit with 0, host parses TAP output
	exec.Command("poweroff", "-f").Run()
}
