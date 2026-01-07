package cmd

import (
	"context"
	"embed"
	"errors"
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
	"strings"
	"syscall"
	"time"

	"grimm.is/glacic/internal/api"
	"grimm.is/glacic/internal/api/storage"
	"grimm.is/glacic/internal/audit"
	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/pki"
	"grimm.is/glacic/internal/state"
	"grimm.is/glacic/internal/tls"
)

// Named constants replacing magic numbers
const (
	// listenerFD is the inherited listener file descriptor (first ExtraFile)
	listenerFD = 3

	// Lock acquisition parameters (30 seconds total, 500ms intervals)
	lockRetryCount    = 60
	lockRetryInterval = 500 * time.Millisecond

	// Control plane connection parameters (30 seconds total, 1s intervals)
	ctlRetryCount    = 30
	ctlRetryInterval = time.Second

	// Server ports
	httpsPort = "8443"
	httpPort  = "8080"
)

// APIRuntimeConfig holds all runtime configuration for the API server.
// Environment variables are parsed once at startup and stored here.
type APIRuntimeConfig struct {
	ListenAddr     string
	DropUser       string
	NoSandbox      bool   // GLACIC_NO_SANDBOX=1
	LocalUIDistDir string // GLACIC_UI_DIST
	ForceTLS       bool   // GLACIC_FORCE_TLS=1
	NoTLS          bool   // GLACIC_NO_TLS=1 (force HTTP for tests)
}

// NewAPIRuntimeConfig parses environment variables and CLI args into config.
func NewAPIRuntimeConfig(listenAddr, dropUser string) *APIRuntimeConfig {
	return &APIRuntimeConfig{
		ListenAddr:     listenAddr,
		DropUser:       dropUser,
		NoSandbox:      os.Getenv("GLACIC_NO_SANDBOX") == "1",
		LocalUIDistDir: os.Getenv("GLACIC_UI_DIST"),
		ForceTLS:       os.Getenv("GLACIC_FORCE_TLS") != "",
		NoTLS:          os.Getenv("GLACIC_NO_TLS") == "1",
	}
}

// tlsFilteredLogger filters out noisy TLS handshake errors from self-signed certificates.
// These errors are expected and benign when clients don't trust our certificate.
type tlsFilteredLogger struct {
	lastMsg      string
	lastTime     time.Time
	rateLimit    time.Duration
	isSelfSigned bool
}

func (l *tlsFilteredLogger) Write(p []byte) (n int, err error) {
	msg := string(p)

	// If using a real CA signed cert (user provided), we want to see these errors
	if !l.isSelfSigned {
		logging.Warn(strings.TrimSpace(msg))
		return len(p), nil
	}

	// Suppress TLS handshake errors from untrusted certificates (only for self-signed)
	if strings.Contains(msg, "TLS handshake error") &&
		(strings.Contains(msg, "unknown certificate") ||
			strings.Contains(msg, "certificate required") ||
			strings.Contains(msg, "bad certificate")) {
		return len(p), nil
	}

	// Pass through other messages
	logging.Warn(strings.TrimSpace(msg))
	return len(p), nil
}

func newTLSFilteredLogger(isSelfSigned bool) *log.Logger {
	return log.New(&tlsFilteredLogger{rateLimit: 60 * time.Second, isSelfSigned: isSelfSigned}, "", 0)
}

// stripCIDR removes the network prefix from a CIDR address, returning just the IP.
// Uses net.ParseCIDR for proper validation instead of string manipulation.
func stripCIDR(cidr string) string {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		// Fallback: might just be a bare IP
		if parsed := net.ParseIP(cidr); parsed != nil {
			return cidr
		}
		return cidr // Return as-is if unparseable
	}
	return ip.String()
}

// alreadyRunningError is returned when another API instance holds the lock.
type alreadyRunningError struct {
	cfg            *config.Config
	sandboxEnabled bool
}

func (e *alreadyRunningError) Error() string {
	return "glacic api is already running"
}

