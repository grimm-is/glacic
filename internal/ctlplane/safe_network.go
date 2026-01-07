package ctlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/firewall"
)

// NetworkConfigurator defines the operations needed for safe network application.
type NetworkConfigurator interface {
	SnapshotInterfaces() ([]config.Interface, error)
	UpdateInterface(args *UpdateInterfaceArgs) error
	RestoreInterfaces(snapshot []config.Interface) error
}

// ConnectivityChecker is a function that checks if targets are reachable.
type ConnectivityChecker func(targets []string, timeout time.Duration) bool

// NetworkSafeApplyManager handles safe network configuration application with rollback.
type NetworkSafeApplyManager struct {
	mu sync.Mutex

	nm NetworkConfigurator
	cc ConnectivityChecker

	// Current pending apply (if any)
	pendingApply *NetworkPendingApply
}

// NetworkPendingApply represents a configuration change awaiting confirmation.
type NetworkPendingApply struct {
	ID             string
	StartTime      time.Time
	RollbackTime   time.Time
	ClientIP       string
	SnapshotPath   string // Path to JSON snapshot
	Confirmed      bool
	RolledBack     bool
	cancelRollback context.CancelFunc
	confirmChan    chan bool
}

// NewNetworkSafeApplyManager creates a new network safe apply manager.
func NewNetworkSafeApplyManager(nm NetworkConfigurator) *NetworkSafeApplyManager {
	s := &NetworkSafeApplyManager{
		nm: nm,
	}
	// Default connectivity checker uses the internal logic
	s.cc = s.verifyConnectivity
	return s
}

// ApplyInterfaceConfig applies interface configuration changes with connectivity verification.
// If connectivity is lost, automatically rolls back.
func (s *NetworkSafeApplyManager) ApplyInterfaceConfig(args *UpdateInterfaceArgs, clientIP string, safeConfig *firewall.SafeApplyConfig) (*firewall.ApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if safeConfig == nil {
		safeConfig = firewall.DefaultSafeApplyConfig()
	}

	// Check for existing pending apply
	if s.pendingApply != nil && !s.pendingApply.Confirmed && !s.pendingApply.RolledBack {
		return nil, fmt.Errorf("another apply is pending confirmation (ID: %s)", s.pendingApply.ID)
	}

	// Snapshot current state
	snapshot, err := s.nm.SnapshotInterfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to snapshot interface config: %w", err)
	}

	snapshotPath, err := s.saveSnapshot(snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to save snapshot: %w", err)
	}

	// Generate unique apply ID
	applyID := fmt.Sprintf("net-apply-%d", clock.Now().UnixNano())

	// Create pending apply record
	ctx, cancel := context.WithCancel(context.Background())
	pending := &NetworkPendingApply{
		ID:             applyID,
		StartTime:      clock.Now(),
		RollbackTime:   clock.Now().Add(safeConfig.RollbackDelay),
		ClientIP:       clientIP,
		SnapshotPath:   snapshotPath,
		cancelRollback: cancel,
		confirmChan:    make(chan bool, 1),
	}
	s.pendingApply = pending

	// Apply the new configuration
	if err := s.nm.UpdateInterface(args); err != nil {
		// Immediate rollback on apply failure
		s.rollback(snapshot)
		s.pendingApply = nil
		return &firewall.ApplyResult{
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
		if !s.cc(pingTargets, safeConfig.PingTimeout) {
			// Connectivity lost - immediate rollback
			s.rollback(snapshot)
			s.pendingApply = nil
			return &firewall.ApplyResult{
				Success: false,
				Message: "Connectivity verification failed - changes rolled back",
			}, nil
		}
	}

	// If confirmation required, start rollback timer
	if safeConfig.RequireConfirmation {
		go s.rollbackTimer(ctx, pending, safeConfig.RollbackDelay, snapshot)

		return &firewall.ApplyResult{
			Success:         true,
			PendingID:       applyID,
			Message:         fmt.Sprintf("Changes applied. Confirm within %v or they will be rolled back.", safeConfig.RollbackDelay),
			RollbackTime:    pending.RollbackTime.Format(time.RFC3339),
			RequiresConfirm: true,
		}, nil
	}

	// No confirmation required - changes are permanent
	s.pendingApply = nil
	os.Remove(snapshotPath) // Cleanup snapshot

	return &firewall.ApplyResult{
		Success: true,
		Message: "Changes applied successfully",
	}, nil
}

// ConfirmApply confirms a pending apply, making changes permanent.
func (s *NetworkSafeApplyManager) ConfirmApply(applyID string) error {
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
	os.Remove(s.pendingApply.SnapshotPath)
	s.pendingApply = nil

	return nil
}

// CancelApply cancels a pending apply and rolls back.
func (s *NetworkSafeApplyManager) CancelApply(applyID string) error {
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

	// Load snapshot
	snapshot, err := s.loadSnapshot(s.pendingApply.SnapshotPath)
	if err != nil {
		// Critical error, but we try to cancel anyway
		s.pendingApply.cancelRollback()
		return fmt.Errorf("failed to load snapshot for rollback: %w", err)
	}

	// Cancel rollback timer
	s.pendingApply.cancelRollback()

	// Perform rollback
	if err := s.rollback(snapshot); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	s.pendingApply.RolledBack = true
	os.Remove(s.pendingApply.SnapshotPath)
	s.pendingApply = nil

	return nil
}

// GetPendingApply returns the current pending apply if any.
func (s *NetworkSafeApplyManager) GetPendingApply() *NetworkPendingApply {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingApply
}

// rollbackTimer waits for confirmation or rolls back after timeout.
func (s *NetworkSafeApplyManager) rollbackTimer(ctx context.Context, pending *NetworkPendingApply, delay time.Duration, snapshot []config.Interface) {
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
		if err := s.rollback(snapshot); err != nil {
			// Log error but can't do much else
			fmt.Fprintf(os.Stderr, "Auto-rollback failed: %v\n", err)
		}

		pending.RolledBack = true
		os.Remove(pending.SnapshotPath)
		s.pendingApply = nil
	}
}

// verifyConnectivity checks if we can reach the specified targets.
func (s *NetworkSafeApplyManager) verifyConnectivity(targets []string, timeout time.Duration) bool {
	for _, target := range targets {
		if s.pingTarget(target, timeout) {
			return true // At least one target reachable
		}
	}
	return false
}

// pingTarget attempts to verify connectivity to a target.
func (s *NetworkSafeApplyManager) pingTarget(target string, timeout time.Duration) bool {
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

func (s *NetworkSafeApplyManager) rollback(snapshot []config.Interface) error {
	return s.nm.RestoreInterfaces(snapshot)
}

func (s *NetworkSafeApplyManager) saveSnapshot(snapshot []config.Interface) (string, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}

	// Use temp dir
	path := filepath.Join(os.TempDir(), fmt.Sprintf("glacic-net-snapshot-%d.json", clock.Now().UnixNano()))
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *NetworkSafeApplyManager) loadSnapshot(path string) ([]config.Interface, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var snapshot []config.Interface
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}
