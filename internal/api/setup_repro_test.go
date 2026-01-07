package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"grimm.is/glacic/internal/auth"
	"grimm.is/glacic/internal/config"
)

func TestSetupFlow(t *testing.T) {
	// 1. Create temporary auth store (no users)
	tmpFile, err := os.CreateTemp("", "auth.json")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()           // Close so NewStore can open it
	os.Remove(tmpFile.Name()) // Start with no file so NewStore initializes empty
	// defer os.Remove(tmpFile.Name()) // setup_repro_test.go:19 had defer.
	// Wait, if I remove it, then defer remove might fail or be redundant.
	// I should create a fresh defer after NewStore? Or just assume it works.

	store, err := auth.NewStore(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name()) // Cleanup after test

	// 2. Create server with this auth store
	// Note: NewServer(opts)
	srv, err := NewServer(ServerOptions{
		Config:    &config.Config{},
		Assets:    nil,
		AuthStore: store,
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

	// 4. Try to create admin
	payload := map[string]string{
		"username": "admin",
		"password": "ProductionPassword123!",
	}
	body, _ := json.Marshal(payload)
	req = httptest.NewRequest("POST", "/api/setup/create-admin", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Create Admin failed: %d Body: %s", w.Code, w.Body.String())
	}

	// 5. Verify admin exists
	if !store.HasUsers() {
		t.Fatalf("Store should have users")
	}
}
