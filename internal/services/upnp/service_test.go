package upnp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"grimm.is/glacic/internal/config"
)

// MockFirewallManager implements FirewallManager for testing
type MockFirewallManager struct {
	AddedRules   []config.NATRule
	AddRuleError error
}

func (m *MockFirewallManager) AddDynamicNATRule(rule config.NATRule) error {
	if m.AddRuleError != nil {
		return m.AddRuleError
	}
	m.AddedRules = append(m.AddedRules, rule)
	return nil
}

func TestNewService(t *testing.T) {
	cfg := Config{
		Enabled:       true,
		ExternalIntf:  "eth0",
		InternalIntfs: []string{"eth1"},
		SecureMode:    true,
	}
	mockFw := &MockFirewallManager{}

	svc := NewService(cfg, mockFw)

	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.config.ExternalIntf != "eth0" {
		t.Errorf("Expected ExternalIntf eth0, got %s", svc.config.ExternalIntf)
	}
	if !svc.config.SecureMode {
		t.Error("Expected SecureMode true")
	}
}

func TestService_StartStop(t *testing.T) {
	cfg := Config{
		Enabled:       true,
		ExternalIntf:  "eth0",
		InternalIntfs: []string{}, // No interfaces to avoid actual network ops
	}
	mockFw := &MockFirewallManager{}
	svc := NewService(cfg, mockFw)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := svc.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic
	svc.Stop()
}

func TestService_StartDisabled(t *testing.T) {
	cfg := Config{
		Enabled: false,
	}
	mockFw := &MockFirewallManager{}
	svc := NewService(cfg, mockFw)

	err := svc.Start(context.Background())
	if err != nil {
		t.Fatalf("Start with disabled config should not error: %v", err)
	}
}

func TestService_HandleRootDesc(t *testing.T) {
	cfg := Config{Enabled: true}
	mockFw := &MockFirewallManager{}
	svc := NewService(cfg, mockFw)

	req := httptest.NewRequest("GET", "/rootDesc.xml", nil)
	w := httptest.NewRecorder()

	svc.handleRootDesc(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/xml" {
		t.Errorf("Expected Content-Type text/xml, got %s", contentType)
	}

	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("InternetGatewayDevice")) {
		t.Error("Response should contain InternetGatewayDevice")
	}
}

func TestService_HandleAddPortMapping(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		ExternalIntf: "eth0",
		SecureMode:   false, // Disable for easier testing
	}
	mockFw := &MockFirewallManager{}
	svc := NewService(cfg, mockFw)

	// SOAP Add Port Mapping Request
	soapBody := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
