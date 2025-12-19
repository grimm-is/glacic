package dns

import (
	"sync"
	"time"
"grimm.is/glacic/internal/clock"
)

// RateLimiter implements per-client DNS query rate limiting.
type RateLimiter struct {
	clients     map[string]*clientBucket
	mu          sync.Mutex
	ratePerSec  int
	burstSize   int
	cleanupTick time.Duration
}

// clientBucket tracks rate limit state for a single client.
type clientBucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(ratePerSec int) *RateLimiter {
	if ratePerSec <= 0 {
		ratePerSec = 100 // Default 100 queries/sec
	}

	rl := &RateLimiter{
		clients:     make(map[string]*clientBucket),
		ratePerSec:  ratePerSec,
		burstSize:   ratePerSec * 2, // Allow 2x burst
		cleanupTick: time.Minute,
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a client is allowed to make a request.
func (rl *RateLimiter) Allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := clock.Now()
	bucket, ok := rl.clients[clientIP]

	if !ok {
		// New client, create bucket with full tokens
		rl.clients[clientIP] = &clientBucket{
			tokens:    float64(rl.burstSize - 1),
			lastCheck: now,
		}
		return true
	}

	// Calculate tokens to add based on time elapsed
	elapsed := now.Sub(bucket.lastCheck).Seconds()
	bucket.tokens += elapsed * float64(rl.ratePerSec)
	bucket.lastCheck = now

	// Cap at burst size
	if bucket.tokens > float64(rl.burstSize) {
		bucket.tokens = float64(rl.burstSize)
	}

	// Check if we have tokens available
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}

	return false
}

// cleanupLoop periodically removes stale client entries.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanupTick)
	defer ticker.Stop()

	for range ticker.C {
		rl.cleanup()
	}
}

// cleanup removes clients that haven't been seen recently.
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := clock.Now().Add(-5 * time.Minute)
	for ip, bucket := range rl.clients {
		if bucket.lastCheck.Before(cutoff) {
			delete(rl.clients, ip)
		}
	}
}

// Stats returns rate limiter statistics.
func (rl *RateLimiter) Stats() map[string]int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	return map[string]int{
		"tracked_clients": len(rl.clients),
		"rate_per_sec":    rl.ratePerSec,
		"burst_size":      rl.burstSize,
	}
}
