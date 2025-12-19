// Package firewall provides safe configuration application with connectivity verification.
package firewall

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"

	"grimm.is/glacic/internal/config"
)

// SafeApplyConfig holds configuration for safe apply operations.
type SafeApplyConfig struct {
	// PingTargets are addresses to ping after applying changes
	// If empty, uses the client's IP address
	PingTargets []string

	// PingTimeout is how long to wait for ping responses
	PingTimeout time.Duration

	// RollbackDelay is how long to wait before auto-rollback if no confirmation
	RollbackDelay time.Duration

	// RequireConfirmation requires explicit confirmation to keep changes
	RequireConfirmation bool
}

// DefaultSafeApplyConfig returns sensible defaults.
func DefaultSafeApplyConfig() *SafeApplyConfig {
	return &SafeApplyConfig{
		PingTargets:         nil, // Will use client IP
		PingTimeout:         5 * time.Second,
		RollbackDelay:       30 * time.Second,
		RequireConfirmation: true,
	}
}

// SafeApplyManager handles safe configuration application with rollback.
type SafeApplyManager struct {
	mu sync.Mutex

	// Firewall manager for applying rules
	fwManager *Manager

	// Backup manager for config versioning
	backupManager *config.BackupManager

	// Rollback manager for nftables rules
	rollbackManager *RollbackManager

	// Current pending apply (if any)
	pendingApply *PendingApply

	// Config path
	configPath string
}

// PendingApply represents a configuration change awaiting confirmation.
type PendingApply struct {
	ID             string
	StartTime      time.Time
	RollbackTime   time.Time
	ClientIP       string
	BackupVersion  int
	RulesetBackup  string
	Confirmed      bool
	RolledBack     bool
	cancelRollback context.CancelFunc
	confirmChan    chan bool
}

// ApplyResult contains the result of a safe apply operation.
type ApplyResult struct {
	Success         bool   `json:"success"`
	PendingID       string `json:"pending_id,omitempty"`
	Message         string `json:"message"`
	RollbackTime    string `json:"rollback_time,omitempty"`
	RequiresConfirm bool   `json:"requires_confirm"`
	BackupVersion   int    `json:"backup_version,omitempty"`
}

// NewSafeApplyManager creates a new safe apply manager.
func NewSafeApplyManager(fwManager *Manager, configPath string) *SafeApplyManager {
	return &SafeApplyManager{
		fwManager:       fwManager,
		backupManager:   config.NewBackupManager(configPath, 20),
		rollbackManager: NewRollbackManager(),
		configPath:      configPath,
	}
}

// SafeApply applies configuration changes with connectivity verification.
// If connectivity is lost, automatically rolls back.
func (s *SafeApplyManager) SafeApply(cfg *config.Config, clientIP string, safeConfig *SafeApplyConfig) (*ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if safeConfig == nil {
		safeConfig = DefaultSafeApplyConfig()
	}

	// Check for existing pending apply
	if s.pendingApply != nil && !s.pendingApply.Confirmed && !s.pendingApply.RolledBack {
		return nil, fmt.Errorf("another apply is pending confirmation (ID: %s)", s.pendingApply.ID)
	}

	// Create config backup
	backup, err := s.backupManager.CreateBackup("Pre-apply backup", true)
	if err != nil {
		return nil, fmt.Errorf("failed to create config backup: %w", err)
	}

	// Save nftables ruleset checkpoint
	if err := s.rollbackManager.SaveCheckpoint(); err != nil {
		return nil, fmt.Errorf("failed to save ruleset checkpoint: %w", err)
	}

	// Generate unique apply ID
	applyID := fmt.Sprintf("apply-%d", clock.Now().UnixNano())

	// Create pending apply record
	ctx, cancel := context.WithCancel(context.Background())
	pending := &PendingApply{
		ID:             applyID,
		StartTime:      clock.Now(),
		RollbackTime:   clock.Now().Add(safeConfig.RollbackDelay),
		ClientIP:       clientIP,
		BackupVersion:  backup.Version,
		RulesetBackup:  s.rollbackManager.backupPath,
		cancelRollback: cancel,
		confirmChan:    make(chan bool, 1),
	}
	s.pendingApply = pending

	// Apply the new configuration
	// Apply the new configuration
	if err := s.fwManager.ApplyConfig(FromGlobalConfig(cfg)); err != nil {
		// Immediate rollback on apply failure
		s.rollbackManager.Rollback()
		s.pendingApply = nil
		return &ApplyResult{
			Success: false,
			Message: fmt.Sprintf("Apply failed: %v", err),
		}, nil
	}

	// Verify connectivity
	pingTargets := safeConfig.PingTargets
	if len(pingTargets) == 0 && clientIP != "" {
		pingTargets = []string{clientIP}
	}

	if len(pingTargets) > 0 {
		if !s.verifyConnectivity(pingTargets, safeConfig.PingTimeout) {
			// Connectivity lost - immediate rollback
			s.rollbackManager.Rollback()
			s.pendingApply = nil
			return &ApplyResult{
				Success: false,
				Message: "Connectivity verification failed - changes rolled back",
			}, nil
		}
	}

	// If confirmation required, start rollback timer
	if safeConfig.RequireConfirmation {
		go s.rollbackTimer(ctx, pending, safeConfig.RollbackDelay)

		return &ApplyResult{
			Success:         true,
			PendingID:       applyID,
			Message:         fmt.Sprintf("Changes applied. Confirm within %v or they will be rolled back.", safeConfig.RollbackDelay),
			RollbackTime:    pending.RollbackTime.Format(time.RFC3339),
			RequiresConfirm: true,
			BackupVersion:   backup.Version,
		}, nil
	}

	// No confirmation required - changes are permanent
	s.pendingApply = nil
	s.rollbackManager.Cleanup()

	return &ApplyResult{
		Success:       true,
		Message:       "Changes applied successfully",
		BackupVersion: backup.Version,
	}, nil
}

