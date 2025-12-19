package vpn

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// MockWireGuardManager for testing
type MockWireGuardManager struct {
	config  WireGuardConfig
	status  *WireGuardStatus
	upErr   error
	downErr error
}

func newMockWireGuardManager() *MockWireGuardManager {
	return &MockWireGuardManager{
		config: WireGuardConfig{
			Enabled:    true,
			Interface:  "wg0",
			ListenPort: 51820,
			Peers: []WireGuardPeer{
				{Name: "peer1", PublicKey: "abc123"},
			},
		},
		status: &WireGuardStatus{
			Running:    true,
			Interface:  "wg0",
			PublicKey:  "pubkey123",
			ListenPort: 51820,
		},
	}
}

func (m *MockWireGuardManager) GetConfig() WireGuardConfig  { return m.config }
func (m *MockWireGuardManager) GetStatus() *WireGuardStatus { return m.status }
func (m *MockWireGuardManager) Up() error                   { return m.upErr }
func (m *MockWireGuardManager) Down() error                 { return m.downErr }
func (m *MockWireGuardManager) IsEnabled() bool             { return m.config.Enabled }
func (m *MockWireGuardManager) IsConnected() bool           { return m.status.Running }
func (m *MockWireGuardManager) Interface() string           { return m.config.Interface }

// AddPeer adds a peer (mock implementation)
func (m *MockWireGuardManager) AddPeer(peer WireGuardPeer) error {
	m.config.Peers = append(m.config.Peers, peer)
	return nil
}

// RemovePeer removes a peer (mock implementation)
func (m *MockWireGuardManager) RemovePeer(publicKey string) error {
	var newPeers []WireGuardPeer
	for _, p := range m.config.Peers {
		if p.PublicKey != publicKey {
			newPeers = append(newPeers, p)
		}
	}
	m.config.Peers = newPeers
	return nil
}

func TestWireGuardAPIHandler_HandleStatus(t *testing.T) {
	mock := newMockWireGuardManager()
	handler := NewWireGuardAPIHandler(mock)

	req := httptest.NewRequest("GET", "/api/wireguard/status", nil)
	rr := httptest.NewRecorder()

	handler.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var resp WireGuardStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	if !resp.Running {
		t.Error("Expected running=true")
	}
	if resp.Interface != "wg0" {
		t.Errorf("Expected interface wg0, got %s", resp.Interface)
	}
}

func TestWireGuardAPIHandler_HandleStatus_MethodNotAllowed(t *testing.T) {
	mock := newMockWireGuardManager()
	handler := NewWireGuardAPIHandler(mock)

	req := httptest.NewRequest("POST", "/api/wireguard/status", nil)
	rr := httptest.NewRecorder()

	handler.handleStatus(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rr.Code)
	}
}

func TestWireGuardAPIHandler_HandleConfig(t *testing.T) {
	mock := newMockWireGuardManager()
	handler := NewWireGuardAPIHandler(mock)

	req := httptest.NewRequest("GET", "/api/wireguard/config", nil)
	rr := httptest.NewRecorder()

	handler.handleConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var resp WireGuardConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	if resp.Interface != "wg0" {
		t.Errorf("Expected interface wg0, got %s", resp.Interface)
	}
	if resp.ListenPort != 51820 {
		t.Errorf("Expected port 51820, got %d", resp.ListenPort)
	}
}

func TestWireGuardAPIHandler_HandleUp(t *testing.T) {
	mock := newMockWireGuardManager()
	handler := NewWireGuardAPIHandler(mock)

	req := httptest.NewRequest("POST", "/api/wireguard/up", nil)
	rr := httptest.NewRecorder()

	handler.handleUp(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestWireGuardAPIHandler_HandleDown(t *testing.T) {
	mock := newMockWireGuardManager()
	handler := NewWireGuardAPIHandler(mock)

	req := httptest.NewRequest("POST", "/api/wireguard/down", nil)
	rr := httptest.NewRecorder()

	handler.handleDown(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestWireGuardAPIHandler_HandleGenerateKey(t *testing.T) {
	mock := newMockWireGuardManager()
	handler := NewWireGuardAPIHandler(mock)

	req := httptest.NewRequest("POST", "/api/wireguard/generate-key", nil)
	rr := httptest.NewRecorder()

	handler.handleGenerateKey(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	if resp["private_key"] == "" {
		t.Error("Expected private_key in response")
	}
	if resp["public_key"] == "" {
		t.Error("Expected public_key in response")
	}
}

func TestWireGuardAPIHandler_HandlePeers_Get(t *testing.T) {
	mock := newMockWireGuardManager()
	handler := NewWireGuardAPIHandler(mock)

	req := httptest.NewRequest("GET", "/api/wireguard/peers", nil)
	rr := httptest.NewRecorder()

	handler.handlePeers(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}

	peers, ok := resp["peers"].([]interface{})
	if !ok {
		t.Fatal("Expected peers array in response")
	}
	if len(peers) != 1 {
		t.Errorf("Expected 1 peer, got %d", len(peers))
	}
}

func TestWireGuardAPIHandler_HandleAddPeer(t *testing.T) {
	mock := newMockWireGuardManager()
	handler := NewWireGuardAPIHandler(mock)

	body := `{
		"name": "new-peer",
		"public_key": "newpubkey123",
		"allowed_ips": ["10.0.0.2/32"],
		"endpoint": "192.168.1.100:51820"
	}`

	req := httptest.NewRequest("POST", "/api/wireguard/peers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.handleAddPeer(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWireGuardAPIHandler_HandleRemovePeer(t *testing.T) {
	mock := newMockWireGuardManager()
	handler := NewWireGuardAPIHandler(mock)

	req := httptest.NewRequest("DELETE", "/api/wireguard/peers/abc123", nil)
	rr := httptest.NewRecorder()

	handler.handleRemovePeer(rr, req, "abc123")

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
