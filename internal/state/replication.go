package state

import (
	"context"
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"io"
	"net"
	"sync"
	"time"

	"grimm.is/glacic/internal/logging"
)

// ReplicationMode defines the replication role.
type ReplicationMode string

const (
	ModePrimary ReplicationMode = "primary"
	ModeReplica ReplicationMode = "replica"
	ModeStandby ReplicationMode = "standby" // For upgrades
)

// ReplicationConfig configures the replication layer.
type ReplicationConfig struct {
	Mode           ReplicationMode
	ListenAddr     string        // For primary: where to accept replica connections
	PrimaryAddr    string        // For replica: where to connect to primary
	ReconnectDelay time.Duration // How long to wait before reconnecting
	SyncTimeout    time.Duration // Timeout for initial sync
}

// DefaultReplicationConfig returns sensible defaults.
func DefaultReplicationConfig() ReplicationConfig {
	return ReplicationConfig{
		Mode:           ModePrimary,
		ListenAddr:     ":9999",
		ReconnectDelay: 5 * time.Second,
		SyncTimeout:    30 * time.Second,
	}
}

// Replicator handles state replication between nodes.
type Replicator struct {
	store  *SQLiteStore
	config ReplicationConfig
	logger *logging.Logger

	mu       sync.RWMutex
	replicas map[string]*replicaConn
	primary  *primaryConn

	ctx    context.Context
	cancel context.CancelFunc
}

// replicaConn represents a connection to a replica.
type replicaConn struct {
	conn    net.Conn
	encoder *json.Encoder
	version uint64
}

// primaryConn represents a connection to the primary.
type primaryConn struct {
	conn    net.Conn
	decoder *json.Decoder
}

// NewReplicator creates a new replicator.
func NewReplicator(store *SQLiteStore, config ReplicationConfig, logger *logging.Logger) *Replicator {
	ctx, cancel := context.WithCancel(context.Background())
	return &Replicator{
		store:    store,
		config:   config,
		logger:   logger,
		replicas: make(map[string]*replicaConn),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins replication based on mode.
func (r *Replicator) Start() error {
	switch r.config.Mode {
	case ModePrimary:
		return r.startPrimary()
	case ModeReplica:
		return r.startReplica()
	case ModeStandby:
		// Standby mode waits for explicit sync
		return nil
	default:
		return fmt.Errorf("unknown replication mode: %s", r.config.Mode)
	}
}

// Stop stops replication.
func (r *Replicator) Stop() {
	r.cancel()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Close replica connections
	for addr, replica := range r.replicas {
		replica.conn.Close()
		delete(r.replicas, addr)
	}

	// Close primary connection
	if r.primary != nil {
		r.primary.conn.Close()
		r.primary = nil
	}
}

// startPrimary starts the primary replication server.
func (r *Replicator) startPrimary() error {
	listener, err := net.Listen("tcp", r.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to start replication listener: %w", err)
	}

	r.logger.Info("Replication primary started", "addr", r.config.ListenAddr)

	// Accept replica connections
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-r.ctx.Done():
					listener.Close()
					return
				default:
					r.logger.Warn("Failed to accept replica connection", "error", err)
					continue
				}
			}
			go r.handleReplica(conn)
		}
	}()

	// Subscribe to changes and broadcast to replicas
	go r.broadcastChanges()

	return nil
}

// handleReplica handles a new replica connection.
func (r *Replicator) handleReplica(conn net.Conn) {
	addr := conn.RemoteAddr().String()
	r.logger.Info("Replica connected", "addr", addr)

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	// Read sync request
	var req syncRequest
	if err := decoder.Decode(&req); err != nil {
		r.logger.Warn("Failed to read sync request", "addr", addr, "error", err)
		conn.Close()
		return
	}

	// Send snapshot or changes since version
	if req.Version == 0 {
		// Full sync
		snapshot, err := r.store.CreateSnapshot()
		if err != nil {
			r.logger.Warn("Failed to create snapshot", "error", err)
			conn.Close()
			return
		}

		resp := syncResponse{
			Type:     "snapshot",
			Snapshot: snapshot,
		}
		if err := encoder.Encode(resp); err != nil {
			r.logger.Warn("Failed to send snapshot", "error", err)
			conn.Close()
			return
		}
		r.logger.Info("Sent full snapshot to replica", "addr", addr, "version", snapshot.Version)
	} else {
		// Incremental sync
		changes, err := r.store.GetChangesSince(req.Version)
		if err != nil {
			r.logger.Warn("Failed to get changes", "error", err)
			conn.Close()
			return
		}

		resp := syncResponse{
			Type:    "changes",
			Changes: changes,
		}
		if err := encoder.Encode(resp); err != nil {
			r.logger.Warn("Failed to send changes", "error", err)
			conn.Close()
			return
		}
		r.logger.Info("Sent incremental changes to replica", "addr", addr, "count", len(changes))
	}

	// Register replica for ongoing updates
	r.mu.Lock()
	r.replicas[addr] = &replicaConn{
		conn:    conn,
		encoder: encoder,
		version: r.store.CurrentVersion(),
	}
	r.mu.Unlock()

	// Keep connection alive and handle disconnects
	go func() {
		buf := make([]byte, 1)
		for {
			conn.SetReadDeadline(clock.Now().Add(30 * time.Second))
			_, err := conn.Read(buf)
			if err != nil {
				r.mu.Lock()
				delete(r.replicas, addr)
				r.mu.Unlock()
				conn.Close()
				r.logger.Info("Replica disconnected", "addr", addr)
				return
			}
		}
	}()
}

