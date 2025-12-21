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
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"grimm.is/glacic/internal/clock"

	"grimm.is/glacic/internal/api/storage"
	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/health"
	"grimm.is/glacic/internal/learning"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/metrics"
	"grimm.is/glacic/internal/ratelimit"
	"grimm.is/glacic/internal/state"
	"grimm.is/glacic/internal/tls"
	"grimm.is/glacic/internal/ui"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

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
		ReadHeaderTimeout: 5 * time.Second,  // Slowloris prevention
		ReadTimeout:       15 * time.Second, // Body read limit
		WriteTimeout:      30 * time.Second, // Response timeout
		IdleTimeout:       60 * time.Second, // Keep-alive timeout
		MaxHeaderBytes:    1 << 16,          // 64KB header limit
		MaxBodyBytes:      10 << 20,         // 10MB body limit
	}
}

// Server handles API requests.
type Server struct {
	Config        *config.Config
	Assets        fs.FS
	client        ctlplane.ControlPlaneClient // RPC client for control plane communication
	authStore     *auth.Store
	authMw        *auth.Middleware
	apiKeyManager *APIKeyManager // Use local type alias/import
	logger        *logging.Logger
	collector     *metrics.Collector
	startTime     time.Time
	learning      *learning.Service
	stateStore    state.Store
	csrfManager   *CSRFManager       // CSRF token manager
	rateLimiter   *ratelimit.Limiter // Rate limiter for auth endpoints
	security      *SecurityManager   // Security manager for IP blocking
	wsManager     *WSManager         // Websocket manager

	mux *http.ServeMux
}

