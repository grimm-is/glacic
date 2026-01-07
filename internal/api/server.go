package api

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"grimm.is/glacic/internal/clock"

	"github.com/pmezard/go-difflib/difflib"

	"grimm.is/glacic/internal/api/storage"
	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/health"
	"grimm.is/glacic/internal/i18n"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/metrics"
	"grimm.is/glacic/internal/ratelimit"
	"grimm.is/glacic/internal/state"
	"grimm.is/glacic/internal/stats"
	"grimm.is/glacic/internal/tls"
	"grimm.is/glacic/internal/ui"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func init() {
	// Ensure MIME types are registered, as minimal environments might lack /etc/mime.types
	mime.AddExtensionType(".js", "application/javascript")
	mime.AddExtensionType(".css", "text/css")
	mime.AddExtensionType(".html", "text/html")
	mime.AddExtensionType(".json", "application/json")
	mime.AddExtensionType(".svg", "image/svg+xml")
	mime.AddExtensionType(".png", "image/png")
	mime.AddExtensionType(".woff2", "font/woff2")
	mime.AddExtensionType(".wasm", "application/wasm")
}

//go:embed spec/openapi.yaml
var openAPISpec []byte //nolint:typecheck

// ServerConfig holds HTTP server security configuration.
// Mitigation: OWASP A05:2021-Security Misconfiguration
type ServerConfig struct {
	ReadHeaderTimeout time.Duration // Slowloris prevention
	ReadTimeout       time.Duration // Body read limit
	WriteTimeout      time.Duration // Response timeout
	IdleTimeout       time.Duration // Keep-alive timeout
	MaxHeaderBytes    int           // Header size limit
	MaxBodyBytes      int64         // Request body size limit
}

// DefaultServerConfig returns secure default server configuration.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		ReadHeaderTimeout: 10 * time.Second, // Slowloris prevention
		ReadTimeout:       15 * time.Second, // Body read limit
		WriteTimeout:      30 * time.Second, // Response timeout
		IdleTimeout:       60 * time.Second, // Keep-alive timeout
		MaxHeaderBytes:    1 << 16,          // 64KB header limit
		MaxBodyBytes:      10 << 20,         // 10MB body limit
	}
}

// Server handles API requests.
type Server struct {
	Config          *config.Config
	Assets          fs.FS
	client          ctlplane.ControlPlaneClient // RPC client for control plane communication
	authStore       auth.AuthStore
	authMw          *auth.Middleware
	apiKeyManager   *APIKeyManager // Use local type alias/import
	logger          *logging.Logger
	collector       *metrics.Collector
	startTime       time.Time
	learning        *learning.Service
	stateStore      state.Store
	configMu        sync.RWMutex       // Mutex to protect Config access
	csrfManager     *CSRFManager       // CSRF token manager
	rateLimiter     *ratelimit.Limiter // Rate limiter for auth endpoints
	security        *SecurityManager   // Security manager for IP blocking
	wsManager       *WSManager         // Websocket manager
	healthy         atomic.Bool        // Cached health status
	adminCreationMu sync.Mutex         // Mutex to prevent race conditions in admin creation

	// ClearPath Policy Editor support
	statsCollector *stats.Collector // Rule stats for sparklines
	deviceLookup   DeviceLookup     // Device name resolution for UI pills

	mux *http.ServeMux
}

// ServerOptions holds dependencies for the API server
type ServerOptions struct {
	Config          *config.Config
	Assets          fs.FS
	Client          ctlplane.ControlPlaneClient
	AuthStore       auth.AuthStore
	APIKeyManager   *APIKeyManager
	Logger          *logging.Logger
	StateStore      state.Store       // Optional: For standalone mode
	LearningService *learning.Service // Optional: For standalone mode
}

// NewServer creates a new API server with the provided options
func NewServer(opts ServerOptions) (*Server, error) {
	logger := opts.Logger
	if logger == nil {
		logger = logging.New(logging.DefaultConfig())
	}

	collector := metrics.NewCollector(logger, 30*time.Second)

	// Initialize security components
	csrfManager := NewCSRFManager()
	rateLimiter := ratelimit.NewLimiter()
	rateLimiter.StartCleanup(10*time.Minute, 1*time.Hour)

	// Create common server structure
	logger.Info("[DEBUG] NewServer initialized (Binary Updated Verify)")
	s := &Server{
		Config:        opts.Config,
		Assets:        opts.Assets,
		logger:        logger,
		collector:     collector,
		startTime:     clock.Now(),
		csrfManager:   csrfManager,
		rateLimiter:   rateLimiter,
		client:        opts.Client,
		authStore:     opts.AuthStore,
		apiKeyManager: opts.APIKeyManager,
		stateStore:    opts.StateStore,
		learning:      opts.LearningService,
	}

	// Setup auth store: use DevStore if no auth configured
	if opts.AuthStore != nil {
		s.authStore = opts.AuthStore
		s.authMw = auth.NewMiddleware(opts.AuthStore)
	} else if opts.Config != nil && opts.Config.API != nil && !opts.Config.API.RequireAuth {
        // Explicitly allowed no-auth mode
        s.logger.Info("Authentication disabled by configuration")
    } else {
        // Fallback or error?
        // To be safe and compliant with the "Hardening" goal, we should NOT strictly default to DevStore invisible.
        // However, existing tests might rely on this.
        // If we strictly follow the plan: "Remove default fallback to auth.NewDevStore()".
        // If we just remove it, s.authStore remains nil.
        // We need to ensure 'require' doesn't panic.
    }

	if opts.Client != nil {
		s.wsManager = NewWSManager(opts.Client, s.checkPendingStatus)
	}

	// Initialize Security Manager for fail2ban-style blocking
	// Note: IPSetService integration via RPC will be added when client is available
	s.security = NewSecurityManager(opts.Client, logger)

	// Start background health check
	go s.runHealthCheck()

	s.initRoutes()
	return s, nil
}

// runHealthCheck periodically updates the health status
func (s *Server) runHealthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	check := func() {
		// Lightweight library-based check
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := checkNFTables(ctx); err != nil {
			s.healthy.Store(false)
		} else {
			s.healthy.Store(true)
		}
	}

	// Initial check
	check()

	for range ticker.C {
		check()
	}
}

