package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
)

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
