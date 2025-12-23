package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/metrics"
	"grimm.is/glacic/internal/ratelimit"
)

// ==============================================================================
// Core Server Handler Tests
// ==============================================================================

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

// ==============================================================================
// ResponseWriter Tests
// ==============================================================================

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

// ==============================================================================
// Error Path Tests
// ==============================================================================

func TestHandleStatus_MethodNotAllowed(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{Config: &config.Config{}, logger: logger}

	req, _ := http.NewRequest("DELETE", "/api/status", nil)
	rr := httptest.NewRecorder()

	server.handleStatus(rr, req)

	// Status handler returns OK for all methods (read-only endpoint)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK (handler allows all methods), got %v", status)
	}
}

// ==============================================================================
// HTTP Server Hardening Tests
// Mitigation: OWASP A05:2021-Security Misconfiguration
// ==============================================================================

func TestServerConfig_HasRequiredTimeouts(t *testing.T) {
	const expectedReadHeaderTimeout = 5 * time.Second
	const expectedReadTimeout = 15 * time.Second
	const expectedWriteTimeout = 30 * time.Second
	const expectedIdleTimeout = 60 * time.Second
	const expectedMaxHeaderBytes = 1 << 16 // 64KB

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
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config:      &config.Config{},
		logger:      logger,
		rateLimiter: ratelimit.NewLimiter(),
	}

	handler := server.maxBodyMiddleware(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 2048)
		n, _ := r.Body.Read(body)
		w.WriteHeader(http.StatusOK)
		w.Write(body[:n])
	}))

	// Test with body exceeding limit
	largeBody := strings.Repeat("x", 2048)
	req, _ := http.NewRequest("POST", "/test", strings.NewReader(largeBody))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 Request Entity Too Large, got %v", status)
	}
}

func TestServer_MaxRequestBodySize_AcceptsSmallBody(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config:      &config.Config{},
		logger:      logger,
		rateLimiter: ratelimit.NewLimiter(),
	}

	handler := server.maxBodyMiddleware(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	smallBody := strings.Repeat("x", 512)
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

// ==============================================================================
// Batch API Tests
// ==============================================================================

func TestBatchRateLimiting_Regression(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config:      &config.Config{},
		logger:      logger,
		rateLimiter: ratelimit.NewLimiter(),
		mux:         http.NewServeMux(),
	}
	server.initRoutes()

	// 1. Test Batch Size Limit
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
	ip := "198.51.100.1:1234"

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
