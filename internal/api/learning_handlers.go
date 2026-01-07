package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleLearningRules handles GET /api/learning/rules
// Returns list of pending rules, optionally filtered by status
func (s *Server) handleLearningRules(w http.ResponseWriter, r *http.Request) {
	// Parse query parameter for status filter
	status := r.URL.Query().Get("status")
	s.logger.Info("handleLearningRules called", "status", status)
	var rules interface{}
	var err error

	if s.client != nil {
		rules, err = s.client.GetLearningRules(status)
	} else if s.learning != nil {
		rules, err = s.learning.GetPendingRules(status)
	} else {
		http.Error(w, "Learning service not available", http.StatusServiceUnavailable)
		return
	}

	if err != nil {
		s.logger.Error("Failed to get pending rules", "error", err)
		http.Error(w, "Failed to retrieve pending rules", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rules); err != nil {
		s.logger.Error("Failed to encode pending rules", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// handleLearningRule handles operations on specific pending rules
// Supports:
//
//	GET /api/learning/rules/{id} - Get specific rule
//	POST /api/learning/rules/{id}/approve - Approve rule
//	POST /api/learning/rules/{id}/deny - Deny rule
//	POST /api/learning/rules/{id}/ignore - Ignore rule
//	DELETE /api/learning/rules/{id} - Delete rule
func (s *Server) handleLearningRule(w http.ResponseWriter, r *http.Request) {
	if s.learning == nil && s.client == nil {
		http.Error(w, "Learning service not available", http.StatusServiceUnavailable)
		return
	}

	// Extract rule ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/learning/rules/")
	if path == "" {
		http.Error(w, "Rule ID required", http.StatusBadRequest)
		return
	}

	// Parse ID and action
	parts := strings.Split(path, "/")
	ruleID := parts[0]
	if ruleID == "" {
		http.Error(w, "Rule ID required", http.StatusBadRequest)
		return
	}

	// Get username from context (set by auth middleware)
	username, ok := r.Context().Value("username").(string)
	if !ok {
		username = "unknown"
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetLearningRule(w, r, ruleID)
	case http.MethodPost:
		if len(parts) < 2 {
			http.Error(w, "Action required (approve/deny/ignore)", http.StatusBadRequest)
			return
		}
		action := parts[1]
		switch action {
		case "approve":
			s.handleApproveLearningRule(w, r, ruleID, username)
		case "deny":
			s.handleDenyLearningRule(w, r, ruleID, username)
		case "ignore":
			s.handleIgnoreLearningRule(w, r, ruleID)
		default:
			http.Error(w, "Invalid action. Use: approve, deny, ignore", http.StatusBadRequest)
		}
	case http.MethodDelete:
		s.handleDeleteLearningRule(w, r, ruleID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetLearningRule handles GET /api/learning/rules/{id}
func (s *Server) handleGetLearningRule(w http.ResponseWriter, r *http.Request, ruleID string) {
	var rule interface{}
	var err error

	if s.client != nil {
		rule, err = s.client.GetLearningRule(ruleID)
	} else {
		rule, err = s.learning.GetPendingRule(ruleID)
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "Rule not found", http.StatusNotFound)
		} else {
			s.logger.Error("Failed to get pending rule", "error", err)
			http.Error(w, "Failed to retrieve rule", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rule); err != nil {
		s.logger.Error("Failed to encode pending rule", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// handleApproveLearningRule handles POST /api/learning/rules/{id}/approve
func (s *Server) handleApproveLearningRule(w http.ResponseWriter, r *http.Request, ruleID, approvedBy string) {
	var rule interface{}
	var err error

	if s.client != nil {
		rule, err = s.client.ApproveRule(ruleID, approvedBy)
	} else {
		rule, err = s.learning.ApproveRule(ruleID, approvedBy)
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "Rule not found", http.StatusNotFound)
		} else {
			s.logger.Error("Failed to approve rule", "error", err)
			http.Error(w, "Failed to approve rule", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rule); err != nil {
		s.logger.Error("Failed to encode approved rule", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// handleDenyLearningRule handles POST /api/learning/rules/{id}/deny
func (s *Server) handleDenyLearningRule(w http.ResponseWriter, r *http.Request, ruleID, deniedBy string) {
	var rule interface{}
	var err error

	if s.client != nil {
		rule, err = s.client.DenyRule(ruleID, deniedBy)
	} else {
		rule, err = s.learning.DenyRule(ruleID, deniedBy)
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "Rule not found", http.StatusNotFound)
		} else {
			s.logger.Error("Failed to deny rule", "error", err)
			http.Error(w, "Failed to deny rule", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rule); err != nil {
		s.logger.Error("Failed to encode denied rule", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// handleIgnoreLearningRule handles POST /api/learning/rules/{id}/ignore
func (s *Server) handleIgnoreLearningRule(w http.ResponseWriter, r *http.Request, ruleID string) {
	var rule interface{}
	var err error

	if s.client != nil {
		rule, err = s.client.IgnoreRule(ruleID)
	} else {
		rule, err = s.learning.IgnoreRule(ruleID)
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "Rule not found", http.StatusNotFound)
		} else {
			s.logger.Error("Failed to ignore rule", "error", err)
			http.Error(w, "Failed to ignore rule", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rule); err != nil {
		s.logger.Error("Failed to encode ignored rule", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// handleDeleteLearningRule handles DELETE /api/learning/rules/{id}
func (s *Server) handleDeleteLearningRule(w http.ResponseWriter, r *http.Request, ruleID string) {
	var err error
	if s.client != nil {
		err = s.client.DeleteRule(ruleID)
	} else {
		err = s.learning.DeleteRule(ruleID)
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "Rule not found", http.StatusNotFound)
		} else {
			s.logger.Error("Failed to delete rule", "error", err)
			http.Error(w, "Failed to delete rule", http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleLearningStats handles GET /api/learning/stats
// Returns learning service statistics
func (s *Server) handleLearningStats(w http.ResponseWriter, r *http.Request) {
	var stats map[string]interface{}
	var err error

	if s.client != nil {
		stats, err = s.client.GetLearningStats()
	} else if s.learning != nil {
		stats, err = s.learning.GetStats()
	} else {
		http.Error(w, "Learning service not available", http.StatusServiceUnavailable)
		return
	}

	if err != nil {
		s.logger.Error("Failed to get learning stats", "error", err)
		http.Error(w, "Failed to retrieve statistics", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		s.logger.Error("Failed to encode learning stats", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}
