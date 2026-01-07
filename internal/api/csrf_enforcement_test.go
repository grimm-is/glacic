package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
)

func TestCSRFEnforcement(t *testing.T) {
	// Setup real AuthStore with temp dir
	tmpDir, err := os.MkdirTemp("", "glacic-test-auth")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	authPath := filepath.Join(tmpDir, "auth.json")
	store, err := auth.NewStore(authPath)
	if err != nil {
		t.Fatal(err)
	}

	// Create admin user
	err = store.CreateUser("admin", "password123", auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}

	// Authenticate to get session
	session, err := store.Authenticate("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := session.Token

	csrfMgr := NewCSRFManager()
	token, _ := csrfMgr.GenerateToken(sessionID)

	// Create Server (lightweight)
	// We just want to test s.require chain, so we use s.mux directly
	// bypassing global s.Handler() which currently has the middleware.
	// This ensures that s.require ITSELF enforces CSRF as requested.
	s := &Server{
		csrfManager: csrfMgr,
		authStore:   store,
		mux:         http.NewServeMux(),
		logger:      logging.New(logging.DefaultConfig()),
		Config:      &config.Config{API: &config.APIConfig{}},
	}
	s.initRoutes() // Registers routes on s.mux

	// Define the target handler - s.mux
	handler := s.mux

	// 1. Test POST without X-CSRF-Token (Expect 403)
	req := httptest.NewRequest("POST", "/api/config/apply", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("CSRF Enforcement failed: Expected 403 Forbidden for missing CSRF token, got %d", w.Code)
	}

	// 2. Test POST with Invalid X-CSRF-Token (Expect 403)
	req = httptest.NewRequest("POST", "/api/config/apply", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	req.Header.Set("X-CSRF-Token", "invalid-token")
	w = httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("CSRF Enforcement failed: Expected 403 Forbidden for invalid CSRF token, got %d", w.Code)
	}

	// 3. Test POST with Valid X-CSRF-Token (Expect 200 or 401/500 depending on next handler)
	// If CSRF passes, it hits s.require -> checks session -> hits handler.
	// Since we mocked session validation (?) or not...
	// Wait, `s.require` calls `s.authStore.ValidateSession`.
	// Since we are passing a zero-value authStore, `ValidateSession` might fail or panic depending on implementation.
	// We just want to know if CSRF passed.
	// If we get "CSRF validation failed..." (403), then it didn't pass.
	// If we get anything else (like 401 Unauthorized from s.require), then CSRF passed!

	req = httptest.NewRequest("POST", "/api/config/apply", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	req.Header.Set("X-CSRF-Token", token)
	w = httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// We expect NOT "CSRF validation failed"
	if w.Code == http.StatusForbidden && strings.Contains(w.Body.String(), "CSRF validation failed") {
		t.Errorf("CSRF Enforcement failed: Valid token rejected")
	}
}