// ServerOptions holds dependencies for the API server
type ServerOptions struct {
	Config          *config.Config
	Assets          fs.FS
	Client          ctlplane.ControlPlaneClient
	AuthStore       *auth.Store
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

	if opts.AuthStore != nil {
		s.authMw = auth.NewMiddleware(opts.AuthStore)
	}

	if opts.Client != nil {
		s.wsManager = NewWSManager(opts.Client)
	} else {
		// Standalone mode: Injected dependencies are used directly (StateStore, LearningService)
		// We no longer automatically create a "firewall.db" here to avoid side effects.
		// If StateStore is nil, some standalone features might be disabled.
		s.security = NewSecurityManager(nil, logger)
	}



	// Debug: List all assets
	if opts.Assets != nil {
		logging.Info("Listing embedded UI assets:")
		fs.WalkDir(opts.Assets, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				logging.Error(fmt.Sprintf("Failed to walk assets: %v", err))
				return err
			}
			logging.Info(fmt.Sprintf(" - %s", path))
			return nil
		})
	}

	s.initRoutes()
	return s, nil
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

	// Protected endpoints
	if s.authStore != nil {
		// Auth is configured - protect endpoints using Unified Auth (User or API Key)

		// General Config
		mux.Handle("GET /api/config", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleConfig)))
		mux.Handle("POST /api/config/apply", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleApplyConfig)))
		mux.Handle("POST /api/config/safe-apply", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleSafeApply)))
		mux.Handle("POST /api/config/confirm", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleConfirmApply)))
		mux.Handle("GET /api/config/pending", s.require(storage.PermReadConfig, http.HandlerFunc(s.handlePendingApply)))
		mux.Handle("POST /api/config/ip-forwarding", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleSetIPForwarding)))
		mux.Handle("POST /api/config/settings", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleSystemSettings)))

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
		mux.Handle("GET /api/network-devices", s.require(storage.PermReadConfig, http.HandlerFunc(s.handleGetNetworkDevices)))

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

		// Device Management
		mux.Handle("POST /api/devices/identity", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUpdateDeviceIdentity)))
		mux.Handle("POST /api/devices/link", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleLinkMAC)))
		mux.Handle("POST /api/devices/unlink", s.require(storage.PermWriteConfig, http.HandlerFunc(s.handleUnlinkMAC)))
	} else if s.authStore == nil {
		// No auth configured (Dev/Test mode) - allow access
		mux.HandleFunc("GET /api/config", s.handleConfig)
		mux.HandleFunc("POST /api/config/settings", s.handleSystemSettings)
		mux.HandleFunc("POST /api/config/apply", s.handleApplyConfig)
		// /api/status is already registered globally
		mux.HandleFunc("GET /api/leases", s.handleLeases)
		mux.HandleFunc("GET /api/users", s.handleGetUsers)
		mux.HandleFunc("POST /api/users", s.handleCreateUser)
		mux.HandleFunc("GET /api/users/", s.handleGetUser)
		mux.HandleFunc("PUT /api/users/", s.handleUpdateUser)
		mux.HandleFunc("DELETE /api/users/", s.handleDeleteUser)
		mux.HandleFunc("GET /api/interfaces", s.handleInterfaces)
		mux.HandleFunc("GET /api/interfaces/available", s.handleAvailableInterfaces)
		mux.HandleFunc("POST /api/interfaces/update", s.handleUpdateInterface)
		mux.HandleFunc("POST /api/vlans", s.handleCreateVLAN)
		mux.HandleFunc("DELETE /api/vlans", s.handleDeleteVLAN)
		mux.HandleFunc("POST /api/bonds", s.handleCreateBond)
		mux.HandleFunc("DELETE /api/bonds", s.handleDeleteBond)
		mux.HandleFunc("GET /api/services", s.handleServices)
		mux.HandleFunc("GET /api/traffic", s.handleTraffic)
		mux.HandleFunc("GET /api/topology", s.handleGetTopology)
		mux.HandleFunc("GET /api/network-devices", s.handleGetNetworkDevices)
		
		// Config sections
		mux.HandleFunc("GET /api/config/policies", s.handleGetPolicies)
		mux.HandleFunc("POST /api/config/policies", s.handleUpdatePolicies)
		mux.HandleFunc("GET /api/config/nat", s.handleGetNAT)
		mux.HandleFunc("POST /api/config/nat", s.handleUpdateNAT)
		mux.HandleFunc("GET /api/config/ipsets", s.handleGetIPSets)
		mux.HandleFunc("POST /api/config/ipsets", s.handleUpdateIPSets)
		mux.HandleFunc("GET /api/config/dhcp", s.handleGetDHCP)
		mux.HandleFunc("POST /api/config/dhcp", s.handleUpdateDHCP)
		mux.HandleFunc("GET /api/config/dns", s.handleGetDNS)
		mux.HandleFunc("POST /api/config/dns", s.handleUpdateDNS)
		mux.HandleFunc("GET /api/config/routes", s.handleGetRoutes)
		mux.HandleFunc("POST /api/config/routes", s.handleUpdateRoutes)
		mux.HandleFunc("GET /api/config/zones", s.handleGetZones)
		mux.HandleFunc("POST /api/config/zones", s.handleUpdateZones)
		mux.HandleFunc("GET /api/config/protections", s.handleGetProtections)
		mux.HandleFunc("POST /api/config/protections", s.handleUpdateProtections)
		mux.HandleFunc("GET /api/config/qos", s.handleGetQoS)
		mux.HandleFunc("POST /api/config/qos", s.handleUpdateQoS)
		mux.HandleFunc("GET /api/config/scheduler", s.handleGetSchedulerConfig)
		mux.HandleFunc("POST /api/config/scheduler", s.handleUpdateSchedulerConfig)
		mux.HandleFunc("GET /api/config/vpn", s.handleGetVPN)
		mux.HandleFunc("POST /api/config/vpn", s.handleUpdateVPN)
		mux.HandleFunc("GET /api/config/mark_rules", s.handleGetMarkRules)
		mux.HandleFunc("POST /api/config/mark_rules", s.handleUpdateMarkRules)
		mux.HandleFunc("GET /api/config/uid_routing", s.handleGetUIDRouting)
		mux.HandleFunc("POST /api/config/uid_routing", s.handleUpdateUIDRouting)

		mux.HandleFunc("POST /api/policies/reorder", s.handlePolicyReorder)
		mux.HandleFunc("POST /api/rules/reorder", s.handleRuleReorder)

		mux.HandleFunc("POST /api/system/reboot", s.handleReboot)
		mux.HandleFunc("GET /api/system/stats", s.handleSystemStats)
		mux.HandleFunc("GET /api/system/routes", s.handleSystemRoutes)
		mux.HandleFunc("GET /api/system/backup", s.handleBackup)
		mux.HandleFunc("POST /api/system/restore", s.handleRestore)
		mux.HandleFunc("GET /api/scheduler/status", s.handleSchedulerStatus)
		mux.HandleFunc("POST /api/scheduler/run", s.handleSchedulerRun)
		// HCL editing (Advanced mode)
		mux.HandleFunc("GET /api/config/hcl", s.handleGetRawHCL)
		mux.HandleFunc("POST /api/config/hcl", s.handleUpdateRawHCL)
		mux.HandleFunc("GET /api/config/hcl/section", s.handleGetSectionHCL)
		mux.HandleFunc("POST /api/config/hcl/section", s.handleUpdateSectionHCL)
		mux.HandleFunc("POST /api/config/hcl/validate", s.handleValidateHCL)
		mux.HandleFunc("POST /api/config/hcl/save", s.handleSaveConfig)
		// Backup management
		mux.HandleFunc("GET /api/backups", s.handleBackups)
		mux.HandleFunc("POST /api/backups/create", s.handleCreateBackup)
		mux.HandleFunc("POST /api/backups/restore", s.handleRestoreBackup)
		mux.HandleFunc("GET /api/backups/content", s.handleBackupContent)
		mux.HandleFunc("POST /api/backups/pin", s.handlePinBackup)
		mux.HandleFunc("POST /api/backups/settings", s.handleUpdateBackupSettings)
		mux.HandleFunc("GET /api/backups/settings", s.handleGetBackupSettings)
		// Safe apply with confirmation
		mux.HandleFunc("POST /api/config/safe-apply", s.handleSafeApply)
		mux.HandleFunc("POST /api/config/confirm", s.handleConfirmApply)
		mux.HandleFunc("GET /api/config/pending", s.handlePendingApply)
		// Logging endpoints
		mux.HandleFunc("GET /api/logs", s.handleLogs)
		mux.HandleFunc("GET /api/logs/sources", s.handleLogSources)
		mux.HandleFunc("GET /api/logs/stream", s.handleLogStream)
		mux.HandleFunc("GET /api/logs/stats", s.handleLogStats)
		// Import Wizard
		mux.HandleFunc("POST /api/import/upload", s.handleImportUpload)
		mux.HandleFunc("/api/import/", s.handleImportConfig)
		// Monitoring endpoints
		mux.HandleFunc("GET /api/monitoring/overview", s.handleMonitoringOverview)
		mux.HandleFunc("GET /api/monitoring/interfaces", s.handleMonitoringInterfaces)
		mux.HandleFunc("GET /api/monitoring/policies", s.handleMonitoringPolicies)
		mux.HandleFunc("GET /api/monitoring/services", s.handleMonitoringServices)
		mux.HandleFunc("GET /api/monitoring/system", s.handleMonitoringSystem)
		mux.HandleFunc("GET /api/monitoring/conntrack", s.handleMonitoringConntrack)

		// Learning firewall endpoints
		if s.learning != nil {
			mux.HandleFunc("GET /api/learning/rules", s.handleLearningRules)
			mux.HandleFunc("/api/learning/rules/", s.handleLearningRule)
			mux.HandleFunc("GET /api/learning/stats", s.handleLearningStats)
		}

		// Device Management
		mux.HandleFunc("POST /api/devices/identity", s.handleUpdateDeviceIdentity)
		mux.HandleFunc("POST /api/devices/link", s.handleLinkMAC)
		mux.HandleFunc("POST /api/devices/unlink", s.handleUnlinkMAC)

		// Quick Wins
		mux.Handle("GET /api/config/diff", s.require(storage.PermAdminSystem, http.HandlerFunc(s.handleGetConfigDiff)))
		mux.HandleFunc("GET /public/ca.crt", s.handlePublicCert) // Public, no auth required


	}

	mux.Handle("/metrics", promhttp.Handler())

	// Serve SPA static files with fallback to index.html
	if s.Assets != nil {
		mux.Handle("/", s.spaHandler(s.Assets, "index.html"))
	}
}