// initRoutes initializes the HTTP router
func (s *Server) initRoutes() {
	mux := http.NewServeMux()
	s.mux = mux

	// Public endpoints (no auth required)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/create-admin", s.handleCreateAdmin)
	mux.HandleFunc("GET /api/status", s.handleStatus) // Health status - public for monitoring

	// Batch API
	// Rate limiting for Batch API is applied inside the handler to account for batch size
	mux.HandleFunc("POST /api/batch", s.handleBatch)

	// Websockets
	mux.HandleFunc("GET /api/ws/status", s.handleStatusWS)

	// Branding (public)
	mux.HandleFunc("GET /api/brand", s.handleBrand)

	// OpenAPI & Docs
	mux.HandleFunc("GET /api/openapi.yaml", s.handleOpenAPI)
	mux.HandleFunc("GET /api/docs", s.handleSwaggerUI)

	// UI Schema endpoints (public - needed before auth)
	mux.HandleFunc("GET /api/ui/menu", s.handleUIMenu)
	mux.HandleFunc("GET /api/ui/pages", s.handleUIPages)
	mux.HandleFunc("GET /api/ui/page/", s.handleUIPage)

	// Health check endpoints (public - for monitoring)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReadiness)

	// Protected endpoints - using Unified Auth (User Session or API Key)
	// DevStore is used when no auth configured, providing full access

	// General Config
	mux.Handle("GET /api/config", s.require(storage.PermReadConfig, s.requireControlPlane(s.handleConfig)))
	mux.Handle("POST /api/config/apply", s.require(storage.PermWriteConfig, s.requireControlPlane(s.handleApplyConfig)))
	mux.Handle("POST /api/config/safe-apply", s.require(storage.PermWriteConfig, s.requireControlPlane(s.handleSafeApply)))
	mux.Handle("POST /api/config/confirm", s.require(storage.PermWriteConfig, s.requireControlPlane(s.handleConfirmApply)))
	mux.Handle("GET /api/config/pending", s.require(storage.PermReadConfig, s.requireControlPlane(s.handlePendingApply)))
	mux.Handle("POST /api/config/ip-forwarding", s.require(storage.PermWriteConfig, s.requireControlPlane(s.handleSetIPForwarding)))
	mux.Handle("POST /api/config/settings", s.require(storage.PermWriteConfig, s.requireControlPlane(s.handleSystemSettings)))

	// Upgrade
	mux.Handle("POST /api/system/upgrade", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleSystemUpgrade)))

	// Scanner
	scannerHandlers := NewScannerHandlers(s, s.client)
	scannerHandlers.RegisterRoutes(mux)

	// Status & Metrics (status is public, registered above)
	mux.Handle("GET /api/traffic", s.require(storage.PermReadMetrics, http.HandlerFunc(s.handleTraffic)))

	// User Management
	mux.Handle("GET /api/users", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleGetUsers)))
	mux.Handle("POST /api/users", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleCreateUser)))
	mux.Handle("GET /api/users/", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleGetUser)))
	mux.Handle("PUT /api/users/", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleUpdateUser)))
	mux.Handle("DELETE /api/users/", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleDeleteUser)))

	// Interface management
	mux.Handle("GET /api/interfaces", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleInterfaces))) // Using Config perms for now
	mux.Handle("GET /api/interfaces/available", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleAvailableInterfaces)))
	mux.Handle("POST /api/interfaces/update", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUpdateInterface)))
	mux.Handle("POST /api/vlans", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleCreateVLAN)))
	mux.Handle("DELETE /api/vlans", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleDeleteVLAN)))
	mux.Handle("POST /api/bonds", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleCreateBond)))
	mux.Handle("DELETE /api/bonds", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleDeleteBond)))

	// Services
	mux.Handle("GET /api/services", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleServices)))
	mux.Handle("GET /api/leases", s.require(storage.PermReadDHCP, http.HandlerFunc(s.handleLeases)))
	mux.Handle("GET /api/topology", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleGetTopology)))
	mux.Handle("GET /api/network", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleGetNetworkDevices)))

	// Config Sections (CRUD handlers usually switch on method, so we keep generic path or would need to register GET/POST separately)
	mux.Handle("GET /api/config/policies", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleGetPolicies)))
	mux.Handle("POST /api/config/policies", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleUpdatePolicies)))
	mux.Handle("GET /api/config/nat", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleGetNAT)))
	mux.Handle("POST /api/config/nat", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleUpdateNAT)))
	mux.Handle("GET /api/config/ipsets", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleGetIPSets)))
	mux.Handle("POST /api/config/ipsets", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleUpdateIPSets)))
	mux.Handle("GET /api/config/dhcp", s.require(storage.PermWriteDHCP, http.HandlerFunc(s.handleGetDHCP)))
	mux.Handle("POST /api/config/dhcp", s.require(storage.PermWriteDHCP, http.HandlerFunc(s.handleUpdateDHCP)))
	mux.Handle("GET /api/config/dns", s.require(storage.PermWriteDNS, http.HandlerFunc(s.handleGetDNS)))
	mux.Handle("POST /api/config/dns", s.require(storage.PermWriteDNS, http.HandlerFunc(s.handleUpdateDNS)))
	// Previously updated handlers here...
	mux.Handle("GET /api/config/routes", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleGetRoutes)))
	mux.Handle("POST /api/config/routes", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUpdateRoutes)))
	mux.Handle("GET /api/config/policy_routes", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleGetPolicyRoutes)))
	mux.Handle("POST /api/config/policy_routes", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUpdatePolicyRoutes)))
	mux.Handle("GET /api/config/zones", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleGetZones)))
	mux.Handle("POST /api/config/zones", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleUpdateZones)))
	mux.Handle("GET /api/config/protections", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleGetProtections)))
	mux.Handle("POST /api/config/protections", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleUpdateProtections)))
	mux.Handle("GET /api/config/qos", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleGetQoS)))
	mux.Handle("POST /api/config/qos", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleUpdateQoS)))
	mux.Handle("GET /api/config/scheduler", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleGetSchedulerConfig)))
	mux.Handle("POST /api/config/scheduler", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUpdateSchedulerConfig)))
	mux.Handle("GET /api/config/vpn", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleGetVPN)))
	mux.Handle("POST /api/config/vpn", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUpdateVPN)))
	mux.Handle("GET /api/config/mark_rules", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleGetMarkRules)))
	mux.Handle("POST /api/config/mark_rules", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUpdateMarkRules)))
	mux.Handle("GET /api/config/uid_routing", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleGetUIDRouting)))
	mux.Handle("POST /api/config/uid_routing", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUpdateUIDRouting)))

	// Reordering
	mux.Handle("POST /api/policies/reorder", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handlePolicyReorder)))
	mux.Handle("POST /api/rules/reorder", s.require(storage.PermWriteFirewall, http.HandlerFunc(s.handleRuleReorder)))

	// ClearPath Policy Editor - Enriched Rules API
	rulesHandler := NewRulesHandler(s, s.statsCollector, s.deviceLookup)
	mux.Handle("GET /api/rules", s.require(storage.PermReadFirewall, http.HandlerFunc(rulesHandler.HandleGetRules)))
	mux.Handle("GET /api/rules/flat", s.require(storage.PermReadFirewall, http.HandlerFunc(rulesHandler.HandleGetFlatRules)))
	mux.Handle("GET /api/rules/groups", s.require(storage.PermReadFirewall, http.HandlerFunc(rulesHandler.HandleGetRuleGroups)))

	// Uplink Management
	uplinkAPI := NewUplinkAPI(s.client)
	mux.Handle("GET /api/uplinks/groups", s.require(storage.PermReadConfig, http.HandlerFunc(uplinkAPI.HandleGetGroups)))
	mux.Handle("POST /api/uplinks/switch", s.require(storage.PermWriteConfig, http.HandlerFunc(uplinkAPI.HandleSwitch)))
	mux.Handle("POST /api/uplinks/toggle", s.require(storage.PermWriteConfig, http.HandlerFunc(uplinkAPI.HandleToggle)))

	// Flow Management
	flowHandlers := NewFlowHandlers(s.client)
	mux.Handle("GET /api/flows", s.require(storage.PermReadFirewall, http.HandlerFunc(flowHandlers.HandleGetFlows)))
	mux.Handle("POST /api/flows/approve", s.require(storage.PermWriteFirewall, http.HandlerFunc(flowHandlers.HandleApprove)))
	mux.Handle("POST /api/flows/deny", s.require(storage.PermWriteFirewall, http.HandlerFunc(flowHandlers.HandleDeny)))
	mux.Handle("DELETE /api/flows", s.require(storage.PermWriteFirewall, http.HandlerFunc(flowHandlers.HandleDelete)))

	// System actions
	mux.Handle("POST /api/system/reboot", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleReboot)))
	mux.Handle("GET /api/system/backup", s.require(storage.PermAdminBackup, http.HandlerFunc(s.handleBackup)))
	mux.Handle("POST /api/system/restore", s.require(storage.PermAdminBackup, http.HandlerFunc(s.handleRestore)))
	mux.Handle("POST /api/system/wol", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleWakeOnLAN)))

	// Safe Mode (emergency lockdown)
	mux.Handle("GET /api/system/safe-mode", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleSafeModeStatus)))
	mux.Handle("POST /api/system/safe-mode", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleEnterSafeMode)))
	mux.Handle("DELETE /api/system/safe-mode", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleExitSafeMode)))

	// Scheduler
	mux.Handle("GET /api/scheduler/status", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleSchedulerStatus)))
	mux.Handle("POST /api/scheduler/run", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleSchedulerRun)))

	// HCL editing (Advanced mode)
	mux.Handle("GET /api/config/hcl", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleGetRawHCL)))
	mux.Handle("POST /api/config/hcl", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleUpdateRawHCL)))
	mux.Handle("GET /api/config/hcl/section", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleGetSectionHCL)))
	mux.Handle("POST /api/config/hcl/section", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleUpdateSectionHCL)))
	mux.Handle("POST /api/config/hcl/validate", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleValidateHCL)))
	mux.Handle("POST /api/config/hcl/save", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleSaveConfig)))

	// Backup management
	mux.Handle("GET /api/backups", s.require(storage.PermAdminBackup, http.HandlerFunc(s.handleBackups)))
	mux.Handle("POST /api/backups/create", s.require(storage.PermAdminBackup, http.HandlerFunc(s.handleCreateBackup)))
	mux.Handle("POST /api/backups/restore", s.require(storage.PermAdminBackup, http.HandlerFunc(s.handleRestoreBackup)))
	mux.Handle("GET /api/backups/content", s.require(storage.PermAdminBackup, http.HandlerFunc(s.handleBackupContent)))
	mux.Handle("POST /api/backups/pin", s.require(storage.PermAdminBackup, http.HandlerFunc(s.handlePinBackup)))
	mux.Handle("GET /api/backups/settings", s.require(storage.PermAdminBackup, http.HandlerFunc(s.handleGetBackupSettings)))
	mux.Handle("POST /api/backups/settings", s.require(storage.PermAdminBackup, http.HandlerFunc(s.handleUpdateBackupSettings)))

	// Logging endpoints
	mux.Handle("GET /api/logs", s.require(storage.PermReadLogs, http.HandlerFunc(s.handleLogs)))
	mux.Handle("GET /api/logs/sources", s.require(storage.PermReadLogs, http.HandlerFunc(s.handleLogSources)))
	mux.Handle("GET /api/logs/stream", s.require(storage.PermReadLogs, http.HandlerFunc(s.handleLogStream)))
	mux.Handle("GET /api/logs/stats", s.require(storage.PermReadLogs, http.HandlerFunc(s.handleLogStats)))

	// Audit log endpoint
	mux.Handle("GET /api/audit", s.require(storage.PermReadAudit, http.HandlerFunc(s.handleAuditQuery)))

	// Extended System Operations
	mux.Handle("GET /api/system/stats", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleSystemStats)))
	mux.Handle("GET /api/system/routes", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleSystemRoutes)))

	// Import Wizard
	mux.Handle("POST /api/import/upload", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleImportUpload)))
	mux.Handle("/api/import/", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleImportConfig)))

	// IPSet management endpoints
	mux.Handle("GET /api/ipsets", s.require(storage.PermReadFirewall, http.HandlerFunc(s.handleIPSetList)))
	mux.Handle("/api/ipsets/", s.require(storage.PermReadFirewall, http.HandlerFunc(s.handleIPSetShow)))
	mux.Handle("GET /api/ipsets/cache/info", s.require(storage.PermReadFirewall, http.HandlerFunc(s.handleIPSetCacheInfo)))

	// Learning Engine
	mux.Handle("GET /api/learning/rules", s.require(storage.PermReadFirewall, http.HandlerFunc(s.handleLearningRules)))
	mux.Handle("/api/learning/rules/", s.require(storage.PermReadFirewall, http.HandlerFunc(s.handleLearningRule)))

	// Device Management
	mux.Handle("POST /api/devices/identity", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUpdateDeviceIdentity)))
	mux.Handle("POST /api/devices/link", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleLinkMAC)))
	mux.Handle("POST /api/devices/unlink", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUnlinkMAC)))
	mux.Handle("GET /api/devices", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleGetDevices)))

	// Staging & Diff
	mux.Handle("GET /api/config/diff", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleGetConfigDiff)))
	mux.Handle("POST /api/config/discard", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleDiscardConfig)))
	mux.Handle("GET /api/config/pending-status", s.require(storage.PermReadConfig, http.HandlerFunc(s.handlePendingStatus)))

	mux.Handle("/metrics", promhttp.Handler())

	// Serve SPA static files with fallback to index.html
	if s.Assets != nil {
		mux.Handle("/", s.spaHandler(s.Assets, "index.html"))
	}
}