// LogAccessURLs prints the URLs where the running instance can be accessed.
func (e *alreadyRunningError) LogAccessURLs() {
	protocol := "https"
	port := httpsPort
	if !e.sandboxEnabled {
		protocol = "http"
		port = httpPort
	}

	logging.Info("Glacic API is already running.")
	logging.Info(fmt.Sprintf("Web Interface: %s://<DEVICE_IP>:%s", protocol, port))

	for _, iface := range e.cfg.Interfaces {
		if iface.AccessWebUI || iface.Zone == "management" || iface.Zone == "lan" {
			if len(iface.IPv4) > 0 {
				ip := stripCIDR(iface.IPv4[0])
				logging.Info(fmt.Sprintf(" -> %s://%s:%s", protocol, ip, port))
			}
		}
	}
}

// connectToControlPlane establishes a connection to the control plane with retries.
// Returns the client and config, or an error after ctlRetryCount attempts.
func connectToControlPlane() (*ctlplane.Client, *config.Config, error) {
	var client *ctlplane.Client
	var err error

	for i := 0; i < ctlRetryCount; i++ {
		client, err = ctlplane.NewClient()
		if err == nil {
			break
		}
		if i%5 == 0 {
			logging.Info(fmt.Sprintf("Waiting for control plane... (%v)", err))
		}
		time.Sleep(ctlRetryInterval)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to control plane after %ds: %w", ctlRetryCount, err)
	}

	// Verify connection
	status, err := client.GetStatus()
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("failed to get status from control plane: %w", err)
	}
	logging.Info(fmt.Sprintf("Connected to control plane (uptime: %s)", status.Uptime))

	cfg, err := client.GetConfig()
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("failed to get config from control plane: %w", err)
	}

	return client, cfg, nil
}

// resolveDropUser looks up the target user for privilege dropping.
// Must be called before chroot (needs /etc/passwd).
func resolveDropUser(username string) (uid, gid int, err error) {
	if username == "" {
		return 0, 0, nil
	}
	u, err := user.Lookup(username)
	if err != nil {
		return 0, 0, fmt.Errorf("user %s not found: %w", username, err)
	}
	uid, _ = strconv.Atoi(u.Uid)
	gid, _ = strconv.Atoi(u.Gid)
	return uid, gid, nil
}

// buildAntiLockoutInterfaces returns the list of interfaces that should bypass firewall rules.
func buildAntiLockoutInterfaces(cfg *config.Config) []string {
	interfaces := []string{"lo"}
	seen := map[string]bool{"lo": true}

	// From interface config
	for _, iface := range cfg.Interfaces {
		if iface.AccessWebUI && !iface.DisableAntiLockout && !seen[iface.Name] {
			interfaces = append(interfaces, iface.Name)
			seen[iface.Name] = true
		}
	}

	// From zone config
	if cfg.Zones != nil {
		for _, zone := range cfg.Zones {
			if zone.Management != nil && (zone.Management.WebUI || zone.Management.SSH) {
				for _, ifaceName := range zone.Interfaces {
					if seen[ifaceName] {
						continue
					}
					disabled := false
					for _, iface := range cfg.Interfaces {
						if iface.Name == ifaceName && iface.DisableAntiLockout {
							disabled = true
							break
						}
					}
					if !disabled {
						interfaces = append(interfaces, ifaceName)
						seen[ifaceName] = true
					}
				}
			}
		}
	}
	return interfaces
}

// acquireSingleInstanceLock attempts to acquire an exclusive lock to ensure single instance.
// Returns the lock file descriptor (caller must keep it open) or an error.
func acquireSingleInstanceLock(cfg *config.Config, sandboxEnabled bool) (int, error) {
	lockFile := "/var/run/glacic_api.lock"
	fd, err := syscall.Open(lockFile, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return -1, fmt.Errorf("failed to open lock file %s: %w", lockFile, err)
	}

	var lockErr error
	for i := 0; i < lockRetryCount; i++ {
		lockErr = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if lockErr == nil {
			return fd, nil
		}
		if lockErr != syscall.EWOULDBLOCK && lockErr != syscall.EAGAIN {
			syscall.Close(fd)
			return -1, fmt.Errorf("lock error: %w", lockErr)
		}
		if i%5 == 0 {
			Printer.Printf("Waiting for previous API instance to release lock... (%d/%d)\n", i, lockRetryCount)
		}
		time.Sleep(lockRetryInterval)
	}

	syscall.Close(fd)
	return -1, &alreadyRunningError{cfg: cfg, sandboxEnabled: sandboxEnabled}
}

