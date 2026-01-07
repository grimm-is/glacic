package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"grimm.is/glacic/internal/config"
)

// MockDeviceLookup for testing alias resolution
type MockDeviceLookup struct {
	devices map[string]struct {
		name      string
		matchType string
	}
}

func (m *MockDeviceLookup) FindByIP(ip string) (name string, matchType string, found bool) {
	if m.devices == nil {
		return "", "", false
	}
	if d, ok := m.devices[ip]; ok {
		return d.name, d.matchType, true
	}
	return "", "", false
}

// MockStatsCollector for testing stats enrichment
type MockStatsCollector struct {
	sparklines map[string][]float64
	bytes      map[string]uint64
}

func (m *MockStatsCollector) GetSparkline(ruleID string) []float64 {
	if m.sparklines == nil {
		return nil
	}
	return m.sparklines[ruleID]
}

func (m *MockStatsCollector) GetTotalBytes(ruleID string) uint64 {
	if m.bytes == nil {
		return 0
	}
	return m.bytes[ruleID]
}

func TestHandleGetRules_NoConfig(t *testing.T) {
	server := &Server{Config: nil}
	handler := NewRulesHandler(server, nil, nil)

	req := httptest.NewRequest("GET", "/api/rules", nil)
	w := httptest.NewRecorder()

	handler.HandleGetRules(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}
}

func TestHandleGetRules_EmptyPolicies(t *testing.T) {
	server := &Server{
		Config: &config.Config{
			Policies: []config.Policy{},
		},
	}
	handler := NewRulesHandler(server, nil, nil)

	req := httptest.NewRequest("GET", "/api/rules", nil)
	w := httptest.NewRecorder()

	handler.HandleGetRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var response []PolicyWithStats
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response) != 0 {
		t.Errorf("Expected empty policies, got %d", len(response))
	}
}

func TestHandleGetRules_WithRules(t *testing.T) {
	server := &Server{
		Config: &config.Config{
			Policies: []config.Policy{
				{
					Name: "test-policy",
					From: "lan",
					To:   "wan",
					Rules: []config.PolicyRule{
						{
							Name:   "allow-http",
							ID:     "rule-1",
							Action: "accept",
							SrcIP:  "10.0.0.0/8",
						},
					},
				},
			},
		},
	}
	handler := NewRulesHandler(server, nil, nil)

	req := httptest.NewRequest("GET", "/api/rules", nil)
	w := httptest.NewRecorder()

	handler.HandleGetRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var response []PolicyWithStats
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response) != 1 {
		t.Fatalf("Expected 1 policy, got %d", len(response))
	}

	if len(response[0].Rules) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(response[0].Rules))
	}

	rule := response[0].Rules[0]
	if rule.PolicyFrom != "lan" {
		t.Errorf("Expected policy_from='lan', got '%s'", rule.PolicyFrom)
	}
	if rule.PolicyTo != "wan" {
		t.Errorf("Expected policy_to='wan', got '%s'", rule.PolicyTo)
	}
	if rule.ResolvedSrc == nil {
		t.Error("Expected resolved_src to be populated")
	} else if rule.ResolvedSrc.Type != "cidr" {
		t.Errorf("Expected resolved_src type='cidr', got '%s'", rule.ResolvedSrc.Type)
	}
}

func TestHandleGetFlatRules_NoConfig(t *testing.T) {
	server := &Server{Config: nil}
	handler := NewRulesHandler(server, nil, nil)

	req := httptest.NewRequest("GET", "/api/rules/flat", nil)
	w := httptest.NewRecorder()

	handler.HandleGetFlatRules(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}
}

func TestHandleGetFlatRules_WithLimit(t *testing.T) {
	server := &Server{
		Config: &config.Config{
			Policies: []config.Policy{
				{
					Name: "policy1",
					From: "lan",
					To:   "wan",
					Rules: []config.PolicyRule{
						{Name: "rule1", Action: "accept"},
						{Name: "rule2", Action: "accept"},
						{Name: "rule3", Action: "accept"},
					},
				},
			},
		},
	}
	handler := NewRulesHandler(server, nil, nil)

	req := httptest.NewRequest("GET", "/api/rules/flat?limit=2", nil)
	w := httptest.NewRecorder()

	handler.HandleGetFlatRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var response []RuleWithStats
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("Expected 2 rules (limited), got %d", len(response))
	}
}