// spaHandler serves static files, falling back to index.html for client-side routing
func (s *Server) spaHandler(assets fs.FS, fallback string) http.Handler {
	fileServer := http.FileServer(http.FS(assets))

	// Read index.html content once
	indexContent, err := fs.ReadFile(assets, fallback)
	if err != nil {
		logging.Error(fmt.Sprintf("Failed to read SPA fallback file %s: %v", fallback, err))
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
	return csrfMiddleware(s.mux)
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
		WriteError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	// Limit batch size
	if len(requests) > 20 {
		WriteError(w, http.StatusBadRequest, "Too many requests in batch")
		return
	}

	responses := make([]BatchResponse, len(requests))
	// Execute requests serially (for now) to avoid race conditions in non-thread-safe handlers if any
	// Although handlers SHOULD be thread safe.
	// But `ServeHTTP` is safe.
	// We can do it serially to be safe.

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

		// Capture response
		rr := httptest.NewRecorder()
		s.mux.ServeHTTP(rr, subReq)

		// Parse body if JSON
		var respBody any
		if rr.Body.Len() > 0 {
			if contentType := rr.Header().Get("Content-Type"); strings.Contains(contentType, "application/json") {
				_ = json.Unmarshal(rr.Body.Bytes(), &respBody)
			} else {
				respBody = rr.Body.String()
			}
		}

		responses[i] = BatchResponse{
			Status: rr.Code,
			Body:   respBody,
		}
	}

	WriteJSON(w, http.StatusOK, responses)
}

func (s *Server) handleBrand(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, brand.Get())
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
	if s.Config != nil && s.Config.API != nil &&
		s.Config.API.TLSCert != "" && s.Config.API.TLSKey != "" {

		// Auto-generate self-signed cert if missing
		// We use a 1 year validity for self-signed certs
		if _, err := tls.EnsureCertificate(s.Config.API.TLSCert, s.Config.API.TLSKey, 365); err != nil {
			logging.APILog("error", "Failed to ensure TLS certificate: %v", err)
			return err
		}

		logging.APILog("info", "API server starting with TLS on %s", addr)
		return server.ListenAndServeTLS(s.Config.API.TLSCert, s.Config.API.TLSKey)
	}

	logging.APILog("info", "API server starting on %s (no TLS)", addr)
	return server.ListenAndServe()
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

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	// Use RPC client if available, otherwise use direct config
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg)
	} else {
		WriteJSON(w, http.StatusOK, s.Config)
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
			WriteError(w, http.StatusInternalServerError, err.Error())
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

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Rate limiting: 5 attempts per minute per IP
	clientIP := getClientIP(r)
	if !s.rateLimiter.Allow(clientIP, 5, time.Minute) {
		s.logger.Warn("Rate limit exceeded for login", "ip", clientIP)
		WriteError(w, http.StatusTooManyRequests, "Too many login attempts. Please try again later.")
		return
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if s.authStore == nil {
		WriteError(w, http.StatusServiceUnavailable, "Auth not configured")
		return
	}

	sess, err := s.authStore.Authenticate(creds.Username, creds.Password)
	if err != nil {
		s.logger.Warn("Failed login attempt", "username", creds.Username, "ip", clientIP)
		WriteError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Successful login - log it
	s.logger.Info("Successful login", "username", creds.Username, "ip", clientIP)

	// Reset rate limit on successful login
	s.rateLimiter.Reset(clientIP)

	// Generate CSRF token for the session
	csrfToken, err := s.csrfManager.GenerateToken(sess.Token)
	if err != nil {
		s.logger.Error("Failed to generate CSRF token", "error", err)
		// Continue anyway - CSRF is defense in depth
	}

	// Set session cookie
	auth.SetSessionCookie(w, sess)

	// Get user for response
	user, _ := s.authStore.GetUser(creds.Username)

	// Return session info with CSRF token
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"username":      user.Username,
		"role":          user.Role,
		"csrf_token":    csrfToken,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cookie, err := r.Cookie("session")
	if err == nil && s.authStore != nil {
		// Clean up CSRF token
		s.csrfManager.DeleteToken(cookie.Value)

		// Logout session
		s.authStore.Logout(cookie.Value)
	}

	auth.ClearSessionCookie(w)

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated":  false,
			"setup_required": true,
		})
		return
	}

	// Check if user is authenticated
	cookie, err := r.Cookie("session")
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated":  false,
			"setup_required": !s.authStore.HasUsers(),
		})
		return
	}

	user, err := s.authStore.ValidateSession(cookie.Value)
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated":  false,
			"setup_required": !s.authStore.HasUsers(),
		})
		return
	}

	// Get or generate CSRF token
	csrfToken, exists := s.csrfManager.GetToken(cookie.Value)
	if !exists {
		var err error
		csrfToken, err = s.csrfManager.GenerateToken(cookie.Value)
		if err != nil {
			s.logger.Error("Failed to generate CSRF token", "error", err)
		}
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated":  true,
		"username":       user.Username,
		"role":           user.Role,
		"setup_required": false,
		"csrf_token":     csrfToken,
	})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	needsSetup := s.authStore == nil || !s.authStore.HasUsers()

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"needs_setup": needsSetup,
	})
}

func (s *Server) handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Rate limiting: 3 attempts per minute per IP for setup
	clientIP := getClientIP(r)
	if !s.rateLimiter.Allow("setup:"+clientIP, 3, time.Minute) {
		s.logger.Warn("Rate limit exceeded for admin creation", "ip", clientIP)
		WriteError(w, http.StatusTooManyRequests, "Too many setup attempts. Please try again later.")
		return
	}

	// Only allow if no users exist
	if s.authStore != nil && s.authStore.HasUsers() {
		WriteError(w, http.StatusForbidden, "Admin already exists")
		return
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if creds.Username == "" || creds.Password == "" {
		WriteError(w, http.StatusBadRequest, "Username and password required")
		return
	}

	// Validate password using new password policy
	policy := auth.DefaultPasswordPolicy()
	if err := auth.ValidatePassword(creds.Password, policy, creds.Username); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Create auth store if it doesn't exist
	if s.authStore == nil {
		store, err := auth.NewStore("")
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "Failed to initialize auth")
			return
		}
		s.authStore = store
		s.authMw = auth.NewMiddleware(store)
	}

	if err := s.authStore.CreateUser(creds.Username, creds.Password, auth.RoleAdmin); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logger.Info("Admin user created", "username", creds.Username, "ip", clientIP)

	// Auto-login the new admin
	session, _ := s.authStore.Authenticate(creds.Username, creds.Password)
	if session != nil {
		auth.SetSessionCookie(w, session)

		// Generate CSRF token for the new session
		csrfToken, _ := s.csrfManager.GenerateToken(session.Token)

		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success":       true,
			"authenticated": true,
			"username":      creds.Username,
			"csrf_token":    csrfToken,
			"role":          auth.RoleAdmin, // Added role here
		})
	} else {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// handleGetUsers returns all users
func (s *Server) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteJSON(w, http.StatusOK, []interface{}{})
		return
	}
	users := s.authStore.ListUsers()
	WriteJSON(w, http.StatusOK, users)
}

