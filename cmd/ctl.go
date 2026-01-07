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
	"grimm.is/glacic/internal/host"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/monitor"
	"grimm.is/glacic/internal/tls"
	"grimm.is/glacic/internal/i18n"
)

// Printer is the global message printer for the CLI
var Printer = i18n.NewCLIPrinter()

// RunCtl runs the privileged control plane daemon with automatic restart on panic.
func RunCtl(configFile string, testMode bool, stateDir string, dryRun bool, listeners map[string]interface{}) error {
	// Loop to restart on panic
	for {
		err := runCtlOnce(configFile, testMode, stateDir, dryRun, listeners)
		if err != nil && strings.HasPrefix(err.Error(), "PANIC:") {
			logging.Error(fmt.Sprintf("Daemon crashed with panic: %v", err))
			logging.Info("Restarting daemon in 1 second...")
			time.Sleep(1 * time.Second)
			continue
		}
		return err
	}
}

// runCtlOnce contains the actual execution logic
func runCtlOnce(configFile string, testMode bool, stateDir string, dryRun bool, listeners map[string]interface{}) (err error) {
	rtCfg := NewCtlRuntimeConfig(configFile, testMode, stateDir, dryRun, listeners)

	// Dry-run mode: generate rules and exit before any daemon setup
	if rtCfg.DryRun {
		return runDryRun(rtCfg.ConfigFile)
	}

	// Check if config file exists
	// For fresh installs, we boot into safe mode to allow setup via Web UI
	noConfig := false
	if _, err := os.Stat(rtCfg.ConfigFile); os.IsNotExist(err) {
		noConfig = true
		logging.Warn(fmt.Sprintf("No configuration file found at %s - booting into safe mode", rtCfg.ConfigFile))
		// Don't error - we'll boot into safe mode below
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
			err = fmt.Errorf("PANIC: %v", r)
			logging.Error(fmt.Sprintf("CRITICAL PANIC in runCtlOnce: %v", r))
			// We do NOT exit here, we return the error to allow the wrapper to restart us
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
	var cfg *config.Config
	if noConfig {
		// Fresh install: use minimal config for safe mode boot
		logging.Info("Fresh install detected - using minimal safe mode configuration")
		cfg = &config.Config{
			SchemaVersion: "1.0",
			IPForwarding:  true, // Required for API sandbox
			API: &config.APIConfig{
				Enabled:   true,
				Listen:    ":8080",
				TLSListen: ":8443",
			},
		}

		// Write default config to disk so CLI users can edit it
		if err := config.SaveFile(cfg, rtCfg.ConfigFile); err != nil {
			logging.Warn(fmt.Sprintf("Could not write default config to %s: %v", rtCfg.ConfigFile, err))
			logging.Info("Configuration can be created via Web UI setup wizard")
		} else {
			logging.Info(fmt.Sprintf("Default configuration written to %s", rtCfg.ConfigFile))
			logging.Info("Edit the file and run 'glacic reload' to apply changes")
		}
	} else {
		var err error
		cfg, err = loadConfiguration(rtCfg)
		if err != nil {
			return err
		}
	}

	// Environment Enforcement
	// 1. Check for port conflicts (Warn only)
	if warnings, err := host.CheckPortConflicts(cfg); err != nil {
		logging.Warn(fmt.Sprintf("Failed to check port conflicts: %v", err))
	} else {
		for _, w := range warnings {
			logging.Warn(w)
		}
	}

	// 2. Enforce Loopback Health (Repair)
	if err := host.EnforceLoopback(cfg); err != nil {
		logging.Error(fmt.Sprintf("Failed to enforce loopback health: %v", err))
		return err
	}

	// Apply service defaults (defaults for mDNS, etc.)
	// Must happen before firewall initialization so default rules are generated.
	applyServiceDefaults(cfg)

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

	// Test mode: run and exit early, only validating core configuration
	// We skip starting the control plane server, device services, etc. to avoid hangs and side effects.
	if rtCfg.TestMode {
		return runTestMode(cfg, netMgr)
	}

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

	// Normal run
	var wg sync.WaitGroup

	if len(cfg.Routes) > 0 {
		netMgr.WaitForLinkIP("eth0", 10)
	}

	monitorLogger := logging.WithComponent("monitor")
	monitor.Start(monitorLogger, cfg.Routes, nil, false)

	logging.Info("Control plane running. Waiting for commands...")

	// Spawn API Server (skip if GLACIC_SKIP_API is set - used by tests that run their own test-api)
	skipAPI := os.Getenv("GLACIC_SKIP_API") == "1"
	if !skipAPI && (cfg.API == nil || cfg.API.Enabled) {
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

	// Spawn Proxy (if API is enabled or always?)
	// Proxy is needed to access API. If API is enabled, Proxy should be too.
	if !skipAPI && (cfg.API == nil || cfg.API.Enabled) {
		exe, err := os.Executable()
		if err == nil {
			wg.Add(1)
			go spawnProxy(monitorsCtx, exe, cfg, &wg)
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

	Printer.Println(rules)
	return nil
}

// spawnAPI runs the API server as a child process and restarts it if it crashes
func spawnAPI(ctx context.Context, exe string, cfg *config.Config, wg *sync.WaitGroup, inheritedFile *os.File) {
	defer wg.Done()

	// Simple backoff for restarts
	failCount := 0
	logging.Info("spawnAPI: Starting API manager loop")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		args := []string{"_api-server"}

		// Force API to listen on Unix socket
		// The proxy will handle the public TCP port
		socketPath := "/var/run/glacic/api/api.sock"
		args = append(args, "-listen", socketPath)

		// Check for flags
		if cfg.API != nil {
			// Ignore cfg.API.Listen for backend (proxy handles it)

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
			Printer.Printf("Clock anchor loaded: %s\n", savedTime.Format(time.RFC3339))

			// If system time is way off, warn the user
			if !clock.IsReasonableTime(now) && clock.IsReasonableTime(savedTime) {
				Printer.Fprintf(os.Stderr, "Warning: System time (%s) appears unreasonable. "+
					"Last known good time was %s. Waiting for NTP sync.\n",
					now.Format(time.RFC3339), savedTime.Format(time.RFC3339))
			}
		}
	} else if !clock.IsReasonableTime(now) {
		Printer.Fprintf(os.Stderr, "Warning: System time (%s) appears unreasonable and no anchor file exists.\n",
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

// spawnProxy runs the TCP proxy as a child process and restarts it if it crashes
func spawnProxy(ctx context.Context, exe string, cfg *config.Config, wg *sync.WaitGroup) {
	defer wg.Done()

	// Backoff for restarts
	failCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		args := []string{"_proxy"}

		// Determine Listen Address (Default :8080)
		listenPort := ":8080"
		useTLS := false
		tlsCert := ""
		tlsKey := ""

		if cfg.Web != nil {
			if cfg.Web.TLSListen != "" {
				listenPort = cfg.Web.TLSListen
				useTLS = true
				// Use explicit certs if provided
				if cfg.Web.TLSCert != "" && cfg.Web.TLSKey != "" {
					tlsCert = cfg.Web.TLSCert
					tlsKey = cfg.Web.TLSKey
				} else {
					// Auto-generate self-signed certs
					certDir := filepath.Join(brand.GetStateDir(), "certs")
					os.MkdirAll(certDir, 0700)
					tlsCert = filepath.Join(certDir, "server.crt")
					tlsKey = filepath.Join(certDir, "server.key")
					if _, err := tls.EnsureCertificate(tlsCert, tlsKey, 365); err != nil {
						logging.Error(fmt.Sprintf("Failed to generate TLS certificates: %v", err))
					}
				}
			} else if cfg.Web.Listen != "" {
				listenPort = cfg.Web.Listen
			}
		} else if cfg.API != nil {
			// Legacy fallback (should be handled by migration, but defensive coding)
			if cfg.API.TLSListen != "" {
				listenPort = cfg.API.TLSListen
				useTLS = true
				if cfg.API.TLSCert != "" && cfg.API.TLSKey != "" {
					tlsCert = cfg.API.TLSCert
					tlsKey = cfg.API.TLSKey
				}
			} else if cfg.API.Listen != "" {
				listenPort = cfg.API.Listen
			}
		}

		// Explicitly use Unix socket path
		targetSock := "/var/run/glacic/api/api.sock"

		args = append(args, "-listen", listenPort, "-target", targetSock)

		// Pass TLS config to proxy
		if useTLS && tlsCert != "" && tlsKey != "" {
			args = append(args, "-tls-cert", tlsCert, "-tls-key", tlsKey)
		}

		// Drop privileges to nobody (if root)
		if os.Geteuid() == 0 {
			args = append(args, "-user", "nobody")
			// Skip chroot when sandbox is disabled (dev/demo mode)
			if cfg.API != nil && cfg.API.DisableSandbox {
				args = append(args, "-no-chroot")
			}
		}

		cmd := exec.Command(exe, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Environment cleanup (same as API)
		newEnv := make([]string, 0, len(os.Environ()))
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "GLACIC_UPGRADE_STANDBY=") {
				newEnv = append(newEnv, e)
			}
		}
		cmd.Env = newEnv

		logging.Info(fmt.Sprintf("Spawning Proxy: %v", args))

		if err := cmd.Start(); err != nil {
			logging.Error(fmt.Sprintf("Failed to start Proxy: %v", err))
			failCount++
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second * time.Duration(failCount)):
				continue
			}
		}

		// Wait / Kill logic (simplified)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				cmd.Process.Signal(syscall.SIGTERM)
			}
			<-done
			return
		case err := <-done:
			if err != nil {
				logging.Error(fmt.Sprintf("Proxy exited with error: %v", err))
			} else {
				logging.Info("Proxy exited cleanly")
			}
		}

		time.Sleep(1 * time.Second)
	}
}
