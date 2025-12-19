package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"grimm.is/glacic/internal/ctlplane"
)

// FlowHandlers provides HTTP handlers for the flow triage API
type FlowHandlers struct {
	client ctlplane.ControlPlaneClient
}

// NewFlowHandlers creates a new FlowHandlers
func NewFlowHandlers(client ctlplane.ControlPlaneClient) *FlowHandlers {
	return &FlowHandlers{
		client: client,
	}
}

// HandleGetFlows returns learned flows with optional filtering
func (h *FlowHandlers) HandleGetFlows(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			limit = v
		}
	}

	offset := 0
	if offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil {
			offset = v
		}
	}

	flows, counts, err := h.client.GetFlows(state, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"flows":  flows,
		"counts": counts,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleApprove approves a flow
func (h *FlowHandlers) HandleApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	if err := h.client.ApproveFlow(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// HandleDeny denies a flow
func (h *FlowHandlers) HandleDeny(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	if err := h.client.DenyFlow(req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// HandleDelete deletes a flow
func (h *FlowHandlers) HandleDelete(w http.ResponseWriter, r *http.Request) {
	// Support both query param ID and body ID?
	// Commonly DELETE uses query param or URL path.
	// But let's check body first to be consistent with POSTs, or use query param "id".

	idStr := r.URL.Query().Get("id")
	var id int64
	var err error

	if idStr != "" {
		id, err = strconv.ParseInt(idStr, 10, 64)
	} else {
		// Try body
		var req struct {
			ID int64 `json:"id"`
		}
		if json.NewDecoder(r.Body).Decode(&req) == nil {
			id = req.ID
		}
	}

	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	if id == 0 {
		http.Error(w, "ID required", http.StatusBadRequest)
		return
	}

	if err := h.client.DeleteFlow(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
