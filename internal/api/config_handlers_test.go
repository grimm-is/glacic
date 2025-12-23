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
