// Package state provides an abstract state storage system with change tracking.
//
// The state store provides:
// - Persistent storage via SQLite with WAL mode for performance
// - Automatic change tracking for replication (HA) and upgrades
// - Typed buckets for different services (DHCP, DNS, sessions, etc.)
// - Real-time change streams for subscribers
// - Snapshot and restore for backup/upgrade scenarios
//
// Services interact with the store without knowing about replication details.
// The store handles serialization, persistence, and change propagation.
//
// SQLite Driver Selection:
// - Default: github.com/mattn/go-sqlite3 (CGO, best performance)
// - Embedded: modernc.org/sqlite (pure Go, no CGO, for cross-compilation)
//
// To build without CGO for embedded systems, use build tags:
//
//	go build -tags sqlite_purego ...
package state

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"strings"
	"sync"
	"time"

	sqlite "modernc.org/sqlite" // Pure Go SQLite driver
)

// init registers custom time functions that use clock.Now() instead of
// system time. This ensures SQLite's time functions respect our time anchoring.
func init() {
	// Override datetime('now', ...) to use clock.Now()
	_ = sqlite.RegisterScalarFunction("datetime", -1, datetimeFunc)

	// Override strftime('%format', 'now', ...) to use clock.Now()
	_ = sqlite.RegisterScalarFunction("strftime", -1, strftimeFunc)

	// Override date('now', ...) to use clock.Now()
	_ = sqlite.RegisterScalarFunction("date", -1, dateFunc)

	// Override time('now', ...) to use clock.Now()
	_ = sqlite.RegisterScalarFunction("time", -1, timeFunc)

	// Override julianday('now', ...) to use clock.Now()
	_ = sqlite.RegisterScalarFunction("julianday", -1, juliandayFunc)

	// CURRENT_TIMESTAMP, CURRENT_DATE, CURRENT_TIME are keywords, not functions,
	// so they cannot be overridden via RegisterScalarFunction.
	// We removed DEFAULT CURRENT_TIMESTAMP from schema and use explicit clock.Now() instead.
}

// datetimeFunc implements datetime() using clock.Now()
func datetimeFunc(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if len(args) == 0 {
		return clock.Now().UTC().Format("2006-01-02 15:04:05"), nil
	}

	// Check if first arg is 'now'
	if s, ok := args[0].(string); ok && strings.ToLower(s) == "now" {
		t := clock.Now().UTC()
		// Handle modifiers like 'localtime'
		for _, arg := range args[1:] {
			if mod, ok := arg.(string); ok {
				switch strings.ToLower(mod) {
				case "localtime":
					t = t.Local()
				case "utc":
					t = t.UTC()
				}
			}
		}
		return t.Format("2006-01-02 15:04:05"), nil
	}

	// For other inputs, return as-is (let SQLite handle parsing)
	return args[0], nil
}

// strftimeFunc implements strftime() using clock.Now() for 'now'
func strftimeFunc(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if len(args) < 2 {
		return nil, errors.New("strftime requires at least 2 arguments")
	}

	format, ok := args[0].(string)
	if !ok {
		return nil, errors.New("strftime format must be a string")
	}

	// Check if second arg is 'now'
	if s, ok := args[1].(string); ok && strings.ToLower(s) == "now" {
		t := clock.Now().UTC()
		// SQLite format codes to Go time format (common ones)
		goFormat := sqliteToGoFormat(format)
		return t.Format(goFormat), nil
	}

	// For other inputs, return empty (let caller handle)
	return "", nil
}

// sqliteToGoFormat converts SQLite strftime format to Go time format
func sqliteToGoFormat(sqliteFormat string) string {
	replacer := strings.NewReplacer(
		"%Y", "2006",
		"%m", "01",
		"%d", "02",
		"%H", "15",
		"%M", "04",
		"%S", "05",
		"%f", "000000",
		"%s", "", // Unix timestamp - handled separately
		"%w", "", // Day of week
		"%j", "", // Day of year
		"%W", "", // Week of year
	)
	return replacer.Replace(sqliteFormat)
}