// checkInheritedListener checks for an inherited listener from socket handoff during upgrade.
func checkInheritedListener() (net.Listener, *os.File) {
	listenerFile := os.NewFile(listenerFD, "listener")
	if l, err := net.FileListener(listenerFile); err == nil {
		return l, listenerFile
	}
	return nil, nil
}

// RunAPI runs the unprivileged API server.
// This process should run as a non-root user and communicates
// with the control plane via RPC over Unix socket.
func RunAPI(uiAssets embed.FS, listenAddr string, dropUser string) {
	// Parse all configuration upfront
	rtCfg := NewAPIRuntimeConfig(listenAddr, dropUser)

	// Check for inherited listener (socket handoff during upgrade)
	inheritedListener, listenerFile := checkInheritedListener()
	if inheritedListener != nil {
		if tcpAddr, ok := inheritedListener.Addr().(*net.TCPAddr); ok {
			rtCfg.ListenAddr = fmt.Sprintf(":%d", tcpAddr.Port)
		}
	}

	// Initialize structured logging
	logCfg := logging.DefaultConfig()
	logCfg.Output = os.Stderr
	logger := logging.New(logCfg).WithComponent("API")
	logging.SetDefault(logger)

	// Connect to control plane
	client, cfg, err := connectToControlPlane()
	if err != nil {
		logging.Error(err.Error())
		logging.Info("Make sure 'glacic ctl' is running first.")
		os.Exit(1)
	}
	defer client.Close()

	// Determine sandbox state from config and environment
	sandboxEnabled := !rtCfg.NoSandbox && (cfg.API == nil || !cfg.API.DisableSandbox)
	isolated := runtime.GOOS == "linux" && isIsolated()

	// Parent process: setup isolation and re-exec into namespace
	if runtime.GOOS == "linux" && syscall.Geteuid() == 0 && sandboxEnabled && !isolated {
		exitCode := runParentProcess(cfg, sandboxEnabled, inheritedListener, listenerFile)
		os.Exit(exitCode)
	}

	// Child process (or dev mode): run the actual server
	if err := runServerProcess(rtCfg, cfg, client, uiAssets, sandboxEnabled, isolated, inheritedListener); err != nil {
		logging.Error(err.Error())
		os.Exit(1)
	}
}

// runParentProcess handles namespace setup and re-execution into the isolated namespace.
// Returns the exit code to use.
func runParentProcess(cfg *config.Config, sandboxEnabled bool, inheritedListener net.Listener, listenerFile *os.File) int {
	// Acquire single instance lock
	_, err := acquireSingleInstanceLock(cfg, sandboxEnabled)
	if err != nil {
		var alreadyRunning *alreadyRunningError
		if errors.As(err, &alreadyRunning) {
			alreadyRunning.LogAccessURLs()
			return 1
		}
		logging.Error(err.Error())
		return 1
	}
	// Lock fd is intentionally kept open - will be closed when parent exits

	// Setup network namespace
	logging.Info("Setting up network isolation namespace...")
	if err := setupNetworkNamespace(); err != nil {
		logging.Error(fmt.Sprintf("Failed to setup network namespace: %v", err))
		return 1
	}

	// Initialize PKI (host side - captures external IPs for SANs)
	certDir := filepath.Join(brand.GetStateDir(), "certs")
	if err := os.MkdirAll(certDir, 0700); err == nil {
		cm := pki.NewCertManager(certDir)
		if err := cm.EnsureCert(); err != nil {
			logging.Warn(fmt.Sprintf("Failed to generate certs on host: %v", err))
		}
	}

	// Configure anti-lockout firewall rules
	antiLockoutInterfaces := buildAntiLockoutInterfaces(cfg)
	if err := configureHostFirewall(antiLockoutInterfaces); err != nil {
		logging.Warn(fmt.Sprintf("Failed to configure host firewall: %v", err))
	}

	// Re-exec inside the namespace
	return reexecInNamespace(inheritedListener, listenerFile)
}

