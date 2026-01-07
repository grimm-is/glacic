// Package client provides an API client for remote Glacic management.
package client

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"grimm.is/glacic/internal/config"

	"github.com/gorilla/websocket"
)

// ServerInfo mirrors the API ServerInfo response.
// Defined locally to avoid importing the heavy internal/api package.
type ServerInfo struct {
	Status          string  `json:"status"`
	Uptime          string  `json:"uptime"`
	HostUptime      string  `json:"host_uptime,omitempty"`
	StartTime       string  `json:"start_time"`
	Version         string  `json:"version"`
	BuildTime       string  `json:"build_time,omitempty"`
	BuildArch       string  `json:"build_arch,omitempty"`
	GitCommit       string  `json:"git_commit,omitempty"`
	GitBranch       string  `json:"git_branch,omitempty"`
	GitMergeBase    string  `json:"git_merge_base,omitempty"`
	FirewallActive  bool    `json:"firewall_active"`
	FirewallApplied string  `json:"firewall_applied,omitempty"`
	WanIP           string  `json:"wan_ip,omitempty"`
	BlockedCount    int64   `json:"blocked_count"`
	CPULoad         float64 `json:"cpu_load"`
	MemUsage        float64 `json:"mem_usage"`
}

// APIClient defines the interface for interacting with a remote Glacic instance.
type APIClient interface {
	GetStatus() (*ServerInfo, error)
	GetConfig() (*config.Config, error)
}

// HTTPClient is an HTTP-based implementation of APIClient.
type HTTPClient struct {
	baseURL             string
	apiKey              string
	httpClient          *http.Client
	expectedFingerprint string
	SeenFingerprint     string
}

// ClientOption configures the HTTPClient.
type ClientOption func(*HTTPClient)

// WithAPIKey sets the API key for authentication.
func WithAPIKey(key string) ClientOption {
	return func(c *HTTPClient) {
		c.apiKey = key
	}
}

// WithFingerprint sets the expected server certificate fingerprint (SHA-256 hex).
func WithFingerprint(fp string) ClientOption {
	return func(c *HTTPClient) {
		c.expectedFingerprint = fp
	}
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *HTTPClient) {
		if c.httpClient != nil {
			c.httpClient.Timeout = d
		}
	}
}

// NewHTTPClient creates a new HTTPClient for the given base URL.
// By default, accepts self-signed certificates (common for appliances).
// Mitigation: OWASP A01:2021-Broken Access Control - Client always sends API key if configured.
func NewHTTPClient(baseURL string, opts ...ClientOption) *HTTPClient {
	c := &HTTPClient{
		baseURL: baseURL,
	}

	// Default client with timeout, will be configured by opts/transport loop
	// We need to establish the client struct first so verify function can access it.

	// Create default timeout
	timeout := 30 * time.Second

	// We apply options to 'c' struct.
	// Note: WithTimeout modifies c.httpClient which is nil here.
	// So we need to init httpClient first.
	c.httpClient = &http.Client{
		Timeout: timeout,
	}

	for _, opt := range opts {
		opt(c)
	}

	// Transport configuration
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // We manually verify via VerifyPeerCertificate
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				if len(rawCerts) == 0 {
					return nil
				}
				// Calculate SHA-256 fingerprint of the leaf certificate
				hash := sha256.Sum256(rawCerts[0])
				fingerprint := hex.EncodeToString(hash[:])

				// Capture fingerprint for display (TOFU)
				c.SeenFingerprint = fingerprint

				// Enforce pinning if configured
				if c.expectedFingerprint != "" && c.expectedFingerprint != fingerprint {
					return fmt.Errorf("certificate fingerprint mismatch! Expected %s, got %s", c.expectedFingerprint, fingerprint)
				}
				return nil
			},
		},
	}
	c.httpClient.Transport = transport

	return c
}

