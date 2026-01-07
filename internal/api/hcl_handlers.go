package api

import (
	"net/http"

	"grimm.is/glacic/internal/config"
)

// --- HCL Editing Handlers (Advanced Mode) ---

// handleSetIPForwarding toggles the global ip_forwarding setting
func (s *Server) handleSetIPForwarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.RequireControlPlane(w, r) {
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !BindJSON(w, r, &req) {
		return
	}

	// 1. Get current Config from Control Plane to ensure we are up to date
	cfg, err := s.client.GetConfig()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to get config: "+err.Error())
		return
	}

	// 2. Update IP Forwarding setting
	cfg.IPForwarding = req.Enabled

	// 3. Apply the updated config
	if err := s.client.ApplyConfig(cfg); err != nil {
		s.logger.Error("Failed to apply panic button change", "error", err)
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "Failed to apply setting: " + err.Error(),
		})
		return
	}

	// 4. Save to disk (persist the change)
	saveResp, err := s.client.SaveConfig()
	if err != nil {
		// Log error but success=true because runtime is updated
		s.logger.Error("Failed to save panic button change to disk", "error", err)
	} else if saveResp.Error != "" {
		s.logger.Error("Failed to save config", "error", saveResp.Error)
	}

	// Update local config cache
	s.configMu.Lock()
	s.Config = cfg
	s.configMu.Unlock()

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"applied": true,
		"saved":   err == nil,
	})
}

// SystemSettingsRequest represents the payload for updating system settings
type SystemSettingsRequest struct {
	IPForwarding      *bool `json:"ip_forwarding,omitempty"`
	MSSClamping       *bool `json:"mss_clamping,omitempty"`
	EnableFlowOffload *bool `json:"enable_flow_offload,omitempty"`

	// Feature Flags
	QoS             *bool `json:"qos,omitempty"`
	ThreatIntel     *bool `json:"threat_intel,omitempty"`
	NetworkLearning *bool `json:"network_learning,omitempty"`
}

// handleSystemSettings handles global system settings updates
func (s *Server) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	if !s.RequireControlPlane(w, r) {
		return
	}

	var req SystemSettingsRequest
	if !BindJSON(w, r, &req) {
		return
	}

	// 1. Get current Config
	cfg, err := s.client.GetConfig()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, "Failed to get config: "+err.Error())
		return
	}

	// 2. Update Settings
	changes := false
	if req.IPForwarding != nil {
		cfg.IPForwarding = *req.IPForwarding
		changes = true
	}
	if req.MSSClamping != nil {
		cfg.MSSClamping = *req.MSSClamping
		changes = true
	}
	if req.EnableFlowOffload != nil {
		cfg.EnableFlowOffload = *req.EnableFlowOffload
		changes = true
	}

	// Update Features
	if req.QoS != nil || req.ThreatIntel != nil || req.NetworkLearning != nil {
		if cfg.Features == nil {
			cfg.Features = &config.Features{}
		}
	}
	if req.QoS != nil {
		cfg.Features.QoS = *req.QoS
		changes = true
	}
	if req.ThreatIntel != nil {
		cfg.Features.ThreatIntel = *req.ThreatIntel
		changes = true
	}
	if req.NetworkLearning != nil {
		cfg.Features.NetworkLearning = *req.NetworkLearning
		changes = true
	}

	if cfg.Features != nil {
		s.logger.Info("DEBUG: handleSystemSettings Features", "qos", cfg.Features.QoS, "threat", cfg.Features.ThreatIntel)
	} else {
		s.logger.Info("DEBUG: handleSystemSettings Features is nil")
	}

	if !changes {
		WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}

	// 3. Apply Config
	if err := s.client.ApplyConfig(cfg); err != nil {
		s.logger.Error("Failed to apply settings change", "error", err)
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "Failed to apply settings: " + err.Error(),
		})
		return
	}

	// 4. Save to disk
	saveResp, err := s.client.SaveConfig()
	if err != nil {
		s.logger.Error("Failed to save settings to disk", "error", err)
	} else if saveResp.Error != "" {
		s.logger.Error("Failed to save config", "error", saveResp.Error)
	}

	// Update local config cache
	s.configMu.Lock()
	s.Config = cfg
	s.configMu.Unlock()

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"applied": true,
		"saved":   err == nil,
	})
}

// handleGetRawHCL handles GET for the entire config as raw HCL
func (s *Server) handleGetRawHCL(w http.ResponseWriter, r *http.Request) {
	if !s.RequireControlPlane(w, r) {
		return
	}

	reply, err := s.client.GetRawHCL()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, reply)
}

// handleUpdateRawHCL handles PUT/POST for the entire config as raw HCL
func (s *Server) handleUpdateRawHCL(w http.ResponseWriter, r *http.Request) {
	if !s.RequireControlPlane(w, r) {
		return
	}

	var req struct {
		HCL string `json:"hcl"`
	}
	if !BindJSON(w, r, &req) {
		return
	}

	reply, err := s.client.SetRawHCL(req.HCL)
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
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleGetSectionHCL handles GET for a specific config section as raw HCL
func (s *Server) handleGetSectionHCL(w http.ResponseWriter, r *http.Request) {
	if !s.RequireControlPlane(w, r) {
		return
	}

	sectionType := r.URL.Query().Get("type")
	labels := r.URL.Query()["label"]

	if sectionType == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Section type required (use ?type=dhcp)")
		return
	}

	reply, err := s.client.GetSectionHCL(sectionType, labels...)
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
	WriteJSON(w, http.StatusOK, map[string]string{"hcl": reply.HCL})
}

// handleUpdateSectionHCL handles PUT/POST for a specific config section as raw HCL
func (s *Server) handleUpdateSectionHCL(w http.ResponseWriter, r *http.Request) {
	if !s.RequireControlPlane(w, r) {
		return
	}

	sectionType := r.URL.Query().Get("type")
	labels := r.URL.Query()["label"]

	if sectionType == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Section type required (use ?type=dhcp)")
		return
	}

	var req struct {
		HCL string `json:"hcl"`
	}
	if !BindJSON(w, r, &req) {
		return
	}

	reply, err := s.client.SetSectionHCL(sectionType, req.HCL, labels...)
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
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleValidateHCL validates HCL without applying it
func (s *Server) handleValidateHCL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.RequireControlPlane(w, r) {
		return
	}

	var req struct {
		HCL string `json:"hcl"`
	}
	if !BindJSON(w, r, &req) {
		return
	}

	reply, err := s.client.ValidateHCL(req.HCL)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleSaveConfig saves the current config to disk
func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !s.RequireControlPlane(w, r) {
		return
	}

	reply, err := s.client.SaveConfig()
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
		"backup_path": reply.BackupPath,
	})
}
