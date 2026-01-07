package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"
)

// IPBlocker is an interface for blocking IPs via IPSets.
// This abstraction allows the API server (running in a sandbox) to block IPs
// by calling through RPC to the control plane.
type IPBlocker interface {
	// AddToIPSet adds an IP to a named IPSet
	AddToIPSet(setName, ip string) error
	// RemoveFromIPSet removes an IP from a named IPSet
	RemoveFromIPSet(setName, ip string) error
	// CheckIPSet checks if an IP is in a named IPSet
	CheckIPSet(setName, ip string) (bool, error)
}

// SecurityManager handles IP blocking using existing IPSet infrastructure
type SecurityManager struct {
	blocker      IPBlocker
	logger       *logging.Logger
	blockedIPSet string // Name of the IPSet for blocked IPs

	// Track attempts in memory for auto-blocking
	mu       sync.RWMutex
	attempts map[string]*attemptTracker
}

type attemptTracker struct {
	count       int
	lastAttempt time.Time
	attempts    []time.Time
}

// NewSecurityManager creates a security manager that uses IPSets for IP blocking
func NewSecurityManager(blocker IPBlocker, logger *logging.Logger) *SecurityManager {
	return &SecurityManager{
		blocker:      blocker,
		logger:       logger,
		blockedIPSet: "blocked_ips", // Standard IPSet for blocked IPs
		attempts:     make(map[string]*attemptTracker),
	}
}

// RecordFailedAttempt records a failed authentication attempt
// Automatically blocks IP if threshold is exceeded
func (sm *SecurityManager) RecordFailedAttempt(ip, reason string, threshold int, window time.Duration) error {
	now := clock.Now()

	// Whitelist localhost to prevent self-lockout
	if ip == "127.0.0.1" || ip == "::1" {
		return nil
	}

	// Get or create tracker (thread-safe)
	sm.mu.Lock()
	defer sm.mu.Unlock()

	tracker, exists := sm.attempts[ip]
	if !exists {
		tracker = &attemptTracker{
			attempts: make([]time.Time, 0),
		}
		sm.attempts[ip] = tracker
	}

	// Add attempt
	tracker.attempts = append(tracker.attempts, now)
	tracker.lastAttempt = now

	// Remove attempts outside window
	validAttempts := make([]time.Time, 0)
	for _, t := range tracker.attempts {
		if now.Sub(t) <= window {
			validAttempts = append(validAttempts, t)
		}
	}
	tracker.attempts = validAttempts
	tracker.count = len(validAttempts)

	// Check if threshold exceeded
	if tracker.count >= threshold {
		sm.logger.Warn("Blocking IP due to repeated failures", "ip", ip, "attempts", tracker.count, "reason", reason)

		// Add to blocked IPSet via IPBlocker interface
		if sm.blocker != nil {
			// Release lock before RPC call to avoid deadlock
			sm.mu.Unlock()
			err := sm.blockIPLocked(ip, reason)
			sm.mu.Lock()
			return err
		}
	}

	return nil
}

// blockIPLocked adds an IP to the blocked IPSet (caller must NOT hold lock)
func (sm *SecurityManager) blockIPLocked(ip, reason string) error {
	if sm.blocker == nil {
		return fmt.Errorf("IPBlocker not available")
	}

	sm.logger.Info("Blocking IP", "ip", ip, "reason", reason)

	if err := sm.blocker.AddToIPSet(sm.blockedIPSet, ip); err != nil {
		return fmt.Errorf("failed to add to blocked set: %w", err)
	}

	// Clear attempt tracker
	sm.mu.Lock()
	delete(sm.attempts, ip)
	sm.mu.Unlock()

	return nil
}

// BlockIP adds an IP to the blocked IPSet (public API)
func (sm *SecurityManager) BlockIP(ip, reason string) error {
	return sm.blockIPLocked(ip, reason)
}

// UnblockIP removes an IP from the blocked IPSet
func (sm *SecurityManager) UnblockIP(ip string) error {
	if sm.blocker == nil {
		return fmt.Errorf("IPBlocker not available")
	}

	sm.logger.Info("Unblocking IP", "ip", ip)

	if err := sm.blocker.RemoveFromIPSet(sm.blockedIPSet, ip); err != nil {
		return fmt.Errorf("failed to remove from blocked set: %w", err)
	}

	return nil
}

// IsBlocked checks if an IP is in the blocked IPSet
func (sm *SecurityManager) IsBlocked(ip string) (bool, error) {
	if sm.blocker == nil {
		return false, nil
	}

	return sm.blocker.CheckIPSet(sm.blockedIPSet, ip)
}

// handleBlockIP is an API endpoint for external tools (like fail2ban) to block IPs
func (s *Server) handleBlockIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		IP     string `json:"ip"`
		Reason string `json:"reason"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.IP == "" {
		http.Error(w, "IP address required", http.StatusBadRequest)
		return
	}

	if err := s.security.BlockIP(req.IP, req.Reason); err != nil {
		http.Error(w, fmt.Sprintf("Failed to block IP: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("IP %s blocked", req.IP),
	})
}

// handleUnblockIP is an API endpoint to unblock IPs
func (s *Server) handleUnblockIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		IP string `json:"ip"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.IP == "" {
		http.Error(w, "IP address required", http.StatusBadRequest)
		return
	}

	if err := s.security.UnblockIP(req.IP); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unblock IP: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("IP %s unblocked", req.IP),
	})
}

// handleGetBlockedIPs returns list of blocked IPs
func (s *Server) handleGetBlockedIPs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get blocked IPs from control plane via RPC
	var blockedIPs []string
	var err error

	if s.client != nil {
		// Use control plane RPC to get elements from blocked_ips IPSet
		blockedIPs, err = s.client.GetIPSetElements("blocked_ips")
		if err != nil {
			logging.APILog("warn", "Failed to get blocked IPs from IPSet: %v", err)
			// Fall through with empty list
			blockedIPs = []string{}
		}
	}

	// Convert to response format with metadata
	blockedIPsResponse := make([]map[string]interface{}, 0, len(blockedIPs))
	for _, ip := range blockedIPs {
		blockedIPsResponse = append(blockedIPsResponse, map[string]interface{}{
			"ip":         ip,
			"blocked_at": nil, // Could track from state store if needed
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"blocked_ips": blockedIPsResponse,
		"count":       len(blockedIPs),
	})
}
