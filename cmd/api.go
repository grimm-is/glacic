package cmd

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"strings"

	"grimm.is/glacic/internal/api"
	"grimm.is/glacic/internal/api/storage"
	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/pki"
	"grimm.is/glacic/internal/state"
	"grimm.is/glacic/internal/tls"
)

// tlsFilteredLogger filters out noisy TLS handshake errors from self-signed certificates.
// These errors are expected and benign when clients don't trust our certificate.
type tlsFilteredLogger struct {
	lastMsg   string
	lastTime  time.Time
	rateLimit time.Duration
}

func (l *tlsFilteredLogger) Write(p []byte) (n int, err error) {
	msg := string(p)

	// Suppress TLS handshake errors from untrusted certificates
	if strings.Contains(msg, "TLS handshake error") &&
		(strings.Contains(msg, "unknown certificate") ||
			strings.Contains(msg, "certificate required") ||
			strings.Contains(msg, "bad certificate")) {
		// Rate limit: only log once per 60 seconds max
		if l.lastMsg == "TLS" && time.Since(l.lastTime) < l.rateLimit {
			return len(p), nil // Suppress duplicate
		}
		l.lastMsg = "TLS"
		l.lastTime = time.Now()
		logging.Warn("TLS handshake failed (client doesn't trust our certificate) - this is normal with self-signed certs")
		return len(p), nil
	}

	// Pass through other messages
	logging.Warn(strings.TrimSpace(msg))
	return len(p), nil
}

func newTLSFilteredLogger() *log.Logger {
	return log.New(&tlsFilteredLogger{rateLimit: 60 * time.Second}, "", 0)
}

