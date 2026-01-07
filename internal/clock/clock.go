// Package clock provides a mockable time source for testing.
// In production, it simply wraps time.Now(). For tests, use MockClock.
//
// Time Sanity Check:
//
//	On boot, call EnsureSaneTime() to set system clock from saved anchor
//	if the current system time is unreasonable (before 2023).
package clock

import (
	"sync"
	"time"
)

// MinReasonableYear is the earliest year we consider valid.
const MinReasonableYear = 2023

// Clock is the interface for time operations.
// Use package-level functions for convenience, or inject a Clock for testing.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
}

// --- Real Clock (simple wrapper) ---

// RealClock provides the actual system time.
type RealClock struct{}

// Now returns the current system time.
func (c *RealClock) Now() time.Time {
	return time.Now()
}

// Since returns the time elapsed since t.
func (c *RealClock) Since(t time.Time) time.Duration {
	return time.Since(t)
}

// Until returns the duration until t.
func (c *RealClock) Until(t time.Time) time.Duration {
	return time.Until(t)
}

// --- Mock Clock (for testing) ---

// MockClock is a test clock with controllable time.
type MockClock struct {
	mu      sync.RWMutex
	current time.Time
}

// NewMockClock creates a mock clock set to the given time.
func NewMockClock(t time.Time) *MockClock {
	return &MockClock{current: t}
}

// Now returns the mock time.
func (c *MockClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// Since returns the duration since t.
func (c *MockClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}

// Until returns the duration until t.
func (c *MockClock) Until(t time.Time) time.Duration {
	return t.Sub(c.Now())
}

// Set sets the mock time.
func (c *MockClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = t
}

// Advance advances the mock time by d.
func (c *MockClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// --- Package-level convenience functions ---

// Now returns the current system time.
func Now() time.Time {
	return time.Now()
}

// Since returns the time elapsed since t.
func Since(t time.Time) time.Duration {
	return time.Since(t)
}

// Until returns the duration until t.
func Until(t time.Time) time.Duration {
	return time.Until(t)
}

// --- Utilities ---

// IsReasonableTime returns true if year >= MinReasonableYear.
func IsReasonableTime(t time.Time) bool {
	return t.Year() >= MinReasonableYear
}
