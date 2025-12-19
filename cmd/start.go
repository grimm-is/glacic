package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"grimm.is/glacic/internal/brand"
)

// RunStart starts the control plane daemon in the background
func RunStart(configFile string) error {
	// 1. Check for existing PID file
	runDir := brand.GetRunDir()
	pidFile := filepath.Join(runDir, brand.LowerName+".pid")

	if _, err := os.Stat(pidFile); err == nil {
		// File exists, check if process is actually running
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, err := strconv.Atoi(string(data))
			if err == nil {
				// Check if process exists by sending signal 0
				process, err := os.FindProcess(pid)
				if err == nil {
					if err := process.Signal(syscall.Signal(0)); err == nil {
						return fmt.Errorf("process already running (PID: %d)", pid)
					}
				}
			}
		}
		// If we get here, PID file exists but process is dead. Warn and cleanup.
		fmt.Printf("Warning: Removing stale PID file %s\n", pidFile)
		os.Remove(pidFile)
	}

	// 2. Prepare command
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// We start 'ctl' command. 'ctl' will handle spawning 'api'.
	args := []string{"ctl"}
	if configFile != "" {
		args = append(args, configFile)
	}

	cmd := exec.Command(exe, args...)

	// 3. Setup logging
	logDir := brand.GetLogDir()
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}
	logFile := filepath.Join(logDir, brand.LowerName+".log")

	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	// We don't close logF here, it stays open for the child process?
	// Actually cmd.Stdout assignment duplicates the fd. We can close it in parent after Start.

	cmd.Stdout = logF
	cmd.Stderr = logF

	// 4. Detach process
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// 5. Start
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	fmt.Printf("Started %s (PID: %d)\n", brand.Name, cmd.Process.Pid)
	fmt.Printf("Logs: %s\n", logFile)

	// Wait a moment to ensure it doesn't crash immediately (optional but nice)
	time.Sleep(100 * time.Millisecond)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return fmt.Errorf("process exited immediately (check logs)")
	}

	return nil
}