// ConfirmApply confirms a pending apply, making changes permanent.
func (s *SafeApplyManager) ConfirmApply(applyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pendingApply == nil {
		return fmt.Errorf("no pending apply to confirm")
	}

	if s.pendingApply.ID != applyID {
		return fmt.Errorf("apply ID mismatch")
	}

	if s.pendingApply.RolledBack {
		return fmt.Errorf("apply was already rolled back")
	}

	if s.pendingApply.Confirmed {
		return fmt.Errorf("apply was already confirmed")
	}

	// Cancel rollback timer
	s.pendingApply.cancelRollback()
	s.pendingApply.Confirmed = true
	s.pendingApply.confirmChan <- true

	// Cleanup
	s.rollbackManager.Cleanup()
	s.pendingApply = nil

	return nil
}

// CancelApply cancels a pending apply and rolls back.
func (s *SafeApplyManager) CancelApply(applyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pendingApply == nil {
		return fmt.Errorf("no pending apply to cancel")
	}

	if s.pendingApply.ID != applyID {
		return fmt.Errorf("apply ID mismatch")
	}

	if s.pendingApply.RolledBack {
		return fmt.Errorf("apply was already rolled back")
	}

	// Cancel rollback timer
	s.pendingApply.cancelRollback()

	// Perform rollback
	if err := s.rollbackManager.Rollback(); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	s.pendingApply.RolledBack = true
	s.pendingApply = nil

	return nil
}

// GetPendingApply returns the current pending apply if any.
func (s *SafeApplyManager) GetPendingApply() *PendingApply {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingApply
}

// rollbackTimer waits for confirmation or rolls back after timeout.
func (s *SafeApplyManager) rollbackTimer(ctx context.Context, pending *PendingApply, delay time.Duration) {
	select {
	case <-ctx.Done():
		// Cancelled (confirmed or manually cancelled)
		return
	case <-pending.confirmChan:
		// Confirmed
		return
	case <-time.After(delay):
		// Timeout - rollback
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.pendingApply != pending || pending.Confirmed || pending.RolledBack {
			return
		}

		// Perform rollback
		if err := s.rollbackManager.Rollback(); err != nil {
			// Log error but can't do much else
			fmt.Fprintf(os.Stderr, "Auto-rollback failed: %v\n", err)
		}

		pending.RolledBack = true
		s.pendingApply = nil
	}
}

// verifyConnectivity checks if we can reach the specified targets.
func (s *SafeApplyManager) verifyConnectivity(targets []string, timeout time.Duration) bool {
	for _, target := range targets {
		if s.pingTarget(target, timeout) {
			return true // At least one target reachable
		}
	}
	return false
}

// pingTarget attempts to verify connectivity to a target.
func (s *SafeApplyManager) pingTarget(target string, timeout time.Duration) bool {
	// Try TCP connection to common ports
	ports := []string{"80", "443", "22"}

	for _, port := range ports {
		addr := net.JoinHostPort(target, port)
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err == nil {
			conn.Close()
			return true
		}
	}

	// Try HTTP ping if target looks like it might be the API client
	client := &http.Client{Timeout: timeout}
	for _, scheme := range []string{"http", "https"} {
		url := fmt.Sprintf("%s://%s/", scheme, target)
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return true
		}
	}

	return false
}

// QuickApply applies changes without confirmation requirement.
// Still creates backup and verifies connectivity.
func (s *SafeApplyManager) QuickApply(cfg *config.Config, clientIP string) (*ApplyResult, error) {
	safeConfig := DefaultSafeApplyConfig()
	safeConfig.RequireConfirmation = false
	return s.SafeApply(cfg, clientIP, safeConfig)
}

// ForceApply applies changes without any safety checks.
// Use only for recovery situations.
func (s *SafeApplyManager) ForceApply(cfg *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel any pending apply
	if s.pendingApply != nil {
		s.pendingApply.cancelRollback()
		s.pendingApply = nil
	}

	return s.fwManager.ApplyConfig(FromGlobalConfig(cfg))
}

// RestoreBackup restores a specific backup version.
func (s *SafeApplyManager) RestoreBackup(version int) error {
	return s.backupManager.RestoreBackup(version)
}

// ListBackups returns all available backups.
func (s *SafeApplyManager) ListBackups() ([]config.BackupInfo, error) {
	return s.backupManager.ListBackups()
}

// CreateManualBackup creates a manual backup with description.
func (s *SafeApplyManager) CreateManualBackup(description string) (*config.BackupInfo, error) {
	return s.backupManager.CreateBackup(description, false)
}