// doRequest performs an HTTP request and decodes the JSON response.
func (c *HTTPClient) doRequest(method, path string, body interface{}, result interface{}) error {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Mitigation: OWASP A07:2021-Identification and Authentication Failures
	// Always send API key if configured.
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read body for error messages
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// GetStatus retrieves the server status.
func (c *HTTPClient) GetStatus() (*ServerInfo, error) {
	var info ServerInfo
	if err := c.doRequest(http.MethodGet, "/api/status", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetConfig retrieves the current configuration.
func (c *HTTPClient) GetConfig() (*config.Config, error) {
	var cfg config.Config
	if err := c.doRequest(http.MethodGet, "/api/config", nil, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UploadBinaryResult is the response from a successful binary upload.
type UploadBinaryResult struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	Checksum string `json:"checksum"`
	Bytes    int64  `json:"bytes"`
}

// UploadBinary uploads a binary file to the remote server for upgrade.
// The arch parameter should be in the format "linux/arm64" (runtime.GOOS + "/" + runtime.GOARCH).
func (c *HTTPClient) UploadBinary(binaryPath, arch string) (*UploadBinaryResult, error) {
	// Open and read the binary file
	file, err := os.Open(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open binary: %w", err)
	}
	defer file.Close()

	// Get file info for size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat binary: %w", err)
	}

	// Calculate checksum first (need to read entire file)
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, fmt.Errorf("failed to calculate checksum: %w", err)
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))

	// Seek back to beginning for upload
	if _, err := file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to seek: %w", err)
	}

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add binary file
	part, err := writer.CreateFormFile("binary", filepath.Base(binaryPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy binary: %w", err)
	}

	// Add checksum field
	if err := writer.WriteField("checksum", checksum); err != nil {
		return nil, fmt.Errorf("failed to write checksum field: %w", err)
	}

	// Add arch field
	if err := writer.WriteField("arch", arch); err != nil {
		return nil, fmt.Errorf("failed to write arch field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	// Create request
	url := c.baseURL + "/api/system/upgrade"
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	// Send request
	fmt.Printf("Uploading binary: %s (%d bytes, checksum: %s...)\n",
		filepath.Base(binaryPath), stat.Size(), checksum[:16])

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result UploadBinaryResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// --- Logging ---

// LogEntry mirrors the API LogEntry.
type LogEntry struct {
	Timestamp string            `json:"timestamp"`
	Source    string            `json:"source"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Facility  string            `json:"facility,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// GetLogsArgs mirrors the API argument structure.
type GetLogsArgs struct {
	Source string `json:"source,omitempty"`
	Level  string `json:"level,omitempty"`
	Search string `json:"search,omitempty"`
	Since  string `json:"since,omitempty"`
	Until  string `json:"until,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// GetLogs retrieves logs from the remote server.
func (c *HTTPClient) GetLogs(args *GetLogsArgs) ([]LogEntry, error) {
	path := "/api/logs"
	// Build query string manually or via URL builder
	// Simple append for now
	path += fmt.Sprintf("?limit=%d", args.Limit)
	if args.Source != "" {
		path += "&source=" + args.Source
	}
	if args.Level != "" {
		path += "&level=" + args.Level
	}
	if args.Search != "" {
		path += "&search=" + args.Search
	}
	// args.Most others are supported by API Logs handler

	var entries []LogEntry
	if err := c.doRequest(http.MethodGet, path, nil, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// TailLogs connects to the WebSocket endpoint and streams logs.
// This blocks until the connection is closed or context error.
// The handler is called for each new log entry batch.
func (c *HTTPClient) TailLogs(onLogs func([]LogEntry)) error {
	// WebSocket URL
	wsURL := strings.Replace(c.baseURL, "http", "ws", 1) + "/api/ws/status"

	// Headers for Auth
	headers := http.Header{}
	if c.apiKey != "" {
		headers.Set("X-API-Key", c.apiKey)
	}

	// Dial
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}
	// Use TLS config from HTTP client (includes fingerprint verification)
	if transport, ok := c.httpClient.Transport.(*http.Transport); ok {
		dialer.TLSClientConfig = transport.TLSClientConfig
	}

	conn, _, err := dialer.Dial(wsURL, headers)
	if err != nil {
		return fmt.Errorf("failed to dial websocket: %w", err)
	}
	defer conn.Close()

	// Subscribe to logs
	subMsg := map[string]interface{}{
		"action": "subscribe",
		"topics": []string{"logs"},
	}
	if err := conn.WriteJSON(subMsg); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// Read Loop
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		// Parse envelope
		var msg struct {
			Topic string          `json:"topic"`
			Data  json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue // Skip malformed
		}

		if msg.Topic == "logs" {
			var entries []LogEntry
			if err := json.Unmarshal(msg.Data, &entries); err != nil {
				continue
			}
			if len(entries) > 0 {
				onLogs(entries)
			}
		}
	}
}