// require middleware ensures the control plane client is available
func (s *Server) requireControlPlane(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.client == nil {
			WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control Plane Disconnected")
			return
		}
		next(w, r)
	}
}

// spaHandler serves static files, falling back to index.html for client-side routing.
//
// Security Analysis (Path Traversal): SAFE
// - Uses fs.FS interface which is confined to the embedded UI assets directory
// - assets.Open(path) cannot escape the fs.FS root regardless of ".." sequences
// - http.FileServer sanitizes paths before serving
// - Fallback to index.html only affects SPA routes, not arbitrary files
func (s *Server) spaHandler(assets fs.FS, fallback string) http.Handler {
	fileServer := http.FileServer(http.FS(assets))

	// Read index.html content once
	indexContent, err := fs.ReadFile(assets, fallback)
	if err != nil {
		logging.Error(fmt.Sprintf("Failed to read SPA fallback file %s: %v. Assets nil? %v", fallback, err, s.Assets == nil))
		indexContent = []byte("SPA Fallback Error")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prepare path (strip leading slash)
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// logging.APILog("debug", "SPA Handler: Request Path: '%s' -> '%s'", r.URL.Path, path)

		// Try to open the file to check if it exists
		f, err := assets.Open(path)
		if err == nil {
			stat, _ := f.Stat()
			isDir := stat.IsDir()
			f.Close()

			if !isDir {
				// File exists and is not a directory, serve it
				// logging.APILog("debug", "SPA Handler: File found: %s", path)
				fileServer.ServeHTTP(w, r)
				return
			}
			// If it's a directory, fall through to fallback behavior (unless it's an asset path)
		} else {
			// logging.APILog("debug", "SPA Handler: File not found: %s", path)
		}

		// Assert: File missing OR is directory.

		// If looking for static assets (js/css/images usually in _app or assets), return 404
		// to avoid serving HTML as JS (which causes syntax errors/redirect loops)
		if strings.HasPrefix(path, "_app/") || strings.HasPrefix(path, "assets/") {
			http.NotFound(w, r)
			return
		}

		// For everything else (SPA Routes/Directories), serve index.html directly
		// logging.APILog("debug", "SPA Handler: Serving fallback content for: %s", r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Now(), bytes.NewReader(indexContent))
	})
}

