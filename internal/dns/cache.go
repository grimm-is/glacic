package dns

import (
	"sync"
	"time"
"grimm.is/glacic/internal/clock"

	"github.com/miekg/dns"
)

// CacheEntry represents a cached DNS response.
type CacheEntry struct {
	Records   []dns.RR
	ExpiresAt time.Time
}

// Cache is a DNS response cache with TTL support.
type Cache struct {
	entries map[string]*CacheEntry
	mu      sync.RWMutex
	maxSize int
	minTTL  time.Duration
	maxTTL  time.Duration
}

// NewCache creates a new DNS cache.
func NewCache(maxSize int, minTTL, maxTTL time.Duration) *Cache {
	if minTTL == 0 {
		minTTL = 60 * time.Second
	}
	if maxTTL == 0 {
		maxTTL = 86400 * time.Second // 24 hours
	}

	c := &Cache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSize,
		minTTL:  minTTL,
		maxTTL:  maxTTL,
	}

	// Start cleanup goroutine
	go c.cleanupLoop()

	return c
}

// cacheKey generates a cache key from name and type.
func cacheKey(name string, qtype uint16) string {
	return name + ":" + dns.TypeToString[qtype]
}

// Get retrieves a cached response.
func (c *Cache) Get(name string, qtype uint16) []dns.RR {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(name, qtype)
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}

	if clock.Now().After(entry.ExpiresAt) {
		return nil
	}

	// Clone records with adjusted TTLs
	remaining := uint32(time.Until(entry.ExpiresAt).Seconds())
	if remaining == 0 {
		remaining = 1
	}

	result := make([]dns.RR, len(entry.Records))
	for i, rr := range entry.Records {
		result[i] = dns.Copy(rr)
		result[i].Header().Ttl = remaining
	}

	return result
}

// Set stores a response in the cache.
func (c *Cache) Set(name string, qtype uint16, records []dns.RR) {
	if len(records) == 0 {
		return
	}

	// Find minimum TTL from records
	var minRecordTTL uint32 = 0xFFFFFFFF
	for _, rr := range records {
		if rr.Header().Ttl < minRecordTTL {
			minRecordTTL = rr.Header().Ttl
		}
	}

	// Apply min/max TTL bounds
	ttl := time.Duration(minRecordTTL) * time.Second
	if ttl < c.minTTL {
		ttl = c.minTTL
	}
	if ttl > c.maxTTL {
		ttl = c.maxTTL
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	key := cacheKey(name, qtype)
	c.entries[key] = &CacheEntry{
		Records:   records,
		ExpiresAt: clock.Now().Add(ttl),
	}
}

// SetNegative stores a negative (NXDOMAIN) response.
func (c *Cache) SetNegative(name string, qtype uint16, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	key := cacheKey(name, qtype)
	c.entries[key] = &CacheEntry{
		Records:   nil, // nil indicates negative cache
		ExpiresAt: clock.Now().Add(ttl),
	}
}

// Flush clears all cache entries.
func (c *Cache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CacheEntry)
}

// Size returns the current number of cache entries.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// evictOldest removes the oldest entry (must be called with lock held).
func (c *Cache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.entries {
		if oldestKey == "" || entry.ExpiresAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.ExpiresAt
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// cleanupLoop periodically removes expired entries.
func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

// cleanup removes expired entries.
func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := clock.Now()
	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, key)
		}
	}
}
