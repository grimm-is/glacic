package cmd

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/upgrade"
)

// RunUpgrade initiates a seamless upgrade to a new binary.
func RunUpgrade(newBinaryPath, configPath string) {
	logger := logging.New(logging.DefaultConfig())
	logger.Info("Starting seamless upgrade process")

	// Verify new binary exists and is executable
	info, err := os.Stat(newBinaryPath)
	if err != nil {
		logger.Error("New binary not found", "path", newBinaryPath, "error", err)
		os.Exit(1)
	}
	if info.Mode()&0111 == 0 {
		logger.Error("New binary is not executable", "path", newBinaryPath)
		os.Exit(1)
	}

	// Connect to control plane
	client, err := ctlplane.NewClient()
	if err != nil {
		logger.Error("Failed to connect to control plane (is the daemon running?)", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	// Security: Copy binary to hardcoded secure path
	// This ensures we only execute binaries we intended to place there
	securePath := filepath.Join("/usr/sbin", brand.BinaryName+"_new")
	logger.Info("Staging new binary...", "path", securePath)

	srcFile, err := os.Open(newBinaryPath)
	if err != nil {
		logger.Error("Failed to open source binary", "error", err)
		os.Exit(1)
	}
	defer srcFile.Close()

	// Open destination with executable permissions
	// Check if source and destination are the same
	srcAbs, _ := filepath.Abs(newBinaryPath)
	dstAbs, _ := filepath.Abs(securePath)
	if srcAbs == dstAbs {
		logger.Info("Source binary is already in staging location, skipping copy")

		// Just ensure permissions are correct
		if err := os.Chmod(securePath, 0755); err != nil {
			logger.Error("Failed to set executable permissions", "path", securePath, "error", err)
			os.Exit(1)
		}
	} else {
		dstFile, err := os.OpenFile(securePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			logger.Error("Failed to open staging path (do you have root permissions?)", "path", securePath, "error", err)
			os.Exit(1)
		}

		if _, err := io.Copy(dstFile, srcFile); err != nil {
			dstFile.Close() // Close on error
			logger.Error("Failed to copy binary", "error", err)
			os.Exit(1)
		}
		// Explicitly close after copy to flush write buffers
		if err := dstFile.Close(); err != nil {
			logger.Error("Failed to close staged binary", "error", err)
			os.Exit(1)
		}
	}

	// Calculate checksum of the binary we just copied
	// We re-open the file at securePath to ensure we hash exactly what the server will see
	hashFile, err := os.Open(securePath)
	if err != nil {
		logger.Error("Failed to read back staged binary", "error", err)
		os.Exit(1)
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, hashFile); err != nil {
		hashFile.Close() // Close on error
		logger.Error("Failed to calculate checksum", "error", err)
		os.Exit(1)
	}
	// Explicitly close before upgrading (avoid "text file busy")
	hashFile.Close()

	checksum := hex.EncodeToString(hasher.Sum(nil))
	logger.Info("Calculated checksum", "hash", checksum)

	// Call Upgrade RPC
	if err := client.Upgrade(checksum); err != nil {
		logger.Error("Upgrade failed", "error", err)
		os.Exit(1)
	}

	logger.Info("Upgrade initiated successfully. The daemon is restarting with the new binary.")
	os.Exit(0)
}

// RunUpgradeStandby runs the new process in standby mode during upgrade.
func RunUpgradeStandby(configPath string, uiAssets embed.FS) {
	// Set process name to "glacic" immediately to hide "glacic_new" origin from ps
	if err := SetProcessName("glacic"); err != nil {
		// Ignore error, purely cosmetic
	}

	logger := logging.New(logging.DefaultConfig())
	logger.Info("Starting in upgrade standby mode")

	// Manage PID file with Watchdog (Self-Healing)
	// Key component for upgrade stability: ensures identity is preserved
	runDir := brand.GetRunDir()
	pidFile := filepath.Join(runDir, brand.LowerName+".pid")

	writePID := func() error {
		return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
	}

	if err := writePID(); err != nil {
		logger.Error("Failed to write PID file", "error", err)
		// Continue anyway?
	}

	// Start Keeper/Watchdog
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			// Check if file exists and is correct
			data, err := os.ReadFile(pidFile)
			if err != nil || strings.TrimSpace(string(data)) != fmt.Sprintf("%d", os.Getpid()) {
				// Restore it
				logger.Info("Restoring PID file (detected missing or invalid)")
				_ = writePID()
			}
			time.Sleep(1 * time.Second)
		}
	}()

	// Create upgrade manager
	mgr := upgrade.NewManager(logger)

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Run standby mode - this will:
	// 1. Load state from old process
	// 2. Validate configuration
	// 3. Signal ready to old process
	// 4. Receive listener file descriptors
	if err := mgr.RunStandby(ctx, configPath); err != nil {
		logger.Error("Standby mode failed", "error", err)
		os.Exit(1)
	}

	// Load configuration
	// RunCtl will load the config itself, so we don't strictly need to load it here
	// unless we wanted to validate it before calling RunCtl.
	// But RunCtl does its own validation/loading.

	// Now we have the listeners, start the services
	logger.Info("Taking over from old process (RunUpgradeStandby -> RunCtl)")

	// Prepare listeners map
	listeners := make(map[string]interface{})
	if ctlListener, ok := mgr.GetListener("ctl"); ok {
		logger.Info("Using handed-off Control Plane listener", "addr", ctlListener.Addr())
		listeners["ctl"] = ctlListener
	}
	if apiListener, ok := mgr.GetListener("api"); ok {
		logger.Info("Using handed-off API listener", "addr", apiListener.Addr())
		listeners["api"] = apiListener
		// Note: RunCtl doesn't use listeners["api"] directly for Start(),
		// but uses it in spawnAPI to extract the file descriptor.
	}

	// Cleanup phase: Ensure no orphaned processes from previous run are holding the lock
	// This is required because older versions might leak the 'ip' process or its children.
	// We query the PIDs inside the namespace and SIGKILL them.
	// This ensures the lock file is released and we can start fresh.
	cleanupCmd := exec.Command("ip", "netns", "pids", brand.LowerName+"-api")
	if out, err := cleanupCmd.Output(); err == nil && len(out) > 0 {
		pids := strings.Fields(string(out))
		if len(pids) > 0 {
			logger.Info("Cleaning up orphaned API processes from previous instance", "count", len(pids))
			for _, pidStr := range pids {
				if pid, err := strconv.Atoi(pidStr); err == nil {
					p, _ := os.FindProcess(pid)
					p.Signal(syscall.SIGKILL)
				}
			}
			// Give them a moment to die and for 'ip' wrapper to exit/release lock
			time.Sleep(1 * time.Second)
		}
	}

	// Finalization: Rename binary BEFORE calling RunCtl
	// spawnAPI uses os.Executable() to find the binary to run
	// We want it to find /usr/sbin/glacic, not /usr/sbin/glacic_new
	executable, err := os.Executable()
	if err != nil {
		logger.Error("Failed to get executable path", "error", err)
		os.Exit(1)
	}

	// Calculate target path using brand name
	targetPath := filepath.Join(filepath.Dir(executable), brand.BinaryName)

	if targetPath != executable {
		logger.Info("Finalizing upgrade: renaming binary", "source", executable, "target", targetPath)

		// Remove old binary first (handles ETXTBSY if old process still has it mapped)
		if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
			logger.Warn("Failed to remove old binary (may still be in use)", "error", err)
		}

		// Rename new binary to target
		if err := os.Rename(executable, targetPath); err != nil {
			logger.Error("Failed to rename binary", "error", err)
			// Continue anyway - API will work but with wrong path in logs
		} else {
			logger.Info("Binary renamed successfully")
		}
	} else {
		logger.Info("Binary rename skipped (already correct path)", "path", targetPath)
	}

	// Debug: Log listener count
	logger.Info("Calling RunCtl with injected listeners", "count", len(listeners), "hasCtl", listeners["ctl"] != nil, "hasApi", listeners["api"] != nil)

	// Call RunCtl with injected listeners
	// This unifies the code path, ensuring full functionality (Network Manager, Watchdog, etc.)
	if err := RunCtl(configPath, false, "", false, listeners); err != nil {
		logger.Error("Control plane failed", "error", err)
		os.Exit(1)
	}
}
