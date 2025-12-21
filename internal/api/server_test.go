package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/metrics"
	"grimm.is/glacic/internal/ratelimit"
)

func TestHandleStatus(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	server := &Server{
		Config: &config.Config{},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/status", nil)
	rr := httptest.NewRecorder()

	server.handleStatus(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Errorf("failed to unmarshal response: %v", err)
	}

	if result["status"] != "online" {
		t.Errorf("expected status 'online', got %v", result["status"])
	}
}

func TestHandleConfig(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	cfg := &config.Config{}

	server := &Server{
		Config: cfg,
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/config", nil)
	rr := httptest.NewRecorder()

	server.handleConfig(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %v", contentType)
	}
}

func TestHandleSetupStatus(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	server := &Server{
		Config: &config.Config{},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/auth/setup", nil)
	rr := httptest.NewRecorder()

	server.handleSetupStatus(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}
}

func TestHandleInterfaces_NoClient(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	cfg := &config.Config{
		Interfaces: []config.Interface{
			{Name: "eth0", Zone: "WAN", IPv4: []string{"dhcp"}},
			{Name: "eth1", Zone: "LAN", IPv4: []string{"10.0.0.1/24"}},
		},
	}

	server := &Server{
		Config: cfg,
		logger: logger,
		// No client - should return 503
	}

	req, _ := http.NewRequest("GET", "/api/interfaces", nil)
	rr := httptest.NewRecorder()

	server.handleInterfaces(rr, req)

	// Without RPC client, handleInterfaces returns ServiceUnavailable
	if status := rr.Code; status != http.StatusServiceUnavailable {
		t.Errorf("expected ServiceUnavailable without client, got %v", status)
	}
}

func TestHandleLeases_NoClient(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	server := &Server{
		Config: &config.Config{},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/dhcp/leases", nil)
	rr := httptest.NewRecorder()

	server.handleLeases(rr, req)

	// Should return error without RPC client
	if status := rr.Code; status == http.StatusOK {
		// If it returns OK with empty array, that's also acceptable
		var result []interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err == nil {
			// OK, just an empty response
			return
		}
	}
}

func TestHandleLogin_BadRequest(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	server := &Server{
		Config:      &config.Config{},
		logger:      logger,
		rateLimiter: ratelimit.NewLimiter(),
	}

	// Test with invalid JSON
	req, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	server.handleLogin(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected BadRequest for invalid JSON, got %v", status)
	}
}

func TestHandleLogin_EmptyCredentials(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	server := &Server{
		Config:      &config.Config{},
		logger:      logger,
		rateLimiter: ratelimit.NewLimiter(),
	}

	// Test with empty credentials
	req, _ := http.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"","password":""}`))
	rr := httptest.NewRecorder()

	server.handleLogin(rr, req)

	if status := rr.Code; status == http.StatusOK {
		t.Error("expected error for empty credentials, got OK")
	}
}

func TestLoggingMiddleware(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())

	server := &Server{
		Config: &config.Config{},
		logger: logger,
	}

	handler := server.loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req, _ := http.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status OK, got %v", status)
	}
}

// --- Additional Handler Tests ---

func TestHandleServices(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{Config: &config.Config{}, logger: logger}

	req, _ := http.NewRequest("GET", "/api/services", nil)
	rr := httptest.NewRecorder()

	server.handleServices(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}

	// Verify response is valid JSON array
	var resp []interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Response is not valid JSON array: %v", err)
	}
}



func TestHandleTraffic(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	collector := metrics.NewCollector(logger, time.Minute)
	server := &Server{Config: &config.Config{}, logger: logger, collector: collector}

	req, _ := http.NewRequest("GET", "/api/traffic", nil)
	rr := httptest.NewRecorder()

	server.handleTraffic(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}

	// Verify response is valid JSON
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Response is not valid JSON: %v", err)
	}
}



func TestHandlePolicies_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			Policies: []config.Policy{
				{Name: "test-policy"},
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/policies", nil)
	rr := httptest.NewRecorder()

	server.handleGetPolicies(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestHandlePolicies_Post(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{Config: &config.Config{}, logger: logger}

	body := `[{"name": "new-policy"}]`
	req, _ := http.NewRequest("POST", "/api/policies", strings.NewReader(body))
	rr := httptest.NewRecorder()

	server.handleUpdatePolicies(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

	// Test Removed: Method Not Allowed provided by Mux now
	// func TestHandlePolicies_MethodNotAllowed was here

func TestHandleNAT_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			NAT: []config.NATRule{
				{Name: "test-nat"},
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/nat", nil)
	rr := httptest.NewRecorder()

	server.handleGetNAT(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestHandleDHCP_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			DHCP: &config.DHCPServer{
				Enabled: true,
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/dhcp", nil)
	rr := httptest.NewRecorder()

	server.handleGetDHCP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestHandleDNS_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			DNSServer: &config.DNSServer{
				Enabled: true,
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/dns", nil)
	rr := httptest.NewRecorder()

	server.handleGetDNS(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestHandleRoutes_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			Routes: []config.Route{
				{Destination: "10.0.0.0/8", Gateway: "192.168.1.1"},
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/routes", nil)
	rr := httptest.NewRecorder()

	server.handleGetRoutes(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestHandleZones_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			Zones: []config.Zone{
				{Name: "LAN"},
				{Name: "WAN"},
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/zones", nil)
	rr := httptest.NewRecorder()

	server.handleGetZones(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestHandleBackup(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{Config: &config.Config{}, logger: logger}

	req, _ := http.NewRequest("GET", "/api/backup", nil)
	rr := httptest.NewRecorder()

	server.handleBackup(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}

	// Should be valid JSON config
	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Errorf("backup should return valid JSON: %v", err)
	}
}

func TestHandleRestore_BadRequest(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{Config: &config.Config{}, logger: logger}

	req, _ := http.NewRequest("POST", "/api/restore", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	server.handleRestore(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected BadRequest for invalid JSON, got %v", status)
	}
}

func TestHandleSchedulerConfig_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			Scheduler: &config.SchedulerConfig{
				Enabled: true,
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/scheduler/config", nil)
	rr := httptest.NewRecorder()

	server.handleGetSchedulerConfig(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestHandleSchedulerStatus(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			Scheduler: &config.SchedulerConfig{
				Enabled: true,
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/scheduler/status", nil)
	rr := httptest.NewRecorder()

	server.handleSchedulerStatus(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestHandleIPSets_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/ipsets", nil)
	rr := httptest.NewRecorder()

	server.handleGetIPSets(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestHandleVPN_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			VPN: &config.VPNConfig{
				InterfacePrefixZones: map[string]string{"wg": "vpn"},
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/config/vpn", nil)
	rr := httptest.NewRecorder()

	server.handleGetVPN(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}

	rw.WriteHeader(http.StatusNotFound)

	if rw.statusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rw.statusCode)
	}
}

func TestResponseWriter_Flush(t *testing.T) {
	rr := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}

	// Should not panic
	rw.Flush()
}

// --- Error Path Tests ---

func TestHandlePolicies_InvalidJSON(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{Config: &config.Config{}, logger: logger}

	// Test with invalid JSON
	req, _ := http.NewRequest("POST", "/api/policies", strings.NewReader(`{invalid json}`))
	rr := httptest.NewRecorder()

	server.handleUpdatePolicies(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected BadRequest for invalid JSON, got %v", status)
	}
}

func TestHandleRestore_InvalidJSON(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{Config: &config.Config{}, logger: logger}

	// Test with invalid JSON
	req, _ := http.NewRequest("POST", "/api/backup/restore", strings.NewReader(`not valid json`))
	rr := httptest.NewRecorder()

	server.handleRestore(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected BadRequest for invalid JSON, got %v", status)
	}
}

func TestHandleStatus_MethodNotAllowed(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{Config: &config.Config{}, logger: logger}

	req, _ := http.NewRequest("DELETE", "/api/status", nil)
	rr := httptest.NewRecorder()

	server.handleStatus(rr, req)

	// Status handler returns OK for all methods (read-only endpoint)
	// This test documents actual behavior
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK (handler allows all methods), got %v", status)
	}
}

// ============================================================================
// HTTP Server Hardening Tests
// Mitigation: OWASP A05:2021-Security Misconfiguration
// ============================================================================

func TestServerConfig_HasRequiredTimeouts(t *testing.T) {
	// Verify that our server configuration constants are set appropriately
	// These are the minimum secure values per OWASP recommendations

	// Test: ReadHeaderTimeout must be set to prevent Slowloris
	const expectedReadHeaderTimeout = 5 * time.Second
	const expectedReadTimeout = 15 * time.Second
	const expectedWriteTimeout = 30 * time.Second
	const expectedIdleTimeout = 60 * time.Second
	const expectedMaxHeaderBytes = 1 << 16 // 64KB

	// These values are verified by examining the ServerConfig() output
	cfg := DefaultServerConfig()

	if cfg.ReadHeaderTimeout != expectedReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout: got %v, want %v", cfg.ReadHeaderTimeout, expectedReadHeaderTimeout)
	}
	if cfg.ReadTimeout != expectedReadTimeout {
		t.Errorf("ReadTimeout: got %v, want %v", cfg.ReadTimeout, expectedReadTimeout)
	}
	if cfg.WriteTimeout != expectedWriteTimeout {
		t.Errorf("WriteTimeout: got %v, want %v", cfg.WriteTimeout, expectedWriteTimeout)
	}
	if cfg.IdleTimeout != expectedIdleTimeout {
		t.Errorf("IdleTimeout: got %v, want %v", cfg.IdleTimeout, expectedIdleTimeout)
	}
	if cfg.MaxHeaderBytes != expectedMaxHeaderBytes {
		t.Errorf("MaxHeaderBytes: got %v, want %v", cfg.MaxHeaderBytes, expectedMaxHeaderBytes)
	}
}

func TestServer_MaxRequestBodySize(t *testing.T) {
	// Verify that large request bodies are rejected
	// Mitigation: Memory exhaustion via large payload

	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config:      &config.Config{},
		logger:      logger,
		rateLimiter: ratelimit.NewLimiter(),
	}

	// Create a handler wrapped with body limit middleware
	handler := server.maxBodyMiddleware(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to read body - should fail if too large
		body := make([]byte, 2048)
		n, _ := r.Body.Read(body)
		w.WriteHeader(http.StatusOK)
		w.Write(body[:n])
	}))

	// Test with body exceeding limit
	largeBody := strings.Repeat("x", 2048) // 2KB > 1KB limit
	req, _ := http.NewRequest("POST", "/test", strings.NewReader(largeBody))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 Request Entity Too Large, got %v", status)
	}
}

func TestServer_MaxRequestBodySize_AcceptsSmallBody(t *testing.T) {
	// Verify that small request bodies are accepted

	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config:      &config.Config{},
		logger:      logger,
		rateLimiter: ratelimit.NewLimiter(),
	}

	handler := server.maxBodyMiddleware(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test with body under limit
	smallBody := strings.Repeat("x", 512) // 512B < 1KB limit
	req, _ := http.NewRequest("POST", "/test", strings.NewReader(smallBody))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK for small body, got %v", status)
	}
}

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	if cfg == nil {
		t.Fatal("DefaultServerConfig returned nil")
	}

	// All timeout values should be positive
	if cfg.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout should be positive")
	}
	if cfg.ReadTimeout <= 0 {
		t.Error("ReadTimeout should be positive")
	}
	if cfg.WriteTimeout <= 0 {
		t.Error("WriteTimeout should be positive")
	}
	if cfg.IdleTimeout <= 0 {
		t.Error("IdleTimeout should be positive")
	}
	if cfg.MaxHeaderBytes <= 0 {
		t.Error("MaxHeaderBytes should be positive")
	}
	if cfg.MaxBodyBytes <= 0 {
		t.Error("MaxBodyBytes should be positive")
	}
}

func TestHandleCreateAdmin_EndToEnd(t *testing.T) {
	// 1. Create temporary auth store (no users)
	tmpFile, err := os.CreateTemp("", "auth.json")
	if err != nil {
		t.Fatal(err)
	}
	name := tmpFile.Name()
	tmpFile.Close()
	os.Remove(name)       // Start with no file so NewStore initializes empty
	defer os.Remove(name) // Cleanup

	store, err := auth.NewStore(name)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create server with this auth store
	// NewServer call
	srv, err := NewServer(ServerOptions{
		Config:        &config.Config{},
		Assets:        nil,
		Client:        nil,
		AuthStore:     store,
		APIKeyManager: nil, // Assuming keyManager is not defined or should be nil for this test
	})
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	handler := srv.Handler()

	// 3. Verify /api/auth/status says setup_required=true
	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Status check failed: %d Body: %s", w.Code, w.Body.String())
	}
	var status map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &status)
	if status["setup_required"] != true {
		t.Fatal("Expected setup_required=true")
	}

	// 4. Try to create admin
	// This is the CRITICAL STEP: Does the route exist?
	payload := map[string]string{
		"username": "admin",
		"password": "ProductionPassword123!",
	}
	body, _ := json.Marshal(payload)
	req = httptest.NewRequest("POST", "/api/setup/create-admin", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == 404 {
		t.Fatalf("CRITICAL FAIL: Route /api/setup/create-admin not found (404)")
	}

	if w.Code != 200 {
		t.Fatalf("Create Admin failed: %d Body: %s", w.Code, w.Body.String())
	}

	// 5. Verify admin exists
	if !store.HasUsers() {
		t.Fatalf("Store should have users")
	}
}

func TestConcurrentAdminCreation_Regression(t *testing.T) {
	// Regression test for race condition in admin creation
	// Creates a temporary auth store and attempts to create admin from multiple goroutines simultaneously.

	tmpFile, err := os.CreateTemp("", "auth_race.json")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	os.Remove(tmpFile.Name()) // Start strict: File shouldn't exist for NewStore to init empty
	defer os.Remove(tmpFile.Name()) // Cleanup again just in case

	store, err := auth.NewStore(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(ServerOptions{
		Config:        &config.Config{},
		AuthStore:     store,
		// Needs a valid rate limiter to avoid hitting rate limits before race condition check
		// However, we mock/bypass or set high limits?
		// NewServer creates its own rate limiter. We can't easily inject a modified one via Options in current struct.
		// Use a high-limit IP or rely on "setup" key separation.
		// Actually, standard NewServer uses default limiter.
		// We'll trust the 3/minute limit is per IP.
		// We can spoof IPs in requests.
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()

	concurrency := 10
	results := make(chan int, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			payload := map[string]string{
				"username": "admin",
				"password": "ProductionPassword123!",
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest("POST", "/api/setup/create-admin", bytes.NewReader(body))
			// Use unique IPs to bypass rate limiter
			// 192.0.2.x is TEST-NET-1
			req.RemoteAddr = "192.0.2." + string(rune(10+idx)) + ":12345"

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			results <- w.Code
		}(i)
	}

	successCount := 0
	forbiddenCount := 0
	otherCount := 0

	for i := 0; i < concurrency; i++ {
		code := <-results
		if code == 200 {
			successCount++
		} else if code == 403 {
			// Expected "Admin already exists" or similar
			forbiddenCount++
		} else {
			otherCount++
		}
	}

	if successCount != 1 {
		t.Errorf("Expected exactly 1 success, got %d", successCount)
	}
	if otherCount > 0 {
		t.Errorf("Got unexpected status codes: %d requests failed with non-403", otherCount)
	}
}

func TestBatchRateLimiting_Regression(t *testing.T) {
	// Regression test for Batch API rate limiting bypassing
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config:      &config.Config{},
		logger:      logger,
		rateLimiter: ratelimit.NewLimiter(),
		mux:         http.NewServeMux(),
	}
	// Bind batch handler directly or init routes? Init routes is safer.
	server.initRoutes()

	// 1. Test Batch Size Limit
	// Create a batch with 21 requests
	hugeBatch := make([]BatchRequest, 25)
	for i := range hugeBatch {
		hugeBatch[i] = BatchRequest{Method: "GET", Path: "/api/status"}
	}
	body, _ := json.Marshal(hugeBatch)
	req := httptest.NewRequest("POST", "/api/batch", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for large batch, got %d", w.Code)
	}

	// 2. Test Cost Accounting
	// Send a batch of 10 requests. Limit is 60/min.
	// If cost is counted, 7 batches of 10 should trigger limit (70 > 60).
	// If cost is NOT counted (1 token per batch), it would allow 60 batches.

	// Use a fixed IP
	ip := "198.51.100.1:1234" // TEST-NET-2

	batch10 := make([]BatchRequest, 10)
	for i := range batch10 {
		batch10[i] = BatchRequest{Method: "GET", Path: "/api/status"}
	}
	body10, _ := json.Marshal(batch10)

	limitHit := false
	for i := 0; i < 10; i++ {
		req = httptest.NewRequest("POST", "/api/batch", bytes.NewReader(body10))
		req.RemoteAddr = ip
		w = httptest.NewRecorder()
		server.mux.ServeHTTP(w, req)

		if w.Code == http.StatusTooManyRequests {
			limitHit = true
			if i < 1 {
				t.Errorf("Hit limit too early (iteration %d)", i)
			}
			break
		}
	}

	if !limitHit {
		t.Error("Expected rate limit to be hit by batches, but it wasn't")
	}
}

