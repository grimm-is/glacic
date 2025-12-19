package ra

import (
	"context"
	"testing"
	"time"

	"grimm.is/glacic/internal/config"
)

func TestNewService(t *testing.T) {
	// Config with 3 interfaces:
	// 1. RA enabled, IPv6 present -> Should work
	// 2. RA disabled, IPv6 present -> Should be skipped
	// 3. RA enabled, no IPv6 -> Should be skipped
	cfg := &config.Config{
		Interfaces: []config.Interface{
			{
				Name: "eth0",
				RA:   true,
				IPv6: []string{"2001:db8::1/64"},
			},
			{
				Name: "eth1",
				RA:   false,
				IPv6: []string{"2001:db8::2/64"},
			},
			{
				Name: "eth2",
				RA:   true,
				IPv6: []string{},
			},
		},
	}

	svc := NewService(cfg)

	if len(svc.config) != 1 {
		t.Errorf("Expected 1 interface configured, got %d", len(svc.config))
	}
	if len(svc.config) > 0 && svc.config[0].Name != "eth0" {
		t.Errorf("Expected eth0 to be configured, got %s", svc.config[0].Name)
	}
}

func TestService_Lifecycle(t *testing.T) {
	// Lifecycle test shouldn't block even if runRA fails
	cfg := &config.Config{
		Interfaces: []config.Interface{
			// Use loopback or dummy interface name if possible,
			// but runRA might fail to find it anyway. That's fine for coverage of Start/Stop.
			{
				Name: "lo",
				RA:   true,
				IPv6: []string{"::1/128"},
			},
		},
	}

	svc := NewService(cfg)

	// Start (non-blocking)
	svc.Start()

	// Wait a tiny bit (allow runRA to start and likely fail)
	time.Sleep(10 * time.Millisecond)

	if !svc.running {
		t.Error("Service should be marked running")
	}

	// Stop
	done := make(chan bool)
	go func() {
		svc.Stop()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Stop() timed out")
	}

	if svc.running {
		t.Error("Service should be marked stopped")
	}
}

func TestService_StartStop_Idempotency(t *testing.T) {
	cfg := &config.Config{}
	svc := NewService(cfg)

	// Multiple Start
	svc.Start()
	svc.Start() // Should no-op
	if !svc.running {
		t.Error("Service should be running")
	}
	if svc.ctx == nil {
		t.Error("Context should be initialized")
	}

	// Multiple Stop
	svc.Stop()
	svc.Stop() // Should no-op
	if svc.running {
		t.Error("Service should be stopped")
	}

	// Check cancellation
	if svc.ctx.Err() != context.Canceled {
		t.Error("Context should be canceled")
	}
}