// handleCreateUser creates a new user
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteError(w, http.StatusServiceUnavailable, "Auth not configured")
		return
	}
	var req struct {
		Username string    `json:"username"`
		Password string    `json:"password"`
		Role     auth.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := s.authStore.CreateUser(req.Username, req.Password, req.Role); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleUserByName handles individual user operations (update, delete)
// handleGetUser returns a single user
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteError(w, http.StatusServiceUnavailable, "Auth not configured")
		return
	}
	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if username == "" {
		WriteError(w, http.StatusBadRequest, "Username required")
		return
	}

	user, err := s.authStore.GetUser(username)
	if err != nil {
		WriteError(w, http.StatusNotFound, "User not found")
		return
	}
	// Don't expose password hash
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"username":   user.Username,
		"role":       user.Role,
		"created_at": user.CreatedAt,
		"updated_at": user.UpdatedAt,
	})
}

// handleUpdateUser updates a user
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteError(w, http.StatusServiceUnavailable, "Auth not configured")
		return
	}
	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if username == "" {
		WriteError(w, http.StatusBadRequest, "Username required")
		return
	}

	var req struct {
		Password string    `json:"password,omitempty"`
		Role     auth.Role `json:"role,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	// Update password if provided
	if req.Password != "" {
		if err := s.authStore.UpdatePassword(username, req.Password); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// Update role if provided
	if req.Role != "" {
		if err := s.authStore.UpdateRole(username, req.Role); err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleDeleteUser deletes a user
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if s.authStore == nil {
		WriteError(w, http.StatusServiceUnavailable, "Auth not configured")
		return
	}
	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if username == "" {
		WriteError(w, http.StatusBadRequest, "Username required")
		return
	}

	// Prevent deleting admin user
	if username == "admin" {
		WriteError(w, http.StatusForbidden, "Cannot delete admin user")
		return
	}
	if err := s.authStore.DeleteUser(username); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// --- Interface Management Handlers ---

func (s *Server) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	interfaces, err := s.client.GetInterfaces()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, interfaces)
}

func (s *Server) handleAvailableInterfaces(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	interfaces, err := s.client.GetAvailableInterfaces()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, interfaces)
}

func (s *Server) handleUpdateInterface(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var args ctlplane.UpdateInterfaceArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if args.Name == "" {
		WriteError(w, http.StatusBadRequest, "Interface name required")
		return
	}

	reply, err := s.client.UpdateInterface(&args)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteError(w, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleCreateVLAN creates a new VLAN interface
func (s *Server) handleCreateVLAN(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var args ctlplane.CreateVLANArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	reply, err := s.client.CreateVLAN(&args)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteError(w, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleDeleteVLAN deletes a VLAN interface
func (s *Server) handleDeleteVLAN(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	ifaceName := r.URL.Query().Get("name")
	if ifaceName == "" {
		WriteError(w, http.StatusBadRequest, "name parameter required")
		return
	}

	reply, err := s.client.DeleteVLAN(ifaceName)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteError(w, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleCreateBond creates a new bond interface
func (s *Server) handleCreateBond(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var args ctlplane.CreateBondArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	reply, err := s.client.CreateBond(&args)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteError(w, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleDeleteBond deletes a bond interface
func (s *Server) handleDeleteBond(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		WriteError(w, http.StatusBadRequest, "name parameter required")
		return
	}

	reply, err := s.client.DeleteBond(name)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteError(w, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleGetConfigDiff returns the diff between saved and in-memory config
func (s *Server) handleGetConfigDiff(w http.ResponseWriter, r *http.Request) {
	// This requires access to the ConfigFile which holds the AST
	// Currently s.Config is just the struct.
	// We need to access the config manager/loader which we don't handle directly here in all modes.
	// However, usually we can just rely on the on-disk file vs current struct?
	// Actually, `internal/config/hcl.go` has `ConfigFile.Diff()`.
	// S doesn't have `ConfigFile`, but `s.Config` is *config.Config.
	
	// Implementation note: The Server struct doesn't strictly hold the `ConfigFile` wrapper.
	// It relies on RPC or direct config struct.
	// If we are in `ctl` mode (integrated), we might have access, but via RPC it's harder.
	// For "Quick Win", let's assume we can read the file from disk and compare?
	// Or better: Add `GetConfigDiff` RPC to control plane.
	
	// BUT: `ctl` mode has `s.Config`.
	// For now, let's implement the RPC call if possible, or direct if we are ctl.
	if s.client != nil {
		diff, err := s.client.GetConfigDiff()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(diff))
		return
	}

	// Fallback/Standalone: Not supported yet without ConfigFile refactor
	WriteError(w, http.StatusNotImplemented, "Config diff not available in this mode")
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
			WriteError(w, http.StatusNotFound, "Certificate not found")
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
		WriteError(w, http.StatusServiceUnavailable, "Collector not initialized")
		return
	}
	stats := s.collector.GetInterfaceStats()
	WriteJSON(w, http.StatusOK, stats)
}

// handleServices returns the available service definitions
func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	// Build response with all service definitions
	type ServiceInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Ports       []struct {
			Port     int    `json:"port"`
			EndPort  int    `json:"end_port,omitempty"`
			Protocol string `json:"protocol"`
		} `json:"ports"`
	}

	services := make([]ServiceInfo, 0, len(config.BuiltinServices))
	for name, svc := range config.BuiltinServices {
		info := ServiceInfo{
			Name:        name,
			Description: svc.Description,
		}
		for _, p := range svc.Ports {
			info.Ports = append(info.Ports, struct {
				Port     int    `json:"port"`
				EndPort  int    `json:"end_port,omitempty"`
				Protocol string `json:"protocol"`
			}{
				Port:     p.Port,
				EndPort:  p.EndPort,
				Protocol: p.Protocol,
			})
		}
		services = append(services, info)
	}

	WriteJSON(w, http.StatusOK, services)
}

// --- Config Update Handlers ---

// handleApplyConfig applies the current configuration
func (s *Server) handleApplyConfig(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	w.Header().Set("Content-Type", "application/json")

	if s.client != nil {
		if err := s.client.ApplyConfig(s.Config); err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
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
func (s *Server) handleGetPolicies(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.Policies)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.Policies)
	}
}

// handleUpdatePolicies updates firewall policies
func (s *Server) handleUpdatePolicies(w http.ResponseWriter, r *http.Request) {
	var policies []config.Policy
	if err := json.NewDecoder(r.Body).Decode(&policies); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.Policies = policies
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetNAT returns NAT configuration
func (s *Server) handleGetNAT(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.NAT)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.NAT)
	}
}

// handleUpdateNAT updates NAT configuration
func (s *Server) handleUpdateNAT(w http.ResponseWriter, r *http.Request) {
	var nat []config.NATRule
	if err := json.NewDecoder(r.Body).Decode(&nat); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.NAT = nat
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetMarkRules returns mark rules
func (s *Server) handleGetMarkRules(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.MarkRules)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.MarkRules)
	}
}

// handleUpdateMarkRules updates mark rules
func (s *Server) handleUpdateMarkRules(w http.ResponseWriter, r *http.Request) {
	var rules []config.MarkRule
	if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.MarkRules = rules // Direct update for standalone, client update needed for real
	if s.client != nil {
		// Get refresh
		cfg, _ := s.client.GetConfig()
		cfg.MarkRules = rules
		s.client.ApplyConfig(cfg)
		s.client.SaveConfig()
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetUIDRouting returns UID routing rules
func (s *Server) handleGetUIDRouting(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.UIDRouting)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.UIDRouting)
	}
}

// handleUpdateUIDRouting updates UID routing rules
func (s *Server) handleUpdateUIDRouting(w http.ResponseWriter, r *http.Request) {
	var rules []config.UIDRouting
	if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.UIDRouting = rules
	if s.client != nil {
		cfg, _ := s.client.GetConfig()
		cfg.UIDRouting = rules
		s.client.ApplyConfig(cfg)
		s.client.SaveConfig()
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetIPSets returns IPSet configuration
func (s *Server) handleGetIPSets(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.IPSets)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.IPSets)
	}
}

// handleUpdateIPSets updates IPSet configuration
func (s *Server) handleUpdateIPSets(w http.ResponseWriter, r *http.Request) {
	var ipsets []config.IPSet
	if err := json.NewDecoder(r.Body).Decode(&ipsets); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.IPSets = ipsets
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetDHCP returns DHCP server configuration
func (s *Server) handleGetDHCP(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.DHCP)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.DHCP)
	}
}

// handleUpdateDHCP updates DHCP server configuration
func (s *Server) handleUpdateDHCP(w http.ResponseWriter, r *http.Request) {
	var dhcp config.DHCPServer
	if err := json.NewDecoder(r.Body).Decode(&dhcp); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.DHCP = &dhcp
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetDNS returns DNS server configuration
func (s *Server) handleGetDNS(w http.ResponseWriter, r *http.Request) {
	var cfg *config.Config
	var err error
	if s.client != nil {
		cfg, err = s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		cfg = s.Config
	}

	// Return both for compatibility
	resp := struct {
		Old *config.DNSServer `json:"dns_server,omitempty"`
		New *config.DNS       `json:"dns,omitempty"`
	}{
		Old: cfg.DNSServer,
		New: cfg.DNS,
	}
	WriteJSON(w, http.StatusOK, resp)
}

// handleUpdateDNS updates DNS server configuration
func (s *Server) handleUpdateDNS(w http.ResponseWriter, r *http.Request) {
	// Try to decode as wrapped format first
	var req struct {
		Old *config.DNSServer `json:"dns_server,omitempty"`
		New *config.DNS       `json:"dns,omitempty"`
	}

	// Read body once
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to read request body")
		return
	}

	if err := json.Unmarshal(body, &req); err == nil && (req.Old != nil || req.New != nil) {
		if req.Old != nil {
			s.Config.DNSServer = req.Old
		}
		if req.New != nil {
			s.Config.DNS = req.New
		}
	} else {
		// Try legacy raw DNSServer format
		var old config.DNSServer
		if err := json.Unmarshal(body, &old); err == nil {
			s.Config.DNSServer = &old
		} else {
			WriteError(w, http.StatusBadRequest, "Invalid DNS configuration format")
			return
		}
	}

	// Run migration
	s.Config.MigrateDNSConfig()

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetRoutes returns static route configuration
func (s *Server) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.Routes)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.Routes)
	}
}

// handleUpdateRoutes updates static route configuration
func (s *Server) handleUpdateRoutes(w http.ResponseWriter, r *http.Request) {
	var routes []config.Route
	if err := json.NewDecoder(r.Body).Decode(&routes); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.Routes = routes
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetZones returns zone configuration
func (s *Server) handleGetZones(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.Zones)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.Zones)
	}
}

// handleUpdateZones updates zone configuration
func (s *Server) handleUpdateZones(w http.ResponseWriter, r *http.Request) {
	var zones []config.Zone
	if err := json.NewDecoder(r.Body).Decode(&zones); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s.Config.Zones = zones
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetProtections returns per-interface protection settings
func (s *Server) handleGetProtections(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(cfg.Protections)
	} else {
		json.NewEncoder(w).Encode(s.Config.Protections)
	}
}

// handleUpdateProtections updates per-interface protection settings
func (s *Server) handleUpdateProtections(w http.ResponseWriter, r *http.Request) {
	// Accept either array directly or wrapped in { protections: [...] }
	var wrapper struct {
		Protections []config.InterfaceProtection `json:"protections"`
	}
	body, _ := io.ReadAll(r.Body)
	// Try parsing as wrapper
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Protections != nil {
		s.Config.Protections = wrapper.Protections
	} else {
		// Try parsing as array
		var protections []config.InterfaceProtection
		if err := json.Unmarshal(body, &protections); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		s.Config.Protections = protections
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetVPN returns the current VPN configuration
func (s *Server) handleGetVPN(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(cfg.VPN)
	} else {
		json.NewEncoder(w).Encode(s.Config.VPN)
	}
}

// handleUpdateVPN updates the VPN configuration
func (s *Server) handleUpdateVPN(w http.ResponseWriter, r *http.Request) {
	var newVPN config.VPNConfig
	if err := json.NewDecoder(r.Body).Decode(&newVPN); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	s.Config.VPN = &newVPN
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetQoS returns QoS policies configuration
func (s *Server) handleGetQoS(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg.QoSPolicies)
	} else {
		WriteJSON(w, http.StatusOK, s.Config.QoSPolicies)
	}
}

// handleUpdateQoS updates QoS policies configuration
func (s *Server) handleUpdateQoS(w http.ResponseWriter, r *http.Request) {
	// Accept either array directly or wrapped in { qos_policies: [...] }
	var wrapper struct {
		QoSPolicies []config.QoSPolicy `json:"qos_policies"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.QoSPolicies != nil {
		s.Config.QoSPolicies = wrapper.QoSPolicies
	} else {
		var qos []config.QoSPolicy
		if err := json.Unmarshal(body, &qos); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		s.Config.QoSPolicies = qos
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleReboot handles system reboot request
func (s *Server) handleReboot(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	if err := s.client.Reboot(); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleBackup exports the current configuration as JSON
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	// Get current config
	var cfg *config.Config
	if s.client != nil {
		var err error
		cfg, err = s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		cfg = s.Config
	}

	// Set headers for file download
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=firewall-config.json")

	// Encode config as JSON
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
}

// handleRestore imports a configuration from JSON
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	// Parse the uploaded config
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid configuration: "+err.Error())
		return
	}

	// Apply the config
	if s.client != nil {
		if err := s.client.ApplyConfig(&cfg); err != nil {
			WriteError(w, http.StatusInternalServerError, "Failed to apply config: "+err.Error())
			return
		}
	} else {
		// Update in-memory config
		*s.Config = cfg
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetSchedulerConfig returns scheduler configuration
func (s *Server) handleGetSchedulerConfig(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"scheduler":       cfg.Scheduler,
			"scheduled_rules": cfg.ScheduledRules,
		})
	} else {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"scheduler":       s.Config.Scheduler,
			"scheduled_rules": s.Config.ScheduledRules,
		})
	}
}

