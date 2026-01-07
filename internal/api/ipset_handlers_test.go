package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"grimm.is/glacic/internal/ctlplane"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/logging"

	"github.com/stretchr/testify/assert"
)

func TestHandleIPSetList(t *testing.T) {
	mockClient := new(ctlplane.MockControlPlaneClient)

	// Setup mock expectation
	expectedSets := []firewall.IPSetMetadata{
		{Name: "test_set", Type: "ipv4_addr", EntriesCount: 100},
		{Name: "blocklist", Type: "ipv4_addr", EntriesCount: 50},
	}
	mockClient.On("ListIPSets").Return(expectedSets, nil)

	// Create server with mock client
	server := &Server{
		client: mockClient,
		logger: logging.New(logging.DefaultConfig()),
	}

	req, _ := http.NewRequest("GET", "/api/ipsets", nil)
	rr := httptest.NewRecorder()

	server.handleIPSetList(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// Response format is {"ipsets": [...], "count": N}
	var result struct {
		IPSets []IPSetResponse `json:"ipsets"`
		Count  int             `json:"count"`
	}
	err := json.NewDecoder(rr.Body).Decode(&result)
	assert.NoError(t, err)
	assert.Len(t, result.IPSets, 2)
	assert.Equal(t, 2, result.Count)
	assert.Equal(t, "test_set", result.IPSets[0].Name)
	assert.Equal(t, 100, result.IPSets[0].EntriesCount)

	mockClient.AssertExpectations(t)
}

func TestHandleIPSetShow(t *testing.T) {
	mockClient := new(ctlplane.MockControlPlaneClient)

	// Setup mock expectation
	expectedMeta := &firewall.IPSetMetadata{
		Name:         "test_set",
		Type:         "ipv4_addr",
		EntriesCount: 100,
	}
	mockClient.On("GetIPSet", "test_set").Return(expectedMeta, nil)

	server := &Server{
		client: mockClient,
		logger: logging.New(logging.DefaultConfig()),
	}

	// Handler extracts name from path: /api/ipsets/{name}
	req, _ := http.NewRequest("GET", "/api/ipsets/test_set", nil)
	rr := httptest.NewRecorder()

	server.handleIPSetShow(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var result IPSetResponse
	err := json.NewDecoder(rr.Body).Decode(&result)
	assert.NoError(t, err)
	assert.Equal(t, "test_set", result.Name)
	assert.Equal(t, 100, result.EntriesCount)

	mockClient.AssertExpectations(t)
}

func TestHandleIPSetRefresh(t *testing.T) {
	mockClient := new(ctlplane.MockControlPlaneClient)

	// Setup mock expectation
	mockClient.On("RefreshIPSet", "blocklist").Return(nil)

	server := &Server{
		client: mockClient,
		logger: logging.New(logging.DefaultConfig()),
	}

	// Handler extracts name: /api/ipsets/{name}/refresh
	req, _ := http.NewRequest("POST", "/api/ipsets/blocklist/refresh", nil)
	rr := httptest.NewRecorder()

	server.handleIPSetRefresh(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var result map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&result)
	assert.NoError(t, err)
	assert.Equal(t, true, result["refreshed"])

	mockClient.AssertExpectations(t)
}

func TestHandleIPSetList_NoClient(t *testing.T) {
	server := &Server{
		client: nil, // No client
		logger: logging.New(logging.DefaultConfig()),
	}

	req, _ := http.NewRequest("GET", "/api/ipsets", nil)
	rr := httptest.NewRecorder()

	server.handleIPSetList(rr, req)

	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}
