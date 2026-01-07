package health

import (
	"context"
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"net/http"
	"os"
	"sync"
	"time"
)

// Status represents the health status of a component.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// Check represents a single health check.
type Check struct {
	Name        string        `json:"name"`
	Status      Status        `json:"status"`
	Message     string        `json:"message,omitempty"`
	LastChecked time.Time     `json:"last_checked"`
	Duration    time.Duration `json:"duration_ms"`
}

// Report represents the overall health report.
type Report struct {
	Status    Status           `json:"status"`
	Checks    map[string]Check `json:"checks"`
	Timestamp time.Time        `json:"timestamp"`
}

// Checker performs health checks.
type Checker struct {
	mu     sync.RWMutex
	checks map[string]CheckFunc
	cache  *Report
	ttl    time.Duration
}

// CheckFunc is a function that performs a health check.
type CheckFunc func(ctx context.Context) Check

// NewChecker creates a new health checker.
func NewChecker() *Checker {
	c := &Checker{
		checks: make(map[string]CheckFunc),
		ttl:    5 * time.Second,
	}

	// Register default checks
	c.Register("nftables", CheckNftables)
	c.Register("conntrack", CheckConntrack)
	c.Register("interfaces", CheckInterfaces)
	c.Register("disk", CheckDisk)
	c.Register("memory", CheckMemory)

	return c
}

// Register adds a health check.
func (c *Checker) Register(name string, fn CheckFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks[name] = fn
}

// Check runs all health checks and returns a report.
func (c *Checker) Check(ctx context.Context) Report {
	c.mu.RLock()
	// Return cached result if fresh
	if c.cache != nil && time.Since(c.cache.Timestamp) < c.ttl {
		report := *c.cache
		c.mu.RUnlock()
		return report
	}
	c.mu.RUnlock()

	// Run checks
	checks := make(map[string]Check)
	overallStatus := StatusHealthy

	c.mu.RLock()
	checkFuncs := make(map[string]CheckFunc)
	for name, fn := range c.checks {
		checkFuncs[name] = fn
	}
	c.mu.RUnlock()

	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, fn := range checkFuncs {
		wg.Add(1)
		go func(name string, fn CheckFunc) {
			defer wg.Done()
			check := fn(ctx)
			check.Name = name

			mu.Lock()
			checks[name] = check
			if check.Status == StatusUnhealthy {
				overallStatus = StatusUnhealthy
			} else if check.Status == StatusDegraded && overallStatus != StatusUnhealthy {
				overallStatus = StatusDegraded
			}
			mu.Unlock()
		}(name, fn)
	}

	wg.Wait()

	report := Report{
		Status:    overallStatus,
		Checks:    checks,
		Timestamp: clock.Now(),
	}

	// Cache result
	c.mu.Lock()
	c.cache = &report
	c.mu.Unlock()

	return report
}

// Handler returns an HTTP handler for health checks.
func (c *Checker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		report := c.Check(ctx)

		w.Header().Set("Content-Type", "application/json")

		switch report.Status {
		case StatusHealthy:
			w.WriteHeader(http.StatusOK)
		case StatusDegraded:
			w.WriteHeader(http.StatusOK) // Still OK but degraded
		case StatusUnhealthy:
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(report)
	}
}

// LivenessHandler returns a simple liveness probe handler.
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
}

// ReadinessHandler returns a readiness probe handler.
func (c *Checker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		report := c.Check(ctx)

		if report.Status == StatusUnhealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("NOT READY"))
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	}
}

// Default check implementations

// CheckDisk verifies disk space is available.
func CheckDisk(ctx context.Context) Check {
	start := clock.Now()
	check := Check{LastChecked: start}

	// Simple check - just verify we can write
	testFile := "/tmp/.health_check"
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		check.Status = StatusDegraded
		check.Message = fmt.Sprintf("disk write failed: %v", err)
	} else {
		os.Remove(testFile)
		check.Status = StatusHealthy
		check.Message = "disk writable"
	}

	check.Duration = time.Since(start)
	return check
}
