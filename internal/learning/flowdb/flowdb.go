// Package flowdb provides a dedicated SQL database layer for the learning engine.
//
// Unlike the bucket-based state.Store (which serializes objects as JSON blobs),
// flowdb uses native SQL tables optimized for high-volume flow data with:
//   - Indexed queries by state, MAC address, port, time ranges
//   - SQL aggregations for statistics
//   - Unique constraints for device+service fingerprinting
//   - Foreign keys for domain hint relationships
//
// This separation allows the learning engine to handle thousands of flows
// efficiently while the rest of the system uses the simpler bucket pattern.
package flowdb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"

	_ "modernc.org/sqlite"
)

// FlowState represents the triage state of a learned flow
type FlowState string

const (
	StatePending FlowState = "pending" // Awaiting triage
	StateAllowed FlowState = "allowed" // Permitted
	StateDenied  FlowState = "denied"  // Blocked
)

// HintSource represents how a domain hint was discovered
type HintSource string

const (
	SourceDNSSnoop HintSource = "dns_snoop" // DNS query monitoring
	SourceSNIPeek  HintSource = "sni_peek"  // TLS SNI inspection
	SourceReverse  HintSource = "rdns"      // Reverse DNS lookup
)

// Flow represents a learned network flow (device+service fingerprint)
type Flow struct {
	ID                 int64     `json:"id"`
	SrcMAC             string    `json:"src_mac"`
	SrcIP              string    `json:"src_ip,omitempty"`
	SrcHostname        string    `json:"src_hostname,omitempty"`
	Protocol           string    `json:"protocol"`
	DstPort            int       `json:"dst_port"`
	DstIPSample        string    `json:"dst_ip_sample,omitempty"`
	Policy             string    `json:"policy,omitempty"` // Policy that triggered the learning
	State              FlowState `json:"state"`
	Scrutiny           bool      `json:"scrutiny"`       // Extra logging, review reminder
	ScrutinyUntil      time.Time `json:"scrutiny_until"` // When to remind for review
	LearningModeActive bool      `json:"learning_mode_active"`
	FirstSeen          time.Time `json:"first_seen"`
	LastSeen           time.Time `json:"last_seen"`
	Occurrences        int       `json:"occurrences"`
	App                string    `json:"app,omitempty"`       // Identified Application (e.g. Netflix)
	Vendor             string    `json:"vendor,omitempty"`    // Device Vendor (e.g. Apple)
	DeviceID           string    `json:"device_id,omitempty"` // ID of the linked device identity (ephemeral)
}

// DomainHint represents DNS context for a flow
type DomainHint struct {
	ID         int64      `json:"id"`
	FlowID     int64      `json:"flow_id"`
	Domain     string     `json:"domain"`
	Confidence int        `json:"confidence"` // 100=SNI, 80=DNS, 20=rDNS
	Source     HintSource `json:"source"`
	DetectedAt time.Time  `json:"detected_at"`
}

// FlowWithHints combines a flow with its domain hints
type FlowWithHints struct {
	Flow
	DomainHints []DomainHint `json:"domain_hints,omitempty"`
	BestHint    string       `json:"best_hint,omitempty"` // Highest confidence domain
}

// Stats holds aggregated flow statistics
type Stats struct {
	TotalFlows       int64 `json:"total_flows"`
	PendingFlows     int64 `json:"pending_flows"`
	AllowedFlows     int64 `json:"allowed_flows"`
	DeniedFlows      int64 `json:"denied_flows"`
	ScrutinyFlows    int64 `json:"scrutiny_flows"`
	TotalOccurrences int64 `json:"total_occurrences"`
	TotalDomainHints int64 `json:"total_domain_hints"`
}

// DB is the flow database interface
type DB struct {
	db     *sql.DB
	logger *logging.Logger
}

// Open opens or creates a flow database at the given path.
// Use ":memory:" for an in-memory database.
func Open(path string, logger *logging.Logger) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if logger == nil {
		logger = logging.New(logging.DefaultConfig())
	}

	fdb := &DB{db: db, logger: logger}
	if err := fdb.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return fdb, nil
}

// OpenWithDB wraps an existing database connection
func OpenWithDB(db *sql.DB, logger *logging.Logger) (*DB, error) {
	if logger == nil {
		logger = logging.New(logging.DefaultConfig())
	}
	fdb := &DB{db: db, logger: logger}
	if err := fdb.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	return fdb, nil
}

