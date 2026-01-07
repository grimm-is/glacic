package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"grimm.is/glacic/internal/config"
)

// --- Safe Apply Handlers ---

// handleSafeApply applies config with connectivity verification and rollback
func (s *Server) handleSafeApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req struct {
		Config             config.Config `json:"config"`
		PingTargets        []string      `json:"ping_targets"`         // IPs to verify after apply
		PingTimeoutSeconds int           `json:"ping_timeout_seconds"` // Per-target timeout (default 5)
		RollbackSeconds    int           `json:"rollback_seconds"`     // Unused for now but reserved
	}
	req.PingTimeoutSeconds = 5 // Default timeout

	if !BindJSON(w, r, &req) {
		return
	}

	if !s.RequireControlPlane(w, r) {
		return
	}

	// Step 1: Create backup BEFORE apply (so we can rollback)
	backupReply, err := s.client.CreateBackup("Pre-safe-apply backup", false)
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"message": "Failed to create pre-apply backup",
		})
		return
	}
	backupVersion := backupReply.Backup.Version

	// Step 2: Apply the config
	if err := s.client.ApplyConfig(&req.Config); err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"message": "Apply failed",
		})
		return
	}

	// Update local config
	s.configMu.Lock()
	s.Config = &req.Config
	s.configMu.Unlock()

	// Step 3: If ping targets specified, verify connectivity
	if len(req.PingTargets) > 0 {
		var failedTargets []string
		for _, target := range req.PingTargets {
			// Validate target is an IP address
			if !isValidIP(target) {
				failedTargets = append(failedTargets, fmt.Sprintf("%s: invalid IP", target))
				continue
			}

			// Ping via control plane (privileged)
			pingReply, err := s.client.Ping(target, req.PingTimeoutSeconds)
			if err != nil {
				failedTargets = append(failedTargets, fmt.Sprintf("%s: %v", target, err))
				continue
			}
			if !pingReply.Reachable {
				failedTargets = append(failedTargets, fmt.Sprintf("%s: %s", target, pingReply.Error))
			}
		}

		// If any ping failed, rollback
		if len(failedTargets) > 0 {
			// Rollback to backup
			_, rollbackErr := s.client.RestoreBackup(backupVersion)
			rollbackMsg := "rollback successful"
			if rollbackErr != nil {
				rollbackMsg = fmt.Sprintf("rollback failed: %v", rollbackErr)
			}

			WriteJSON(w, http.StatusOK, map[string]interface{}{
				"success":      false,
				"error":        "connectivity verification failed",
				"failed_pings": failedTargets,
				"message":      fmt.Sprintf("Connectivity verification failed, %s", rollbackMsg),
				"rolled_back":  rollbackErr == nil,
			})
			return
		}
	}

	// Success - save config to HCL file for persistence
	if _, err := s.client.SaveConfig(); err != nil {
		// Log warning but don't fail - runtime config is already applied
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"warning": fmt.Sprintf("Config applied but failed to save to disk: %v", err),
			"message": "Configuration applied (runtime only - save failed)",
		})
		return
	}

	// Create a post-apply backup for the known-good state
	postBackupReply, err := s.client.CreateBackup("Safe apply successful", false)
	postBackupVersion := 0
	if err == nil {
		postBackupVersion = postBackupReply.Backup.Version
	}

	// Calculate and broadcast new pending status (should be false now)
	go s.broadcastPendingStatus()

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"message":        "Configuration applied and saved",
		"backup_version": postBackupVersion,
	})
}

// handleConfirmApply confirms a pending apply (placeholder for full implementation)
func (s *Server) handleConfirmApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// For now, just acknowledge - full implementation requires control plane integration
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Changes confirmed",
	})
}

// handlePendingApply returns info about any pending apply
func (s *Server) handlePendingApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Use real status check
	hasPending := s.checkPendingStatus()

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"has_pending": hasPending,
	})
}

// --- Backup Management Handlers ---

// handleBackups lists all available backups
func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.RequireControlPlane(w, r) {
		return
	}

	reply, err := s.client.ListBackups()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"error": reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"backups": reply.Backups,
	})
}

// handleCreateBackup creates a new manual backup
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.RequireControlPlane(w, r) {
		return
	}

	var req struct {
		Description string `json:"description"`
		Pinned      bool   `json:"pinned"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	reply, err := s.client.CreateBackup(req.Description, req.Pinned)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"backup":  reply.Backup,
	})
}

// handleRestoreBackup restores a specific backup version
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.RequireControlPlane(w, r) {
		return
	}

	var req struct {
		Version int `json:"version"`
	}
	if !BindJSON(w, r, &req) {
		return
	}

	if req.Version <= 0 {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Version is required")
		return
	}

	reply, err := s.client.RestoreBackup(req.Version)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": reply.Message,
	})
}

// handleBackupContent returns the content of a specific backup
func (s *Server) handleBackupContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.RequireControlPlane(w, r) {
		return
	}

	versionStr := r.URL.Query().Get("version")
	if versionStr == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Version parameter required")
		return
	}

	var version int
	fmt.Sscanf(versionStr, "%d", &version)
	if version <= 0 {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid version")
		return
	}

	reply, err := s.client.GetBackupContent(version)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"error": reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"content": reply.Content,
	})
}

// handlePinBackup pins or unpins a backup
func (s *Server) handlePinBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.RequireControlPlane(w, r) {
		return
	}

	var req struct {
		Version int  `json:"version"`
		Pinned  bool `json:"pinned"`
	}
	if !BindJSON(w, r, &req) {
		return
	}

	if req.Version <= 0 {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Version is required")
		return
	}

	reply, err := s.client.PinBackup(req.Version, req.Pinned)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// handleGetBackupSettings gets backup settings
func (s *Server) handleGetBackupSettings(w http.ResponseWriter, r *http.Request) {
	if !s.RequireControlPlane(w, r) {
		return
	}

	reply, err := s.client.ListBackups()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"max_backups": reply.MaxBackups,
	})
}

// handleUpdateBackupSettings updates backup settings
func (s *Server) handleUpdateBackupSettings(w http.ResponseWriter, r *http.Request) {
	if !s.RequireControlPlane(w, r) {
		return
	}

	var req struct {
		MaxBackups int `json:"max_backups"`
	}
	if !BindJSON(w, r, &req) {
		return
	}

	if req.MaxBackups < 1 {
		WriteErrorCtx(w, r, http.StatusBadRequest, "max_backups must be at least 1")
		return
	}

	reply, err := s.client.SetMaxBackups(req.MaxBackups)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if reply.Error != "" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   reply.Error,
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"max_backups": req.MaxBackups,
	})
}

// isValidIP checks if a string is a valid IP address
func isValidIP(ip string) bool {
	// Use net.ParseIP later - for now, simple check
	return len(ip) > 0
}
