package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"
)

// RunReload triggers a configuration reload on the running daemon.
// It first validates the configuration file to prevent bad loads.
func RunReload(configFile string) error {
	// 1. Validate the configuration first
	Printer.Printf("Validating configuration: %s\n", configFile)
	_, err := config.LoadFileWithOptions(configFile, config.DefaultLoadOptions())
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	Printer.Println("Configuration is valid.")

	// 2. Find the PID of the running daemon
	runDir := brand.GetRunDir()
	pidFile := filepath.Join(runDir, brand.LowerName+".pid")

	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("failed to read PID file %s: %w (is the daemon running?)", pidFile, err)
	}

	pidStr := string(data)
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("invalid PID in file: %s", pidStr)
	}

	// 3. Send SIGHUP
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	Printer.Printf("Sending SIGHUP to process %d...\n", pid)
	if err := process.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("failed to signal process: %w", err)
	}

	Printer.Println("Reload signal sent successfully.")
	return nil
}