// dateFunc implements date() using clock.Now()
func dateFunc(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if len(args) == 0 {
		return clock.Now().UTC().Format("2006-01-02"), nil
	}

	// Check if first arg is 'now'
	if s, ok := args[0].(string); ok && strings.ToLower(s) == "now" {
		return clock.Now().UTC().Format("2006-01-02"), nil
	}

	// For other inputs, return as-is
	return args[0], nil
}

// timeFunc implements time() using clock.Now()
func timeFunc(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if len(args) == 0 {
		return clock.Now().UTC().Format("15:04:05"), nil
	}

	// Check if first arg is 'now'
	if s, ok := args[0].(string); ok && strings.ToLower(s) == "now" {
		return clock.Now().UTC().Format("15:04:05"), nil
	}

	// For other inputs, return as-is
	return args[0], nil
}

// juliandayFunc implements julianday() using clock.Now()
func juliandayFunc(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if len(args) == 0 {
		return toJulianDay(clock.Now()), nil
	}

	// Check if first arg is 'now'
	if s, ok := args[0].(string); ok && strings.ToLower(s) == "now" {
		return toJulianDay(clock.Now()), nil
	}

	// For other inputs, return nil (let SQLite handle)
	return nil, nil
}

// toJulianDay converts a time.Time to Julian day number
func toJulianDay(t time.Time) float64 {
	// Julian day algorithm
	year, month, day := t.Date()
	hour, min, sec := t.Clock()
	nsec := t.Nanosecond()

	if month <= 2 {
		year--
		month += 12
	}

	a := year / 100
	b := 2 - a + a/4

	jd := float64(int(365.25*float64(year+4716))) +
		float64(int(30.6001*float64(month+1))) +
		float64(day) + float64(b) - 1524.5 +
		float64(hour)/24.0 + float64(min)/1440.0 +
		float64(sec)/86400.0 + float64(nsec)/86400000000000.0

	return jd
}

// Common errors
var (
	ErrNotFound      = errors.New("key not found")
	ErrBucketExists  = errors.New("bucket already exists")
	ErrBucketMissing = errors.New("bucket does not exist")
	ErrStoreClosed   = errors.New("store is closed")
)

// ChangeType represents the type of state change.
type ChangeType string

const (
	ChangeInsert ChangeType = "insert"
	ChangeUpdate ChangeType = "update"
	ChangeDelete ChangeType = "delete"
)

// Change represents a single state change for replication.
type Change struct {
	ID        uint64     `json:"id"`
	Bucket    string     `json:"bucket"`
	Key       string     `json:"key"`
	Value     []byte     `json:"value,omitempty"` // nil for deletes
	Type      ChangeType `json:"type"`
	Timestamp time.Time  `json:"timestamp"`
	Version   uint64     `json:"version"` // Monotonic version for conflict resolution
}

// Snapshot represents a point-in-time snapshot of the entire store.
type Snapshot struct {
	Version   uint64            `json:"version"`
	Timestamp time.Time         `json:"timestamp"`
	Buckets   map[string]Bucket `json:"buckets"`
}

// Bucket represents a collection of key-value pairs.
type Bucket map[string]Entry

// Entry represents a single stored value with metadata.
type Entry struct {
	Value     []byte    `json:"value"`
	Version   uint64    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"` // Zero means no expiry
}