// handleUpdateSchedulerConfig updates scheduler configuration
func (s *Server) handleUpdateSchedulerConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scheduler      *config.SchedulerConfig `json:"scheduler"`
		ScheduledRules []config.ScheduledRule  `json:"scheduled_rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Scheduler != nil {
		s.Config.Scheduler = req.Scheduler
	}
	if req.ScheduledRules != nil {
		s.Config.ScheduledRules = req.ScheduledRules
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleSchedulerStatus returns the status of all scheduled tasks
func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	// Build status from config
	type TaskStatusInfo struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
		Schedule    string `json:"schedule"`
		Type        string `json:"type"` // "system" or "rule"
	}

	tasks := []TaskStatusInfo{
		{ID: "ipset-update", Name: "IPSet Update", Description: "Refresh IPSets from external sources", Enabled: true, Schedule: "Every 24h", Type: "system"},
		{ID: "dns-blocklist-update", Name: "DNS Blocklist Update", Description: "Refresh DNS blocklists", Enabled: true, Schedule: "Every 24h", Type: "system"},
		{ID: "config-backup", Name: "Configuration Backup", Description: "Automatic config backup", Enabled: false, Schedule: "Daily at 2:00 AM", Type: "system"},
	}

	// Add scheduled rules
	var scheduledRules []config.ScheduledRule
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err == nil {
			scheduledRules = cfg.ScheduledRules
		}
	} else {
		scheduledRules = s.Config.ScheduledRules
	}

	for _, rule := range scheduledRules {
		tasks = append(tasks, TaskStatusInfo{
			ID:          "rule-" + rule.Name,
			Name:        rule.Name,
			Description: rule.Description,
			Enabled:     rule.Enabled,
			Schedule:    rule.Schedule,
			Type:        "rule",
		})
	}

	WriteJSON(w, http.StatusOK, tasks)
}

// handleSchedulerRun triggers immediate execution of a scheduled task
func (s *Server) handleSchedulerRun(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	taskID := r.URL.Query().Get("task")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "Task ID required")
		return
	}

	if s.client == nil {
		http.Error(w, "Control plane client not connected", http.StatusServiceUnavailable)
		return
	}

	// Trigger via RPC
	if err := s.client.TriggerTask(taskID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to trigger task: %v", err), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Task " + taskID + " triggered",
	})
}

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
		WriteError(w, http.StatusBadRequest, "Invalid request body")
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
		WriteError(w, http.StatusBadRequest, "policy_name and relative_to are required")
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
		WriteError(w, http.StatusNotFound, "Policy not found")
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
	s.Config.Policies = newPolicies

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleRuleReorder reorders rules within a policy
func (s *Server) handleRuleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
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
		WriteError(w, http.StatusBadRequest, "policy_name is required")
		return
	}

	// Find the policy
	var policyIdx int = -1
	for i, p := range s.Config.Policies {
		if p.Name == req.PolicyName {
			policyIdx = i
			break
		}
	}

	if policyIdx == -1 {
		WriteError(w, http.StatusNotFound, "Policy not found")
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
		WriteError(w, http.StatusBadRequest, "rule_name and relative_to are required")
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
		WriteError(w, http.StatusNotFound, "Rule not found")
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
func (s *Server) handleSetIPForwarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 1. Get current Config from Control Plane to ensure we are up to date
	cfg, err := s.client.GetConfig()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to get config: "+err.Error())
		return
	}

	// 2. Update IP Forwarding setting
	cfg.IPForwarding = req.Enabled

	// 3. Apply the updated config
	if err := s.client.ApplyConfig(cfg); err != nil {
		s.logger.Error("Failed to apply panic button change", "error", err)
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "Failed to apply setting: " + err.Error(),
		})
		return
	}

	// 4. Save to disk (persist the change)
	saveResp, err := s.client.SaveConfig()
	if err != nil {
		// Log error but success=true because runtime is updated
		s.logger.Error("Failed to save panic button change to disk", "error", err)
	} else if saveResp.Error != "" {
		s.logger.Error("Failed to save config", "error", saveResp.Error)
	}

	// Update local config cache
	s.Config = cfg

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"applied": true,
		"saved":   err == nil,
	})
}

// SystemSettingsRequest represents the payload for updating system settings
type SystemSettingsRequest struct {
	IPForwarding      *bool `json:"ip_forwarding,omitempty"`
	MSSClamping       *bool `json:"mss_clamping,omitempty"`
	EnableFlowOffload *bool `json:"enable_flow_offload,omitempty"`
}

// handleSystemSettings handles global system settings updates
func (s *Server) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	// Method check removed (handled by router)

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var req SystemSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 1. Get current Config
	cfg, err := s.client.GetConfig()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to get config: "+err.Error())
		return
	}

	// 2. Update Settings
	changes := false
	if req.IPForwarding != nil {
		cfg.IPForwarding = *req.IPForwarding
		changes = true
	}
	if req.MSSClamping != nil {
		cfg.MSSClamping = *req.MSSClamping
		changes = true
	}
	if req.EnableFlowOffload != nil {
		cfg.EnableFlowOffload = *req.EnableFlowOffload
		changes = true
	}

	if !changes {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}

	// 3. Apply Config
	if err := s.client.ApplyConfig(cfg); err != nil {
		s.logger.Error("Failed to apply settings change", "error", err)
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "Failed to apply settings: " + err.Error(),
		})
		return
	}

	// 4. Save to disk
	saveResp, err := s.client.SaveConfig()
	if err != nil {
		s.logger.Error("Failed to save settings to disk", "error", err)
	} else if saveResp.Error != "" {
		s.logger.Error("Failed to save config", "error", saveResp.Error)
	}

	// Update local config cache
	s.Config = cfg

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"applied": true,
		"saved":   err == nil,
	})
}

// handleGetRawHCL handles GET for the entire config as raw HCL
func (s *Server) handleGetRawHCL(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	reply, err := s.client.GetRawHCL()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, reply)
}

// handleUpdateRawHCL handles PUT/POST for the entire config as raw HCL
func (s *Server) handleUpdateRawHCL(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var req struct {
		HCL string `json:"hcl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	reply, err := s.client.SetRawHCL(req.HCL)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleSectionHCL handles GET/PUT for a specific config section as raw HCL