func TestHandleGetFlatRules_WithGroupFilter(t *testing.T) {
	server := &Server{
		Config: &config.Config{
			Policies: []config.Policy{
				{
					Name: "policy1",
					From: "lan",
					To:   "wan",
					Rules: []config.PolicyRule{
						{Name: "rule1", Action: "accept", GroupTag: "web"},
						{Name: "rule2", Action: "accept", GroupTag: "ssh"},
						{Name: "rule3", Action: "accept", GroupTag: "web"},
					},
				},
			},
		},
	}
	handler := NewRulesHandler(server, nil, nil)

	req := httptest.NewRequest("GET", "/api/rules/flat?group=web", nil)
	w := httptest.NewRecorder()

	handler.HandleGetFlatRules(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var response []RuleWithStats
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("Expected 2 rules with group='web', got %d", len(response))
	}

	for _, rule := range response {
		if rule.GroupTag != "web" {
			t.Errorf("Expected all rules to have group='web', got '%s'", rule.GroupTag)
		}
	}
}

func TestHandleGetRuleGroups_NoConfig(t *testing.T) {
	server := &Server{Config: nil}
	handler := NewRulesHandler(server, nil, nil)

	req := httptest.NewRequest("GET", "/api/rules/groups", nil)
	w := httptest.NewRecorder()

	handler.HandleGetRuleGroups(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}
}

func TestHandleGetRuleGroups_UniqueGroups(t *testing.T) {
	server := &Server{
		Config: &config.Config{
			Policies: []config.Policy{
				{
					Name: "policy1",
					Rules: []config.PolicyRule{
						{Name: "rule1", GroupTag: "web"},
						{Name: "rule2", GroupTag: "ssh"},
						{Name: "rule3", GroupTag: "web"},
						{Name: "rule4", GroupTag: ""},
					},
				},
				{
					Name: "policy2",
					Rules: []config.PolicyRule{
						{Name: "rule5", GroupTag: "database"},
					},
				},
			},
		},
	}
	handler := NewRulesHandler(server, nil, nil)

	req := httptest.NewRequest("GET", "/api/rules/groups", nil)
	w := httptest.NewRecorder()

	handler.HandleGetRuleGroups(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var response []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have 3 unique groups: web, ssh, database (empty string is ignored)
	if len(response) != 3 {
		t.Errorf("Expected 3 unique groups, got %d", len(response))
	}

	// Check counts
	groupCounts := make(map[string]int)
	for _, g := range response {
		groupCounts[g.Name] = g.Count
	}

	if groupCounts["web"] != 2 {
		t.Errorf("Expected 'web' count=2, got %d", groupCounts["web"])
	}
	if groupCounts["ssh"] != 1 {
		t.Errorf("Expected 'ssh' count=1, got %d", groupCounts["ssh"])
	}
	if groupCounts["database"] != 1 {
		t.Errorf("Expected 'database' count=1, got %d", groupCounts["database"])
	}
}

func TestHandleGetRules_WithDeviceLookup(t *testing.T) {
	deviceLookup := &MockDeviceLookup{
		devices: map[string]struct {
			name      string
			matchType string
		}{
			"10.0.0.5": {name: "Johns-iPhone", matchType: "identity"},
		},
	}

	server := &Server{
		Config: &config.Config{
			Policies: []config.Policy{
				{
					Name: "test",
					From: "lan",
					To:   "wan",
					Rules: []config.PolicyRule{
						{Name: "rule1", SrcIP: "10.0.0.5"},
					},
				},
			},
		},
	}
	handler := NewRulesHandler(server, nil, deviceLookup)

	req := httptest.NewRequest("GET", "/api/rules", nil)
	w := httptest.NewRecorder()

	handler.HandleGetRules(w, req)

	var response []PolicyWithStats
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response) == 0 || len(response[0].Rules) == 0 {
		t.Fatal("Expected at least one policy with one rule")
	}

	rule := response[0].Rules[0]
	if rule.ResolvedSrc == nil {
		t.Fatal("Expected resolved_src")
	}

	if rule.ResolvedSrc.DisplayName != "Johns-iPhone" {
		t.Errorf("Expected device name 'Johns-iPhone', got '%s'", rule.ResolvedSrc.DisplayName)
	}
	if rule.ResolvedSrc.Type != "device_named" {
		t.Errorf("Expected type 'device_named', got '%s'", rule.ResolvedSrc.Type)
	}
}
