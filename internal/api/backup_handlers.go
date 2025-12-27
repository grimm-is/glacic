package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"grimm.is/glacic/internal/config"
)

// --- Backup/Restore and Reboot Handlers ---

// handleReboot handles system reboot request
func (s *Server) handleReboot(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	if err := s.client.Reboot(); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleBackup exports the current configuration as JSON
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	// Get current config
	var cfg *config.Config
	if c := s.GetConfigSnapshot(w, r); c != nil {
		cfg = c
	} else {
		return
	}

	// Set headers for file download
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=firewall-config.json")

	// Encode config as JSON
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
}

// handleRestore imports a configuration from JSON
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	// Parse the uploaded config
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid configuration: "+err.Error())
		return
	}

	// Apply the config
	if s.client != nil {
		if err := s.client.ApplyConfig(&cfg); err != nil {
			WriteError(w, http.StatusInternalServerError, "Failed to apply config: "+err.Error())
			return
		}
	} else {
		// Update in-memory config
		s.configMu.Lock()
		*s.Config = cfg
		s.configMu.Unlock()
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// --- Scheduler Handlers ---

// handleGetSchedulerConfig returns scheduler configuration
func (s *Server) handleGetSchedulerConfig(w http.ResponseWriter, r *http.Request) {
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"scheduler":       cfg.Scheduler,
			"scheduled_rules": cfg.ScheduledRules,
		})
	} else {
		s.configMu.RLock()
		defer s.configMu.RUnlock()
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"scheduler":       s.Config.Scheduler,
			"scheduled_rules": s.Config.ScheduledRules,
		})
	}
}

// handleUpdateSchedulerConfig updates scheduler configuration
func (s *Server) handleUpdateSchedulerConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scheduler      *config.SchedulerConfig `json:"scheduler"`
		ScheduledRules []config.ScheduledRule  `json:"scheduled_rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Scheduler != nil {
		s.configMu.Lock()
		s.Config.Scheduler = req.Scheduler
		s.configMu.Unlock()
	}
	if req.ScheduledRules != nil {
		s.configMu.Lock()
		s.Config.ScheduledRules = req.ScheduledRules
		s.configMu.Unlock()
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// TaskStatusInfo represents a scheduled task status
type TaskStatusInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Schedule    string `json:"schedule"`
	Type        string `json:"type"` // "system" or "rule"
}

// handleSchedulerStatus returns the status of all scheduled tasks
func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	tasks := []TaskStatusInfo{
		{ID: "ipset-update", Name: "IPSet Update", Description: "Refresh IPSets from external sources", Enabled: true, Schedule: "Every 24h", Type: "system"},
		{ID: "dns-blocklist-update", Name: "DNS Blocklist Update", Description: "Refresh DNS blocklists", Enabled: true, Schedule: "Every 24h", Type: "system"},
		{ID: "config-backup", Name: "Configuration Backup", Description: "Automatic config backup", Enabled: false, Schedule: "Daily at 2:00 AM", Type: "system"},
	}

	// Add scheduled rules
	var scheduledRules []config.ScheduledRule
	if s.client != nil {
		cfg, err := s.client.GetConfig()
		if err == nil {
			scheduledRules = cfg.ScheduledRules
		}
	} else {
		s.configMu.RLock()
		scheduledRules = s.Config.ScheduledRules // Copy slice header
		// If deep copy needed, we should clone, but slice iteration is read-only here?
		// Iteration below happens after we release lock if we defer?
		// No, we are creating 'tasks' slice.
		// Safe to hold RLock during iteration.
		s.configMu.RUnlock() // Wait, cannot defer if inside if/else block without function scope.
		// Better to just lock around access.
	}

	for _, rule := range scheduledRules {
		tasks = append(tasks, TaskStatusInfo{
			ID:          "rule-" + rule.Name,
			Name:        rule.Name,
			Description: rule.Description,
			Enabled:     rule.Enabled,
			Schedule:    rule.Schedule,
			Type:        "rule",
		})
	}

	WriteJSON(w, http.StatusOK, tasks)
}

// handleSchedulerRun triggers immediate execution of a scheduled task
func (s *Server) handleSchedulerRun(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task")
	if taskID == "" {
		WriteError(w, http.StatusBadRequest, "Task ID required")
		return
	}

	if s.client == nil {
		WriteError(w, http.StatusServiceUnavailable, "Control plane client not connected")
		return
	}

	// Trigger via RPC
	if err := s.client.TriggerTask(taskID); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to trigger task: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Task " + taskID + " triggered",
	})
}
