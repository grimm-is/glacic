package events

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

// Aggregator subscribes to events and writes time-series data to SQLite.
// It implements RRD-like 3-tier storage with automatic rollups.
type Aggregator struct {
	db  *sql.DB
	hub *Hub

	// Write buffer to reduce SQLite IOPS
	buffer   []NFTCounterData
	bufferMu sync.Mutex

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// AggregatorConfig configures the stats aggregator.
type AggregatorConfig struct {
	// FlushInterval is how often to flush buffered writes (default: 10s)
	FlushInterval time.Duration

	// JanitorInterval is how often to run rollups (default: 1h)
	JanitorInterval time.Duration

	// RawRetention is how long to keep raw data (default: 2h)
	RawRetention time.Duration

	// HourlyRetention is how long to keep hourly data (default: 30d)
	HourlyRetention time.Duration

	// DailyRetention is how long to keep daily data (default: 365d)
	DailyRetention time.Duration
}

// DefaultAggregatorConfig returns sensible defaults.
func DefaultAggregatorConfig() AggregatorConfig {
	return AggregatorConfig{
		FlushInterval:   10 * time.Second,
		JanitorInterval: 1 * time.Hour,
		RawRetention:    2 * time.Hour,
		HourlyRetention: 30 * 24 * time.Hour,
		DailyRetention:  365 * 24 * time.Hour,
	}
}

// NewAggregator creates a new stats aggregator.
func NewAggregator(db *sql.DB, hub *Hub) (*Aggregator, error) {
	ctx, cancel := context.WithCancel(context.Background())

	a := &Aggregator{
		db:     db,
		hub:    hub,
		buffer: make([]NFTCounterData, 0, 1000),
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize schema
	if err := a.initSchema(); err != nil {
		cancel()
		return nil, err
	}

	return a, nil
}

// initSchema creates the 3-tier stats tables if they don't exist.
func (a *Aggregator) initSchema() error {
	schema := `
	-- Tier 1: Raw stats (kept for 2 hours, flushed every 10s)
	CREATE TABLE IF NOT EXISTS stats_raw (
		timestamp INTEGER NOT NULL,
		rule_id TEXT NOT NULL,
		bytes INTEGER DEFAULT 0,
		packets INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_stats_raw_ts ON stats_raw(timestamp);
	CREATE INDEX IF NOT EXISTS idx_stats_raw_rule ON stats_raw(rule_id);

	-- Tier 2: Hourly aggregates (kept for 30 days)
	CREATE TABLE IF NOT EXISTS stats_hourly (
		hour_bucket TEXT NOT NULL,
		rule_id TEXT NOT NULL,
		bytes INTEGER DEFAULT 0,
		packets INTEGER DEFAULT 0,
		PRIMARY KEY (hour_bucket, rule_id)
	);

	-- Tier 3: Daily aggregates (kept for 1 year)
	CREATE TABLE IF NOT EXISTS stats_daily (
		day_bucket TEXT NOT NULL,
		rule_id TEXT NOT NULL,
		bytes INTEGER DEFAULT 0,
		packets INTEGER DEFAULT 0,
		PRIMARY KEY (day_bucket, rule_id)
	);
	`
	_, err := a.db.Exec(schema)
	return err
}

// Start begins the aggregator background processing.
func (a *Aggregator) Start(cfg AggregatorConfig) {
	// Subscribe to counter events
	events := a.hub.Subscribe(1000, EventNFTCounter)

	// Event consumer goroutine
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			select {
			case <-a.ctx.Done():
				return
			case e := <-events:
				if data, ok := e.Data.(NFTCounterData); ok {
					a.bufferMu.Lock()
					a.buffer = append(a.buffer, data)
					a.bufferMu.Unlock()
				}
			}
		}
	}()

	// Flush ticker
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(cfg.FlushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-a.ctx.Done():
				a.flush() // Final flush on shutdown
				return
			case <-ticker.C:
				a.flush()
			}
		}
	}()

	// Janitor ticker
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(cfg.JanitorInterval)
		defer ticker.Stop()

		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.runJanitor(cfg)
			}
		}
	}()
}

// Stop gracefully shuts down the aggregator.
func (a *Aggregator) Stop() {
	a.cancel()
	a.wg.Wait()
}

