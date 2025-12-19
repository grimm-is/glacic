package health

import (
	"context"
	"testing"
)

func TestHealthRegistry(t *testing.T) {
	ctx := context.Background()

	// Test NewChecker
	checker := NewChecker()
	if checker == nil {
		t.Fatal("NewChecker returned nil")
	}

	// Test Register
	checker.Register("test_check", func(ctx context.Context) Check {
		return Check{
			Status:  StatusHealthy,
			Message: "OK",
		}
	})

	// Test Check
	report := checker.Check(ctx)
	if len(report.Checks) != 6 {
		t.Errorf("Expected 6 results (5 defaults + 1 custom), got %d", len(report.Checks))
	}
	if report.Checks["test_check"].Status != StatusHealthy {
		t.Error("Check failed")
	}
}

func TestStubChecks(t *testing.T) {
	// These will run the stub versions on macOS, or real versions on Linux.
	// Both should return valid Check structs without panic.
	ctx := context.Background()

	checks := []struct {
		name string
		fn   func(context.Context) Check
	}{
		{"Nftables", CheckNftables},
		{"Conntrack", CheckConntrack},
		{"Interfaces", CheckInterfaces},
		{"Memory", CheckMemory},
		{"Disk", CheckDisk},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.fn(ctx)
			if res.Status == "" {
				t.Error("Check returned empty status")
			}
		})
	}
}

func TestHandlers(t *testing.T) {
	checker := NewChecker()

	h := checker.Handler()
	if h == nil {
		t.Error("Handler returned nil")
	}

	l := LivenessHandler()
	if l == nil {
		t.Error("LivenessHandler returned nil")
	}

	r := checker.ReadinessHandler()
	if r == nil {
		t.Error("ReadinessHandler returned nil")
	}
}