// Close closes the database connection
func (fdb *DB) Close() error {
	return fdb.db.Close()
}

// initSchema creates the flow tables if they don't exist
func (fdb *DB) initSchema() error {
	schema := `
		-- Learned flows: device+service fingerprints
		CREATE TABLE IF NOT EXISTS learned_flows (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			src_mac TEXT NOT NULL,
			src_ip TEXT,
			src_hostname TEXT,
			proto TEXT NOT NULL,
			dst_port INTEGER NOT NULL,
			dst_ip_sample TEXT,
			policy TEXT,
			state TEXT DEFAULT 'pending',
			scrutiny BOOLEAN DEFAULT 0,
			scrutiny_until DATETIME,
			learning_mode_active BOOLEAN DEFAULT 0,
			first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
			occurrences INTEGER DEFAULT 1,
			app TEXT,
			vendor TEXT,
			UNIQUE(src_mac, proto, dst_port)
		);

		-- Domain hints: DNS context for flows
		CREATE TABLE IF NOT EXISTS flow_domain_hints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			flow_id INTEGER NOT NULL,
			domain TEXT NOT NULL,
			confidence INTEGER DEFAULT 50,
			source TEXT DEFAULT 'unknown',
			detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(flow_id) REFERENCES learned_flows(id) ON DELETE CASCADE
		);

		-- Change log for HA replication (mirrors state.Store pattern)
		CREATE TABLE IF NOT EXISTS flow_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			table_name TEXT NOT NULL,        -- 'flows' or 'hints'
			row_id INTEGER NOT NULL,         -- ID of affected row
			change_type TEXT NOT NULL,       -- 'insert', 'update', 'delete'
			changed_fields TEXT,             -- JSON of changed fields (for partial sync)
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			version INTEGER NOT NULL         -- Monotonic version for ordering
		);

		-- Replication metadata
		CREATE TABLE IF NOT EXISTS flow_replication_meta (
			key TEXT PRIMARY KEY,
			value TEXT
		);

		-- Indexes for common queries
		CREATE INDEX IF NOT EXISTS idx_flows_state ON learned_flows(state);
		CREATE INDEX IF NOT EXISTS idx_flows_mac ON learned_flows(src_mac);
		CREATE INDEX IF NOT EXISTS idx_flows_last_seen ON learned_flows(last_seen);
		CREATE INDEX IF NOT EXISTS idx_flows_fingerprint ON learned_flows(src_mac, proto, dst_port);
		CREATE INDEX IF NOT EXISTS idx_hints_flow ON flow_domain_hints(flow_id);
		CREATE INDEX IF NOT EXISTS idx_hints_domain ON flow_domain_hints(domain);
		CREATE INDEX IF NOT EXISTS idx_changes_version ON flow_changes(version);
		CREATE INDEX IF NOT EXISTS idx_changes_timestamp ON flow_changes(timestamp);
	`

	_, err := fdb.db.Exec(schema)
	if err != nil {
		return err
	}

	// Initialize version counter if not exists
	_, err = fdb.db.Exec(`
		INSERT OR IGNORE INTO flow_replication_meta (key, value) VALUES ('version', '0')
	`)
	if err != nil {
		return err
	}

	// Schema Migrations (Idempotent)
	// Add app column if missing
	if _, err := fdb.db.Exec("ALTER TABLE learned_flows ADD COLUMN app TEXT"); err != nil {
		// Ignore "duplicate column" error, but log others if needed for debug
		// strings.Contains(err.Error(), "duplicate column") ...
	}
	// Add vendor column if missing
	if _, err := fdb.db.Exec("ALTER TABLE learned_flows ADD COLUMN vendor TEXT"); err != nil {
		// Ignore
	}
	// Add policy column if missing
	if _, err := fdb.db.Exec("ALTER TABLE learned_flows ADD COLUMN policy TEXT"); err != nil {
		// Ignore
	}

	return nil
}

// --- Flow CRUD Operations ---

