package api

import (
	"encoding/json"
	"net/http"

	"grimm.is/glacic/internal/config"
)

// ==============================================================================
// Common Error Messages - Deduplication of repeated strings
// ==============================================================================

const (
	ErrInvalidBody      = "Invalid request body"
	ErrControlPlaneDown = "Control plane not connected"
	ErrNotFound         = "Not found"
	ErrVersionRequired  = "Version is required"
	ErrUnauthorized     = "Unauthorized"
	ErrForbidden        = "Forbidden"
)

// ==============================================================================
// Generic JSON Binding Helper
// Reduces: var req T; if err := json.NewDecoder(r.Body).Decode(&req); err != nil { ... }
// ==============================================================================

// BindJSON decodes JSON from request body into the provided pointer.
// Returns true on success, false if decoding failed (error response already sent).
// BindJSON decodes JSON from request body into the provided pointer.
// Returns true on success, false if decoding failed (error response already sent).
func BindJSON[T any](w http.ResponseWriter, r *http.Request, dest *T) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, ErrInvalidBody)
		// logging.Error("JSON Decode failed: " + err.Error())
		return false
	}
	return true
}

// BindJSONCustomErr decodes JSON with a custom error message.
func BindJSONCustomErr[T any](w http.ResponseWriter, r *http.Request, dest *T, errMsg string) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, errMsg)
		return false
	}
	return true
}

// BindJSONLenient decodes JSON but allows unknown fields (like _status from UI).
// Use this for endpoints where UI may send extra metadata fields.
func BindJSONLenient[T any](w http.ResponseWriter, r *http.Request, dest *T) bool {
	// Don't call DisallowUnknownFields - UI sends _status which should be ignored
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, ErrInvalidBody)
		return false
	}
	return true
}

// ==============================================================================
// Generic Handler Patterns
// Reduces: cfg := s.getConfigOrWrite(w); if cfg == nil { return }; WriteJSON(w, 200, cfg.Field)
// ==============================================================================

// HandleGet wraps a simple GET handler that returns data.
// dataFn is called to get the data to return.
// If dataFn returns an error, it's written as 500 response.
func HandleGet(w http.ResponseWriter, r *http.Request, dataFn func() (interface{}, error)) {
	data, err := dataFn()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, data)
}

// HandleGetData wraps a GET handler returning typed data without error.
func HandleGetData(w http.ResponseWriter, data interface{}) {
	WriteJSON(w, http.StatusOK, data)
}

// HandleUpdate wraps a POST/PUT handler that updates config.
// updateFn is called to perform the update.
// Returns success response if updateFn returns nil.
func HandleUpdate(w http.ResponseWriter, r *http.Request, updateFn func() error) {
	if err := updateFn(); err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// RequireControlPlane returns true if control plane client is connected.
// Writes 503 error and returns false if not connected.
func (s *Server) RequireControlPlane(w http.ResponseWriter, r *http.Request) bool {
	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, ErrControlPlaneDown)
		return false
	}
	return true
}

// GetConfigSnapshot gets config from control plane or returns a local snapshot.
// Returns nil if error occurred (response already sent).
// GetConfigSnapshot gets config from control plane or returns a local snapshot.
// Supports ?source=running (Control Plane) or ?source=staged (Local Memory, Default)
func (s *Server) GetConfigSnapshot(w http.ResponseWriter, r *http.Request) *config.Config {
	source := r.URL.Query().Get("source")
	// If source is "running", strictly require control plane fetch.
	// We ignore "staged" explicitly here as that falls through to default.
	if source == "running" && s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
			return nil
		}
		return cfg
	}
	// Default: Return local Staged config
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.Config.Clone()
}

// ==============================================================================
// Success/Error Response Helpers
// ==============================================================================

// SuccessResponse writes a standard success JSON response.
func SuccessResponse(w http.ResponseWriter) {
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// SuccessWithData writes success with additional data fields.
func SuccessWithData(w http.ResponseWriter, data map[string]interface{}) {
	data["success"] = true
	WriteJSON(w, http.StatusOK, data)
}

// SendErrorJSON writes a standard error JSON response.
func SendErrorJSON(w http.ResponseWriter, err error) {
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": false,
		"error":   err.Error(),
	})
}

// ErrorMessage writes an error with a message string.
func ErrorMessage(w http.ResponseWriter, msg string) {
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": false,
		"error":   msg,
	})
}
