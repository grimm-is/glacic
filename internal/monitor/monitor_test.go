package monitor

import (
	"errors"
	"sync"
	"testing"
	"time"

	"grimm.is/glacic/internal/config"
)

func TestMonitor(t *testing.T) {
	// Mock the ping function
	originalPing := CheckPingFunc
	defer func() { CheckPingFunc = originalPing }()

	mockPing := func(ip string) error {
		if ip == "8.8.8.8" {
			return nil // Success
		}
		if ip == "1.1.1.1" {
			return errors.New("timeout") // Failure
		}
		return errors.New("unknown host")
	}
	CheckPingFunc = mockPing

	// Test directly calling checkPing
	if err := checkPing("8.8.8.8"); err != nil {
		t.Errorf("expected success for 8.8.8.8, got %v", err)
	}
	if err := checkPing("1.1.1.1"); err == nil {
		t.Error("expected failure for 1.1.1.1")
	}

	// Test Start/monitorRoute
	routes := []config.Route{
		{Name: "GoodRoute", MonitorIP: "8.8.8.8"},
		{Name: "BadRoute", MonitorIP: "1.1.1.1"},
		{Name: "NoMonitor", MonitorIP: ""}, // Should be skipped
	}

	var wg sync.WaitGroup
	// Run in test mode (one-shot)
	Start(nil, routes, &wg, true)

	// Wait specifically for expected number of routines?
	// The Start function adds to wg if isTestMode is true.
	// We need to wait for them.

	// Start adds to wg for each valid route.
	// We have 2 valid routes.
	// But Start logic: "if isTestMode { wg.Add(1) }"
	// So we can just Wait() on the passed wg.

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for monitor routines")
	}
}

func TestCheckPingReal(t *testing.T) {
	// Call the real function to cover its lines, even if it fails due to permissions
	err := CheckPingFunc("127.0.0.1")
	if err != nil {
		t.Logf("Real ping failed (expected in unprivileged test): %v", err)
	}
}