// handleGetSectionHCL handles GET for a specific config section as raw HCL
func (s *Server) handleGetSectionHCL(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	sectionType := r.URL.Query().Get("type")
	labels := r.URL.Query()["label"]

	if sectionType == "" {
		WriteError(w, http.StatusBadRequest, "Section type required (use ?type=dhcp)")
		return
	}

	reply, err := s.client.GetSectionHCL(sectionType, labels...)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"error": reply.Error,
		})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"hcl": reply.HCL})
}

// handleUpdateSectionHCL handles PUT/POST for a specific config section as raw HCL
func (s *Server) handleUpdateSectionHCL(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	sectionType := r.URL.Query().Get("type")
	labels := r.URL.Query()["label"]

	if sectionType == "" {
		WriteError(w, http.StatusBadRequest, "Section type required (use ?type=dhcp)")
		return
	}

	var req struct {
		HCL string `json:"hcl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	reply, err := s.client.SetSectionHCL(sectionType, req.HCL, labels...)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleValidateHCL validates HCL without applying it
func (s *Server) handleValidateHCL(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var req struct {
		HCL string `json:"hcl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	reply, err := s.client.ValidateHCL(req.HCL)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleSaveConfig saves the current config to disk
func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	reply, err := s.client.SaveConfig()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"backup_path": reply.BackupPath,
	})
}

