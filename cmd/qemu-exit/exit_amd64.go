//go:build linux && (amd64 || 386)

package main

import (
	"fmt"
	"os"
	"os/exec"
)

// qemuExit writes to the isa-debug-exit I/O port 0xf4
// QEMU exit code = (value << 1) | 1
// So: value=0 -> exit 1 (success), value=1 -> exit 3 (failure)
func qemuExit(exitCode int) {
	// Try /dev/port first (requires root)
	f, err := os.OpenFile("/dev/port", os.O_WRONLY, 0)
	if err == nil {
		defer f.Close()

		// Seek to port 0xf4 (244 decimal)
		_, err = f.Seek(0xf4, 0)
		if err != nil {
			Printer.Fprintf(os.Stderr, "Failed to seek to port: %v\n", err)
			fallbackPoweroff()
			return
		}

		// Write 0 for success, 1 for failure
		value := byte(0)
		if exitCode != 0 {
			value = 1
		}

		_, err = f.Write([]byte{value})
		if err != nil {
			Printer.Fprintf(os.Stderr, "Failed to write to port: %v\n", err)
			fallbackPoweroff()
		}
		// If write succeeds, QEMU will exit immediately
		return
	}

	Printer.Fprintf(os.Stderr, "Cannot access /dev/port: %v, falling back to poweroff\n", err)
	fallbackPoweroff()
}

func fallbackPoweroff() {
	exec.Command("poweroff", "-f").Run()
}