// RunAPI runs the unprivileged API server
// This process should run as a non-root user and communicates
// with the control plane via RPC over Unix socket
func RunAPI(uiAssets embed.FS, listenAddr string, dropUser string) {
	// Check for inherited listener (FD 3) - implementing socket handoff
	var inheritedListener net.Listener
	listenerFile := os.NewFile(3, "listener") // FD 3 is the first ExtraFile

	// Check if FD is valid by attempting to turn it into a listener
	if l, err := net.FileListener(listenerFile); err == nil {
		inheritedListener = l
		// If we are getting a specific listener, logging it is good
		if tcpAddr, ok := l.Addr().(*net.TCPAddr); ok {
			// We can update listenAddr to match truth
			listenAddr = fmt.Sprintf(":%d", tcpAddr.Port)
		}
		// Don't log here yet, logger not init
	}

	// Check for sandbox bypass (for testing)
	noSandbox := os.Getenv("GLACIC_NO_SANDBOX") == "1"

	// Initialize structured logging
	logCfg := logging.DefaultConfig()
	logCfg.Output = os.Stderr
	logger := logging.New(logCfg).WithComponent("API")
	logging.SetDefault(logger)

	// 2. Initialize dependencies (Host Side - for both Setup and Runtime)
	// Connect to control plane (opens socket FD)
	var client *ctlplane.Client
	var err error

	// Retry connection for up to 30 seconds
	for i := 0; i < 30; i++ {
		client, err = ctlplane.NewClient()
		if err == nil {
			break
		}
		if i%5 == 0 { // Log every 5 seconds
			logging.Info(fmt.Sprintf("Waiting for control plane... (%v)", err))
		}
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		logging.Error(fmt.Sprintf("Failed to connect to control plane after 30s: %v", err))
		logging.Info("Make sure 'glacic ctl' is running first.")
		os.Exit(1)
	}
	defer client.Close()

	// Verify connection by getting status
	status, err := client.GetStatus()
	if err != nil {
		logging.Error(fmt.Sprintf("Failed to get status from control plane: %v", err))
		os.Exit(1)
	}
	logging.Info(fmt.Sprintf("Connected to control plane (uptime: %s)", status.Uptime))

	// Get config from control plane
	cfg, err := client.GetConfig()
	if err != nil {
		logging.Error(fmt.Sprintf("Failed to get config from control plane: %v", err))
		os.Exit(1)
	}

	// 0. Setup Network Namespace Isolation (Linux Root Only)
	// Default to sandbox enabled unless explicitly disabled in config or env
	sandboxEnabled := true
	if cfg.API != nil && cfg.API.DisableSandbox {
		sandboxEnabled = false
	}
	// Fallback for env var bypass (legacy/testing)
	if noSandbox {
		sandboxEnabled = false
	}

	// Capture isolation state early (before chroot might break it)
	isolated := false
	if runtime.GOOS == "linux" {
		isolated = isIsolated()
	}

	// Single Instance Lock (Parent Only, before netns setup)
	if runtime.GOOS == "linux" && syscall.Geteuid() == 0 && sandboxEnabled && !isolated {
		lockFile := "/var/run/glacic_api.lock"
		// Use O_CLOEXEC to prevent inheritance by child processes (netns/ip)
		fd, err := syscall.Open(lockFile, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC, 0600)
		if err != nil {
			log.Printf("Failed to open lock file %s: %v", lockFile, err)
			os.Exit(1)
		}
		// Try to acquire lock (Non-blocking) with retry
		// During upgrade, the old process might still be shutting down.
		// We give it 30 seconds to release the lock.
		var flopErr error
		for i := 0; i < 60; i++ {
			flopErr = syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB)
			if flopErr == nil {
				break
			}
			if flopErr != syscall.EWOULDBLOCK && flopErr != syscall.EAGAIN {
				break // Fatal error
			}
			if i%5 == 0 { // Log every 2.5s roughly
				// We use fmt here because logger might not be fully init or we want raw output
				fmt.Printf("Waiting for previous API instance to release lock... (%d/60)\n", i)
			}
			// Wait and retry
			time.Sleep(500 * time.Millisecond)
		}

		if flopErr != nil {
			if flopErr == syscall.EWOULDBLOCK || flopErr == syscall.EAGAIN {
				fmt.Println("Glacic API is already running.")

				// Determine likely protocol/port based on config
				protocol := "https"
				port := "8443"
				if !sandboxEnabled {
					protocol = "http"
					port = "8080"
				}

				logging.Info("Glacic API is already running.")
				logging.Info(fmt.Sprintf("Web Interface: %s://<DEVICE_IP>:%s", protocol, port))
				// Determine management IPs from config for convenience
				for _, iface := range cfg.Interfaces {
					if iface.AccessWebUI || iface.Zone == "management" || iface.Zone == "lan" {
						if len(iface.IPv4) > 0 {
							// Strip CIDR
							ip := iface.IPv4[0]
							/*
							   Primitive strip CIDR (since we don't want to import net/ip just for this if possible,
							   but we probably should to be safe. Actually we have strings.)
							*/
							for i, c := range ip {
								if c == '/' {
									ip = ip[:i]
									break
								}
							}
							logging.Info(fmt.Sprintf(" -> %s://%s:%s", protocol, ip, port))
						}
					}
				}
				os.Exit(1)
			}
			logging.Error(fmt.Sprintf("Failed to acquire lock on %s: %v", lockFile, err))
			os.Exit(1)
		}
		// We hold the lock fd open. It will be closed (and lock released) when process exits.
		// NOTE: During re-exec, file descriptors are inherited by default?
		// We want the PARENT to hold it. The child (in netns) is a separate process.
		// But wait, the parent waits for the child.
		// So parent holding lock is correct.

		logging.Info("Setting up network isolation namespace...")
		if err := setupNetworkNamespace(); err != nil {
			logging.Error(fmt.Sprintf("Failed to setup network namespace: %v", err))
			os.Exit(1)
		}

		// Calculate allowed interfaces for Anti-Lockout
		// Always include loopback
		antiLockoutInterfaces := []string{"lo"}

		// Add configured interfaces that have WebUI access enabled (directly or via Zone) and AntiLockout NOT disabled
		// 1. Direct Interface Config
		for _, iface := range cfg.Interfaces {
			if iface.AccessWebUI && !iface.DisableAntiLockout {
				antiLockoutInterfaces = append(antiLockoutInterfaces, iface.Name)
			}
		}

		// 2. Zone Config (Management)
		if cfg.Zones != nil {
			for _, zone := range cfg.Zones {
				// If management WebUI or SSH is enabled for the zone
				// CHECK FOR NIL MANAGEMENT POINTER
				if zone.Management != nil && (zone.Management.WebUI || zone.Management.SSH) {
					for _, ifaceName := range zone.Interfaces {
						// Check if interface explicitly disables protection
						// We need to look up the interface config
						disabled := false
						for _, iface := range cfg.Interfaces {
							if iface.Name == ifaceName && iface.DisableAntiLockout {
								disabled = true
								break
							}
						}
						if !disabled {
							// Append unique
							exists := false
							for _, existing := range antiLockoutInterfaces {
								if existing == ifaceName {
									exists = true
									break
								}
							}
							if !exists {
								antiLockoutInterfaces = append(antiLockoutInterfaces, ifaceName)
							}
						}
					}
				}
			}
		}

		// Initialize PKI and Ensure Certs exist (Host Side to capture external IPs)
		// This is critical so the certificate SANs include the external interfaces
		certDir := filepath.Join(brand.GetStateDir(), "certs")
		if err := os.MkdirAll(certDir, 0700); err == nil {
			cm := pki.NewCertManager(certDir)
			if err := cm.EnsureCert(); err != nil {
				logging.Warn(fmt.Sprintf("Failed to generate certs on host: %v", err))
			}
		}

		if err := configureHostFirewall(antiLockoutInterfaces); err != nil {
			logging.Warn(fmt.Sprintf("Failed to configure host firewall: %v", err))
		}

		// Re-exec inside the namespace
		self, err := os.Executable()
		if err != nil {
			logging.Error(fmt.Sprintf("Failed to get executable: %v", err))
			os.Exit(1)
		}

		args := append([]string{"netns", "exec", brand.LowerName + "-api", self}, os.Args[1:]...)
		cmd := exec.Command("ip", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()

		// Pass inherited listener failure to child if it exists
		if inheritedListener != nil {
			cmd.ExtraFiles = []*os.File{listenerFile}
		}

		logging.Info(fmt.Sprintf("Re-executing inside namespace %s-api...", brand.LowerName), "self", self)

		if err := cmd.Start(); err != nil {
			logging.Error(fmt.Sprintf("Failed to start re-exec: %v", err))
			os.Exit(1)
		}

		// Forward signals to child
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			sig := <-sigCh
			if cmd.Process != nil {
				cmd.Process.Signal(sig)
			}
		}()

		if err := cmd.Wait(); err != nil {
			// This determines the exit code of the parent
			if exitError, ok := err.(*exec.ExitError); ok {
				os.Exit(exitError.ExitCode())
			}
			// Don't log error if it was a signal kill from us
			logging.Error(fmt.Sprintf("Failed to re-exec: %v", err))
			os.Exit(1)
		}
		// Parent exits successfully after child is done
		os.Exit(0)
	}

	// 1. Resolve user BEFORE chroot (needs /etc/passwd)
	var uid, gid int
	if dropUser != "" {
		u, err := user.Lookup(dropUser)
		if err != nil {
			logging.Error(fmt.Sprintf("Error: user %s not found: %v", dropUser, err))
			os.Exit(1)
		}
		uid, _ = strconv.Atoi(u.Uid)
		gid, _ = strconv.Atoi(u.Gid)
	}

	// Ensure auth directory exists with proper permissions BEFORE chroot/drop
	authDir := brand.GetStateDir()
	if err := os.MkdirAll(authDir, 0755); err != nil {
		logging.Warn(fmt.Sprintf("Warning: failed to create auth directory: %v", err))
	}

	// Change ownership of auth directory to drop user
	if dropUser != "" {
		os.Chown(authDir, uid, gid)
		os.Chown(filepath.Join(authDir, "auth.json"), uid, gid)

		// Also fix certs dir permissions since Parent created it as root
		certsDir := filepath.Join(authDir, "certs")
		os.Chown(certsDir, uid, gid)
		filepath.Walk(certsDir, func(path string, info fs.FileInfo, err error) error {
			if err == nil {
				os.Chown(path, uid, gid)
			}
			return nil
		})
	}

	// 2. Initialize dependencies (Inside NS or Dev Mode)

	// Initialize auth store (loads from host path)
	authStore, err := auth.NewStore("")
	if err != nil {
		logging.Warn(fmt.Sprintf("Warning: auth not available: %v", err))
	}

	// Client is already initialized at start of function
	// But if we re-execed, we are in a new process, so we need to init again?
	// WAIT: If we re-execed, the parent exited.
	// So we ARE the child process here (or dev mode).
	// The child process starts from main() -> RunAPI().
	// So the code at the top of RunAPI runs AGAIN in the child.
	// So "client" is valid here too.

	// 3. Setup and Enter Chroot (Only if running as root AND sandbox enabled)
	if syscall.Geteuid() == 0 && sandboxEnabled {
		jailPath := "/run/" + brand.LowerName + "-api-jail"
		if err := setupChroot(jailPath); err != nil {
			logging.Error(fmt.Sprintf("Failed to setup chroot: %v", err))
			os.Exit(1)
		}

		logging.Info(fmt.Sprintf("Entering chroot jail at %s", jailPath))
		if err := enterChroot(jailPath); err != nil {
			logging.Error(fmt.Sprintf("Failed to enter chroot: %v", err))
			os.Exit(1)
		}
	} else {
		logging.Warn("Warning: Not running as root or sandbox disabled, chroot sandbox disabled")
	}

	// 4. Drop Privileges (Inside Chroot)
	if dropUser != "" {
		if err := applyPrivileges(uid, gid); err != nil {
			logging.Warn(fmt.Sprintf("Warning: failed to drop privileges: %v", err))
		} else {
			logging.Info(fmt.Sprintf("Dropped privileges to user %s (uid=%d, gid=%d)", dropUser, uid, gid))
		}
	}

	// Start API server with auth
	logging.Info(fmt.Sprintf("Starting API server on %s", listenAddr))
	if authStore != nil {
		users := authStore.ListUsers()
		logging.Info(fmt.Sprintf("Auth Store Path: %s, Users found: %d", filepath.Join(authDir, "auth.json"), len(users)))
		for _, u := range users {
			logging.Info(fmt.Sprintf(" - User: %s, Role: %s", u.Username, u.Role))
		}
	}

	var apiServer *api.Server
	requireAuth := true
	if cfg.API != nil && !cfg.API.RequireAuth {
		requireAuth = false
	}

	var assets fs.FS

	if localDist := os.Getenv("GLACIC_UI_DIST"); localDist != "" {
		logging.Info(fmt.Sprintf("Using local UI assets from %s", localDist))
		assets = os.DirFS(localDist)
	} else {
		assets, err = fs.Sub(uiAssets, "ui/dist")
		if err != nil {
			logging.Warn(fmt.Sprintf("Warning: failed to load UI assets (ui/dist not found?): %v", err))
			// Continue without assets (UI will be disabled)
			assets = nil
		}
	}

	if requireAuth {
		if authStore != nil && authStore.HasUsers() {
			logging.Info("Authentication enabled")
		} else {
			logging.Info("No users configured - running in setup mode")
		}

		// Initialize API Key Manager - ALWAYS when auth is required
		// Even if cfg.API is nil, we need the manager for CLI-generated keys
		var apiKeyManager *api.APIKeyManager
		{
			// API Key Persistence
			// Use SQLite backed store as requested
			dbPath := filepath.Join(authDir, "api_state.db")

			// Initialize state store
			stateOpts := state.DefaultOptions(dbPath)
			stateStore, err := state.NewSQLiteStore(stateOpts)
			if err != nil {
				logging.Warn(fmt.Sprintf("Warning: failed to initialize persistent state store for API keys: %v (falling back to memory)", err))
				// Fallback? If state fails, we probably can't persist.
			}

			var store storage.APIKeyStore
			if stateStore != nil {
				persistentStore, err := storage.NewStateAPIKeyStore(stateStore)
				if err != nil {
					logging.Warn(fmt.Sprintf("Warning: failed to create persistent key store: %v", err))
				} else {
					store = persistentStore
					logging.Info(fmt.Sprintf("Initialized persistent API key store at %s", dbPath))
				}
			}

			if store == nil {
				// Fallback to memory if everything failed
				store = storage.NewMemoryAPIKeyStore()
			}

			apiKeyManager = api.NewAPIKeyManager(store)

			// Load keys from config (if api{} block exists)
			if cfg.API != nil {
				for _, k := range cfg.API.Keys {
					perms := make([]storage.Permission, len(k.Permissions))
					for i, p := range k.Permissions {
						perms[i] = storage.Permission(p)
					}

					// Import key
					// Note: PersistentAPIKeyStore handles duplicates cleanly
					if err := apiKeyManager.ImportKey(k.Name, k.Key, perms,
						api.WithAllowedIPs(k.AllowedIPs),
						api.WithAllowedPaths(k.AllowedPaths),
						api.WithRateLimit(k.RateLimit),
						api.WithDescription(k.Description),
					); err != nil {
						// We don't error on duplicates since config is applied on every start
						// But we should log real errors
						// fmt.Printf("Warning: failed to import key %s: %v\n", k.Name, err)
					} else {
						logging.Info(fmt.Sprintf("Loaded API key from config: %s", k.Name))
					}
				}
			}
		}

		apiServer, err = api.NewServer(api.ServerOptions{
			Config:        cfg,
			Assets:        assets,
			Client:        client,
			AuthStore:     authStore,
			APIKeyManager: apiKeyManager,
		})
	} else {
		logging.Info("Authentication disabled by configuration")
		apiServer, err = api.NewServer(api.ServerOptions{
			Config: cfg,
			Assets: assets,
			Client: client,
		})
	}

	if err != nil {
		logging.Error(fmt.Sprintf("Failed to initialize API server: %v", err))
		os.Exit(1)
	}

	// 4. Initialize PKI and Ensure Certs exist (only if using HTTPS)
	// Priority:
	// 1. Explicit Config in HCL (API.TLSCert/Key) -> Use specified paths
	// 2. Default/Isolated Mode -> Use global state dir paths

	var certFile, keyFile string
	useTLS := false

	// Check HCL Config first
	if cfg.API != nil && cfg.API.TLSCert != "" && cfg.API.TLSKey != "" {
		useTLS = true
		certFile = cfg.API.TLSCert
		keyFile = cfg.API.TLSKey
		logging.Info(fmt.Sprintf("Using configured TLS certificate: %s", certFile))

		// Ensure they exist (Autogen if missing)
		if _, err := tls.EnsureCertificate(certFile, keyFile, 365); err != nil {
			logging.Error(fmt.Sprintf("failed to ensure configured TLS certificates: %v", err))
			os.Exit(1)
		}
	} else {
		// Fallback to default behavior (Isolated or Forced)
		// Default: Only in isolated mode (production).
		// Overrides: GLACIC_FORCE_TLS=1 forces TLS in dev/no-sandbox.
		if (isolated && !noSandbox) || os.Getenv("GLACIC_FORCE_TLS") != "" {
			useTLS = true
			certDir := filepath.Join(brand.GetStateDir(), "certs")
			if err := os.MkdirAll(certDir, 0700); err != nil {
				logging.Error(fmt.Sprintf("failed to create cert dir: %v", err))
				os.Exit(1)
			}
			
			certFile = filepath.Join(certDir, "cert.pem")
			keyFile = filepath.Join(certDir, "key.pem")

			// Use PKI manager for default certs (better IP discovery)
			cm := pki.NewCertManager(certDir)
			if err := cm.EnsureCert(); err != nil {
				logging.Error(fmt.Sprintf("failed to ensure TLS certificates: %v", err))
				os.Exit(1)
			}
			// Start Auto-Renew (Daily check)
			cm.StartAutoRenew(context.Background(), 24*time.Hour)
		}
	}

	// 5. Start API Server
	// HTTPS (8443) if TLS enabled, otherwise HTTP (8080)
	// Allow config override for listen address? 
	// cfg.API.Listen takes precedence if set?
	// Existing code didn't use cfg.API.Listen. 
	// We should probably respect it if possible, but the requirement was about TLS.
	// Let's stick to the generated address logic but switch port based on TLS.
	
	serverListenAddr := "0.0.0.0:8443"
	if !useTLS {
		serverListenAddr = "0.0.0.0:8080"
	}
	// If config has listen addr, use it?
	if cfg.API != nil && cfg.API.Listen != "" {
		serverListenAddr = cfg.API.Listen
	}

	// Use secure default configuration (Timeouts)
	serverConfig := api.DefaultServerConfig()
	srv := &http.Server{
		Addr:              serverListenAddr,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: serverConfig.ReadHeaderTimeout,
		ReadTimeout:       serverConfig.ReadTimeout,
		WriteTimeout:      serverConfig.WriteTimeout,
		IdleTimeout:       serverConfig.IdleTimeout,
		MaxHeaderBytes:    serverConfig.MaxHeaderBytes,
		ErrorLog:          newTLSFilteredLogger(),
	}

	go func() {
		if useTLS {
			logging.Info(fmt.Sprintf("Starting API server on %s (HTTPS)", serverListenAddr))

			if inheritedListener != nil {
				logging.Info("Resuming operation on inherited socket")
				go func() {
					if err := srv.ServeTLS(inheritedListener, certFile, keyFile); err != nil && err != http.ErrServerClosed {
						logging.Error(fmt.Sprintf("API server failed: %v", err))
						os.Exit(1)
					}
				}()
			} else {
				go func() {
					if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
						logging.Error(fmt.Sprintf("API server failed: %v", err))
						os.Exit(1)
					}
				}()
			}

			// Start HTTP -> HTTPS Redirect Server on port 8080
			// Only if we are not listening on 8080 already
			if !strings.HasSuffix(serverListenAddr, ":8080") {
				// DNAT rules forward host:8080 -> ns:8080, so we need to listen here.
				logging.Info("Starting HTTP redirect server on :8080")
				go func() {
					redirectSrv := &http.Server{
						Addr:              ":8080",
						ReadHeaderTimeout: 5 * time.Second,
						ReadTimeout:       5 * time.Second,
						WriteTimeout:      5 * time.Second,
						Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							// Redirect to same host but https port 8443 (or whatever configured)
							// If we configured a custom port, we assume 8443 unless we parse serverListenAddr
							targetPort := "8443"
							if _, p, err := net.SplitHostPort(serverListenAddr); err == nil {
								targetPort = p
							}

							target := "https://" + r.Host
							if host, _, err := net.SplitHostPort(r.Host); err == nil {
								target = "https://" + host
							}
							
							// Reconstruct URL
							target = target + ":" + targetPort + r.URL.Path
							if r.URL.RawQuery != "" {
								target += "?" + r.URL.RawQuery
							}

							http.Redirect(w, r, target, http.StatusMovedPermanently)
						}),
					}
					if err := redirectSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
						logging.Warn(fmt.Sprintf("HTTP redirect server failed: %v", err))
					}
				}()
			}
		} else {
			logging.Info(fmt.Sprintf("Dev Mode: Starting API server on %s (HTTP)", serverListenAddr))
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logging.Error(fmt.Sprintf("API server failed: %v", err))
				os.Exit(1)
			}
		}
	}()

	// Wait for signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logging.Info("Shutting down API server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logging.Error(fmt.Sprintf("API server shutdown failed: %v", err))
		os.Exit(1)
	}
} // This is the closing brace for the RunAPI function.

// applyPrivileges drops root privileges to the specified uid/gid
func applyPrivileges(uid, gid int) error {
	// Set supplementary groups
	if err := syscall.Setgroups([]int{gid}); err != nil {
		return fmt.Errorf("setgroups failed: %w", err)
	}

	// Set GID first (must be done before setuid)
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("setgid failed: %w", err)
	}

	// Set UID
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("setuid failed: %w", err)
	}

	return nil
}