// UpsertFlow inserts a new flow or updates an existing one (by fingerprint)
func (fdb *DB) UpsertFlow(f *Flow) error {
	if f.FirstSeen.IsZero() {
		f.FirstSeen = clock.Now()
	}
	f.LastSeen = clock.Now()
	if f.Occurrences == 0 {
		f.Occurrences = 1
	}

	query := `
		INSERT INTO learned_flows
			(src_mac, src_ip, src_hostname, proto, dst_port, dst_ip_sample,
			 state, learning_mode_active, first_seen, last_seen, occurrences, app, vendor, policy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(src_mac, proto, dst_port) DO UPDATE SET
			src_ip = excluded.src_ip,
			src_hostname = COALESCE(excluded.src_hostname, src_hostname),
			dst_ip_sample = excluded.dst_ip_sample,
			last_seen = excluded.last_seen,
			occurrences = occurrences + 1,
			app = COALESCE(excluded.app, app),
			vendor = COALESCE(excluded.vendor, vendor),
			policy = COALESCE(excluded.policy, policy)
		RETURNING id, first_seen, occurrences
	`

	err := fdb.db.QueryRow(query,
		f.SrcMAC, f.SrcIP, f.SrcHostname, f.Protocol, f.DstPort, f.DstIPSample,
		f.State, f.LearningModeActive, f.FirstSeen, f.LastSeen, f.Occurrences, f.App, f.Vendor, f.Policy,
	).Scan(&f.ID, &f.FirstSeen, &f.Occurrences)

	return err
}

// GetFlow retrieves a flow by ID
func (fdb *DB) GetFlow(id int64) (*Flow, error) {
	query := `
		SELECT id, src_mac, src_ip, src_hostname, proto, dst_port, dst_ip_sample,
		       state, learning_mode_active, first_seen, last_seen, occurrences, app, vendor, policy
		FROM learned_flows WHERE id = ?
	`

	f := &Flow{}
	var app, vendor, policy sql.NullString
	err := fdb.db.QueryRow(query, id).Scan(
		&f.ID, &f.SrcMAC, &f.SrcIP, &f.SrcHostname, &f.Protocol, &f.DstPort,
		&f.DstIPSample, &f.State, &f.LearningModeActive, &f.FirstSeen,
		&f.LastSeen, &f.Occurrences, &app, &vendor, &policy,
	)
	f.App = app.String
	f.Vendor = vendor.String
	f.Policy = policy.String
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

// FindFlow finds a flow by its fingerprint (MAC + protocol + port)
func (fdb *DB) FindFlow(srcMAC, protocol string, dstPort int) (*Flow, error) {
	query := `
		SELECT id, src_mac, src_ip, src_hostname, proto, dst_port, dst_ip_sample,
		       state, learning_mode_active, first_seen, last_seen, occurrences, app, vendor, policy
		FROM learned_flows WHERE src_mac = ? AND proto = ? AND dst_port = ?
	`

	f := &Flow{}
	var app, vendor, policy sql.NullString
	err := fdb.db.QueryRow(query, srcMAC, protocol, dstPort).Scan(
		&f.ID, &f.SrcMAC, &f.SrcIP, &f.SrcHostname, &f.Protocol, &f.DstPort,
		&f.DstIPSample, &f.State, &f.LearningModeActive, &f.FirstSeen,
		&f.LastSeen, &f.Occurrences, &app, &vendor, &policy,
	)
	f.App = app.String
	f.Vendor = vendor.String
	f.Policy = policy.String
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

// ListFlows returns flows with optional filtering
type ListOptions struct {
	State   FlowState // Filter by state (empty = all)
	SrcMAC  string    // Filter by source MAC
	Limit   int       // Max results (0 = no limit)
	Offset  int       // Pagination offset
	OrderBy string    // "last_seen", "first_seen", "occurrences"
	Desc    bool      // Descending order
}

func (fdb *DB) ListFlows(opts ListOptions) ([]Flow, error) {
	query := `
		SELECT id, src_mac, src_ip, src_hostname, proto, dst_port, dst_ip_sample,
		       state, scrutiny, scrutiny_until, learning_mode_active, first_seen, last_seen, occurrences, app, vendor, policy
		FROM learned_flows
	`

	var conditions []string
	var args []interface{}

	if opts.State != "" {
		conditions = append(conditions, "state = ?")
		args = append(args, opts.State)
	}
	if opts.SrcMAC != "" {
		conditions = append(conditions, "src_mac = ?")
		args = append(args, opts.SrcMAC)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Order
	orderCol := "last_seen"
	if opts.OrderBy != "" {
		switch opts.OrderBy {
		case "first_seen", "last_seen", "occurrences":
			orderCol = opts.OrderBy
		}
	}
	if opts.Desc {
		query += " ORDER BY " + orderCol + " DESC"
	} else {
		query += " ORDER BY " + orderCol + " ASC"
	}

	// Pagination
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
		if opts.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, opts.Offset)
		}
	}

	rows, err := fdb.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flows []Flow
	for rows.Next() {
		var f Flow
		var scrutinyUntil sql.NullTime
		var app, vendor, policy sql.NullString
		err := rows.Scan(
			&f.ID, &f.SrcMAC, &f.SrcIP, &f.SrcHostname, &f.Protocol, &f.DstPort,
			&f.DstIPSample, &f.State, &f.Scrutiny, &scrutinyUntil, &f.LearningModeActive,
			&f.FirstSeen, &f.LastSeen, &f.Occurrences, &app, &vendor, &policy,
		)
		if err != nil {
			return nil, err
		}
		if scrutinyUntil.Valid {
			f.ScrutinyUntil = scrutinyUntil.Time
		}
		f.App = app.String
		f.Vendor = vendor.String
		f.Policy = policy.String
		flows = append(flows, f)
	}

	return flows, rows.Err()
}

