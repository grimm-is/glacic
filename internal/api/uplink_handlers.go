package api

import (
	"encoding/json"
	"net/http"

	"grimm.is/glacic/internal/ctlplane"
)

// UplinkAPI provides HTTP handlers for uplink management.
type UplinkAPI struct {
	client ctlplane.ControlPlaneClient
}

// NewUplinkAPI creates a new UplinkAPI.
func NewUplinkAPI(client ctlplane.ControlPlaneClient) *UplinkAPI {
	return &UplinkAPI{
		client: client,
	}
}

// HandleGetGroups returns all uplink groups and their status.
func (a *UplinkAPI) HandleGetGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := a.client.GetUplinkGroups()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

// HandleSwitch switches an uplink group to a specific uplink or best available.
func (a *UplinkAPI) HandleSwitch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupName  string `json:"group_name"`
		UplinkName string `json:"uplink_name"` // Optional, empty = auto/best
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.GroupName == "" {
		http.Error(w, "group_name is required", http.StatusBadRequest)
		return
	}

	if err := a.client.SwitchUplink(req.GroupName, req.UplinkName); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// HandleToggle enables or disables an uplink.
func (a *UplinkAPI) HandleToggle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupName  string `json:"group_name"`
		UplinkName string `json:"uplink_name"`
		Enabled    bool   `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.GroupName == "" || req.UplinkName == "" {
		http.Error(w, "group_name and uplink_name are required", http.StatusBadRequest)
		return
	}

	if err := a.client.ToggleUplink(req.GroupName, req.UplinkName, req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