<s:Body>
<u:AddPortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
<NewExternalPort>8080</NewExternalPort>
<NewProtocol>TCP</NewProtocol>
<NewInternalPort>80</NewInternalPort>
<NewInternalClient>192.168.1.100</NewInternalClient>
<NewEnabled>1</NewEnabled>
<NewPortMappingDescription>Test Mapping</NewPortMappingDescription>
<NewLeaseDuration>3600</NewLeaseDuration>
</u:AddPortMapping>
</s:Body>
</s:Envelope>`

	req := httptest.NewRequest("POST", "/ctl/IPConn", bytes.NewReader([]byte(soapBody)))
	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("SOAPACTION", "urn:schemas-upnp-org:service:WANIPConnection:1#AddPortMapping")
	req.RemoteAddr = "192.168.1.100:54321" // Matches NewInternalClient

	w := httptest.NewRecorder()

	svc.handleIPConnControl(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d. Body: %s", resp.StatusCode, w.Body.String())
	}

	// Verify firewall rule was added
	if len(mockFw.AddedRules) != 1 {
		t.Fatalf("Expected 1 added rule, got %d", len(mockFw.AddedRules))
	}

	rule := mockFw.AddedRules[0]
	if rule.Type != "dnat" {
		t.Errorf("Expected rule type dnat, got %s", rule.Type)
	}
	if rule.DestPort != "8080" {
		t.Errorf("Expected DestPort 8080, got %s", rule.DestPort)
	}
	if rule.ToIP != "192.168.1.100" {
		t.Errorf("Expected ToIP 192.168.1.100, got %s", rule.ToIP)
	}
	if rule.ToPort != "80" {
		t.Errorf("Expected ToPort 80, got %s", rule.ToPort)
	}
	if rule.Protocol != "tcp" {
		t.Errorf("Expected protocol tcp, got %s", rule.Protocol)
	}

	// Verify mapping was stored
	svc.mu.Lock()
	mapping, ok := svc.mappings["8080/TCP"]
	svc.mu.Unlock()
	if !ok {
		t.Error("Mapping not stored")
	} else if mapping.InternalClient != "192.168.1.100" {
		t.Errorf("Wrong internal client: %s", mapping.InternalClient)
	}
}

func TestService_HandleAddPortMapping_SecureMode(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		ExternalIntf: "eth0",
		SecureMode:   true, // Enabled
	}
	mockFw := &MockFirewallManager{}
	svc := NewService(cfg, mockFw)

	// Request from different IP than target
	soapBody := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
<s:Body>
<u:AddPortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
<NewExternalPort>8080</NewExternalPort>
<NewProtocol>TCP</NewProtocol>
<NewInternalPort>80</NewInternalPort>
<NewInternalClient>192.168.1.200</NewInternalClient>
<NewEnabled>1</NewEnabled>
<NewPortMappingDescription>Malicious</NewPortMappingDescription>
<NewLeaseDuration>3600</NewLeaseDuration>
</u:AddPortMapping>
</s:Body>
</s:Envelope>`

	req := httptest.NewRequest("POST", "/ctl/IPConn", bytes.NewReader([]byte(soapBody)))
	req.Header.Set("Content-Type", "text/xml")
	req.RemoteAddr = "192.168.1.100:54321" // Different from NewInternalClient

	w := httptest.NewRecorder()
	svc.handleIPConnControl(w, req)

	resp := w.Result()
	// Should fail with 500 (SOAP error)
	if resp.StatusCode == http.StatusOK {
		t.Error("Expected error response in SecureMode when IPs don't match")
	}

	// No rules should be added
	if len(mockFw.AddedRules) != 0 {
		t.Errorf("Expected 0 rules, got %d", len(mockFw.AddedRules))
	}
}

func TestService_HandleDeletePortMapping(t *testing.T) {
	cfg := Config{Enabled: true}
	mockFw := &MockFirewallManager{}
	svc := NewService(cfg, mockFw)

	// Pre-populate a mapping
	svc.mappings["8080/TCP"] = PortMapping{
		ExternalPort:   8080,
		Protocol:       "TCP",
		InternalClient: "192.168.1.100",
		InternalPort:   80,
	}

	soapBody := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
<s:Body>
<u:DeletePortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
<NewExternalPort>8080</NewExternalPort>
<NewProtocol>TCP</NewProtocol>
</u:DeletePortMapping>
</s:Body>
</s:Envelope>`

	req := httptest.NewRequest("POST", "/ctl/IPConn", bytes.NewReader([]byte(soapBody)))
	req.Header.Set("Content-Type", "text/xml")

	w := httptest.NewRecorder()
	svc.handleIPConnControl(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	// Mapping should be removed
	svc.mu.Lock()
	_, ok := svc.mappings["8080/TCP"]
	svc.mu.Unlock()
	if ok {
		t.Error("Mapping should have been deleted")
	}
}

func TestService_HandleGetExternalIPAddress(t *testing.T) {
	cfg := Config{Enabled: true}
	mockFw := &MockFirewallManager{}
	svc := NewService(cfg, mockFw)
	svc.wanIP = "203.0.113.42" // Pre-set for test

	soapBody := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
<s:Body>
<u:GetExternalIPAddress xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
</u:GetExternalIPAddress>
</s:Body>
</s:Envelope>`

	req := httptest.NewRequest("POST", "/ctl/IPConn", bytes.NewReader([]byte(soapBody)))
	req.Header.Set("Content-Type", "text/xml")

	w := httptest.NewRecorder()
	svc.handleIPConnControl(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("203.0.113.42")) {
		t.Errorf("Response should contain WAN IP. Body: %s", body)
	}
}

func TestService_HandleInvalidXML(t *testing.T) {
	cfg := Config{Enabled: true}
	mockFw := &MockFirewallManager{}
	svc := NewService(cfg, mockFw)

	req := httptest.NewRequest("POST", "/ctl/IPConn", bytes.NewReader([]byte("not xml")))
	req.Header.Set("Content-Type", "text/xml")

	w := httptest.NewRecorder()
	svc.handleIPConnControl(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", resp.StatusCode)
	}
}
