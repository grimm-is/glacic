package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/services/scanner"
)

// MockScannerClient implements ScannerClient for testing
type MockScannerClient struct {
	MockStartScanNetwork func(cidr string, timeoutSeconds int) error
	MockGetScanStatus    func() (bool, *scanner.ScanResult, error)
	MockGetScanResult    func() (*scanner.ScanResult, error)
	MockScanHost         func(ip string) (*scanner.HostResult, error)
	MockGetCommonPorts   func() ([]scanner.Port, error)
}

func (m *MockScannerClient) StartScanNetwork(cidr string, timeoutSeconds int) error {
	if m.MockStartScanNetwork != nil {
		return m.MockStartScanNetwork(cidr, timeoutSeconds)
	}
	return nil
}

func (m *MockScannerClient) GetScanStatus() (bool, *scanner.ScanResult, error) {
	if m.MockGetScanStatus != nil {
		return m.MockGetScanStatus()
	}
	return false, nil, nil
}

func (m *MockScannerClient) GetScanResult() (*scanner.ScanResult, error) {
	if m.MockGetScanResult != nil {
		return m.MockGetScanResult()
	}
	return nil, nil // Return nil result by default
}

func (m *MockScannerClient) ScanHost(ip string) (*scanner.HostResult, error) {
	if m.MockScanHost != nil {
		return m.MockScanHost(ip)
	}
	return nil, nil
}

func (m *MockScannerClient) GetCommonPorts() ([]scanner.Port, error) {
	if m.MockGetCommonPorts != nil {
		return m.MockGetCommonPorts()
	}
	return []scanner.Port{{Number: 80, Name: "http", Description: "Hypertext Transfer Protocol"}}, nil
}

func TestScannerHandlers_CommonPorts(t *testing.T) {
	logger := &logging.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sc := &MockScannerClient{}
	server := &Server{logger: logger}
	handlers := NewScannerHandlers(server, sc)

	req, _ := http.NewRequest("GET", "/api/scanner/ports", nil)
	rr := httptest.NewRecorder()

	handlers.CommonPorts(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}
}

func TestScannerHandlers_CommonPorts_MethodNotAllowed(t *testing.T) {
	logger := &logging.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sc := &MockScannerClient{}
	server := &Server{logger: logger}
	handlers := NewScannerHandlers(server, sc)

	req, _ := http.NewRequest("POST", "/api/scanner/ports", nil)
	rr := httptest.NewRecorder()

	handlers.CommonPorts(rr, req)

	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("expected MethodNotAllowed, got %v", status)
	}
}

func TestScannerHandlers_Status(t *testing.T) {
	logger := &logging.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sc := &MockScannerClient{
		MockGetScanStatus: func() (bool, *scanner.ScanResult, error) {
			return true, nil, nil
		},
	}
	server := &Server{logger: logger}
	handlers := NewScannerHandlers(server, sc)

	req, _ := http.NewRequest("GET", "/api/scanner/status", nil)
	rr := httptest.NewRecorder()

	handlers.Status(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, ok := resp["scanning"]; !ok {
		t.Error("expected 'scanning' field in response")
	}
	if resp["scanning"] != true {
		t.Error("expected scanning to be true")
	}
}

func TestScannerHandlers_LastResult_Empty(t *testing.T) {
	logger := &logging.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sc := &MockScannerClient{
		MockGetScanResult: func() (*scanner.ScanResult, error) {
			return nil, nil
		},
	}
	server := &Server{logger: logger}
	handlers := NewScannerHandlers(server, sc)

	req, _ := http.NewRequest("GET", "/api/scanner/result", nil)
	rr := httptest.NewRecorder()

	handlers.LastResult(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["message"] != "No scan results available" {
		t.Errorf("expected 'No scan results available' message, got %v", resp["message"])
	}
}

func TestScannerHandlers_ScanHost_InvalidIP(t *testing.T) {
	logger := &logging.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sc := &MockScannerClient{}
	server := &Server{logger: logger}
	handlers := NewScannerHandlers(server, sc)

	body := bytes.NewBufferString(`{"ip": ""}`) // Missing/Empty IP triggers bad request before calling client
	req, _ := http.NewRequest("POST", "/api/scanner/host", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handlers.ScanHost(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected BadRequest, got %v", status)
	}
}

func TestScannerHandlers_ScanHost_ClientError(t *testing.T) {
	logger := &logging.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sc := &MockScannerClient{
		MockScanHost: func(ip string) (*scanner.HostResult, error) {
			return nil, errors.New("host unreachable")
		},
	}
	server := &Server{logger: logger}
	handlers := NewScannerHandlers(server, sc)

	body := bytes.NewBufferString(`{"ip": "10.0.0.1"}`)
	req, _ := http.NewRequest("POST", "/api/scanner/host", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handlers.ScanHost(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected BadRequest (from client error), got %v", status)
	}
}

func TestScannerHandlers_ScanNetwork_MissingCIDR(t *testing.T) {
	logger := &logging.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sc := &MockScannerClient{}
	server := &Server{logger: logger}
	handlers := NewScannerHandlers(server, sc)

	body := bytes.NewBufferString(`{}`)
	req, _ := http.NewRequest("POST", "/api/scanner/network", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handlers.ScanNetwork(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected BadRequest for missing CIDR, got %v", status)
	}
}

func TestScannerHandlers_ScanNetwork_StartsAsync(t *testing.T) {
	logger := &logging.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sc := &MockScannerClient{
		MockStartScanNetwork: func(cidr string, timeoutSeconds int) error {
			return nil
		},
	}
	server := &Server{logger: logger}
	handlers := NewScannerHandlers(server, sc)

	body := bytes.NewBufferString(`{"cidr": "127.0.0.1/32"}`)
	req, _ := http.NewRequest("POST", "/api/scanner/network", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handlers.ScanNetwork(rr, req)

	// Should return 202 Accepted since scan is async
	if status := rr.Code; status != http.StatusAccepted {
		t.Errorf("expected Accepted (202), got %v", status)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["status"] != "started" {
		t.Errorf("expected status 'started', got %v", resp["status"])
	}
}
