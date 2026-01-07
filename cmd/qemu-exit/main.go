//go:build linux

// qemu-exit: Signal exit code from QEMU guest to host
//
// Usage: qemu-exit [exit_code]
//
// On x86_64 with -device isa-debug-exit: writes to I/O port 0xf4
// On aarch64 with -semihosting: uses HLT #0xF000 semihosting call
package main

import (
	"fmt"
	"os"
	"strconv"
)

func main() {
	exitCode := 0
	if len(os.Args) > 1 {
		var err error
		exitCode, err = strconv.Atoi(os.Args[1])
		if err != nil {
			Printer.Fprintf(os.Stderr, "Invalid exit code: %s\n", os.Args[1])
			os.Exit(1)
		}
	}

	qemuExit(exitCode)
}