// Handler returns the HTTP handler with security middleware applied
// Mitigation: OWASP A01:2021-Broken Access Control (CSRF prevention)
func (s *Server) Handler() http.Handler {
	// Apply CSRF middleware to protect against cross-site request forgery
	csrfMiddleware := CSRFMiddleware(s.csrfManager, s.authStore)

	// Chain: AccessLog -> CSRF -> i18n -> Mux
	return AccessLogger(csrfMiddleware(i18n.Middleware(s.mux)))
}

// Batch Request/Response types
type BatchRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   any    `json:"body"` // Optional
}

type BatchResponse struct {
	Status int `json:"status"`
	Body   any `json:"body"`
}

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	var requests []BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&requests); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	// Limit batch size
	if len(requests) > 20 {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Too many requests in batch")
		return
	}

	// Apply Rate Limiting based on Batch Size
	// Cost is 1 token per request in the batch
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}

	// Check if the ratelimiter allows 'n' events
	// Use a separate "batch" namespace to avoid conflicting with auth login limits directly,
	// but enforce a reasonable total load limit (e.g., 60 ops/minute).
	if !s.rateLimiter.AllowN("batch:"+ip, 60, time.Minute, len(requests)) {
		WriteErrorCtx(w, r, http.StatusTooManyRequests, "Rate limit exceeded for batch")
		return
	}

	responses := make([]BatchResponse, len(requests))
	// Execute requests serially
	for i, req := range requests {
		// specific implementation
		// We need to construct a new wrapper request

		// Body handling
		var bodyReader io.Reader
		if req.Body != nil {
			b, err := json.Marshal(req.Body)
			if err == nil {
				bodyReader = bytes.NewReader(b)
			}
		}

		subReq, err := http.NewRequest(req.Method, req.Path, bodyReader)
		if err != nil {
			responses[i] = BatchResponse{Status: 500, Body: "Failed to create request: " + err.Error()}
			continue
		}

		// Propagate RemoteAddr for downstream rate limiting (IMPORTANT)
		subReq.RemoteAddr = r.RemoteAddr

		// Copy headers from batch request

		// Copy headers from batch request?
		// Maybe Auth header?
		subReq.Header.Set("Content-Type", "application/json")
		if auth := r.Header.Get("Authorization"); auth != "" {
			subReq.Header.Set("Authorization", auth)
		}
		// Cookie?
		for _, c := range r.Cookies() {
			subReq.AddCookie(c)
		}

		// Capture response using our own writer (avoids httptest dependency)
		rr := &batchResponseWriter{header: make(http.Header), statusCode: http.StatusOK}
		s.mux.ServeHTTP(rr, subReq)

		// Parse body if JSON
		var respBody any
		if rr.body.Len() > 0 {
			if contentType := rr.header.Get("Content-Type"); strings.Contains(contentType, "application/json") {
				_ = json.Unmarshal(rr.body.Bytes(), &respBody)
			} else {
				respBody = rr.body.String()
			}
		}

		responses[i] = BatchResponse{
			Status: rr.statusCode,
			Body:   respBody,
		}
	}

	WriteJSON(w, http.StatusOK, responses)
}

func (s *Server) handleBrand(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, brand.Get())
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Write(openAPISpec)
}

func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	// Simple Swagger UI HTML
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="description" content="SwaggerHTT" />
    <title>Glacic API Documentation</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" crossorigin></script>
<script>
    window.onload = () => {
        window.ui = SwaggerUIBundle({
            url: '/api/openapi.yaml',
            dom_id: '#swagger-ui',
        });
    };
</script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// Start starts the HTTP server.
// Mitigation: OWASP A02:2021-Cryptographic Failures (TLS encryption)
func (s *Server) Start(addr string) error {
	// Create server with proper timeouts
	cfg := DefaultServerConfig()
	server := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	// Start metrics collector
	go s.collector.Start()

	// Update system metrics
	uptime := time.Since(s.startTime)
	s.collector.UpdateSystemMetrics(uptime)

	// Check if TLS is configured
	// TLS is enabled if tls_listen is set OR if tls_cert/tls_key are set
	tlsEnabled := s.Config != nil && s.Config.API != nil &&
		(s.Config.API.TLSListen != "" ||
			(s.Config.API.TLSCert != "" && s.Config.API.TLSKey != ""))

	if tlsEnabled {
		// Use default cert paths if not specified
		tlsCert := s.Config.API.TLSCert
		tlsKey := s.Config.API.TLSKey
		if tlsCert == "" {
			tlsCert = "/var/lib/glacic/certs/server.crt"
		}
		if tlsKey == "" {
			tlsKey = "/var/lib/glacic/certs/server.key"
		}

		// Auto-generate self-signed cert if missing
		// We use a 1 year validity for self-signed certs
		if _, err := tls.EnsureCertificate(tlsCert, tlsKey, 365); err != nil {
			logging.APILog("error", "Failed to ensure TLS certificate: %v", err)
			return err
		}

		// Determine TLS listen address (use TLSListen if set, otherwise addr)
		tlsAddr := addr
		if s.Config.API.TLSListen != "" {
			tlsAddr = s.Config.API.TLSListen
		}

		// Create TLS server
		tlsServer := &http.Server{
			Addr:              tlsAddr,
			Handler:           s.Handler(),
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			MaxHeaderBytes:    cfg.MaxHeaderBytes,
		}

		// Start HTTP redirect server (unless disabled)
		if !s.Config.API.DisableHTTPRedirect {
			go s.startHTTPRedirectServer()
		}

		logging.APILog("info", "API server starting with TLS on %s", tlsAddr)
		return tlsServer.ListenAndServeTLS(tlsCert, tlsKey)
	}

	logging.APILog("info", "API server starting on %s (no TLS)", addr)
	return server.ListenAndServe()
}

// startHTTPRedirectServer starts a plain HTTP server that redirects to HTTPS.
func (s *Server) startHTTPRedirectServer() {
	// Determine listen address for HTTP redirect
	httpAddr := ":8080" // default
	if s.Config != nil && s.Config.API != nil && s.Config.API.Listen != "" {
		httpAddr = s.Config.API.Listen
	}

	redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Build HTTPS URL - use port 8443 for internal redirect, or 443 for external
		target := "https://" + r.Host
		// If connecting to 8080, redirect to 8443
		// If connecting to 80 (DNATed), redirect to 443
		if strings.Contains(r.Host, ":8080") {
			target = strings.Replace(target, ":8080", ":8443", 1)
		} else if !strings.Contains(r.Host, ":") {
			// No port in Host header, assume standard ports (80->443)
			// Keep target as-is (will use default 443)
		}
		target += r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})

	redirectServer := &http.Server{
		Addr:              httpAddr,
		Handler:           redirectHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logging.APILog("info", "HTTP redirect server starting on %s -> HTTPS", httpAddr)
	if err := redirectServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logging.APILog("error", "HTTP redirect server error: %v", err)
	}
}

