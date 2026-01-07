package vpn

import (
	"encoding/json"
	"net/http"
)

// TailscaleAPIHandler provides HTTP handlers for Tailscale management.
type TailscaleAPIHandler struct {
	manager *TailscaleManager
}

// NewTailscaleAPIHandler creates a new Tailscale API handler.
func NewTailscaleAPIHandler(manager *TailscaleManager) *TailscaleAPIHandler {
	return &TailscaleAPIHandler{manager: manager}
}

// RegisterRoutes registers the Tailscale API routes.
func (h *TailscaleAPIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/tailscale/status", h.handleStatus)
	mux.HandleFunc("/api/tailscale/peers", h.handlePeers)
	mux.HandleFunc("/api/tailscale/config", h.handleConfig)
	mux.HandleFunc("/api/tailscale/up", h.handleUp)
	mux.HandleFunc("/api/tailscale/down", h.handleDown)
	mux.HandleFunc("/api/tailscale/routes", h.handleRoutes)
	mux.HandleFunc("/api/tailscale/exit-node", h.handleExitNode)
}

// handleStatus returns the current Tailscale status.
func (h *TailscaleAPIHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := h.manager.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handlePeers returns the list of connected peers.
func (h *TailscaleAPIHandler) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := h.manager.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"peers": status.Peers,
		"count": len(status.Peers),
	})
}

// handleConfig returns the current Tailscale configuration.
func (h *TailscaleAPIHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	config := h.manager.GetConfig()
	// Don't expose auth key
	config.AuthKey = ""

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// handleUp brings Tailscale up.
func (h *TailscaleAPIHandler) handleUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.manager.Up(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDown brings Tailscale down.
func (h *TailscaleAPIHandler) handleDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.manager.Down(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// routesRequest is the request body for updating routes.
type routesRequest struct {
	Routes []string `json:"routes"`
}

// handleRoutes updates the advertised routes.
func (h *TailscaleAPIHandler) handleRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		config := h.manager.GetConfig()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"advertised_routes": config.AdvertiseRoutes,
			"accept_routes":     config.AcceptRoutes,
		})

	case http.MethodPost:
		var req routesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := h.manager.AdvertiseRoutes(req.Routes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// exitNodeRequest is the request body for exit node settings.
type exitNodeRequest struct {
	Enable bool `json:"enable"`
}

// handleExitNode enables or disables exit node mode.
func (h *TailscaleAPIHandler) handleExitNode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status := h.manager.GetStatus()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"exit_node":        status.ExitNode,
			"exit_node_active": status.ExitNodeActive,
		})

	case http.MethodPost:
		var req exitNodeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if err := h.manager.SetExitNode(req.Enable); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
