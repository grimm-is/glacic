package api

import (
	"encoding/json"
	"net/http"

	"grimm.is/glacic/internal/api/storage"
	"grimm.is/glacic/internal/services/scanner"
)

// ScannerClient defines the interface for scanner operations
type ScannerClient interface {
	StartScanNetwork(cidr string, timeoutSeconds int) error
	GetScanStatus() (bool, *scanner.ScanResult, error)
	GetScanResult() (*scanner.ScanResult, error)
	ScanHost(ip string) (*scanner.HostResult, error)
	GetCommonPorts() ([]scanner.Port, error)
}

// ScannerHandlers handles network scanner API requests
type ScannerHandlers struct {
	client ScannerClient
	server *Server
}

// NewScannerHandlers creates a new scanner handlers instance
func NewScannerHandlers(s *Server, c ScannerClient) *ScannerHandlers {
	return &ScannerHandlers{
		client: c,
		server: s,
	}
}

// RegisterRoutes registers scanner API routes
func (h *ScannerHandlers) RegisterRoutes(mux *http.ServeMux) {
	// Scan operations require write permission
	mux.Handle("/api/scanner/network", h.server.require(storage.PermWriteConfig, http.HandlerFunc(h.ScanNetwork)))
	mux.Handle("/api/scanner/host", h.server.require(storage.PermWriteConfig, http.HandlerFunc(h.ScanHost)))
	mux.Handle("/api/scanner/status", h.server.require(storage.PermReadConfig, http.HandlerFunc(h.Status)))
	mux.Handle("/api/scanner/result", h.server.require(storage.PermReadConfig, http.HandlerFunc(h.LastResult)))
	mux.Handle("/api/scanner/ports", h.server.require(storage.PermReadConfig, http.HandlerFunc(h.CommonPorts)))
}

// ScanNetwork starts a network scan
func (h *ScannerHandlers) ScanNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		CIDR    string `json:"cidr"`
		Timeout int    `json:"timeout_seconds,omitempty"` // Optional override
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.CIDR == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "CIDR is required")
		return
	}

	if err := h.client.StartScanNetwork(req.CIDR, req.Timeout); err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":  "started",
		"network": req.CIDR,
		"message": "Network scan started. Check /api/scanner/status for progress.",
	})
}

// ScanHost scans a single host (synchronous)
func (h *ScannerHandlers) ScanHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		IP string `json:"ip"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.IP == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "IP address is required")
		return
	}

	result, err := h.client.ScanHost(req.IP)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// Status returns the current scanner status
func (h *ScannerHandlers) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	scanning, lastResult, err := h.client.GetScanStatus()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	response := map[string]interface{}{
		"scanning": scanning,
	}

	if lastResult != nil {
		// Provide summary for status endpoint as expected by UI
		response["last_scan"] = map[string]interface{}{
			"network":             lastResult.Network,
			"total_hosts":         lastResult.TotalHosts,
			"hosts_with_services": len(lastResult.Hosts),
			"duration_ms":         lastResult.Duration.Milliseconds(),
			"started_at":          lastResult.StartedAt,
		}
	}

	WriteJSON(w, http.StatusOK, response)
}

// LastResult returns the full last scan result
func (h *ScannerHandlers) LastResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	result, err := h.client.GetScanResult()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if result == nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"message": "No scan results available",
		})
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// CommonPorts returns the list of ports that are scanned
func (h *ScannerHandlers) CommonPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ports, err := h.client.GetCommonPorts()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, ports)
}
