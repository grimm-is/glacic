package ratelimit

import (
	"grimm.is/glacic/internal/clock"
	"sync"
	"time"
)

// Limiter manages rate limiting for multiple keys
type Limiter struct {
	limiters map[string]*bucket
	mu       sync.RWMutex
}

// bucket implements a token bucket rate limiter
type bucket struct {
	tokens   int
	limit    int
	interval time.Duration
	lastFill time.Time
	mu       sync.Mutex
}

// NewLimiter creates a new rate limiter
func NewLimiter() *Limiter {
	return &Limiter{
		limiters: make(map[string]*bucket),
	}
}

// Allow checks if a request for the given key is allowed
// limit: maximum number of requests
// interval: time window (e.g., time.Minute for requests per minute)
func (l *Limiter) Allow(key string, limit int, interval time.Duration) bool {
	l.mu.Lock()
	b, exists := l.limiters[key]
	if !exists {
		b = &bucket{
			tokens:   limit,
			limit:    limit,
			interval: interval,
			lastFill: clock.Now(),
		}
		l.limiters[key] = b
	}
	l.mu.Unlock()

	return b.take()
}

// take attempts to take a token from the bucket
func (b *bucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Refill tokens based on elapsed time
	now := clock.Now()
	elapsed := now.Sub(b.lastFill)

	if elapsed >= b.interval {
		// Reset tokens after interval
		b.tokens = b.limit
		b.lastFill = now
	}

	// Check if we have tokens available
	if b.tokens <= 0 {
		return false
	}

	// Take a token
	b.tokens--
	return true
}

// AllowN checks if n requests are allowed
func (l *Limiter) AllowN(key string, limit int, interval time.Duration, n int) bool {
	l.mu.Lock()
	b, exists := l.limiters[key]
	if !exists {
		b = &bucket{
			tokens:   limit,
			limit:    limit,
			interval: interval,
			lastFill: clock.Now(),
		}
		l.limiters[key] = b
	}
	l.mu.Unlock()

	return b.takeN(n)
}

// takeN attempts to take n tokens from the bucket
func (b *bucket) takeN(n int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Refill tokens based on elapsed time
	now := clock.Now()
	elapsed := now.Sub(b.lastFill)

	if elapsed >= b.interval {
		// Reset tokens after interval
		b.tokens = b.limit
		b.lastFill = now
	}

	// Check if we have n tokens available
	if b.tokens < n {
		return false
	}

	// Take n tokens
	b.tokens -= n
	return true
}

// Reset clears rate limit for a specific key
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.limiters, key)
}

// CleanupExpired removes expired buckets (ones that haven't been used recently)
func (l *Limiter) CleanupExpired(maxAge time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := clock.Now()
	for key, b := range l.limiters {
		b.mu.Lock()
		if now.Sub(b.lastFill) > maxAge {
			delete(l.limiters, key)
		}
		b.mu.Unlock()
	}
}

// StartCleanup starts a background goroutine to clean up expired buckets
func (l *Limiter) StartCleanup(interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			l.CleanupExpired(maxAge)
		}
	}()
}
