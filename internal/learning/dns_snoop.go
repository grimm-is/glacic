package learning

import (
	"context"
	"net"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"

	"grimm.is/glacic/internal/learning/flowdb"
)

// DNSCorrelationEntry stores a domain mapping for an IP
type DNSCorrelationEntry struct {
	Domain    string
	ExpiresAt time.Time
	Source    flowdb.HintSource
}

// DNSSnoopCache provides IP-to-domain correlation for the learning engine
type DNSSnoopCache struct {
	mu      sync.RWMutex
	entries map[string]*DNSCorrelationEntry // IP string -> entry
	logger  *logging.Logger
	minTTL  time.Duration
	maxTTL  time.Duration
	maxSize int

	// Cleanup
	ctx    context.Context
	cancel context.CancelFunc
}

// NewDNSSnoopCache creates a new DNS snooping cache
func NewDNSSnoopCache(logger *logging.Logger, maxSize int) *DNSSnoopCache {
	if logger == nil {
		logger = logging.Default()
	}
	if maxSize <= 0 {
		maxSize = 10000 // Default 10k entries
	}

	ctx, cancel := context.WithCancel(context.Background())

	cache := &DNSSnoopCache{
		entries: make(map[string]*DNSCorrelationEntry),
		logger:  logger.WithComponent("dns_snoop"),
		minTTL:  5 * time.Minute, // Minimum TTL
		maxTTL:  24 * time.Hour,  // Maximum TTL
		maxSize: maxSize,
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start cleanup goroutine
	go cache.cleanupLoop()

	return cache
}

// Set stores a domain mapping for an IP
func (c *DNSSnoopCache) Set(ip string, domain string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Enforce TTL bounds
	if ttl < c.minTTL {
		ttl = c.minTTL
	}
	if ttl > c.maxTTL {
		ttl = c.maxTTL
	}

	// Evict if at capacity
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[ip] = &DNSCorrelationEntry{
		Domain:    domain,
		ExpiresAt: clock.Now().Add(ttl),
		Source:    flowdb.SourceDNSSnoop,
	}

	c.logger.Debug("cached DNS correlation", "ip", ip, "domain", domain, "ttl", ttl)
}

// Get retrieves the domain for an IP
func (c *DNSSnoopCache) Get(ip string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[ip]
	if !ok {
		return "", false
	}

	// Check if expired
	if clock.Now().After(entry.ExpiresAt) {
		return "", false
	}

	return entry.Domain, true
}

// GetWithSource retrieves the domain and source for an IP
func (c *DNSSnoopCache) GetWithSource(ip string) (string, flowdb.HintSource, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[ip]
	if !ok {
		return "", "", false
	}

	if clock.Now().After(entry.ExpiresAt) {
		return "", "", false
	}

	return entry.Domain, entry.Source, true
}

// Delete removes an entry
func (c *DNSSnoopCache) Delete(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, ip)
}

// Size returns the current cache size
func (c *DNSSnoopCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Stop stops the cache cleanup goroutine
func (c *DNSSnoopCache) Stop() {
	c.cancel()
}

// evictOldest removes the oldest entry (must be called with lock held)
func (c *DNSSnoopCache) evictOldest() {
	var oldestIP string
	var oldestTime time.Time

	for ip, entry := range c.entries {
		if oldestIP == "" || entry.ExpiresAt.Before(oldestTime) {
			oldestIP = ip
			oldestTime = entry.ExpiresAt
		}
	}

	if oldestIP != "" {
		delete(c.entries, oldestIP)
	}
}

// cleanupLoop periodically removes expired entries
func (c *DNSSnoopCache) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.cleanup()
		}
	}
}

// cleanup removes expired entries
func (c *DNSSnoopCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := clock.Now()
	removed := 0

	for ip, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, ip)
			removed++
		}
	}

	if removed > 0 {
		c.logger.Debug("cleaned up expired DNS entries", "removed", removed, "remaining", len(c.entries))
	}
}

// HandleDNSResponse processes a DNS response to populate the correlation cache
// This should be called by the DNS server when forwarding responses
func (c *DNSSnoopCache) HandleDNSResponse(question string, answerIP net.IP, ttl uint32) {
	if answerIP == nil || question == "" {
		return
	}

	// Convert TTL to duration (minimum 5 minutes)
	duration := time.Duration(ttl) * time.Second
	if duration < c.minTTL {
		duration = c.minTTL
	}

	c.Set(answerIP.String(), question, duration)
}

// LookupReverse performs a reverse DNS lookup and caches the result
func (c *DNSSnoopCache) LookupReverse(ip string) (string, error) {
	names, err := net.LookupAddr(ip)
	if err != nil {
		return "", err
	}

	if len(names) == 0 {
		return "", nil
	}

	// Use the first name
	domain := names[0]
	// Remove trailing dot
	if len(domain) > 0 && domain[len(domain)-1] == '.' {
		domain = domain[:len(domain)-1]
	}

	// Cache with reverse DNS source
	c.mu.Lock()
	c.entries[ip] = &DNSCorrelationEntry{
		Domain:    domain,
		ExpiresAt: clock.Now().Add(c.minTTL), // Short TTL for reverse lookups
		Source:    flowdb.SourceReverse,
	}
	c.mu.Unlock()

	return domain, nil
}

// Stats returns cache statistics
func (c *DNSSnoopCache) Stats() map[string]int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := make(map[string]int64)
	stats["size"] = int64(len(c.entries))
	stats["max_size"] = int64(c.maxSize)

	// Count by source
	var dnsSnoop, sniPeek, rdns int64
	now := clock.Now()
	var expired int64

	for _, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			expired++
			continue
		}
		switch entry.Source {
		case flowdb.SourceDNSSnoop:
			dnsSnoop++
		case flowdb.SourceSNIPeek:
			sniPeek++
		case flowdb.SourceReverse:
			rdns++
		}
	}

	stats["dns_snoop_entries"] = dnsSnoop
	stats["sni_peek_entries"] = sniPeek
	stats["rdns_entries"] = rdns
	stats["expired_entries"] = expired

	return stats
}
