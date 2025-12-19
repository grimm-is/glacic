package ratelimit

import (
	"testing"
	"time"
)

func TestNewLimiter(t *testing.T) {
	l := NewLimiter()
	if l == nil {
		t.Fatal("NewLimiter returned nil")
	}
	if l.limiters == nil {
		t.Error("limiters map not initialized")
	}
}

func TestLimiter_Allow_Basic(t *testing.T) {
	l := NewLimiter()

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		if !l.Allow("test-key", 3, time.Minute) {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if l.Allow("test-key", 3, time.Minute) {
		t.Error("4th request should be denied (over limit)")
	}
}

func TestLimiter_Allow_DifferentKeys(t *testing.T) {
	l := NewLimiter()

	// Each key has independent limits
	for i := 0; i < 2; i++ {
		if !l.Allow("key1", 2, time.Minute) {
			t.Errorf("key1 request %d should be allowed", i+1)
		}
		if !l.Allow("key2", 2, time.Minute) {
			t.Errorf("key2 request %d should be allowed", i+1)
		}
	}

	// Both keys should now be at limit
	if l.Allow("key1", 2, time.Minute) {
		t.Error("key1 should be rate limited")
	}
	if l.Allow("key2", 2, time.Minute) {
		t.Error("key2 should be rate limited")
	}
}

func TestLimiter_Allow_Refill(t *testing.T) {
	l := NewLimiter()

	// Use up all tokens
	for i := 0; i < 2; i++ {
		l.Allow("refill-key", 2, 50*time.Millisecond)
	}

	// Should be rate limited
	if l.Allow("refill-key", 2, 50*time.Millisecond) {
		t.Error("Should be rate limited before interval")
	}

	// Wait for interval to pass
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again after refill
	if !l.Allow("refill-key", 2, 50*time.Millisecond) {
		t.Error("Should be allowed after interval refill")
	}
}

func TestLimiter_Reset(t *testing.T) {
	l := NewLimiter()

	// Use up all tokens
	for i := 0; i < 3; i++ {
		l.Allow("reset-key", 3, time.Minute)
	}

	// Should be rate limited
	if l.Allow("reset-key", 3, time.Minute) {
		t.Error("Should be rate limited")
	}

	// Reset the key
	l.Reset("reset-key")

	// Should be allowed again
	if !l.Allow("reset-key", 3, time.Minute) {
		t.Error("Should be allowed after Reset")
	}
}

func TestLimiter_CleanupExpired(t *testing.T) {
	l := NewLimiter()

	// Create some entries
	l.Allow("key1", 10, time.Minute)
	l.Allow("key2", 10, time.Minute)

	// Verify entries exist
	l.mu.RLock()
	initialCount := len(l.limiters)
	l.mu.RUnlock()
	if initialCount != 2 {
		t.Errorf("Expected 2 limiters, got %d", initialCount)
	}

	// Cleanup with very short maxAge - entries just created should survive
	l.CleanupExpired(time.Hour)

	l.mu.RLock()
	afterCleanup := len(l.limiters)
	l.mu.RUnlock()
	if afterCleanup != 2 {
		t.Errorf("Expected 2 limiters after cleanup (entries are fresh), got %d", afterCleanup)
	}

	// Cleanup with zero maxAge - all entries should be removed
	l.CleanupExpired(0)

	l.mu.RLock()
	afterZeroCleanup := len(l.limiters)
	l.mu.RUnlock()
	if afterZeroCleanup != 0 {
		t.Errorf("Expected 0 limiters after zero-age cleanup, got %d", afterZeroCleanup)
	}
}

func TestLimiter_ConcurrentAccess(t *testing.T) {
	l := NewLimiter()
	done := make(chan bool)

	// Spawn multiple goroutines hitting same key
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				l.Allow("concurrent-key", 1000, time.Minute)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// No panic means success - concurrent access is safe
}