// Store is the main state storage interface.
type Store interface {
	// Bucket operations
	CreateBucket(name string) error
	DeleteBucket(name string) error
	ListBuckets() ([]string, error)

	// Key-value operations
	Get(bucket, key string) ([]byte, error)
	GetWithMeta(bucket, key string) (*Entry, error)
	Set(bucket, key string, value []byte) error
	SetWithTTL(bucket, key string, value []byte, ttl time.Duration) error
	Delete(bucket, key string) error
	List(bucket string) (map[string][]byte, error)
	ListKeys(bucket string) ([]string, error)

	// Typed helpers
	GetJSON(bucket, key string, v interface{}) error
	SetJSON(bucket, key string, v interface{}) error
	SetJSONWithTTL(bucket, key string, v interface{}, ttl time.Duration) error

	// Change tracking
	Subscribe(ctx context.Context) <-chan Change
	GetChangesSince(version uint64) ([]Change, error)
	CurrentVersion() uint64

	// Snapshot operations
	CreateSnapshot() (*Snapshot, error)
	RestoreSnapshot(snapshot *Snapshot) error

	// Lifecycle
	Close() error
}

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db      *sql.DB
	mu      sync.RWMutex
	version uint64
	closed  bool
	clock   clock.Clock // Time source for testability

	// Change subscribers
	subMu       sync.RWMutex
	subscribers map[uint64]chan Change
	nextSubID   uint64

	// Background cleanup
	ctx    context.Context
	cancel context.CancelFunc

	// OnWrite is called after each successful write operation.
	// Used to trigger clock anchor updates without circular imports.
	OnWrite func()
}

// Options configures the SQLite store.
type Options struct {
	Path            string        // Database file path (":memory:" for in-memory)
	WALMode         bool          // Enable WAL mode for better concurrency
	CleanupInterval time.Duration // How often to clean expired entries
	ChangeRetention time.Duration // How long to keep change history
	Clock           clock.Clock   // Optional: time source (defaults to RealClock if nil)
}

// DefaultOptions returns sensible defaults.
func DefaultOptions(path string) Options {
	return Options{
		Path:            path,
		WALMode:         true,
		CleanupInterval: 5 * time.Minute,
		ChangeRetention: 24 * time.Hour,
	}
}

// NewSQLiteStore creates a new SQLite-backed state store.
func NewSQLiteStore(opts Options) (*SQLiteStore, error) {
	// Open database with appropriate flags
	dsn := opts.Path
	if opts.WALMode && opts.Path != ":memory:" {
		dsn += "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Apply performance PRAGMAs
	// mmap_size: Memory map the DB (up to 256MB) for zero-copy reads
	// temp_store: Keep temporary tables/indices in RAM
	pragmas := []string{
		"PRAGMA mmap_size = 268435456",
		"PRAGMA temp_store = MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to execute pragma %q: %w", p, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Use provided clock or default to RealClock
	clk := opts.Clock
	if clk == nil {
		clk = &clock.RealClock{}
	}

	s := &SQLiteStore{
		db:          db,
		clock:       clk,
		subscribers: make(map[uint64]chan Change),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize schema
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Load current version
	if err := s.loadVersion(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to load version: %w", err)
	}

	// Start background cleanup
	if opts.CleanupInterval > 0 {
		go s.cleanupLoop(opts.CleanupInterval, opts.ChangeRetention)
	}

	return s, nil
}

// initSchema creates the database tables.
func (s *SQLiteStore) initSchema() error {
	schema := `
		-- Buckets table
		CREATE TABLE IF NOT EXISTS buckets (
			name TEXT PRIMARY KEY,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Key-value store
		CREATE TABLE IF NOT EXISTS entries (
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			value BLOB,
			version INTEGER NOT NULL,
			updated_at DATETIME NOT NULL,
			expires_at DATETIME,
			PRIMARY KEY (bucket, key),
			FOREIGN KEY (bucket) REFERENCES buckets(name) ON DELETE CASCADE
		);

		-- Change log for replication
		CREATE TABLE IF NOT EXISTS changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			bucket TEXT NOT NULL,
			key TEXT NOT NULL,
			value BLOB,
			change_type TEXT NOT NULL,
			version INTEGER NOT NULL,
			timestamp DATETIME NOT NULL
		);

		-- Metadata
		CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT
		);

		-- Indexes
		CREATE INDEX IF NOT EXISTS idx_entries_expires ON entries(expires_at) WHERE expires_at IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_changes_version ON changes(version);
		CREATE INDEX IF NOT EXISTS idx_changes_timestamp ON changes(timestamp);
	`

	_, err := s.db.Exec(schema)
	return err
}

// loadVersion loads the current version from the database.
func (s *SQLiteStore) loadVersion() error {
	var version sql.NullInt64
	err := s.db.QueryRow("SELECT MAX(version) FROM changes").Scan(&version)
	if err != nil {
		return err
	}
	if version.Valid {
		s.version = uint64(version.Int64)
	}
	return nil
}

// cleanupLoop periodically removes expired entries and old changes.
func (s *SQLiteStore) cleanupLoop(interval, retention time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.cleanup(retention)
		}
	}
}

// cleanup removes expired entries and old change history.
func (s *SQLiteStore) cleanup(retention time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	now := clock.Now()

	// Delete expired entries
	_, _ = s.db.Exec(
		"DELETE FROM entries WHERE expires_at IS NOT NULL AND expires_at < ?",
		now,
	)

	// Delete old change history
	cutoff := now.Add(-retention)
	_, _ = s.db.Exec("DELETE FROM changes WHERE timestamp < ?", cutoff)
}

// CreateBucket creates a new bucket.
func (s *SQLiteStore) CreateBucket(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	// Use clock.Now() for consistent timestamps with anchor support
	_, err := s.db.Exec("INSERT INTO buckets (name, created_at) VALUES (?, ?)", name, clock.Now())
	if err != nil {
		// Check if it's a unique constraint violation
		return ErrBucketExists
	}
	return nil
}

// DeleteBucket removes a bucket and all its entries.
func (s *SQLiteStore) DeleteBucket(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	result, err := s.db.Exec("DELETE FROM buckets WHERE name = ?", name)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrBucketMissing
	}
	return nil
}

