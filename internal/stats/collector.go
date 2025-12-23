// Package stats provides real-time rule statistics collection for the UI.
// It polls nftables counters and maintains a sliding window of rate data
// for sparkline visualizations.
package stats

import (
	"sync"
	"time"
)

// RingBuffer holds a sliding window of data points (rates).
// Used for sparkline visualizations in the UI.
type RingBuffer struct {
	data     []float64
	head     int
	capacity int
	isFull   bool
}

// NewRingBuffer creates a new ring buffer with the specified capacity.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data:     make([]float64, size),
		capacity: size,
	}
}

// Add inserts a new value, overwriting the oldest if full.
func (r *RingBuffer) Add(val float64) {
	r.data[r.head] = val
	r.head = (r.head + 1) % r.capacity
	if r.head == 0 {
		r.isFull = true
	}
}

// Snapshot returns the data ordered from oldest to newest.
func (r *RingBuffer) Snapshot() []float64 {
	result := make([]float64, 0, r.capacity)

	if r.isFull {
		// If wrapped, read from head to end, then 0 to head
		result = append(result, r.data[r.head:]...)
		result = append(result, r.data[:r.head]...)
	} else {
		// Not full yet, just read 0 to head
		result = append(result, r.data[:r.head]...)
	}
	return result
}

// Len returns the number of data points currently in the buffer.
func (r *RingBuffer) Len() int {
	if r.isFull {
		return r.capacity
	}
	return r.head
}

// Collector manages polling and rate calculation for rule statistics.
// It maintains a map of rule IDs to their historical rate data.
type Collector struct {
	mu       sync.RWMutex
	buffers  map[string]*RingBuffer // Map[RuleID] -> History
	lastRaw  map[string]uint64      // Map[RuleID] -> Last Byte Count
	interval time.Duration
	capacity int // Ring buffer size (data points)
	stopCh   chan struct{}
	running  bool
	fetcher  CounterFetcher // Abstracted for testing
}

// CounterFetcher abstracts the nftables counter retrieval.
// This allows mocking in tests.
type CounterFetcher interface {
	// FetchCounters returns a map of rule ID -> current byte count
	FetchCounters() (map[string]uint64, error)
}

// CollectorOption configures the Collector.
type CollectorOption func(*Collector)

// WithCapacity sets the ring buffer capacity (number of data points).
// Default: 60 (120 seconds at 2s interval)
func WithCapacity(n int) CollectorOption {
	return func(c *Collector) {
		c.capacity = n
	}
}

// WithFetcher sets the counter fetcher implementation.
func WithFetcher(f CounterFetcher) CollectorOption {
	return func(c *Collector) {
		c.fetcher = f
	}
}

// NewCollector creates a new stats collector.
// Default interval: 2 seconds, capacity: 60 data points (120s window)
func NewCollector(interval time.Duration, opts ...CollectorOption) *Collector {
	c := &Collector{
		buffers:  make(map[string]*RingBuffer),
		lastRaw:  make(map[string]uint64),
		interval: interval,
		capacity: 60, // Default: 60 pts @ 2s = 120s window
		stopCh:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Start begins the background polling goroutine.
func (c *Collector) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()

	ticker := time.NewTicker(c.interval)
	go func() {
		for {
			select {
			case <-c.stopCh:
				ticker.Stop()
				return
			case <-ticker.C:
				c.tick()
			}
		}
	}()
}

// Stop gracefully shuts down the collector.
func (c *Collector) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()
	close(c.stopCh)
}

// GetSparkline returns the historical rate data for a specific rule.
// Returns an empty slice if the rule has no history yet.
func (c *Collector) GetSparkline(ruleID string) []float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if buf, ok := c.buffers[ruleID]; ok {
		return buf.Snapshot()
	}
	// Return empty slice for unknown rules
	return []float64{}
}

// GetAllStats returns sparkline data for all tracked rules.
func (c *Collector) GetAllStats() map[string][]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string][]float64, len(c.buffers))
	for id, buf := range c.buffers {
		result[id] = buf.Snapshot()
	}
	return result
}

// GetTotalBytes returns the last recorded total bytes for a rule.
func (c *Collector) GetTotalBytes(ruleID string) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastRaw[ruleID]
}

// tick performs one collection cycle.
func (c *Collector) tick() {
	if c.fetcher == nil {
		return
	}

	counters, err := c.fetcher.FetchCounters()
	if err != nil {
		// Log error but continue - stats are best-effort
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	seconds := c.interval.Seconds()

	for id, currentBytes := range counters {
		// Initialize if new rule
		if _, exists := c.buffers[id]; !exists {
			c.buffers[id] = NewRingBuffer(c.capacity)
			c.lastRaw[id] = currentBytes
			// Don't add a data point on first sight - we need two samples to compute rate
			continue
		}

		// Calculate delta
		prevBytes := c.lastRaw[id]
		var delta uint64
		if currentBytes >= prevBytes {
			delta = currentBytes - prevBytes
		} else {
			// Handle counter reset or overflow
			// Assume delta is just currentBytes (started from 0)
			delta = currentBytes
		}

		// Calculate rate (bytes per second)
		rate := float64(delta) / seconds

		// Update state
		c.buffers[id].Add(rate)
		c.lastRaw[id] = currentBytes
	}
}

// Reset clears all collected statistics.
// Useful when firewall rules are reloaded.
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buffers = make(map[string]*RingBuffer)
	c.lastRaw = make(map[string]uint64)
}