// ServeListener starts the API server using an existing listener.
// This is used during seamless upgrades when the listener is handed off.
func (s *Server) ServeListener(listener net.Listener) error {
	cfg := DefaultServerConfig()
	server := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	go s.collector.Start()

	// Start learning service if enabled
	if s.learning != nil {
		if err := s.learning.Start(); err != nil {
			s.logger.Error("Failed to start learning service", "error", err)
		} else {
			s.logger.Info("Learning service started successfully")
		}
	}

	logging.APILog("info", "API server starting on handed-off listener %s", listener.Addr())
	return server.Serve(listener)
}

// loggingMiddleware logs all API requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := clock.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		// Log the request (skip static assets and metrics)
		if !strings.HasPrefix(r.URL.Path, "/assets/") && r.URL.Path != "/metrics" {
			level := "info"
			if wrapped.statusCode >= 400 {
				level = "warn"
			}
			if wrapped.statusCode >= 500 {
				level = "error"
			}
			logging.APILog(level, "%s %s %d %v", r.Method, r.URL.Path, wrapped.statusCode, duration.Round(time.Millisecond))
		}
	})
}

// maxBodyMiddleware limits the size of request bodies to prevent memory exhaustion.
// Mitigation: OWASP A05:2021-Security Misconfiguration
func (s *Server) maxBodyMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip body limit for GET/HEAD/OPTIONS
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Check Content-Length header first (fast path)
			if r.ContentLength > maxBytes {
				http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
				return
			}

			// Wrap body with LimitReader for streaming requests
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Implement http.Flusher for SSE support
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Implement http.Hijacker for websocket support
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijack not supported")
}

// batchResponseWriter captures HTTP responses for batch API processing.
// This avoids importing net/http/httptest in production code.
type batchResponseWriter struct {
	header     http.Header
	body       bytes.Buffer
	statusCode int
}

func (w *batchResponseWriter) Header() http.Header {
	return w.header
}

func (w *batchResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *batchResponseWriter) WriteHeader(code int) {
	w.statusCode = code
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	// Support source=running to get raw running config without status
	if r.URL.Query().Get("source") == "running" {
		if s.client != nil {
			cfg, err := s.client.GetConfig()
			if err != nil {
				WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
				return
			}
			WriteJSON(w, http.StatusOK, cfg)
		} else {
			WriteJSON(w, http.StatusOK, s.Config)
		}
		return
	}

	// Default: Return config with _status fields for UI
	s.configMu.RLock()
	staged := s.Config
	s.configMu.RUnlock()

	if s.client != nil {
		// Get running config to compute status
		running, err := s.client.GetConfig()
		if err != nil {
			// Can't reach control plane - return staged without status
			WriteJSON(w, http.StatusOK, staged)
			return
		}
		// Return config with _status fields for each item
		configWithStatus := BuildConfigWithStatus(staged, running)
		WriteJSON(w, http.StatusOK, configWithStatus)
	} else {
		// No client - return raw config
		WriteJSON(w, http.StatusOK, staged)
	}
}

// getServerInfo returns comprehensive server information.
func (s *Server) getServerInfo() ServerInfo {
	uptime := time.Since(s.startTime)

	info := ServerInfo{
		Status:       "online",
		Uptime:       uptime.String(),
		StartTime:    s.startTime.Format(time.RFC3339),
		Version:      brand.Version,
		BuildTime:    brand.BuildTime,
		BuildArch:    brand.BuildArch,
		GitCommit:    brand.GitCommit,
		GitBranch:    brand.GitBranch,
		GitMergeBase: brand.GitMergeBase,
	}

	// Get host uptime from /proc/uptime (Linux)
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if seconds, err := strconv.ParseFloat(fields[0], 64); err == nil {
				info.HostUptime = time.Duration(seconds * float64(time.Second)).String()
			}
		}
	}

	return info
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Build comprehensive server info
	info := s.getServerInfo()

	// Use RPC client if available for control plane status
	if s.client != nil {
		// 1. Basic Status
		status, err := s.client.GetStatus()
		if err == nil {
			info.Uptime = status.Uptime
			info.FirewallActive = status.FirewallActive
		}

		// 2. System Stats (CPU/Mem)
		sysStats, err := s.client.GetSystemStats()
		if err == nil {
			info.CPULoad = sysStats.CPUUsage
			if sysStats.MemoryTotal > 0 {
				info.MemUsage = float64(sysStats.MemoryUsed) / float64(sysStats.MemoryTotal) * 100
			}
		}

		// 3. Log Stats (Blocked Count)
		// Try to get log stats - might fail if logging not ready, ignore error
		// LogStats not explicitly exposed in client interface?
		// Use manual log stats if needed or check if defined.
		// Assuming GetLogStats exists based on types.
		// Is it exposed in ControlPlaneClient interface?
		// If not, we skip. But types.go had GetLogStatsReply. The client interface must match.

		// 4. WAN IP
		// Find WAN interface from config
		var wanIfaceName string
		if s.Config != nil {
			for _, iface := range s.Config.Interfaces {
				if strings.ToUpper(iface.Zone) == "WAN" {
					wanIfaceName = iface.Name
					break
				}
			}
		}

		// Get interface statuses to find IP
		if wanIfaceName != "" {
			ifaces, err := s.client.GetInterfaces()
			if err == nil {
				for _, iface := range ifaces {
					if iface.Name == wanIfaceName {
						if len(iface.IPv4Addrs) > 0 {
							info.WanIP = iface.IPv4Addrs[0]
						}
						break
					}
				}
			}
		} else {
			// Fallback: Try to find interface named "eth0" or "wan" if no config
			ifaces, err := s.client.GetInterfaces()
			if err == nil {
				for _, iface := range ifaces {
					if iface.Name == "eth0" || iface.Name == "wan" {
						if len(iface.IPv4Addrs) > 0 {
							info.WanIP = iface.IPv4Addrs[0]
						}
						break
					}
				}
			}
		}
	}

	WriteJSON(w, http.StatusOK, info)
}

// ServerInfo contains comprehensive information about the server.
type ServerInfo struct {
	Status          string  `json:"status"`
	Uptime          string  `json:"uptime"`                     // Router process uptime
	HostUptime      string  `json:"host_uptime,omitempty"`      // System uptime
	StartTime       string  `json:"start_time"`                 // When router started
	Version         string  `json:"version"`                    // Software version
	BuildTime       string  `json:"build_time,omitempty"`       // When binary was built
	BuildArch       string  `json:"build_arch,omitempty"`       // Build architecture
	GitCommit       string  `json:"git_commit,omitempty"`       // Git commit hash
	GitBranch       string  `json:"git_branch,omitempty"`       // Git branch name
	GitMergeBase    string  `json:"git_merge_base,omitempty"`   // Git merge-base with main
	FirewallActive  bool    `json:"firewall_active"`            // Whether firewall is running
	FirewallApplied string  `json:"firewall_applied,omitempty"` // When rules were last applied
	WanIP           string  `json:"wan_ip,omitempty"`           // WAN IP Address
	BlockedCount    int64   `json:"blocked_count"`              // Total blocked packets (last 24h/session)
	CPULoad         float64 `json:"cpu_load"`                   // CPU Usage %
	MemUsage        float64 `json:"mem_usage"`                  // Memory Usage %
}

func (s *Server) handleLeases(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		leases, err := s.client.GetDHCPLeases()
		if err != nil {
			WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if leases == nil {
			leases = []ctlplane.DHCPLease{}
		}
		WriteJSON(w, http.StatusOK, leases)
	} else {
		WriteJSON(w, http.StatusOK, []interface{}{})
	}
}