// reexecInNamespace re-executes the current binary inside the isolated network namespace.
func reexecInNamespace(inheritedListener net.Listener, listenerFile *os.File) int {
	self, err := os.Executable()
	if err != nil {
		logging.Error(fmt.Sprintf("Failed to get executable: %v", err))
		return 1
	}

	args := append([]string{"netns", "exec", brand.LowerName + "-api", self}, os.Args[1:]...)
	cmd := exec.Command("ip", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	// Pass inherited listener to child if it exists
	if inheritedListener != nil {
		cmd.ExtraFiles = []*os.File{listenerFile}
	}

	logging.Info(fmt.Sprintf("Re-executing inside namespace %s-api...", brand.LowerName), "self", self)

	if err := cmd.Start(); err != nil {
		logging.Error(fmt.Sprintf("Failed to start re-exec: %v", err))
		return 1
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
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode()
		}
		logging.Error(fmt.Sprintf("Failed to re-exec: %v", err))
		return 1
	}
	return 0
}

// runServerProcess runs the actual HTTP server (inside namespace or dev mode).
func runServerProcess(
	rtCfg *APIRuntimeConfig,
	cfg *config.Config,
	client *ctlplane.Client,
	uiAssets embed.FS,
	sandboxEnabled bool,
	isolated bool,
	inheritedListener net.Listener,
) error {
	// Resolve user for privilege dropping (must be before chroot)
	uid, gid, err := resolveDropUser(rtCfg.DropUser)
	if err != nil {
		return err
	}

	// Ensure auth directory exists with proper permissions
	authDir := brand.GetStateDir()
	if err := os.MkdirAll(authDir, 0755); err != nil {
		logging.Warn(fmt.Sprintf("Warning: failed to create auth directory: %v", err))
	}

	// Fix ownership of auth directory for drop user
	if rtCfg.DropUser != "" {
		fixAuthDirOwnership(authDir, uid, gid)

		// Also ensure socket directory exists and is owned by drop user
		sockDir := "/var/run/glacic/api"
		if err := os.MkdirAll(sockDir, 0755); err != nil {
			logging.Warn(fmt.Sprintf("Warning: failed to create socket directory: %v", err))
		}
		if err := os.Chown(sockDir, uid, gid); err != nil {
			logging.Warn(fmt.Sprintf("Warning: failed to chown socket directory: %v", err))
		}
	}

	// Setup and enter chroot (only if root and sandbox enabled)
	if syscall.Geteuid() == 0 && sandboxEnabled {
		jailPath := "/run/" + brand.LowerName + "-api-jail"
		if err := setupChroot(jailPath); err != nil {
			return fmt.Errorf("failed to setup chroot: %w", err)
		}
		logging.Info(fmt.Sprintf("Entering chroot jail at %s", jailPath))
		if err := enterChroot(jailPath); err != nil {
			return fmt.Errorf("failed to enter chroot: %w", err)
		}
	} else {
		logging.Warn("Warning: Not running as root or sandbox disabled, chroot sandbox disabled")
	}

	// Drop privileges
	if rtCfg.DropUser != "" {
		if err := applyPrivileges(uid, gid); err != nil {
			logging.Warn(fmt.Sprintf("Warning: failed to drop privileges: %v", err))
		} else {
			logging.Info(fmt.Sprintf("Dropped privileges to user %s (uid=%d, gid=%d)", rtCfg.DropUser, uid, gid))
		}
	}

	// Initialize auth store
	authStore, err := auth.NewStore("")
	if err != nil {
		logging.Warn(fmt.Sprintf("Warning: auth not available: %v", err))
	}

	// Initialize API server
	apiServer, err := initializeAPIServer(rtCfg, cfg, client, uiAssets, authStore, authDir)
	if err != nil {
		return err
	}

	// TLS is now handled by the proxy layer.
	// API server always serves plain HTTP on Unix socket.
	// Start HTTP server
	return startHTTPServer(rtCfg, cfg, apiServer, inheritedListener)
}

// fixAuthDirOwnership changes ownership of auth directory and its contents.
func fixAuthDirOwnership(authDir string, uid, gid int) {
	os.Chown(authDir, uid, gid)
	os.Chown(filepath.Join(authDir, "auth.json"), uid, gid)

	certsDir := filepath.Join(authDir, "certs")
	os.Chown(certsDir, uid, gid)
	filepath.Walk(certsDir, func(path string, info fs.FileInfo, err error) error {
		if err == nil {
			os.Chown(path, uid, gid)
		}
		return nil
	})
}

