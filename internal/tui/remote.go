package tui

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"grimm.is/glacic/internal/config"
)

// RemoteBackend implements Backend using the HTTP API
type RemoteBackend struct {
	BaseURL string
	Client  *http.Client
	APIKey  string
}

// NewRemoteBackend creates a new remote backend
func NewRemoteBackend(baseURL, apiKey string, insecure bool) *RemoteBackend {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}

	return &RemoteBackend{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
	}
}

func (b *RemoteBackend) do(method, path string) (*http.Response, error) {
	url := b.BaseURL + path
	DebugLog("REQ %s %s", method, url)
	start := time.Now()

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		DebugLog("ERR init %s: %v", url, err)
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+b.APIKey)
	req.Header.Set("X-API-Key", b.APIKey) // Support both standard and custom header
	req.Header.Set("Accept", "application/json")

	resp, err := b.Client.Do(req)
	duration := time.Since(start)

	if err != nil {
		DebugLog("ERR %s %s (%s): %v", method, url, duration, err)
		return nil, err
	}

	DebugLog("RES %s %s %d (%s)", method, url, resp.StatusCode, duration)
	return resp, nil
}

func (b *RemoteBackend) GetStatus() (*EnrichedStatus, error) {
	resp, err := b.do("GET", "/api/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: %s", resp.Status)
	}

	// API returns specific status JSON, we map it to EnrichedStatus
	// For now, let's assume we decode a map and extract
	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	status := &EnrichedStatus{
		Running: true,
		Uptime:  "Online", // TODO: Parse duration
	}

	if val, ok := data["uptime"].(string); ok {
		status.Uptime = val
	}

	return status, nil
}

// apiFlow matches the JSON response from /api/flows
type apiFlow struct {
	Proto string `json:"proto"`
	Src   string `json:"src"`
	Dst   string `json:"dst"`
	State string `json:"state"`
}

func (b *RemoteBackend) GetFlows(filter string) ([]Flow, error) {
	resp, err := b.do("GET", "/api/flows")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: %s", resp.Status)
	}

	var data struct {
		Flows []apiFlow `json:"flows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	flows := make([]Flow, len(data.Flows))
	for i, f := range data.Flows {
		flows[i] = Flow{
			Proto: f.Proto,
			Src:   f.Src,
			Dst:   f.Dst,
			State: f.State,
		}
	}

	return flows, nil
}

func (b *RemoteBackend) GetConfig() (*config.Config, error) {
	resp, err := b.do("GET", "/api/config")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: %s", resp.Status)
	}

	var cfg config.Config
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
