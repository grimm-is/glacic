package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"grimm.is/glacic/internal/logging"
)

// UpgradeManager handles state transfer during seamless upgrades.
// It uses the same replication protocol as HA, making upgrades
// essentially a "promote replica" operation.
type UpgradeManager struct {
	store  *SQLiteStore
	logger *logging.Logger
}

// NewUpgradeManager creates a new upgrade manager.
func NewUpgradeManager(store *SQLiteStore, logger *logging.Logger) *UpgradeManager {
	return &UpgradeManager{
		store:  store,
		logger: logger,
	}
}

// PrepareUpgrade prepares the old process for upgrade.
// Returns a listener that the new process should connect to.
func (m *UpgradeManager) PrepareUpgrade(ctx context.Context, socketPath string) (net.Listener, error) {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create upgrade socket: %w", err)
	}

	m.logger.Info("Upgrade socket ready", "path", socketPath)
	return listener, nil
}

// ServeUpgrade handles the upgrade handoff to a new process.
func (m *UpgradeManager) ServeUpgrade(ctx context.Context, listener net.Listener) error {
	// Accept connection from new process
	conn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("failed to accept upgrade connection: %w", err)
	}
	defer conn.Close()

	m.logger.Info("New process connected for upgrade")

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Wait for ready signal
	var msg upgradeMsg
	if err := decoder.Decode(&msg); err != nil {
		return fmt.Errorf("failed to read ready message: %w", err)
	}
	if msg.Type != "ready" {
		return fmt.Errorf("expected ready, got %s", msg.Type)
	}

	m.logger.Info("New process ready, sending state")

	// Create and send snapshot
	snapshot, err := m.store.CreateSnapshot()
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	if err := encoder.Encode(upgradeMsg{
		Type:     "snapshot",
		Snapshot: snapshot,
	}); err != nil {
		return fmt.Errorf("failed to send snapshot: %w", err)
	}

	m.logger.Info("Snapshot sent", "version", snapshot.Version, "buckets", len(snapshot.Buckets))

	// Subscribe to changes and forward them
	changeCh := m.store.Subscribe(ctx)

	// Send changes until we receive handoff_complete
	doneCh := make(chan struct{})
	go func() {
		for {
			var msg upgradeMsg
			if err := decoder.Decode(&msg); err != nil {
				close(doneCh)
				return
			}
			if msg.Type == "handoff_complete" {
				close(doneCh)
				return
			}
		}
	}()

	// Forward changes
	for {
		select {
		case change, ok := <-changeCh:
			if !ok {
				return nil
			}
			if err := encoder.Encode(upgradeMsg{
				Type:   "change",
				Change: &change,
			}); err != nil {
				m.logger.Warn("Failed to send change", "error", err)
			}
		case <-doneCh:
			m.logger.Info("Upgrade handoff complete")
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ConnectForUpgrade connects to the old process and receives state.
func (m *UpgradeManager) ConnectForUpgrade(ctx context.Context, socketPath string) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to upgrade socket: %w", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Signal ready
	if err := encoder.Encode(upgradeMsg{Type: "ready"}); err != nil {
		return fmt.Errorf("failed to send ready: %w", err)
	}

	m.logger.Info("Sent ready signal, waiting for state")

	// Receive snapshot
	var msg upgradeMsg
	if err := decoder.Decode(&msg); err != nil {
		return fmt.Errorf("failed to receive snapshot: %w", err)
	}
	if msg.Type != "snapshot" {
		return fmt.Errorf("expected snapshot, got %s", msg.Type)
	}

	if err := m.store.RestoreSnapshot(msg.Snapshot); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	m.logger.Info("Restored snapshot", "version", msg.Snapshot.Version)

	// Receive changes until we're ready to take over
	// This runs in a goroutine so we can signal when ready
	go func() {
		for {
			var msg upgradeMsg
			if err := decoder.Decode(&msg); err != nil {
				return
			}
			if msg.Type == "change" && msg.Change != nil {
				m.applyChange(*msg.Change)
			}
		}
	}()

	return nil
}

// SignalHandoffComplete tells the old process we're done.
func (m *UpgradeManager) SignalHandoffComplete(conn net.Conn) error {
	encoder := json.NewEncoder(conn)
	return encoder.Encode(upgradeMsg{Type: "handoff_complete"})
}

// applyChange applies a single change to the store.
func (m *UpgradeManager) applyChange(change Change) error {
	switch change.Type {
	case ChangeInsert, ChangeUpdate:
		return m.store.Set(change.Bucket, change.Key, change.Value)
	case ChangeDelete:
		err := m.store.Delete(change.Bucket, change.Key)
		if err == ErrNotFound {
			return nil
		}
		return err
	default:
		return fmt.Errorf("unknown change type: %s", change.Type)
	}
}

// upgradeMsg is the protocol message for upgrade coordination.
type upgradeMsg struct {
	Type     string    `json:"type"` // "ready", "snapshot", "change", "handoff_complete"
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Change   *Change   `json:"change,omitempty"`
}

// WaitForChanges waits for a duration to receive any final changes.
func (m *UpgradeManager) WaitForChanges(duration time.Duration) {
	time.Sleep(duration)
}