// initializeAPIServer creates and configures the API server instance.
func initializeAPIServer(
	rtCfg *APIRuntimeConfig,
	cfg *config.Config,
	client *ctlplane.Client,
	uiAssets embed.FS,
	authStore *auth.Store,
	authDir string,
) (*api.Server, error) {
	// Log auth store status
	logging.Info(fmt.Sprintf("Starting API server on %s", rtCfg.ListenAddr))
	if authStore != nil {
		users := authStore.ListUsers()
		logging.Info(fmt.Sprintf("Auth Store Path: %s, Users found: %d", filepath.Join(authDir, "auth.json"), len(users)))
		for _, u := range users {
			logging.Info(fmt.Sprintf(" - User: %s, Role: %s", u.Username, u.Role))
		}
	}

	// Determine auth requirement
	requireAuth := cfg.API == nil || cfg.API.RequireAuth

	// Load UI assets
	var assets fs.FS
	if rtCfg.LocalUIDistDir != "" {
		logging.Info(fmt.Sprintf("Using local UI assets from %s", rtCfg.LocalUIDistDir))
		assets = os.DirFS(rtCfg.LocalUIDistDir)
	} else {
		var err error
		assets, err = fs.Sub(uiAssets, "ui/dist")
		if err != nil {
			logging.Warn(fmt.Sprintf("Warning: failed to load UI assets (ui/dist not found?): %v", err))
			assets = nil
		}
	}

	// Initialize audit store if enabled
	if cfg.Audit != nil && cfg.Audit.Enabled {
		stateDir := cfg.StateDir
		if stateDir == "" {
			stateDir = "/var/lib/glacic"
		}
		dbPath := cfg.Audit.DatabasePath
		if dbPath == "" {
			dbPath = filepath.Join(stateDir, "audit.db")
		}
		retentionDays := cfg.Audit.RetentionDays
		if retentionDays <= 0 {
			retentionDays = 90
		}

		auditStore, err := audit.NewStore(dbPath, retentionDays, cfg.Audit.KernelAudit)
		if err != nil {
			logging.Warn(fmt.Sprintf("Failed to initialize audit store: %v", err))
		} else {
			logging.Info(fmt.Sprintf("Audit logging enabled: %s (retention: %d days)", dbPath, retentionDays))
			api.SetAPIAuditStore(auditStore)
			ctlplane.SetAuditStore(auditStore)
		}
	}

	if !requireAuth {
		logging.Info("Authentication disabled by configuration")
		return api.NewServer(api.ServerOptions{
			Config: cfg,
			Assets: assets,
			Client: client,
		})
	}

	// Auth required path
	if authStore != nil && authStore.HasUsers() {
		logging.Info("Authentication enabled")
	} else {
		logging.Info("No users configured - running in setup mode")
	}

	// Initialize API Key Manager
	apiKeyManager := initializeAPIKeyManager(cfg, authDir)

	return api.NewServer(api.ServerOptions{
		Config:        cfg,
		Assets:        assets,
		Client:        client,
		AuthStore:     authStore,
		APIKeyManager: apiKeyManager,
	})
}

// initializeAPIKeyManager creates and populates the API key manager.
func initializeAPIKeyManager(cfg *config.Config, authDir string) *api.APIKeyManager {
	dbPath := filepath.Join(authDir, "api_state.db")

	stateOpts := state.DefaultOptions(dbPath)
	stateStore, err := state.NewSQLiteStore(stateOpts)
	if err != nil {
		logging.Warn(fmt.Sprintf("Warning: failed to initialize persistent state store for API keys: %v (falling back to memory)", err))
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
		store = storage.NewMemoryAPIKeyStore()
	}

	apiKeyManager := api.NewAPIKeyManager(store)

	// Load keys from config
	if cfg.API != nil {
		for _, k := range cfg.API.Keys {
			perms := make([]storage.Permission, len(k.Permissions))
			for i, p := range k.Permissions {
				perms[i] = storage.Permission(p)
			}

			if err := apiKeyManager.ImportKey(k.Name, k.Key, perms,
				api.WithAllowedIPs(k.AllowedIPs),
				api.WithAllowedPaths(k.AllowedPaths),
				api.WithRateLimit(k.RateLimit),
				api.WithDescription(k.Description),
			); err == nil {
				logging.Info(fmt.Sprintf("Loaded API key from config: %s", k.Name))
			}
		}
	}
	if cfg.API != nil {
		logging.Info(fmt.Sprintf("Found %d API keys in config", len(cfg.API.Keys)))
	} else {
		logging.Info("No API config found (cfg.API is nil)")
	}

	return apiKeyManager
}