// Auth handlers

// handleGetUsers returns all users

// handleCreateUser creates a new user

// handleUserByName handles individual user operations (update, delete)
// handleGetUser returns a single user

// handleUpdateUser updates a user

// handleDeleteUser deletes a user

// --- Interface Management Handlers ---

// handleCreateVLAN creates a new VLAN interface

// handleDeleteVLAN deletes a VLAN interface

// handleCreateBond creates a new bond interface

// handleDeleteBond deletes a bond interface

// handleGetConfigDiff returns the diff between saved and in-memory config
// handleGetConfigDiff returns the diff between saved and in-memory config
func (s *Server) handleGetConfigDiff(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	// 1. Get Running Config
	runningCfg, err := s.client.GetConfig()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to fetch running config: "+err.Error())
		return
	}

	// 2. Get Staged Config
	stagedCfg := s.Config

	// 3. Marshal to JSON for comparison
	runningJSON, _ := json.MarshalIndent(runningCfg, "", "  ")
	stagedJSON, _ := json.MarshalIndent(stagedCfg, "", "  ")

	// 4. Generate Diff
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(runningJSON)),
		B:        difflib.SplitLines(string(stagedJSON)),
		FromFile: "Running",
		ToFile:   "Staged",
		Context:  3,
	}
	text, _ := difflib.GetUnifiedDiffString(diff)

	if text == "" {
		text = "No changes."
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(text))
}

// handlePublicCert serves the root CA certificate publicly
func (s *Server) handlePublicCert(w http.ResponseWriter, r *http.Request) {
	// Root cert is in config dir
	certPath := "/etc/glacic/certs/cert.pem" // Default
	if s.Config != nil {
		// Try to deduce cert dir. Usually flags passed to ctl.
		// For this quick win, we'll try standard path or relative.
		// Ideally we inject CertManager into Server.
	}

	// Try standard location
	data, err := os.ReadFile(certPath)
	if err != nil {
		// Try local dev path
		data, err = os.ReadFile("local/certs/cert.pem")
		if err != nil {
			WriteErrorCtx(w, r, http.StatusNotFound, "Certificate not found")
			return
		}
	}

	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", "attachment; filename=glacic-ca.crt")
	w.Write(data)
}

// handleTraffic returns traffic accounting statistics
func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	// Integrate with firewall.Accounting to get real stats
	if s.collector == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Collector not initialized")
		return
	}
	stats := s.collector.GetInterfaceStats()
	WriteJSON(w, http.StatusOK, stats)
}

// handleServices returns the available service definitions
func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	// Build response with all service definitions from firewall package
	type ServiceInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Ports       []struct {
			Port     int    `json:"port"`
			EndPort  int    `json:"end_port,omitempty"`
			Protocol string `json:"protocol"`
		} `json:"ports"`
	}

	services := make([]ServiceInfo, 0, len(firewall.BuiltinServices))
	for name, svc := range firewall.BuiltinServices {
		info := ServiceInfo{
			Name:        name,
			Description: svc.Description,
		}
		// Handle protocol flags and ports
		if svc.Protocol&firewall.ProtoTCP != 0 {
			if len(svc.Ports) > 0 {
				for _, p := range svc.Ports {
					info.Ports = append(info.Ports, struct {
						Port     int    `json:"port"`
						EndPort  int    `json:"end_port,omitempty"`
						Protocol string `json:"protocol"`
					}{Port: p, Protocol: "tcp"})
				}
			} else if svc.Port > 0 {
				info.Ports = append(info.Ports, struct {
					Port     int    `json:"port"`
					EndPort  int    `json:"end_port,omitempty"`
					Protocol string `json:"protocol"`
				}{Port: svc.Port, EndPort: svc.EndPort, Protocol: "tcp"})
			}
		}
		if svc.Protocol&firewall.ProtoUDP != 0 {
			if len(svc.Ports) > 0 {
				for _, p := range svc.Ports {
					info.Ports = append(info.Ports, struct {
						Port     int    `json:"port"`
						EndPort  int    `json:"end_port,omitempty"`
						Protocol string `json:"protocol"`
					}{Port: p, Protocol: "udp"})
				}
			} else if svc.Port > 0 {
				info.Ports = append(info.Ports, struct {
					Port     int    `json:"port"`
					EndPort  int    `json:"end_port,omitempty"`
					Protocol string `json:"protocol"`
				}{Port: svc.Port, EndPort: svc.EndPort, Protocol: "udp"})
			}
		}
		if svc.Protocol&firewall.ProtoICMP != 0 {
			info.Ports = append(info.Ports, struct {
				Port     int    `json:"port"`
				EndPort  int    `json:"end_port,omitempty"`
				Protocol string `json:"protocol"`
			}{Protocol: "icmp"})
		}
		services = append(services, info)
	}

	WriteJSON(w, http.StatusOK, services)
}

// handleDiscardConfig discards staged changes by reloading from the control plane
func (s *Server) handleDiscardConfig(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	cfg, err := s.client.GetConfig()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to fetch running config: "+err.Error())
		return
	}

	// Overwrite staged config
	s.Config = cfg
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handlePendingStatus returns whether there are pending changes
func (s *Server) handlePendingStatus(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"has_changes": false,
			"reason":      "no control plane",
		})
		return
	}

	// Get Running Config
	runningCfg, err := s.client.GetConfig()
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"has_changes": false,
			"reason":      "failed to get running config",
		})
		return
	}

	// Compare by JSON serialization
	runningJSON, _ := json.Marshal(runningCfg)
	stagedJSON, _ := json.Marshal(s.Config)

	hasChanges := string(runningJSON) != string(stagedJSON)

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"has_changes": hasChanges,
	})
}

// --- Config Update Handlers ---

// handleApplyConfig applies the current configuration
func (s *Server) handleApplyConfig(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	w.Header().Set("Content-Type", "application/json")

	if s.client != nil {
		s.configMu.RLock()
		cfg := s.Config.Clone()
		s.configMu.RUnlock()

		if err := s.client.ApplyConfig(cfg); err != nil {
			WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
			return
		}

		// Save config to HCL file for persistence across restarts
		if _, err := s.client.SaveConfig(); err != nil {
			// Log warning but don't fail - runtime config is already applied
			WriteJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"warning": fmt.Sprintf("Config applied but failed to save: %v", err),
			})
			return
		}

		// Create backup AFTER successful apply - captures known-good state
		backupReply, _ := s.client.CreateBackup("Applied configuration", false)
		backupVersion := 0
		if backupReply != nil && backupReply.Success {
			backupVersion = backupReply.Backup.Version
		}

		// Push config update and notification to WebSocket subscribers
		if s.wsManager != nil {
			s.wsManager.Publish("config", s.Config)
			s.wsManager.PublishNotification(NotifySuccess, "Configuration Applied", "Firewall rules have been updated")
		}

		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"backup_version": backupVersion,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetPolicies returns firewall policies

// handleUpdatePolicies updates firewall policies

// handleGetNAT returns NAT configuration

// handleUpdateNAT updates NAT configuration

// handleGetMarkRules returns mark rules

// handleUpdateMarkRules updates mark rules

// handleGetUIDRouting returns UID routing rules

// handleUpdateUIDRouting updates UID routing rules

// handleGetIPSets returns IPSet configuration

// handleUpdateIPSets updates IPSet configuration

// handleGetDHCP returns DHCP server configuration