// --- Backup Management Handlers ---

// handleBackups lists all available backups
func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	reply, err := s.client.ListBackups()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"error": reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"backups": reply.Backups,
	})
}

// handleCreateBackup creates a new manual backup
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var req struct {
		Description string `json:"description"`
		Pinned      bool   `json:"pinned"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	reply, err := s.client.CreateBackup(req.Description, req.Pinned)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"backup":  reply.Backup,
	})
}

// handleRestoreBackup restores a specific backup version
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var req struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Version <= 0 {
		WriteError(w, http.StatusBadRequest, "Version is required")
		return
	}

	reply, err := s.client.RestoreBackup(req.Version)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": reply.Message,
	})
}

// handleBackupContent returns the content of a specific backup
func (s *Server) handleBackupContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	versionStr := r.URL.Query().Get("version")
	if versionStr == "" {
		WriteError(w, http.StatusBadRequest, "Version parameter required")
		return
	}

	var version int
	fmt.Sscanf(versionStr, "%d", &version)
	if version <= 0 {
		WriteError(w, http.StatusBadRequest, "Invalid version")
		return
	}

	reply, err := s.client.GetBackupContent(version)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"error": reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"content": reply.Content,
	})
}

// --- Safe Apply Handlers ---

// handleSafeApply applies config with connectivity verification and rollback
func (s *Server) handleSafeApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req struct {
		Config             config.Config `json:"config"`
		PingTargets        []string      `json:"ping_targets"`         // IPs to verify after apply
		PingTimeoutSeconds int           `json:"ping_timeout_seconds"` // Per-target timeout (default 5)
		RollbackSeconds    int           `json:"rollback_seconds"`     // Unused for now but reserved
	}
	req.PingTimeoutSeconds = 5 // Default timeout

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if s.client == nil {
		http.Error(w, "Control plane not connected", http.StatusServiceUnavailable)
		return
	}

	// Step 1: Create backup BEFORE apply (so we can rollback)
	backupReply, err := s.client.CreateBackup("Pre-safe-apply backup", false)
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"message": "Failed to create pre-apply backup",
		})
		return
	}
	backupVersion := backupReply.Backup.Version

	// Step 2: Apply the config
	if err := s.client.ApplyConfig(&req.Config); err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"message": "Apply failed",
		})
		return
	}

	// Update local config
	s.Config = &req.Config

	// Step 3: If ping targets specified, verify connectivity
	if len(req.PingTargets) > 0 {
		var failedTargets []string
		for _, target := range req.PingTargets {
			// Validate target is an IP address
			if net.ParseIP(target) == nil {
				failedTargets = append(failedTargets, fmt.Sprintf("%s: invalid IP", target))
				continue
			}

			// Ping via control plane (privileged)
			pingReply, err := s.client.Ping(target, req.PingTimeoutSeconds)
			if err != nil {
				failedTargets = append(failedTargets, fmt.Sprintf("%s: %v", target, err))
				continue
			}
			if !pingReply.Reachable {
				failedTargets = append(failedTargets, fmt.Sprintf("%s: %s", target, pingReply.Error))
			}
		}

		// If any ping failed, rollback
		if len(failedTargets) > 0 {
			// Rollback to backup
			_, rollbackErr := s.client.RestoreBackup(backupVersion)
			rollbackMsg := "rollback successful"
			if rollbackErr != nil {
				rollbackMsg = fmt.Sprintf("rollback failed: %v", rollbackErr)
			}

			WriteJSON(w, http.StatusOK, map[string]interface{}{
				"success":      false,
				"error":        "connectivity verification failed",
				"failed_pings": failedTargets,
				"message":      fmt.Sprintf("Connectivity verification failed, %s", rollbackMsg),
				"rolled_back":  rollbackErr == nil,
			})
			return
		}
	}

	// Success - create a post-apply backup for the known-good state
	postBackupReply, err := s.client.CreateBackup("Safe apply successful", false)
	postBackupVersion := 0
	if err == nil {
		postBackupVersion = postBackupReply.Backup.Version
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"message":        "Configuration applied successfully",
		"backup_version": postBackupVersion,
	})
}

// handleConfirmApply confirms a pending apply (placeholder for full implementation)
func (s *Server) handleConfirmApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// For now, just acknowledge - full implementation requires control plane integration
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Changes confirmed",
	})
}

// handlePendingApply returns info about any pending apply
func (s *Server) handlePendingApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// For now, return no pending - full implementation requires control plane integration
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"has_pending": false,
	})
}

// handlePinBackup pins or unpins a backup
func (s *Server) handlePinBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var req struct {
		Version int  `json:"version"`
		Pinned  bool `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Version <= 0 {
		WriteError(w, http.StatusBadRequest, "Version is required")
		return
	}

	reply, err := s.client.PinBackup(req.Version, req.Pinned)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// handleGetBackupSettings gets backup settings
func (s *Server) handleGetBackupSettings(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	reply, err := s.client.ListBackups()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"max_backups": reply.MaxBackups,
	})
}