// flush writes buffered events to SQLite.
func (a *Aggregator) flush() {
	a.bufferMu.Lock()
	if len(a.buffer) == 0 {
		a.bufferMu.Unlock()
		return
	}
	toFlush := a.buffer
	a.buffer = make([]NFTCounterData, 0, 1000)
	a.bufferMu.Unlock()

	// Batch insert
	tx, err := a.db.Begin()
	if err != nil {
		log.Printf("[events] Failed to begin transaction: %v", err)
		return
	}

	stmt, err := tx.Prepare(`INSERT INTO stats_raw (timestamp, rule_id, bytes, packets) VALUES (?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		log.Printf("[events] Failed to prepare statement: %v", err)
		return
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, d := range toFlush {
		_, err := stmt.Exec(now, d.RuleID, d.Bytes, d.Packets)
		if err != nil {
			log.Printf("[events] Failed to insert: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[events] Failed to commit: %v", err)
	}
}

// runJanitor performs the RRD-style rollups and cleanup.
func (a *Aggregator) runJanitor(cfg AggregatorConfig) {
	log.Printf("[events] Running janitor...")

	// 1. Rollup raw → hourly (for data older than 1 hour)
	_, err := a.db.Exec(`
		INSERT OR REPLACE INTO stats_hourly (hour_bucket, rule_id, bytes, packets)
		SELECT
			strftime('%Y-%m-%d %H:00', timestamp, 'unixepoch') as hb,
			rule_id,
			COALESCE((SELECT bytes FROM stats_hourly WHERE hour_bucket = hb AND stats_hourly.rule_id = stats_raw.rule_id), 0) + sum(bytes),
			COALESCE((SELECT packets FROM stats_hourly WHERE hour_bucket = hb AND stats_hourly.rule_id = stats_raw.rule_id), 0) + sum(packets)
		FROM stats_raw
		WHERE timestamp < strftime('%s', 'now', '-1 hour')
		GROUP BY 1, 2
	`)
	if err != nil {
		log.Printf("[events] Rollup raw→hourly failed: %v", err)
	}

	// 2. Delete raw data older than retention
	rawCutoff := time.Now().Add(-cfg.RawRetention).Unix()
	_, err = a.db.Exec(`DELETE FROM stats_raw WHERE timestamp < ?`, rawCutoff)
	if err != nil {
		log.Printf("[events] Cleanup raw failed: %v", err)
	}

	// 3. Rollup hourly → daily (for data older than 30 days)
	hourlyCutoff := time.Now().Add(-cfg.HourlyRetention).Format("2006-01-02")
	_, err = a.db.Exec(`
		INSERT OR REPLACE INTO stats_daily (day_bucket, rule_id, bytes, packets)
		SELECT
			substr(hour_bucket, 1, 10) as db,
			rule_id,
			COALESCE((SELECT bytes FROM stats_daily WHERE day_bucket = db AND stats_daily.rule_id = stats_hourly.rule_id), 0) + sum(bytes),
			COALESCE((SELECT packets FROM stats_daily WHERE day_bucket = db AND stats_daily.rule_id = stats_hourly.rule_id), 0) + sum(packets)
		FROM stats_hourly
		WHERE hour_bucket < ?
		GROUP BY 1, 2
	`, hourlyCutoff)
	if err != nil {
		log.Printf("[events] Rollup hourly→daily failed: %v", err)
	}

	// 4. Delete hourly data older than retention
	_, err = a.db.Exec(`DELETE FROM stats_hourly WHERE hour_bucket < ?`, hourlyCutoff)
	if err != nil {
		log.Printf("[events] Cleanup hourly failed: %v", err)
	}

	// 5. Delete daily data older than 1 year
	dailyCutoff := time.Now().Add(-cfg.DailyRetention).Format("2006-01-02")
	_, err = a.db.Exec(`DELETE FROM stats_daily WHERE day_bucket < ?`, dailyCutoff)
	if err != nil {
		log.Printf("[events] Cleanup daily failed: %v", err)
	}

	log.Printf("[events] Janitor complete")
}

// ──────────────────────────────────────────────────────────────────────────────
// Query Methods (for API/UI)
// ──────────────────────────────────────────────────────────────────────────────

// GetRecentStats returns recent raw stats for a rule (sparkline data).
func (a *Aggregator) GetRecentStats(ruleID string, duration time.Duration) ([]TimeSeriesPoint, error) {
	cutoff := time.Now().Add(-duration).Unix()

	rows, err := a.db.Query(`
		SELECT timestamp, bytes, packets
		FROM stats_raw
		WHERE rule_id = ? AND timestamp >= ?
		ORDER BY timestamp
	`, ruleID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TimeSeriesPoint
	for rows.Next() {
		var p TimeSeriesPoint
		var ts int64
		if err := rows.Scan(&ts, &p.Bytes, &p.Packets); err != nil {
			continue
		}
		p.Timestamp = time.Unix(ts, 0)
		points = append(points, p)
	}
	return points, nil
}

// GetHourlyStats returns hourly aggregated stats for a rule.
func (a *Aggregator) GetHourlyStats(ruleID string, days int) ([]TimeSeriesPoint, error) {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02 15:00")

	rows, err := a.db.Query(`
		SELECT hour_bucket, bytes, packets
		FROM stats_hourly
		WHERE rule_id = ? AND hour_bucket >= ?
		ORDER BY hour_bucket
	`, ruleID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TimeSeriesPoint
	for rows.Next() {
		var p TimeSeriesPoint
		var bucket string
		if err := rows.Scan(&bucket, &p.Bytes, &p.Packets); err != nil {
			continue
		}
		p.Timestamp, _ = time.Parse("2006-01-02 15:04", bucket)
		points = append(points, p)
	}
	return points, nil
}

// TimeSeriesPoint is a single data point for charts.
type TimeSeriesPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Bytes     uint64    `json:"bytes"`
	Packets   uint64    `json:"packets"`
}