// handleUpdateDHCP updates DHCP server configuration

// handleGetDNS returns DNS server configuration

// handleUpdateDNS updates DNS server configuration

// handleGetRoutes returns static route configuration

// handleUpdateRoutes updates static route configuration

// handleGetZones returns zone configuration

// handleUpdateZones updates zone configuration

// handleGetProtections returns per-interface protection settings

// handleUpdateProtections updates per-interface protection settings

// handleGetVPN returns the current VPN configuration

// handleUpdateVPN updates the VPN configuration

// handleGetQoS returns QoS policies configuration

// handleUpdateQoS updates QoS policies configuration

// handleReboot handles system reboot request

// handleBackup exports the current configuration as JSON

// handleRestore imports a configuration from JSON

// handleGetSchedulerConfig returns scheduler configuration

// handleUpdateSchedulerConfig updates scheduler configuration

// handleSchedulerStatus returns the status of all scheduled tasks

// handleSchedulerRun triggers immediate execution of a scheduled task

// handlePolicyReorder reorders policies
func (s *Server) handlePolicyReorder(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	var req struct {
		PolicyName string   `json:"policy_name"`         // Policy to move
		Position   string   `json:"position"`            // "before" or "after"
		RelativeTo string   `json:"relative_to"`         // Target policy name
		NewOrder   []string `json:"new_order,omitempty"` // Or provide complete new order
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	// If new_order is provided, use it directly
	if len(req.NewOrder) > 0 {
		policyMap := make(map[string]config.Policy)
		for _, p := range s.Config.Policies {
			policyMap[p.Name] = p
		}

		newPolicies := make([]config.Policy, 0, len(req.NewOrder))
		for _, name := range req.NewOrder {
			if p, ok := policyMap[name]; ok {
				newPolicies = append(newPolicies, p)
			}
		}
		s.Config.Policies = newPolicies
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}

	// Otherwise, move single policy relative to another
	if req.PolicyName == "" || req.RelativeTo == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "policy_name and relative_to are required")
		return
	}

	// Find indices
	var moveIdx, targetIdx int = -1, -1
	for i, p := range s.Config.Policies {
		if p.Name == req.PolicyName {
			moveIdx = i
		}
		if p.Name == req.RelativeTo {
			targetIdx = i
		}
	}

	if moveIdx == -1 || targetIdx == -1 {
		WriteErrorCtx(w, r, http.StatusNotFound, "Policy not found")
		return
	}

	// Remove policy from current position
	policy := s.Config.Policies[moveIdx]
	policies := append(s.Config.Policies[:moveIdx], s.Config.Policies[moveIdx+1:]...)

	// Adjust target index if needed
	if moveIdx < targetIdx {
		targetIdx--
	}

	// Insert at new position
	insertIdx := targetIdx
	if req.Position == "after" {
		insertIdx++
	}

	// Insert
	newPolicies := make([]config.Policy, 0, len(policies)+1)
	newPolicies = append(newPolicies, policies[:insertIdx]...)
	newPolicies = append(newPolicies, policy)
	newPolicies = append(newPolicies, policies[insertIdx:]...)

	s.configMu.Lock()
	s.Config.Policies = newPolicies
	s.configMu.Unlock()

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleRuleReorder reorders rules within a policy
func (s *Server) handleRuleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		PolicyName string   `json:"policy_name"`         // Policy containing the rules
		RuleName   string   `json:"rule_name"`           // Rule to move
		Position   string   `json:"position"`            // "before" or "after"
		RelativeTo string   `json:"relative_to"`         // Target rule name
		NewOrder   []string `json:"new_order,omitempty"` // Or provide complete new order
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.PolicyName == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "policy_name is required")
		return
	}

	// Find the policy
	var policyIdx int = -1
	s.configMu.RLock()
	for i, p := range s.Config.Policies {
		if p.Name == req.PolicyName {
			policyIdx = i
			break
		}
	}
	s.configMu.RUnlock()

	if policyIdx == -1 {
		WriteErrorCtx(w, r, http.StatusNotFound, "Policy not found")
		return
	}

	policy := &s.Config.Policies[policyIdx]

	// If new_order is provided, use it directly
	if len(req.NewOrder) > 0 {
		ruleMap := make(map[string]config.PolicyRule)
		for _, r := range policy.Rules {
			ruleMap[r.Name] = r
		}

		newRules := make([]config.PolicyRule, 0, len(req.NewOrder))
		for _, name := range req.NewOrder {
			if r, ok := ruleMap[name]; ok {
				newRules = append(newRules, r)
			}
		}
		policy.Rules = newRules
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}

	// Otherwise, move single rule relative to another
	if req.RuleName == "" || req.RelativeTo == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "rule_name and relative_to are required")
		return
	}

	// Find indices
	var moveIdx, targetIdx int = -1, -1
	for i, r := range policy.Rules {
		if r.Name == req.RuleName {
			moveIdx = i
		}
		if r.Name == req.RelativeTo {
			targetIdx = i
		}
	}

	if moveIdx == -1 || targetIdx == -1 {
		WriteErrorCtx(w, r, http.StatusNotFound, "Rule not found")
		return
	}

	// Remove rule from current position
	rule := policy.Rules[moveIdx]
	rules := append(policy.Rules[:moveIdx], policy.Rules[moveIdx+1:]...)

	// Adjust target index if needed
	if moveIdx < targetIdx {
		targetIdx--
	}

	// Insert at new position
	insertIdx := targetIdx
	if req.Position == "after" {
		insertIdx++
	}

	// Insert
	newRules := make([]config.PolicyRule, 0, len(rules)+1)
	newRules = append(newRules, rules[:insertIdx]...)
	newRules = append(newRules, rule)
	newRules = append(newRules, rules[insertIdx:]...)
	policy.Rules = newRules

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// --- HCL Editing Handlers (Advanced Mode) ---

// handleSetIPForwarding toggles the global ip_forwarding setting

// SystemSettingsRequest represents the payload for updating system settings

// handleSystemSettings handles global system settings updates

// handleGetRawHCL handles GET for the entire config as raw HCL

// handleUpdateRawHCL handles PUT/POST for the entire config as raw HCL

// handleSectionHCL handles GET/PUT for a specific config section as raw HCL
// handleGetSectionHCL handles GET for a specific config section as raw HCL

// handleUpdateSectionHCL handles PUT/POST for a specific config section as raw HCL

// handleValidateHCL validates HCL without applying it

// handleSaveConfig saves the current config to disk

// --- Backup Management Handlers ---

// handleBackups lists all available backups

// handleCreateBackup creates a new manual backup

// handleRestoreBackup restores a specific backup version

// handleBackupContent returns the content of a specific backup

// --- Safe Apply Handlers ---

// handleSafeApply applies config with connectivity verification and rollback

// handleConfirmApply confirms a pending apply (placeholder for full implementation)

// handlePendingApply returns info about any pending apply

// handlePinBackup pins or unpins a backup

// handleGetBackupSettings gets backup settings

// handleUpdateBackupSettings updates backup settings

// handleUIMenu returns the navigation menu schema.
func (s *Server) handleUIMenu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := ui.MenuJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

// handleUIPages returns all page schemas.
func (s *Server) handleUIPages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	data, err := ui.AllPagesJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

