package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"grimm.is/glacic/internal/config"
)

func TestHTTPClient_GetStatus(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			t.Errorf("expected path /api/status, got %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		// Check API key header
		if apiKey := r.Header.Get("X-API-Key"); apiKey != "test-key" {
			t.Errorf("expected X-API-Key 'test-key', got '%s'", apiKey)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ServerInfo{
			Status:         "online",
			Uptime:         "1h30m",
			Version:        "1.0.0",
			FirewallActive: true,
			CPULoad:        5.5,
			MemUsage:       42.1,
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, WithAPIKey("test-key"))
	info, err := client.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if info.Status != "online" {
		t.Errorf("expected status 'online', got '%s'", info.Status)
	}
	if info.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", info.Version)
	}
	if !info.FirewallActive {
		t.Error("expected FirewallActive to be true")
	}
}

func TestHTTPClient_GetConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config" {
			t.Errorf("expected path /api/config, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config.Config{
			SchemaVersion: "1.0",
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	cfg, err := client.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if cfg.SchemaVersion != "1.0" {
		t.Errorf("expected SchemaVersion '1.0', got '%s'", cfg.SchemaVersion)
	}
}

func TestHTTPClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid API key"}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL)
	_, err := client.GetStatus()
	if err == nil {
		t.Error("expected error for 401 response")
	}
}
