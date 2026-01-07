package vpn

import (
	"encoding/json"
	"net/http"
	"strings"
)

// WireGuardAPIManager defines the interface needed by the API handler.
// This allows mocking the WireGuardManager for testing.
type WireGuardAPIManager interface {
	GetConfig() WireGuardConfig
	GetStatus() *WireGuardStatus
	Up() error
	Down() error
	IsEnabled() bool
	IsConnected() bool
	Interface() string
	AddPeer(peer WireGuardPeer) error
	RemovePeer(publicKey string) error
}

// WireGuardAPIHandler provides HTTP handlers for WireGuard management.
type WireGuardAPIHandler struct {
	manager WireGuardAPIManager

	// For dynamic peer management (called when peers change)
	onPeerChange func() error
}

// NewWireGuardAPIHandler creates a new WireGuard API handler.
func NewWireGuardAPIHandler(manager WireGuardAPIManager) *WireGuardAPIHandler {
	return &WireGuardAPIHandler{manager: manager}
}

// SetOnPeerChange sets a callback for peer changes (to trigger config reload).
func (h *WireGuardAPIHandler) SetOnPeerChange(fn func() error) {
	h.onPeerChange = fn
}

// RegisterRoutes registers the WireGuard API routes.
func (h *WireGuardAPIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/wireguard/status", h.handleStatus)
	mux.HandleFunc("/api/wireguard/config", h.handleConfig)
	mux.HandleFunc("/api/wireguard/up", h.handleUp)
	mux.HandleFunc("/api/wireguard/down", h.handleDown)
	mux.HandleFunc("/api/wireguard/generate-key", h.handleGenerateKey)
	mux.HandleFunc("/api/wireguard/peers", h.handlePeersRoute)
	// Peer by public key handled in handlePeersRoute
}

// handleStatus returns the current WireGuard status.
func (h *WireGuardAPIHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := h.manager.GetStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleConfig returns the current WireGuard configuration.
func (h *WireGuardAPIHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	config := h.manager.GetConfig()
	// Don't expose private key
	config.PrivateKey = ""

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// handleUp brings WireGuard interface up.
func (h *WireGuardAPIHandler) handleUp(w http.ResponseWriter, r *http.Request) {
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

// handleDown brings WireGuard interface down.
func (h *WireGuardAPIHandler) handleDown(w http.ResponseWriter, r *http.Request) {
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

// handleGenerateKey generates a new WireGuard key pair.
func (h *WireGuardAPIHandler) handleGenerateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	privKey, pubKey, err := GenerateKeyPair()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"private_key": privKey,
		"public_key":  pubKey,
	})
}

// handlePeersRoute routes peer requests based on method and path.
func (h *WireGuardAPIHandler) handlePeersRoute(w http.ResponseWriter, r *http.Request) {
	// Check for /api/wireguard/peers/{publicKey} pattern
	path := strings.TrimPrefix(r.URL.Path, "/api/wireguard/peers")

	if path == "" || path == "/" {
		// Collection endpoint
		switch r.Method {
		case http.MethodGet:
			h.handlePeers(w, r)
		case http.MethodPost:
			h.handleAddPeer(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Individual peer endpoint: /api/wireguard/peers/{publicKey}
	publicKey := strings.TrimPrefix(path, "/")
	if r.Method == http.MethodDelete {
		h.handleRemovePeer(w, r, publicKey)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePeers returns the list of configured peers.
func (h *WireGuardAPIHandler) handlePeers(w http.ResponseWriter, r *http.Request) {
	config := h.manager.GetConfig()
	status := h.manager.GetStatus()

	// Merge config peers with status info
	peers := make([]map[string]interface{}, 0, len(config.Peers))
	for _, p := range config.Peers {
		peer := map[string]interface{}{
			"name":                 p.Name,
			"public_key":           p.PublicKey,
			"endpoint":             p.Endpoint,
			"allowed_ips":          p.AllowedIPs,
			"persistent_keepalive": p.PersistentKeepalive,
		}

		// Add status info if available
		if status != nil {
			for _, s := range status.Peers {
				if s.PublicKey == p.PublicKey {
					peer["latest_handshake"] = s.LatestHandshake
					peer["transfer_rx"] = s.TransferRx
					peer["transfer_tx"] = s.TransferTx
					peer["connected"] = !s.LatestHandshake.IsZero()
					break
				}
			}
		}

		peers = append(peers, peer)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"peers": peers,
		"count": len(peers),
	})
}

// PeerRequest is the request body for adding a peer.
type PeerRequest struct {
	Name                string   `json:"name"`
	PublicKey           string   `json:"public_key"`
	PresharedKey        string   `json:"preshared_key,omitempty"`
	Endpoint            string   `json:"endpoint,omitempty"`
	AllowedIPs          []string `json:"allowed_ips"`
	PersistentKeepalive int      `json:"persistent_keepalive,omitempty"`
}

// handleAddPeer adds a new peer.
// Note: This requires config persistence which is handled externally.
func (h *WireGuardAPIHandler) handleAddPeer(w http.ResponseWriter, r *http.Request) {
	var req PeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.PublicKey == "" {
		http.Error(w, "public_key is required", http.StatusBadRequest)
		return
	}

	if len(req.AllowedIPs) == 0 {
		http.Error(w, "allowed_ips is required", http.StatusBadRequest)
		return
	}

	// Convert request to peer config
	peer := WireGuardPeer{
		Name:                req.Name,
		PublicKey:           req.PublicKey,
		PresharedKey:        req.PresharedKey,
		Endpoint:            req.Endpoint,
		AllowedIPs:          req.AllowedIPs,
		PersistentKeepalive: req.PersistentKeepalive,
	}

	// Add peer to running interface
	if err := h.manager.AddPeer(peer); err != nil {
		http.Error(w, "Failed to add peer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Trigger config persistence via callback
	if h.onPeerChange != nil {
		if err := h.onPeerChange(); err != nil {
			// Peer was added but config save failed - log but don't fail
			// The peer is active but won't persist across restarts
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":     "partial",
				"message":    "Peer added but config save failed: " + err.Error(),
				"public_key": req.PublicKey,
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"message":    "Peer added successfully",
		"public_key": req.PublicKey,
	})
}

// handleRemovePeer removes a peer by public key.
func (h *WireGuardAPIHandler) handleRemovePeer(w http.ResponseWriter, r *http.Request, publicKey string) {
	if publicKey == "" {
		http.Error(w, "public_key is required", http.StatusBadRequest)
		return
	}

	// Remove peer from running interface
	if err := h.manager.RemovePeer(publicKey); err != nil {
		http.Error(w, "Failed to remove peer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Trigger config persistence via callback
	if h.onPeerChange != nil {
		if err := h.onPeerChange(); err != nil {
			// Peer was removed but config save failed
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":     "partial",
				"message":    "Peer removed but config save failed: " + err.Error(),
				"public_key": publicKey,
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"message":    "Peer removed successfully",
		"public_key": publicKey,
	})
}
