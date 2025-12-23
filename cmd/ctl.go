package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/config"
	fw "grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/monitor"
)

// RunCtl runs the privileged control plane daemon.
// This process must run as root and handles all privileged operations:
// - Network interface configuration (netlink)
// - Firewall rules (nftables)
// - DHCP server/client (raw sockets, port 67)
// - DNS server (port 53)
// - Routing
// listeners argument allows injecting pre-opened listeners (for zero-downtime upgrades)
func RunCtl(configFile string, testMode bool, stateDir string, dryRun bool, listeners map[string]interface{}) error {
	rtCfg := NewCtlRuntimeConfig(configFile, testMode, stateDir, dryRun, listeners)

	// Dry-run mode: generate rules and exit before any daemon setup
	if rtCfg.DryRun {
		return runDryRun(rtCfg.ConfigFile)
	}

	// Check if config file exists BEFORE redirecting stderr to log file
	// This ensures the error message goes to the terminal, not a log file
	if _, err := os.Stat(rtCfg.ConfigFile); os.IsNotExist(err) {
		return fmt.Errorf("configuration file not found: %s\n\n"+
			"To create a configuration file, run:\n"+
			"  %s setup\n\n"+
			"Or create a minimal config manually:\n"+
			"  mkdir -p /etc/glacic\n"+
			"  echo 'schema_version = \"1.0\"' > /etc/glacic/glacic.hcl",
			rtCfg.ConfigFile, brand.BinaryName)
	}

	// Context for services
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Context for monitors (watchdog, API auto-restart) that should be stopped during upgrade
	monitorsCtx, monitorsCancel := context.WithCancel(ctx)
	defer monitorsCancel()

	// Global Panic Recovery
	defer func() {
		if r := recover(); r != nil {
			logging.Error(fmt.Sprintf("CRITICAL PANIC in RunCtl: %v", r))
			os.Exit(1)
		}
	}()

	// Initialize logging
	initializeCtlLogging()

	// Ensure state directory exists
	if err := os.MkdirAll(brand.GetStateDir(), 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Setup PID file with watchdog
	pidCleanup, err := setupPIDFile(monitorsCtx)
	if err != nil {
		return err
	}
	defer pidCleanup()

	// System time sanity check
	initClockAnchor()

	// Load configuration with crash loop protection
	cfg, err := loadConfiguration(rtCfg)
	if err != nil {
		return err
	}

	// Configure syslog if enabled
	configureSyslog(cfg)

	// Initialize state store
	stateStore, err := initializeStateStore(rtCfg, cfg)
	if err != nil {
		return err
	}
	defer stateStore.Close()

	// Configure replication
	replicationStop := configureReplication(cfg, stateStore)
	defer replicationStop()

	// Initialize network stack
	netMgr, err := initializeNetworkStack(cfg)
	if err != nil {
		return err
	}
	defer netMgr.StopDHCPClientManager()

	// Apply static routes
	if err := applyStaticRoutes(cfg, netMgr); err != nil {
		logging.Error(err.Error())
		os.Exit(1)
	}

	// Initialize core services
	services, err := initializeCoreServices(ctx, cfg, netMgr, stateStore)
	if err != nil {
		return err
	}
	defer services.Shutdown()

	// Start control plane RPC server
	if err := startControlPlaneServer(cfg, configFile, netMgr, services, listeners); err != nil {
		logging.Error(err.Error())
		os.Exit(1)
	}
	services.ctlServer.SetDisarmFunc(monitorsCancel)

	// Initialize additional services
	initializeAdditionalServices(ctx, cfg, services)

	// Initialize device management
	initializeDeviceServices(ctx, cfg, services)

	// Initialize learning service
	initializeLearningService(cfg, services)

	// Firewall integrity monitoring
	if cfg.Features != nil && cfg.Features.IntegrityMonitoring && services.fwMgr != nil {
		go services.fwMgr.MonitorIntegrity(ctx, cfg)
	}

	// Test mode: run and exit
	if rtCfg.TestMode {
		return runTestMode(cfg, netMgr)
	}

	// Normal run
	var wg sync.WaitGroup

	if len(cfg.Routes) > 0 {
		netMgr.WaitForLinkIP("eth0", 10)
	}

	monitorLogger := logging.WithComponent("monitor")
	monitor.Start(monitorLogger, cfg.Routes, nil, false)

	logging.Info("Control plane running. Waiting for commands...")

	// Spawn API Server
	if cfg.API == nil || cfg.API.Enabled {
		exe, err := os.Executable()
		if err == nil {
			wg.Add(1)
			var apiListenerFile *os.File
			if listeners != nil && listeners["api"] != nil {
				if fileL, ok := listeners["api"].(interface{ File() (*os.File, error) }); ok {
					f, err := fileL.File()
					if err == nil {
						apiListenerFile = f
						logging.Info("Passing inherited API listener to child process")
					} else {
						logging.Error("Failed to get file from API listener", "error", err)
					}
				}
			}
			go spawnAPI(monitorsCtx, exe, cfg, &wg, apiListenerFile)
		} else {
			logging.Error(fmt.Sprintf("Failed to determine executable path, cannot spawn API: %v", err))
		}
	}

	// Run main event loop
	return runMainEventLoop(ctx, cancel, configFile, services.ctlServer, &wg)
}

// runDryRun generates firewall rules and exits.
func runDryRun(configFile string) error {
	result, err := config.LoadFileWithOptions(configFile, config.DefaultLoadOptions())
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	dryRunLogger := logging.WithComponent("firewall")
	fwMgr := fw.NewManagerWithConn(nil, dryRunLogger, "")
	rules, err := fwMgr.GenerateRules(fw.FromGlobalConfig(result.Config))
	if err != nil {
		return fmt.Errorf("dry-run generation failed: %w", err)
	}

	fmt.Println(rules)
	return nil
}

// spawnAPI runs the API server as a child process and restarts it if it crashes
func spawnAPI(ctx context.Context, exe string, cfg *config.Config, wg *sync.WaitGroup, inheritedFile *os.File) {
	defer wg.Done()

	// Simple backoff for restarts
	failCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		args := []string{"_api-server"}

		// Check for flags
		if cfg.API != nil {
			if cfg.API.Listen != "" {
				args = append(args, "-listen", cfg.API.Listen)
			}
			// Only root processes can change user
			if os.Geteuid() == 0 {
				args = append(args, "-user", "nobody")
			}
		} else {
			// minimal defaults
			if os.Geteuid() == 0 {
				args = append(args, "-user", "nobody")
			}
		}

		cmd := exec.Command(exe, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Build clean environment
		// We must explicitly ensure GLACIC_UPGRADE_STANDBY is NOT passed to child
		// to prevent recursion loops where 'glacic api' thinks it's an upgrade standby.
		newEnv := make([]string, 0, len(os.Environ()))
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "GLACIC_UPGRADE_STANDBY=") {
				newEnv = append(newEnv, e)
			}
		}
		cmd.Env = newEnv

		if inheritedFile != nil {
			cmd.ExtraFiles = []*os.File{inheritedFile}
		}

		// Create a separate mechanism to kill on context cancellation
		killCh := make(chan struct{})
		// Wait for context cancellation in background
		go func() {
			select {
			case <-ctx.Done():
				if cmd.Process != nil {
					logging.Info("Context cancelled, sending SIGTERM to API server...")
					// Try graceful shutdown first
					cmd.Process.Signal(syscall.SIGTERM)

					// Force kill after timeout if still running
					time.Sleep(5 * time.Second)
					if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
						logging.Warn("API server did not exit in time, force killing...")
						cmd.Process.Kill()
					}
				}
			case <-killCh:
				// Process finished naturally, stop waiting
				return
			}
		}()

		if cfg.API == nil || !cfg.API.Enabled {
			// Should actuall check "DisableSandbox" here?
			// The original code passed no flags for sandbox bypass, checking environment.
			// But wait, the environment is inherited.
			// Let's stick to original behavior for args.
		}

		logging.Info(fmt.Sprintf("Spawning API server: %v", args), "executable", exe)
		if err := cmd.Start(); err != nil {
			logging.Error(fmt.Sprintf("Failed to start API server: %v", err))
			failCount++
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second * time.Duration(failCount)):
				continue
			}
		}

		// Wait for it to exit
		err := cmd.Wait()
		close(killCh) // Stop the background killer

		// If context is done, we expect it to be killed or exiting
		if ctx.Err() != nil {
			logging.Info("API server shutting down (context cancelled)")
			return
		}

		if err != nil {
			logging.Error(fmt.Sprintf("API server exited with error: %v. Restarting...", err))
		} else {
			logging.Info("API server exited cleanly.")
			// Check context again in case it happened during cleanup
			if ctx.Err() != nil {
				return
			}
		}

		// Reset failCount? Or keep incrementing?
		// Sleep a bit before restart
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
}

