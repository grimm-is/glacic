package api

import (
	"encoding/json"
	"net/http"

	"grimm.is/glacic/internal/ctlplane"
)

// handleUpdateDeviceIdentity updates a device identity
// POST /api/devices/identity
func (s *Server) handleUpdateDeviceIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		ID    string   `json:"id"`
		Alias *string  `json:"alias"`
		Owner *string  `json:"owner"`
		Type  *string  `json:"type"`
		Tags  []string `json:"tags"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Device ID is required")
		return
	}

	args := &ctlplane.UpdateDeviceIdentityArgs{
		ID:    req.ID,
		Alias: req.Alias,
		Owner: req.Owner,
		Type:  req.Type,
		Tags:  req.Tags,
	}

	identity, err := s.client.UpdateDeviceIdentity(args)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, identity)
}

// handleLinkMAC links a MAC address to a device identity
// POST /api/devices/link
func (s *Server) handleLinkMAC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		MAC        string `json:"mac"`
		IdentityID string `json:"identity_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.MAC == "" || req.IdentityID == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "MAC and IdentityID are required")
		return
	}

	if err := s.client.LinkMAC(req.MAC, req.IdentityID); err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleUnlinkMAC unlinks a MAC address from a device identity
// POST /api/devices/unlink
func (s *Server) handleUnlinkMAC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		MAC string `json:"mac"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.MAC == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "MAC is required")
		return
	}

	if err := s.client.UnlinkMAC(req.MAC); err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetDevices returns a list of all known device identities
// GET /api/devices
func (s *Server) handleGetDevices(w http.ResponseWriter, r *http.Request) {
	// Stub implementation for now
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("[]"))
}