// broadcastChanges subscribes to store changes and sends to all replicas.
func (r *Replicator) broadcastChanges() {
	changes := r.store.Subscribe(r.ctx)

	for change := range changes {
		r.mu.RLock()
		for addr, replica := range r.replicas {
			msg := replicationMessage{
				Type:   "change",
				Change: &change,
			}
			if err := replica.encoder.Encode(msg); err != nil {
				r.logger.Warn("Failed to send change to replica", "addr", addr, "error", err)
				// Will be cleaned up by the read goroutine
			}
		}
		r.mu.RUnlock()
	}
}

// startReplica connects to the primary and receives updates.
func (r *Replicator) startReplica() error {
	go r.replicaLoop()
	return nil
}

// replicaLoop maintains connection to primary.
func (r *Replicator) replicaLoop() {
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		if err := r.connectToPrimary(); err != nil {
			r.logger.Warn("Failed to connect to primary", "error", err)
			time.Sleep(r.config.ReconnectDelay)
			continue
		}

		// Receive updates until disconnected
		if err := r.receiveUpdates(); err != nil {
			r.logger.Warn("Lost connection to primary", "error", err)
			r.mu.Lock()
			if r.primary != nil {
				r.primary.conn.Close()
				r.primary = nil
			}
			r.mu.Unlock()
			time.Sleep(r.config.ReconnectDelay)
		}
	}
}

// connectToPrimary establishes connection and performs initial sync.
func (r *Replicator) connectToPrimary() error {
	conn, err := net.DialTimeout("tcp", r.config.PrimaryAddr, r.config.SyncTimeout)
	if err != nil {
		return err
	}

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	// Send sync request
	req := syncRequest{
		Version: r.store.CurrentVersion(),
	}
	if err := encoder.Encode(req); err != nil {
		conn.Close()
		return err
	}

	// Receive response
	var resp syncResponse
	if err := decoder.Decode(&resp); err != nil {
		conn.Close()
		return err
	}

	// Apply sync data
	switch resp.Type {
	case "snapshot":
		if err := r.store.RestoreSnapshot(resp.Snapshot); err != nil {
			conn.Close()
			return fmt.Errorf("failed to restore snapshot: %w", err)
		}
		r.logger.Info("Restored snapshot from primary", "version", resp.Snapshot.Version)

	case "changes":
		for _, change := range resp.Changes {
			if err := r.applyChange(change); err != nil {
				r.logger.Warn("Failed to apply change", "error", err)
			}
		}
		r.logger.Info("Applied incremental changes", "count", len(resp.Changes))
	}

	r.mu.Lock()
	r.primary = &primaryConn{
		conn:    conn,
		decoder: decoder,
	}
	r.mu.Unlock()

	r.logger.Info("Connected to primary", "addr", r.config.PrimaryAddr)
	return nil
}

// receiveUpdates receives and applies changes from primary.
func (r *Replicator) receiveUpdates() error {
	r.mu.RLock()
	primary := r.primary
	r.mu.RUnlock()

	if primary == nil {
		return fmt.Errorf("not connected to primary")
	}

	for {
		var msg replicationMessage
		if err := primary.decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				return fmt.Errorf("primary closed connection")
			}
			return err
		}

		switch msg.Type {
		case "change":
			if msg.Change != nil {
				if err := r.applyChange(*msg.Change); err != nil {
					r.logger.Warn("Failed to apply change", "error", err)
				}
			}
		}
	}
}

// applyChange applies a replicated change to the local store.
func (r *Replicator) applyChange(change Change) error {
	switch change.Type {
	case ChangeInsert, ChangeUpdate:
		return r.store.Set(change.Bucket, change.Key, change.Value)
	case ChangeDelete:
		err := r.store.Delete(change.Bucket, change.Key)
		if err == ErrNotFound {
			return nil // Already deleted
		}
		return err
	default:
		return fmt.Errorf("unknown change type: %s", change.Type)
	}
}

// SyncFromPeer performs a one-time sync from another node.
// Used for upgrades and initial HA setup.
func (r *Replicator) SyncFromPeer(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, r.config.SyncTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	// Request full sync
	req := syncRequest{Version: 0}
	if err := encoder.Encode(req); err != nil {
		return err
	}

	var resp syncResponse
	if err := decoder.Decode(&resp); err != nil {
		return err
	}

	if resp.Type != "snapshot" {
		return fmt.Errorf("expected snapshot, got %s", resp.Type)
	}

	return r.store.RestoreSnapshot(resp.Snapshot)
}

// ExportForUpgrade creates a snapshot for upgrade handoff.
func (r *Replicator) ExportForUpgrade() (*Snapshot, error) {
	return r.store.CreateSnapshot()
}

// ImportFromUpgrade restores state from an upgrade handoff.
func (r *Replicator) ImportFromUpgrade(snapshot *Snapshot) error {
	return r.store.RestoreSnapshot(snapshot)
}

// Protocol messages

type syncRequest struct {
	Version uint64 `json:"version"`
}

type syncResponse struct {
	Type     string    `json:"type"` // "snapshot" or "changes"
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Changes  []Change  `json:"changes,omitempty"`
}

type replicationMessage struct {
	Type   string  `json:"type"` // "change"
	Change *Change `json:"change,omitempty"`
}