var clockAnchorFile = filepath.Join(brand.GetStateDir(), "clock_anchor")

// initClockAnchor loads the clock anchor from persistent storage and checks system time sanity.
// If system time is unreasonable (before 2023), it logs a warning.
// The anchor is saved lazily (via OnWrite hook) to avoid flash wear.
func initClockAnchor() {
	now := time.Now()

	// Load saved anchor time for reference
	data, err := os.ReadFile(clockAnchorFile)
	if err == nil {
		var savedTime time.Time
		if err := savedTime.UnmarshalText(data); err == nil {
			fmt.Printf("Clock anchor loaded: %s\n", savedTime.Format(time.RFC3339))

			// If system time is way off, warn the user
			if !clock.IsReasonableTime(now) && clock.IsReasonableTime(savedTime) {
				fmt.Fprintf(os.Stderr, "Warning: System time (%s) appears unreasonable. "+
					"Last known good time was %s. Waiting for NTP sync.\n",
					now.Format(time.RFC3339), savedTime.Format(time.RFC3339))
			}
		}
	} else if !clock.IsReasonableTime(now) {
		fmt.Fprintf(os.Stderr, "Warning: System time (%s) appears unreasonable and no anchor file exists.\n",
			now.Format(time.RFC3339))
	}
}

// SaveClockAnchor persists the current time to storage.
// Called by state store OnWrite hook to piggyback anchor updates on other writes.
func SaveClockAnchor() {
	now := time.Now()
	// Only save if time is reasonable (don't persist bad timestamps)
	if !clock.IsReasonableTime(now) {
		return
	}
	data, err := now.MarshalText()
	if err != nil {
		return
	}
	_ = os.WriteFile(clockAnchorFile, data, 0644)
}
