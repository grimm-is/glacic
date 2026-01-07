package cmd

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

const (
	// crashThreshold is the time window to consider a crash "too fast"
	crashThreshold = 5 * time.Second
	// maxFastCrashes is the number of fast crashes allowed before backing off
	maxFastCrashes = 3
)

// RunMonitor runs the "monitor" command, which acts as a supervisor for the control plane.
// It starts "glacic ctl" and restarts it if it crashes.
// If "glacic ctl" exits cleanly (code 0), the monitor also exits (assuming intentional stop).
func RunMonitor(args []string) {
	Printer.Println("Starting Glacic Self-Healing Monitor...")

	fastCrashCount := 0
	lastStartTime := time.Time{}

	// Setup signal handling - forward to child
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	for {
		// Determine binary path (usually ourselves)
		binPath, err := os.Executable()
		if err != nil {
			Printer.Fprintf(os.Stderr, "Failed to determine executable path: %v\n", err)
			os.Exit(1)
		}

		// Prepare command: "glacic ctl [args...]"
		cmdArgs := append([]string{"ctl"}, args...)
		cmd := exec.Command(binPath, cmdArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		// Start the process
		if err := cmd.Start(); err != nil {
			Printer.Fprintf(os.Stderr, "Monitor: Failed to start child: %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}

		Printer.Printf("Monitor: Started glacic ctl (pid=%d)\n", cmd.Process.Pid)
		lastStartTime = time.Now()

		// Monitor loop for this instance
		stopCh := make(chan struct{})

		// Signal forwarder routine
		go func() {
			select {
			case sig := <-sigCh:
				// Forward signal to child
				if cmd.Process != nil {
					Printer.Printf("Monitor: Forwarding signal %v to child...\n", sig)
					cmd.Process.Signal(sig)
				}
			case <-stopCh:
				// Child exited, stop forwarder
				return
			}
		}()

		// Wait for exit
		err = cmd.Wait()
		close(stopCh) // Stop signal forwarder

		// Analyze exit
		if err == nil {
			Printer.Println("Monitor: Child exited cleanly. Shutting down monitor.")
			os.Exit(0)
		}

		// Exit code handling
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}

		// Check for valid exit codes that might not return nil error (though 0 usually does)
		if exitCode == 0 {
			Printer.Println("Monitor: Child exited with code 0. Shutting down monitor.")
			os.Exit(0)
		}

		Printer.Printf("Monitor: Child crashed with exit code %d (err: %v). Restarting...\n", exitCode, err)

		// Crash Loop Detection
		if time.Since(lastStartTime) < crashThreshold {
			fastCrashCount++
		} else {
			fastCrashCount = 0
		}

		if fastCrashCount >= maxFastCrashes {
			Printer.Printf("Monitor: Detected crash loop (%d fast crashes). Backing off for 30s...\n", fastCrashCount)
			fastCrashCount = 0 // Reset after backoff
			time.Sleep(30 * time.Second)
		} else {
			// Small delay to prevent tight spin
			time.Sleep(1 * time.Second)
		}
	}
}
