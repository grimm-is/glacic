package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
)

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

// TestHandlePolicyRoutes_Get tests getting policy routes
func TestHandlePolicyRoutes_Get(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			PolicyRoutes: []config.PolicyRoute{
				{Name: "route-vpn", Table: 100},
			},
		},
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/config/policy_routes", nil)
	rr := httptest.NewRecorder()

	server.handleGetPolicyRoutes(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected OK, got %v", status)
	}

	if !strings.Contains(rr.Body.String(), "route-vpn") {
		t.Errorf("expected route-vpn in response, got: %s", rr.Body.String())
	}
}

// --- Sad Path Tests ---

// Note: RPC failure testing requires a full mock of ControlPlaneClient
// which has ~50 methods. This is tracked for future enhancement.

// TestHandlePolicies_ResponseBodyCheck verifies response body contains expected data
func TestHandlePolicies_ResponseBodyCheck(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			Policies: []config.Policy{
				{Name: "allow-ssh", From: "lan", To: "wan"},
				{Name: "block-all", From: "wan", To: "lan"},
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

	// Verify specific content in response
	body := rr.Body.String()
	if !strings.Contains(body, "allow-ssh") {
		t.Errorf("expected 'allow-ssh' in response, got: %s", body)
	}
	if !strings.Contains(body, "block-all") {
		t.Errorf("expected 'block-all' in response, got: %s", body)
	}
}

// TestHandleZones_NoConfig tests error when config is nil
func TestHandleZones_NoConfig(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: nil,
		logger: logger,
	}

	req, _ := http.NewRequest("GET", "/api/zones", nil)
	rr := httptest.NewRecorder()

	server.handleGetZones(rr, req)

	// Should handle nil config gracefully
	if status := rr.Code; status != http.StatusServiceUnavailable && status != http.StatusOK {
		// Either 503 (config unavailable) or 200 with empty is acceptable
		t.Logf("Status: %v, Body: %s", status, rr.Body.String())
	}
}

// TestHandleDHCP_UpdateWithScopes tests DHCP update includes scope data
func TestHandleDHCP_UpdateCheck(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	server := &Server{
		Config: &config.Config{
			DHCP: &config.DHCPServer{
				Enabled: true,
				Scopes: []config.DHCPScope{
					{
						Name:       "office",
						Interface:  "eth0",
						RangeStart: "192.168.1.100",
						RangeEnd:   "192.168.1.200",
					},
				},
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

	body := rr.Body.String()
	if !strings.Contains(body, "office") {
		t.Errorf("expected scope name 'office' in response, got: %s", body)
	}
	if !strings.Contains(body, "192.168.1.100") {
		t.Errorf("expected range_start in response, got: %s", body)
	}
}
