package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/ratelimit"
)

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
	srv, err := NewServer(ServerOptions{
		Config:        &config.Config{},
		Assets:        nil,
		Client:        nil,
		AuthStore:     store,
		APIKeyManager: nil,
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

	tmpFile, err := os.CreateTemp("", "auth_race.json")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	os.Remove(tmpFile.Name())
	defer os.Remove(tmpFile.Name())

	store, err := auth.NewStore(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	srv, err := NewServer(ServerOptions{
		Config:    &config.Config{},
		AuthStore: store,
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