// UpdateState changes a flow's state
func (fdb *DB) UpdateState(id int64, state FlowState) error {
	result, err := fdb.db.Exec("UPDATE learned_flows SET state = ? WHERE id = ?", state, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteFlow removes a flow and its hints
func (fdb *DB) DeleteFlow(id int64) error {
	// Hints are deleted via CASCADE
	result, err := fdb.db.Exec("DELETE FROM learned_flows WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Domain Hint Operations ---

// AddHint adds a domain hint to a flow
func (fdb *DB) AddHint(h *DomainHint) error {
	if h.DetectedAt.IsZero() {
		h.DetectedAt = clock.Now()
	}

	result, err := fdb.db.Exec(`
		INSERT INTO flow_domain_hints (flow_id, domain, confidence, source, detected_at)
		VALUES (?, ?, ?, ?, ?)
	`, h.FlowID, h.Domain, h.Confidence, h.Source, h.DetectedAt)
	if err != nil {
		return err
	}

	h.ID, _ = result.LastInsertId()
	return nil
}

// GetHints returns all hints for a flow, ordered by confidence
func (fdb *DB) GetHints(flowID int64) ([]DomainHint, error) {
	rows, err := fdb.db.Query(`
		SELECT id, flow_id, domain, confidence, source, detected_at
		FROM flow_domain_hints WHERE flow_id = ?
		ORDER BY confidence DESC, detected_at DESC
	`, flowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hints []DomainHint
	for rows.Next() {
		var h DomainHint
		if err := rows.Scan(&h.ID, &h.FlowID, &h.Domain, &h.Confidence, &h.Source, &h.DetectedAt); err != nil {
			return nil, err
		}
		hints = append(hints, h)
	}

	return hints, rows.Err()
}

// GetBestHint returns the highest-confidence domain for a flow
func (fdb *DB) GetBestHint(flowID int64) (string, error) {
	var domain string
	err := fdb.db.QueryRow(`
		SELECT domain FROM flow_domain_hints
		WHERE flow_id = ? ORDER BY confidence DESC LIMIT 1
	`, flowID).Scan(&domain)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return domain, err
}

// --- Aggregated Queries ---

// GetFlowWithHints returns a flow with all its domain hints
func (fdb *DB) GetFlowWithHints(id int64) (*FlowWithHints, error) {
	flow, err := fdb.GetFlow(id)
	if err != nil || flow == nil {
		return nil, err
	}

	hints, err := fdb.GetHints(id)
	if err != nil {
		return nil, err
	}

	fwh := &FlowWithHints{Flow: *flow, DomainHints: hints}
	if len(hints) > 0 {
		fwh.BestHint = hints[0].Domain
	}

	return fwh, nil
}

// ListFlowsWithHints returns flows with their best domain hint
func (fdb *DB) ListFlowsWithHints(opts ListOptions) ([]FlowWithHints, error) {
	flows, err := fdb.ListFlows(opts)
	if err != nil {
		return nil, err
	}

	result := make([]FlowWithHints, len(flows))
	for i, f := range flows {
		result[i] = FlowWithHints{Flow: f}
		result[i].BestHint, _ = fdb.GetBestHint(f.ID)
	}

	return result, nil
}

// GetStats returns aggregated statistics
func (fdb *DB) GetStats() (*Stats, error) {
	stats := &Stats{}

	// Count by state
	rows, err := fdb.db.Query(`
		SELECT state, COUNT(*), SUM(occurrences)
		FROM learned_flows GROUP BY state
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var state string
		var count, occurrences int64
		if err := rows.Scan(&state, &count, &occurrences); err != nil {
			return nil, err
		}
		stats.TotalFlows += count
		stats.TotalOccurrences += occurrences
		switch FlowState(state) {
		case StatePending:
			stats.PendingFlows = count
		case StateAllowed:
			stats.AllowedFlows = count
		case StateDenied:
			stats.DeniedFlows = count
		}
	}

	// Count hints
	fdb.db.QueryRow("SELECT COUNT(*) FROM flow_domain_hints").Scan(&stats.TotalDomainHints)

	// Count scrutiny flows
	fdb.db.QueryRow("SELECT COUNT(*) FROM learned_flows WHERE scrutiny = 1").Scan(&stats.ScrutinyFlows)

	return stats, nil
}

// --- Maintenance ---

// Cleanup removes old pending flows based on retention policy
func (fdb *DB) Cleanup(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = 30
	}

	cutoff := clock.Now().AddDate(0, 0, -retentionDays)

	// Only delete pending flows; keep allowed/denied as they represent user decisions
	result, err := fdb.db.Exec(`
		DELETE FROM learned_flows
		WHERE state = 'pending' AND last_seen < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// BulkUpdateState updates state for multiple flows
func (fdb *DB) BulkUpdateState(ids []int64, state FlowState) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = state
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := fmt.Sprintf(
		"UPDATE learned_flows SET state = ? WHERE id IN (%s)",
		strings.Join(placeholders, ","),
	)

	_, err := fdb.db.Exec(query, args...)
	return err
}

// AllowAllPending changes all pending flows to allowed
func (fdb *DB) AllowAllPending() (int64, error) {
	result, err := fdb.db.Exec("UPDATE learned_flows SET state = 'allowed' WHERE state = 'pending'")
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SetScrutiny enables or disables scrutiny for a flow with optional review reminder
func (fdb *DB) SetScrutiny(id int64, enabled bool, reviewAfter time.Duration) error {
	var scrutinyUntil interface{}
	if enabled && reviewAfter > 0 {
		scrutinyUntil = clock.Now().Add(reviewAfter)
	}
	_, err := fdb.db.Exec(`
		UPDATE learned_flows SET scrutiny = ?, scrutiny_until = ? WHERE id = ?
	`, enabled, scrutinyUntil, id)
	return err
}

// GetScrutinyDue returns flows with scrutiny enabled whose review time has passed
func (fdb *DB) GetScrutinyDue() ([]Flow, error) {
	rows, err := fdb.db.Query(`
		SELECT id, src_mac, src_ip, src_hostname, proto, dst_port, dst_ip_sample,
		       state, scrutiny, scrutiny_until, learning_mode_active, first_seen, last_seen, occurrences, app, vendor, policy
		FROM learned_flows
		WHERE scrutiny = 1 AND scrutiny_until IS NOT NULL AND scrutiny_until < ?
		ORDER BY scrutiny_until ASC
	`, clock.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flows []Flow
	for rows.Next() {
		var f Flow
		var scrutinyUntil sql.NullTime
		var app, vendor, policy sql.NullString
		err := rows.Scan(
			&f.ID, &f.SrcMAC, &f.SrcIP, &f.SrcHostname, &f.Protocol, &f.DstPort,
			&f.DstIPSample, &f.State, &f.Scrutiny, &scrutinyUntil, &f.LearningModeActive,
			&f.FirstSeen, &f.LastSeen, &f.Occurrences, &app, &vendor, &policy,
		)
		if err != nil {
			return nil, err
		}
		if scrutinyUntil.Valid {
			f.ScrutinyUntil = scrutinyUntil.Time
		}
		f.App = app.String
		f.Vendor = vendor.String
		flows = append(flows, f)
	}
	return flows, rows.Err()
}

// --- Replication Support ---

// Change represents a single change for replication
type Change struct {
	ID            int64     `json:"id"`
	TableName     string    `json:"table_name"` // "flows" or "hints"
	RowID         int64     `json:"row_id"`
	ChangeType    string    `json:"change_type"`    // "insert", "update", "delete"
	ChangedFields string    `json:"changed_fields"` // JSON of changed fields
	Timestamp     time.Time `json:"timestamp"`
	Version       int64     `json:"version"`
}

// logChange records a change for replication
func (fdb *DB) logChange(tableName string, rowID int64, changeType string, changedFields string) error {
	// Get and increment version atomically
	var version int64
	err := fdb.db.QueryRow(`
		UPDATE flow_replication_meta SET value = CAST(value AS INTEGER) + 1
		WHERE key = 'version' RETURNING CAST(value AS INTEGER)
	`).Scan(&version)
	if err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	}

	_, err = fdb.db.Exec(`
		INSERT INTO flow_changes (table_name, row_id, change_type, changed_fields, version)
		VALUES (?, ?, ?, ?, ?)
	`, tableName, rowID, changeType, changedFields, version)

	return err
}

// GetVersion returns the current replication version
func (fdb *DB) GetVersion() (int64, error) {
	var version int64
	err := fdb.db.QueryRow(`
		SELECT CAST(value AS INTEGER) FROM flow_replication_meta WHERE key = 'version'
	`).Scan(&version)
	return version, err
}

// GetChangesSince returns all changes since a given version
func (fdb *DB) GetChangesSince(sinceVersion int64) ([]Change, error) {
	rows, err := fdb.db.Query(`
		SELECT id, table_name, row_id, change_type, changed_fields, timestamp, version
		FROM flow_changes WHERE version > ?
		ORDER BY version ASC
	`, sinceVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []Change
	for rows.Next() {
		var c Change
		var changedFields sql.NullString
		err := rows.Scan(&c.ID, &c.TableName, &c.RowID, &c.ChangeType, &changedFields, &c.Timestamp, &c.Version)
		if err != nil {
			return nil, err
		}
		c.ChangedFields = changedFields.String
		changes = append(changes, c)
	}

	return changes, rows.Err()
}

// ApplyChange applies a replicated change from a partner instance
func (fdb *DB) ApplyChange(c Change) error {
	switch c.TableName {
	case "flows":
		return fdb.applyFlowChange(c)
	case "hints":
		return fdb.applyHintChange(c)
	default:
		return fmt.Errorf("unknown table: %s", c.TableName)
	}
}

func (fdb *DB) applyFlowChange(c Change) error {
	switch c.ChangeType {
	case "delete":
		_, err := fdb.db.Exec("DELETE FROM learned_flows WHERE id = ?", c.RowID)
		return err
	case "insert", "update":
		// For insert/update, we need the full flow data
		// The partner should send the flow in ChangedFields as JSON
		if c.ChangedFields == "" {
			return fmt.Errorf("missing flow data for %s", c.ChangeType)
		}
		var f Flow
		if err := json.Unmarshal([]byte(c.ChangedFields), &f); err != nil {
			return fmt.Errorf("failed to unmarshal flow: %w", err)
		}
		f.ID = c.RowID
		return fdb.UpsertFlowWithID(&f)
	default:
		return fmt.Errorf("unknown change type: %s", c.ChangeType)
	}
}

func (fdb *DB) applyHintChange(c Change) error {
	switch c.ChangeType {
	case "delete":
		_, err := fdb.db.Exec("DELETE FROM flow_domain_hints WHERE id = ?", c.RowID)
		return err
	case "insert":
		if c.ChangedFields == "" {
			return fmt.Errorf("missing hint data for insert")
		}
		var h DomainHint
		if err := json.Unmarshal([]byte(c.ChangedFields), &h); err != nil {
			return fmt.Errorf("failed to unmarshal hint: %w", err)
		}
		h.ID = c.RowID
		return fdb.insertHintWithID(&h)
	default:
		return fmt.Errorf("unknown change type for hints: %s", c.ChangeType)
	}
}

// UpsertFlowWithID inserts or updates a flow with a specific ID (for replication)
func (fdb *DB) UpsertFlowWithID(f *Flow) error {
	_, err := fdb.db.Exec(`
		INSERT OR REPLACE INTO learned_flows
			(id, src_mac, src_ip, src_hostname, proto, dst_port, dst_ip_sample,
			 state, learning_mode_active, first_seen, last_seen, occurrences, app, vendor, policy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, f.ID, f.SrcMAC, f.SrcIP, f.SrcHostname, f.Protocol, f.DstPort, f.DstIPSample,
		f.State, f.LearningModeActive, f.FirstSeen, f.LastSeen, f.Occurrences, f.App, f.Vendor, f.Policy)
	return err
}

// insertHintWithID inserts a hint with a specific ID (for replication)
func (fdb *DB) insertHintWithID(h *DomainHint) error {
	_, err := fdb.db.Exec(`
		INSERT OR REPLACE INTO flow_domain_hints (id, flow_id, domain, confidence, source, detected_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, h.ID, h.FlowID, h.Domain, h.Confidence, h.Source, h.DetectedAt)
	return err
}

// PruneChanges removes old change log entries
func (fdb *DB) PruneChanges(keepDays int) (int64, error) {
	if keepDays <= 0 {
		keepDays = 7
	}
	cutoff := clock.Now().AddDate(0, 0, -keepDays)
	result, err := fdb.db.Exec("DELETE FROM flow_changes WHERE timestamp < ?", cutoff)
	if err != nil {
		return 0, err
	}

	// Optimize storage space (VACUUM)
	// This addresses the "Disk Exhaustion" risk by compacting the SQLite file.
	if _, err := fdb.db.Exec("VACUUM"); err != nil {
		// Log warning but return success for the pruning itself
		fdb.logger.Warn("failed to VACUUM flowdb", "error", err)
	}

	return result.RowsAffected()
}

// ExportSnapshot exports all flows and hints for full sync
func (fdb *DB) ExportSnapshot() (*Snapshot, error) {
	version, err := fdb.GetVersion()
	if err != nil {
		return nil, err
	}

	flows, err := fdb.ListFlows(ListOptions{})
	if err != nil {
		return nil, err
	}

	// Get all hints
	rows, err := fdb.db.Query(`
		SELECT id, flow_id, domain, confidence, source, detected_at
		FROM flow_domain_hints ORDER BY flow_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hints []DomainHint
	for rows.Next() {
		var h DomainHint
		if err := rows.Scan(&h.ID, &h.FlowID, &h.Domain, &h.Confidence, &h.Source, &h.DetectedAt); err != nil {
			return nil, err
		}
		hints = append(hints, h)
	}

	return &Snapshot{
		Version:   version,
		Timestamp: clock.Now(),
		Flows:     flows,
		Hints:     hints,
	}, nil
}

// Snapshot represents a point-in-time export for full sync
type Snapshot struct {
	Version   int64        `json:"version"`
	Timestamp time.Time    `json:"timestamp"`
	Flows     []Flow       `json:"flows"`
	Hints     []DomainHint `json:"hints"`
}

// ImportSnapshot imports a full snapshot (replaces all data)
func (fdb *DB) ImportSnapshot(snap *Snapshot) error {
	tx, err := fdb.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing data
	if _, err := tx.Exec("DELETE FROM flow_domain_hints"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM learned_flows"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM flow_changes"); err != nil {
		return err
	}

	// Import flows
	for _, f := range snap.Flows {
		_, err := tx.Exec(`
			INSERT OR REPLACE INTO learned_flows
			(id, src_mac, src_ip, src_hostname, proto, dst_port, dst_ip_sample,
			 state, learning_mode_active, first_seen, last_seen, occurrences, app, vendor)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, f.ID, f.SrcMAC, f.SrcIP, f.SrcHostname, f.Protocol, f.DstPort, f.DstIPSample,
			f.State, f.LearningModeActive, f.FirstSeen, f.LastSeen, f.Occurrences, f.App, f.Vendor)
		if err != nil {
			return fmt.Errorf("failed to import flow %d: %w", f.ID, err)
		}
	}

	// Import hints
	for _, h := range snap.Hints {
		_, err := tx.Exec(`
			INSERT INTO flow_domain_hints (id, flow_id, domain, confidence, source, detected_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, h.ID, h.FlowID, h.Domain, h.Confidence, h.Source, h.DetectedAt)
		if err != nil {
			return fmt.Errorf("failed to import hint %d: %w", h.ID, err)
		}
	}

	// Update version
	_, err = tx.Exec(`
		UPDATE flow_replication_meta SET value = ? WHERE key = 'version'
	`, snap.Version)
	if err != nil {
		return err
	}

	return tx.Commit()
}