// setupTLS determines TLS configuration and ensures certificates exist.
func setupTLS(rtCfg *APIRuntimeConfig, cfg *config.Config, isolated bool) (certFile, keyFile string, useTLS bool, isSelfSigned bool) {
	// Force HTTP mode if NoTLS is set (for integration tests)
	if rtCfg.NoTLS {
		logging.Info("TLS disabled via GLACIC_NO_TLS - running HTTP only")
		return "", "", false, false
	}

	// Check HCL Config first - explicit cert paths
	if cfg.API != nil && cfg.API.TLSCert != "" && cfg.API.TLSKey != "" {
		useTLS = true
		certFile = cfg.API.TLSCert
		keyFile = cfg.API.TLSKey
		isSelfSigned = false // User provided -> treat as "Real" (warn on errors)
		logging.Info(fmt.Sprintf("Using configured TLS certificate: %s", certFile))

		if _, err := tls.EnsureCertificate(certFile, keyFile, 365); err != nil {
			logging.Error(fmt.Sprintf("failed to ensure configured TLS certificates: %v", err))
			os.Exit(1)
		}
		return
	}

	// TLSListen set - enable TLS with default paths
	if cfg.API != nil && cfg.API.TLSListen != "" {
		useTLS = true
		isSelfSigned = true
		certDir := filepath.Join(brand.GetStateDir(), "certs")
		if err := os.MkdirAll(certDir, 0700); err != nil {
			logging.Error(fmt.Sprintf("failed to create cert dir: %v", err))
			os.Exit(1)
		}

		certFile = filepath.Join(certDir, "server.crt")
		keyFile = filepath.Join(certDir, "server.key")

		if _, err := tls.EnsureCertificate(certFile, keyFile, 365); err != nil {
			logging.Error(fmt.Sprintf("failed to ensure TLS certificates: %v", err))
			os.Exit(1)
		}
		logging.Info(fmt.Sprintf("TLS enabled via tls_listen, using auto-generated certificates"))
		return
	}

	// Fallback to default behavior (sandbox isolation)
	if (isolated && !rtCfg.NoSandbox) || rtCfg.ForceTLS {
		useTLS = true
		isSelfSigned = true
		certDir := filepath.Join(brand.GetStateDir(), "certs")
		if err := os.MkdirAll(certDir, 0700); err != nil {
			logging.Error(fmt.Sprintf("failed to create cert dir: %v", err))
			os.Exit(1)
		}

		certFile = filepath.Join(certDir, "cert.pem")
		keyFile = filepath.Join(certDir, "key.pem")

		cm := pki.NewCertManager(certDir)
		if err := cm.EnsureCert(); err != nil {
			logging.Error(fmt.Sprintf("failed to ensure TLS certificates: %v", err))
			os.Exit(1)
		}
		cm.StartAutoRenew(context.Background(), 24*time.Hour)
	}
	return
}