// handleUpdateBackupSettings updates backup settings
func (s *Server) handleUpdateBackupSettings(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var req struct {
		MaxBackups int `json:"max_backups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.MaxBackups < 1 {
		WriteError(w, http.StatusBadRequest, "max_backups must be at least 1")
		return
	}

	reply, err := s.client.SetMaxBackups(req.MaxBackups)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"max_backups": req.MaxBackups,
	})
}

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
		WriteError(w, http.StatusNotFound, "Page not found")
		return
	}

	WriteJSON(w, http.StatusOK, page)
}

// handleHealth returns the overall health status.
// Uses the health package to check system components.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Create a health checker with some basic checks
	checker := health.NewChecker()

	// Add basic system checks
	checker.Register("nftables", func(ctx context.Context) health.Check {
		// Check if nftables is responding
		cmd := exec.CommandContext(ctx, "nft", "list", "tables")
		err := cmd.Run()
		if err != nil {
			return health.Check{
				Name:    "nftables",
				Status:  health.StatusUnhealthy,
				Message: fmt.Sprintf("nftables command failed: %v", err),
			}
		}
		return health.Check{
			Name:    "nftables",
			Status:  health.StatusHealthy,
			Message: "nftables is responding",
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
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
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
type MonitoringOverview struct {
	Timestamp  int64                              `json:"timestamp"`
	System     *metrics.SystemStats               `json:"system"`
	Interfaces map[string]*metrics.InterfaceStats `json:"interfaces"`
	Policies   map[string]*metrics.PolicyStats    `json:"policies"`
	Services   *metrics.ServiceStats              `json:"services"`
	Conntrack  *metrics.ConntrackStats            `json:"conntrack"`
}

// handleMonitoringOverview returns all monitoring data in one request.
func (s *Server) handleMonitoringOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	overview := MonitoringOverview{
		Timestamp:  s.collector.GetLastUpdate().Unix(),
		System:     s.collector.GetSystemStats(),
		Interfaces: s.collector.GetInterfaceStats(),
		Policies:   s.collector.GetPolicyStats(),
		Services:   s.collector.GetServiceStats(),
		Conntrack:  s.collector.GetConntrackStats(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(overview)
}

// handleMonitoringInterfaces returns interface traffic statistics.
func (s *Server) handleMonitoringInterfaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.collector.GetInterfaceStats()

	// Convert to sorted slice for consistent ordering
	type ifaceWithStats struct {
		*metrics.InterfaceStats
	}
	result := make([]ifaceWithStats, 0, len(stats))
	for _, stat := range stats {
		result = append(result, ifaceWithStats{stat})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp":  s.collector.GetLastUpdate().Unix(),
		"interfaces": result,
	})
}

// handleMonitoringPolicies returns firewall policy statistics.
func (s *Server) handleMonitoringPolicies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.collector.GetPolicyStats()

	// Convert to slice
	result := make([]*metrics.PolicyStats, 0, len(stats))
	for _, stat := range stats {
		result = append(result, stat)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp": s.collector.GetLastUpdate().Unix(),
		"policies":  result,
	})
}

// handleMonitoringServices returns service statistics (DHCP, DNS).
func (s *Server) handleMonitoringServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.collector.GetServiceStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp": s.collector.GetLastUpdate().Unix(),
		"services":  stats,
	})
}

// handleMonitoringSystem returns system statistics (CPU, memory, load).
func (s *Server) handleMonitoringSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.collector.GetSystemStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp": s.collector.GetLastUpdate().Unix(),
		"system":    stats,
	})
}

// handleMonitoringConntrack returns connection tracking statistics.
func (s *Server) handleMonitoringConntrack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.collector.GetConntrackStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"timestamp": s.collector.GetLastUpdate().Unix(),
		"conntrack": stats,
	})
}

// require checks for sufficient permission from EITHER an API Key OR a User Session.
func (s *Server) require(perm storage.Permission, handler http.Handler) http.Handler {
	// Apply CSRF protection to the inner handler
	// The CSRF middleware itself handles skipping for API keys or non-state-changing methods
	protectedHandler := CSRFMiddleware(s.csrfManager, s.authStore)(handler)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			writeAuthError(w, http.StatusUnauthorized, "invalid api key")
			return
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

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/yaml")
	w.Write(openAPISpec)
}

func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="description" content="SwaggerUI" />
    <title>Glacic Firewall API</title>
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
</html>`)
}