// ListBuckets returns all bucket names.
func (s *SQLiteStore) ListBuckets() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	rows, err := s.db.Query("SELECT name FROM buckets ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		buckets = append(buckets, name)
	}
	return buckets, rows.Err()
}

// Get retrieves a value by bucket and key.
func (s *SQLiteStore) Get(bucket, key string) ([]byte, error) {
	entry, err := s.GetWithMeta(bucket, key)
	if err != nil {
		return nil, err
	}
	return entry.Value, nil
}

// GetWithMeta retrieves a value with its metadata.
func (s *SQLiteStore) GetWithMeta(bucket, key string) (*Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	var entry Entry
	var expiresAt sql.NullTime

	err := s.db.QueryRow(`
		SELECT value, version, updated_at, expires_at
		FROM entries
		WHERE bucket = ? AND key = ?
		  AND (expires_at IS NULL OR expires_at > ?)
	`, bucket, key, clock.Now()).Scan(&entry.Value, &entry.Version, &entry.UpdatedAt, &expiresAt)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if expiresAt.Valid {
		entry.ExpiresAt = expiresAt.Time
	}

	return &entry, nil
}

// Set stores a value.
func (s *SQLiteStore) Set(bucket, key string, value []byte) error {
	return s.setInternal(bucket, key, value, time.Time{})
}

// SetWithTTL stores a value with a time-to-live.
func (s *SQLiteStore) SetWithTTL(bucket, key string, value []byte, ttl time.Duration) error {
	return s.setInternal(bucket, key, value, clock.Now().Add(ttl))
}