// handleUIPage returns a single page schema.
func (s *Server) handleUIPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract page ID from path: /api/ui/page/{id}
	pageID := ui.MenuID(strings.TrimPrefix(r.URL.Path, "/api/ui/page/"))
	if pageID == "" {
		http.Error(w, "Page ID required", http.StatusBadRequest)
		return
	}

	page := ui.GetPage(pageID)
	if page == nil {
		WriteErrorCtx(w, r, http.StatusNotFound, "Page not found")
		return
	}

	WriteJSON(w, http.StatusOK, page)
}

// handleHealth returns the overall health status.
// Uses the health package to check system components.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Create a health checker with some basic checks
	checker := health.NewChecker()

	// Add nftables check (cached)
	checker.Register("nftables", func(ctx context.Context) health.Check {
		if s.healthy.Load() {
			return health.Check{
				Name:    "nftables",
				Status:  health.StatusHealthy,
				Message: "NFTables is responding",
			}
		}
		return health.Check{
			Name:    "nftables",
			Status:  health.StatusUnhealthy,
			Message: "NFTables is unresponsive",
		}
	})

	checker.Register("control-plane", func(ctx context.Context) health.Check {
		// Check if control plane is accessible
		if s.client == nil {
			return health.Check{
				Name:    "control-plane",
				Status:  health.StatusDegraded,
				Message: "Control plane client not initialized",
			}
		}

		// Try to get status from control plane
		_, err := s.client.GetStatus()
		if err != nil {
			return health.Check{
				Name:    "control-plane",
				Status:  health.StatusUnhealthy,
				Message: fmt.Sprintf("Control plane unreachable: %v", err),
			}
		}
		return health.Check{
			Name:    "control-plane",
			Status:  health.StatusHealthy,
			Message: "Control plane is responding",
		}
	})

	checker.Register("config", func(ctx context.Context) health.Check {
		// Check if config is loaded and valid
		if s.Config == nil {
			return health.Check{
				Name:    "config",
				Status:  health.StatusUnhealthy,
				Message: "No configuration loaded",
			}
		}
		return health.Check{
			Name:    "config",
			Status:  health.StatusHealthy,
			Message: "Configuration loaded successfully",
		}
	})

	// Run health checks
	report := checker.Check(context.Background())

	// Set HTTP status based on overall health
	var statusCode int
	switch report.Status {
	case health.StatusHealthy:
		statusCode = http.StatusOK
	case health.StatusDegraded:
		statusCode = http.StatusOK // Still 200 but indicates issues
	case health.StatusUnhealthy:
		statusCode = http.StatusServiceUnavailable
	default:
		statusCode = http.StatusServiceUnavailable
	}

	WriteJSON(w, statusCode, report)
}

// handleReadiness returns readiness status for Kubernetes-style probes.
// Simpler check - just verifies basic services are ready.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Simple readiness check - are we ready to serve requests?
	ready := true
	message := "Ready"

	if s.Config == nil {
		ready = false
		message = "Configuration not loaded"
	} else if s.client == nil {
		ready = false
		message = "Control plane not connected"
	}

	if ready {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ready",
			"message": message,
		})
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "not ready",
			"message": message,
		})
	}
}

// ==============================================================================
// Monitoring Handlers
// ==============================================================================

// MonitoringOverview is the combined monitoring data response.

// handleMonitoringOverview returns all monitoring data in one request.

// handleMonitoringInterfaces returns interface traffic statistics.

// handleMonitoringPolicies returns firewall policy statistics.

// handleMonitoringServices returns service statistics (DHCP, DNS).

// handleMonitoringSystem returns system statistics (CPU, memory, load).

// handleMonitoringConntrack returns connection tracking statistics.

// require checks for sufficient permission from EITHER an API Key OR a User Session.
func (s *Server) require(perm storage.Permission, handler http.Handler) http.Handler {
	// Chain: handler -> audit -> CSRF -> auth check
	auditedHandler := s.auditMiddleware(handler)

	// Apply CSRF protection to the inner handler
	// The CSRF middleware itself handles skipping for API keys or non-state-changing methods
	protectedHandler := CSRFMiddleware(s.csrfManager, s.authStore)(auditedHandler)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bypass auth if not required by configuration (replicates legacy behavior)

		s.configMu.RLock()
		s.logger.Info("require middleware: checking bypass", "RequireAuth", s.Config.API.RequireAuth)
		if s.Config != nil && s.Config.API != nil && !s.Config.API.RequireAuth {
			s.configMu.RUnlock()
			s.logger.Info("require middleware: bypassing auth")
			handler.ServeHTTP(w, r)
			return
		}
		s.configMu.RUnlock()

		// 1. API Key Check
		authHeader := r.Header.Get("Authorization")
		var apiKeyStr string
		if strings.HasPrefix(authHeader, "Bearer ") {
			// Could be Session Token OR API Key.
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if strings.HasPrefix(token, "gfw_") && s.apiKeyManager != nil {
				apiKeyStr = token
			}
		} else if strings.HasPrefix(authHeader, "ApiKey ") {
			apiKeyStr = strings.TrimPrefix(authHeader, "ApiKey ")
		} else {
			apiKeyStr = r.Header.Get("X-API-Key")
		}

		if apiKeyStr != "" && s.apiKeyManager != nil {
			key, err := s.apiKeyManager.ValidateKey(apiKeyStr)
			if err == nil {
				// Valid API Key found
				if key.HasPermission(perm) {
					// Success! Inject key into context
					ctx := WithAPIKey(r.Context(), key)
					protectedHandler.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				// Key exists but lacks permission
				writeAuthError(w, http.StatusForbidden, fmt.Sprintf("api key permission denied: %s required", perm))
				return
			}
			// Invalid key provided
			clientIP := getClientIP(r)
			logging.Error(fmt.Sprintf("Auth failed: invalid api key from %s", clientIP))

			// Record failed attempt for Fail2Ban-style blocking
			if s.security != nil {
				_ = s.security.RecordFailedAttempt(clientIP, "invalid_api_key", 5, 5*time.Minute)
			}

			writeAuthError(w, http.StatusUnauthorized, "invalid api key")
			return
		}

		if s.apiKeyManager == nil {
			logging.Error("Auth failed: apiKeyManager is nil")
		}

		// 2. User Session Check
		if s.authStore != nil {
			if cookie, err := r.Cookie("session"); err == nil {
				if user, err := s.authStore.ValidateSession(cookie.Value); err == nil {
					// Valid User Session found
					requiredRole := s.permToRole(perm)
					if user.Role.CanAccess(requiredRole) {
						// Success! Inject user into context
						ctx := context.WithValue(r.Context(), auth.UserContextKey, user)
						protectedHandler.ServeHTTP(w, r.WithContext(ctx))
						return
					}
					writeAuthError(w, http.StatusForbidden, "user role insufficient")
					return
				}
			}
		}

		// 3. Fallback
		writeAuthError(w, http.StatusUnauthorized, "authentication required (api key or user session)")
	})
}

// permToRole maps fine-grained permissions to coarse-grained user roles.
// permToRole maps fine-grained permissions to coarse-grained user roles.
func (s *Server) permToRole(perm storage.Permission) string {
	// Admin permissions require Admin role
	if strings.HasPrefix(string(perm), "admin:") {
		return "admin"
	}

	// Write permissions require Operator role (modify)
	if strings.HasSuffix(string(perm), ":write") {
		return "modify"
	}

	// Default/Read permissions require Viewer role (view)
	return "view"
}
