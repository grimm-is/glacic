package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"grimm.is/glacic/internal/audit"
)

// auditStore is the global audit store for the API server.
// Set via SetAuditStore.
var apiAuditStore *audit.Store

// SetAPIAuditStore sets the audit store for API handlers.
func SetAPIAuditStore(store *audit.Store) {
	apiAuditStore = store
}

// handleAuditQuery handles GET /api/audit requests.
// Query params: start, end (RFC3339), action, user, limit
func (s *Server) handleAuditQuery(w http.ResponseWriter, r *http.Request) {
	if apiAuditStore == nil {
		http.Error(w, "Audit logging not enabled", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	q := r.URL.Query()

	// Default time range: last 24 hours
	end := time.Now()
	start := end.Add(-24 * time.Hour)

	if startStr := q.Get("start"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}
	if endStr := q.Get("end"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}

	action := q.Get("action")
	user := q.Get("user")

	limit := 100 // Default limit
	if limitStr := q.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if limit > 1000 {
		limit = 1000 // Max limit
	}

	// Query events
	events, err := apiAuditStore.Query(start, end, action, user, limit)
	if err != nil {
		http.Error(w, "Failed to query audit log: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get total count for pagination info
	count, _ := apiAuditStore.Count()

	response := struct {
		Events []audit.Event `json:"events"`
		Total  int64         `json:"total"`
		Limit  int           `json:"limit"`
	}{
		Events: events,
		Total:  count,
		Limit:  limit,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