// startHTTPServer starts the HTTP server and blocks until shutdown.
// TLS termination is handled by the proxy layer.
func startHTTPServer(
	rtCfg *APIRuntimeConfig,
	cfg *config.Config,
	apiServer *api.Server,
	inheritedListener net.Listener,
) error {
	// Determine listen address
	serverListenAddr := rtCfg.ListenAddr
	if serverListenAddr == "" {
		serverListenAddr = "0.0.0.0:" + httpPort
	}

	// Identify socket type
	network := "tcp"
	if strings.HasPrefix(serverListenAddr, "/") {
		network = "unix"
	}

	// Create listener manually to support both TCP and Unix
	var listener net.Listener
	var err error

	if inheritedListener != nil {
		listener = inheritedListener
		logging.Info("Resuming operation on inherited (" + network + ") socket")
	} else {
		// Cleanup old socket if needed
		if network == "unix" {
			os.Remove(serverListenAddr) // best effort
		}

		listener, err = net.Listen(network, serverListenAddr)
		if err != nil {
			logging.Error(fmt.Sprintf("Failed to bind %s listener on %s: %v", network, serverListenAddr, err))
			os.Exit(1)
		}

		// If unix socket, ensure permissions are open enough for proxy
		if network == "unix" {
			if err := os.Chmod(serverListenAddr, 0770); err != nil {
				logging.Warn(fmt.Sprintf("Failed to set socket permissions: %v", err))
			}
		}
	}

	// Create server with secure defaults
	serverConfig := api.DefaultServerConfig()
	srv := &http.Server{
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: serverConfig.ReadHeaderTimeout,
		ReadTimeout:       serverConfig.ReadTimeout,
		WriteTimeout:      serverConfig.WriteTimeout,
		IdleTimeout:       serverConfig.IdleTimeout,
		MaxHeaderBytes:    serverConfig.MaxHeaderBytes,
	}

	// Start server in goroutine
	go func() {
		logging.Info(fmt.Sprintf("Starting API server on %s (%s, HTTP)", serverListenAddr, network))

		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			logging.Error(fmt.Sprintf("API server failed: %v", err))
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logging.Info("Shutting down API server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// startTLSServer starts the HTTPS server with optional HTTP redirect.
func startTLSServer(rtCfg *APIRuntimeConfig, cfg *config.Config, srv *http.Server, serverListenAddr, certFile, keyFile string, inheritedListener net.Listener) {
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

	// Start HTTP -> HTTPS redirect server if not listening on 8080 and not disabled
	disableRedirect := cfg.API != nil && cfg.API.DisableHTTPRedirect
	if !disableRedirect && !strings.HasSuffix(serverListenAddr, ":"+httpPort) {
		// Determine HTTP listen address
		httpListenAddr := ":" + httpPort
		if cfg.API != nil && cfg.API.Listen != "" {
			httpListenAddr = cfg.API.Listen
		}

		// Don't start if HTTP and HTTPS ports are the same (avoid conflict)
		if !addressesOverlap(httpListenAddr, serverListenAddr) {
			go startHTTPRedirectServer(httpListenAddr, serverListenAddr)
		}
	}
}

// startHTTPRedirectServer starts a server that redirects HTTP to HTTPS.
func startHTTPRedirectServer(listenAddr, httpsAddr string) {
	redirectSrv := &http.Server{
		Addr:              listenAddr,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			targetPort := httpsPort
			if _, p, err := net.SplitHostPort(httpsAddr); err == nil {
				targetPort = p
			}

			target := "https://" + r.Host
			if host, _, err := net.SplitHostPort(r.Host); err == nil {
				target = "https://" + host
			}

			target = target + ":" + targetPort + r.URL.Path
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}

			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}),
	}
	logging.Info(fmt.Sprintf("Starting HTTP redirect server on %s -> %s", listenAddr, httpsAddr))
	if err := redirectSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logging.Warn(fmt.Sprintf("HTTP redirect server failed: %v", err))
	}
}

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

// addressesOverlap checks if two listen addresses would conflict on the same port.
// It considers ports and host wildcarding.
func addressesOverlap(addr1, addr2 string) bool {
	h1, p1, err1 := net.SplitHostPort(addr1)
	h2, p2, err2 := net.SplitHostPort(addr2)

	// If unparseable, play it safe and assume conflict if strings differ
	if err1 != nil || err2 != nil {
		return addr1 == addr2
	}

	// Different ports definitely don't overlap
	if p1 != p2 {
		return false
	}

	// Same port - check hosts
	if h1 == h2 {
		return true
	}

	// Wildcard conflicts with everything on same port
	if h1 == "" || h1 == "0.0.0.0" || h1 == "::" || h2 == "" || h2 == "0.0.0.0" || h2 == "::" {
		return true
	}

	return false
}