// setInternal handles the actual set operation.
func (s *SQLiteStore) setInternal(bucket, key string, value []byte, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	now := clock.Now()
	// Optimistic version increment (will roll back on error)
	s.version++
	version := s.version

	// Start atomic transaction
	tx, err := s.db.Begin()
	if err != nil {
		s.version--
		return err
	}
	defer tx.Rollback()

	// Check if this is an insert or update
	var exists bool
	err = tx.QueryRow(
		"SELECT 1 FROM entries WHERE bucket = ? AND key = ?",
		bucket, key,
	).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		s.version--
		return err
	}
	isUpdate := err == nil

	// Upsert the entry
	var expiresAtPtr interface{}
	if !expiresAt.IsZero() {
		expiresAtPtr = expiresAt
	}

	_, err = tx.Exec(`
		INSERT INTO entries (bucket, key, value, version, updated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(bucket, key) DO UPDATE SET
			value = excluded.value,
			version = excluded.version,
			updated_at = excluded.updated_at,
			expires_at = excluded.expires_at
	`, bucket, key, value, version, now, expiresAtPtr)
	if err != nil {
		s.version--
		return err
	}

	// Record the change
	changeType := ChangeInsert
	if isUpdate {
		changeType = ChangeUpdate
	}

	change := Change{
		Bucket:    bucket,
		Key:       key,
		Value:     value,
		Type:      changeType,
		Timestamp: now,
		Version:   version,
	}

	// recordChange now uses the transaction
	if err := s.recordChangeTx(tx, &change); err != nil {
		s.version--
		return err
	}

	if err := tx.Commit(); err != nil {
		s.version--
		return err
	}

	// Notify subscribers (after commit)
	s.notifySubscribers(change)

	// Trigger write hook (e.g., for clock anchor update)
	if s.OnWrite != nil {
		s.OnWrite()
	}

	return nil
}

// Delete removes a key.
func (s *SQLiteStore) Delete(bucket, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"DELETE FROM entries WHERE bucket = ? AND key = ?",
		bucket, key,
	)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}

	now := clock.Now()
	s.version++

	change := Change{
		Bucket:    bucket,
		Key:       key,
		Type:      ChangeDelete,
		Timestamp: now,
		Version:   s.version,
	}

	if err := s.recordChangeTx(tx, &change); err != nil {
		s.version--
		return err
	}

	if err := tx.Commit(); err != nil {
		s.version--
		return err
	}

	s.notifySubscribers(change)

	return nil
}

// List returns all key-value pairs in a bucket.
func (s *SQLiteStore) List(bucket string) (map[string][]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	rows, err := s.db.Query(`
		SELECT key, value FROM entries
		WHERE bucket = ? AND (expires_at IS NULL OR expires_at > ?)
	`, bucket, clock.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]byte)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, rows.Err()
}

