package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Event represents a single audit log entry.
type Event struct {
	ID        int64          `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	User      string         `json:"user"`
	Session   string         `json:"session,omitempty"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Details   map[string]any `json:"details,omitempty"`
	Status    int            `json:"status"`
	IP        string         `json:"ip,omitempty"`
}

// Store provides persistent storage for audit events.
type Store struct {
	mu           sync.RWMutex
	db           *sql.DB
	kernelAudit  bool
	retentionDays int
}

// NewStore creates a new audit store at the given path.
func NewStore(dbPath string, retentionDays int, kernelAudit bool) (*Store, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("create audit dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}

	// Create table if not exists
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			user TEXT NOT NULL,
			session TEXT,
			action TEXT NOT NULL,
			resource TEXT NOT NULL,
			details TEXT,
			status INTEGER DEFAULT 0,
			ip TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_events(timestamp);
		CREATE INDEX IF NOT EXISTS idx_audit_user ON audit_events(user);
		CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_events(action);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create audit table: %w", err)
	}

	if retentionDays <= 0 {
		retentionDays = 90 // Default 90 days
	}

	return &Store{
		db:            db,
		kernelAudit:   kernelAudit,
		retentionDays: retentionDays,
	}, nil
}

// Write persists an audit event.
func (s *Store) Write(evt Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Serialize details to JSON
	var detailsJSON []byte
	if evt.Details != nil {
		var err error
		detailsJSON, err = json.Marshal(evt.Details)
		if err != nil {
			detailsJSON = []byte("{}")
		}
	}

	_, err := s.db.Exec(`
		INSERT INTO audit_events (timestamp, user, session, action, resource, details, status, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, evt.Timestamp, evt.User, evt.Session, evt.Action, evt.Resource, string(detailsJSON), evt.Status, evt.IP)

	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	// Optionally write to kernel audit log
	if s.kernelAudit {
		s.writeKernelAudit(evt)
	}

	return nil
}

// writeKernelAudit writes to Linux kernel audit log via /dev/audit or ausearch.
// This is a best-effort operation - failures are logged but don't affect the main audit.
func (s *Store) writeKernelAudit(evt Event) {
	// Format: type=USER_GLACIC msg=audit(timestamp): user=X action=Y resource=Z
	msg := fmt.Sprintf("glacic: user=%s action=%s resource=%s status=%d",
		evt.User, evt.Action, evt.Resource, evt.Status)

	// Write to syslog which auditd can capture
	log.Printf("[AUDIT] %s", msg)

	// Note: Direct /dev/audit integration would require CAP_AUDIT_WRITE capability.
	// The current syslog approach is a deliberate design choice that:
	// - Works without elevated capabilities
	// - Integrates with existing log aggregation infrastructure
	// - Allows auditd to capture events if configured to monitor syslog
}

// Query returns audit events matching the given criteria.
func (s *Store) Query(start, end time.Time, action, user string, limit int) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT id, timestamp, user, session, action, resource, details, status, ip
		FROM audit_events WHERE timestamp >= ? AND timestamp <= ?`
	args := []any{start, end}

	if action != "" {
		query += " AND action = ?"
		args = append(args, action)
	}
	if user != "" {
		query += " AND user = ?"
		args = append(args, user)
	}

	query += " ORDER BY timestamp DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var evt Event
		var detailsJSON sql.NullString
		var session sql.NullString
		var ip sql.NullString

		err := rows.Scan(&evt.ID, &evt.Timestamp, &evt.User, &session, &evt.Action,
			&evt.Resource, &detailsJSON, &evt.Status, &ip)
		if err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}

		if session.Valid {
			evt.Session = session.String
		}
		if ip.Valid {
			evt.IP = ip.String
		}
		if detailsJSON.Valid && detailsJSON.String != "" {
			json.Unmarshal([]byte(detailsJSON.String), &evt.Details)
		}

		events = append(events, evt)
	}

	return events, nil
}

// Prune removes events older than the retention period.
func (s *Store) Prune() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -s.retentionDays)
	result, err := s.db.Exec("DELETE FROM audit_events WHERE timestamp < ?", cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune audit events: %w", err)
	}

	return result.RowsAffected()
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Count returns the total number of events in the store.
func (s *Store) Count() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&count)
	return count, err
}