// ListKeys returns all keys in a bucket.
func (s *SQLiteStore) ListKeys(bucket string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	rows, err := s.db.Query(`
		SELECT key FROM entries
		WHERE bucket = ? AND (expires_at IS NULL OR expires_at > ?)
		ORDER BY key
	`, bucket, clock.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// GetJSON retrieves and unmarshals a JSON value.
func (s *SQLiteStore) GetJSON(bucket, key string, v interface{}) error {
	data, err := s.Get(bucket, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// SetJSON marshals and stores a JSON value.
func (s *SQLiteStore) SetJSON(bucket, key string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Set(bucket, key, data)
}

// SetJSONWithTTL marshals and stores a JSON value with TTL.
func (s *SQLiteStore) SetJSONWithTTL(bucket, key string, v interface{}, ttl time.Duration) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.SetWithTTL(bucket, key, data, ttl)
}

// recordChange writes a change to the change log (legacy non-atomic).
func (s *SQLiteStore) recordChange(change Change) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.recordChangeTx(tx, &change); err != nil {
		return err
	}
	return tx.Commit()
}

// recordChangeTx writes a change to the change log using an existing transaction.
func (s *SQLiteStore) recordChangeTx(tx *sql.Tx, change *Change) error {
	result, err := tx.Exec(`
		INSERT INTO changes (bucket, key, value, change_type, version, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)
	`, change.Bucket, change.Key, change.Value, change.Type, change.Version, change.Timestamp)
	if err != nil {
		return err
	}

	id, _ := result.LastInsertId()
	change.ID = uint64(id)
	return nil
}

// notifySubscribers sends a change to all subscribers.
func (s *SQLiteStore) notifySubscribers(change Change) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()

	for _, ch := range s.subscribers {
		select {
		case ch <- change:
		default:
			// Subscriber is slow, skip
		}
	}
}

// Subscribe returns a channel that receives all changes.
func (s *SQLiteStore) Subscribe(ctx context.Context) <-chan Change {
	s.subMu.Lock()
	id := s.nextSubID
	s.nextSubID++
	ch := make(chan Change, 100)
	s.subscribers[id] = ch
	s.subMu.Unlock()

	// Cleanup on context cancellation
	go func() {
		<-ctx.Done()
		s.subMu.Lock()
		defer s.subMu.Unlock()
		// Only close if the channel is still registered (prevents double-close)
		if _, exists := s.subscribers[id]; exists {
			delete(s.subscribers, id)
			close(ch)
		}
	}()

	return ch
}

// GetChangesSince returns all changes since a given version.
func (s *SQLiteStore) GetChangesSince(version uint64) ([]Change, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	rows, err := s.db.Query(`
		SELECT id, bucket, key, value, change_type, version, timestamp
		FROM changes
		WHERE version > ?
		ORDER BY version
	`, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []Change
	for rows.Next() {
		var c Change
		var changeType string
		if err := rows.Scan(&c.ID, &c.Bucket, &c.Key, &c.Value, &changeType, &c.Version, &c.Timestamp); err != nil {
			return nil, err
		}
		c.Type = ChangeType(changeType)
		changes = append(changes, c)
	}
	return changes, rows.Err()
}

// CurrentVersion returns the current version number.
func (s *SQLiteStore) CurrentVersion() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// CreateSnapshot creates a point-in-time snapshot.
func (s *SQLiteStore) CreateSnapshot() (*Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	snapshot := &Snapshot{
		Version:   s.version,
		Timestamp: clock.Now(),
		Buckets:   make(map[string]Bucket),
	}

	// Get all buckets
	buckets, err := s.db.Query("SELECT name FROM buckets")
	if err != nil {
		return nil, err
	}
	defer buckets.Close()

	var bucketNames []string
	for buckets.Next() {
		var name string
		if err := buckets.Scan(&name); err != nil {
			return nil, err
		}
		bucketNames = append(bucketNames, name)
	}

	// Get entries for each bucket
	for _, bucketName := range bucketNames {
		rows, err := s.db.Query(`
			SELECT key, value, version, updated_at, expires_at
			FROM entries
			WHERE bucket = ?
		`, bucketName)
		if err != nil {
			return nil, err
		}

		bucket := make(Bucket)
		for rows.Next() {
			var key string
			var entry Entry
			var expiresAt sql.NullTime

			if err := rows.Scan(&key, &entry.Value, &entry.Version, &entry.UpdatedAt, &expiresAt); err != nil {
				rows.Close()
				return nil, err
			}
			if expiresAt.Valid {
				entry.ExpiresAt = expiresAt.Time
			}
			bucket[key] = entry
		}
		rows.Close()

		snapshot.Buckets[bucketName] = bucket
	}

	return snapshot, nil
}

// RestoreSnapshot restores the store from a snapshot.
func (s *SQLiteStore) RestoreSnapshot(snapshot *Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing data
	if _, err := tx.Exec("DELETE FROM entries"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM buckets"); err != nil {
		return err
	}

	// Restore buckets and entries
	for bucketName, bucket := range snapshot.Buckets {
		if _, err := tx.Exec("INSERT INTO buckets (name) VALUES (?)", bucketName); err != nil {
			return err
		}

		for key, entry := range bucket {
			var expiresAt interface{}
			if !entry.ExpiresAt.IsZero() {
				expiresAt = entry.ExpiresAt
			}

			if _, err := tx.Exec(`
				INSERT INTO entries (bucket, key, value, version, updated_at, expires_at)
				VALUES (?, ?, ?, ?, ?, ?)
			`, bucketName, key, entry.Value, entry.Version, entry.UpdatedAt, expiresAt); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.version = snapshot.Version
	return nil
}

// Close closes the store.
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	s.cancel()

	// Close all subscriber channels
	s.subMu.Lock()
	for id, ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, id)
	}
	s.subMu.Unlock()

	return s.db.Close()
}
