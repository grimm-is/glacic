package vpn

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"
)

// MockProvider implements Provider for testing
type MockProvider struct {
	TypeVal             Type
	InterfaceVal        string
	Connected           bool
	ManagementAccessVal bool
	StartCalled         bool
	StopCalled          bool
}

func (m *MockProvider) Type() Type {
	return m.TypeVal
}

func (m *MockProvider) Start(ctx context.Context) error {
	m.StartCalled = true
	m.Connected = true
	return nil
}

func (m *MockProvider) Stop() error {
	m.StopCalled = true
	m.Connected = false
	return nil
}

func (m *MockProvider) Status() ProviderStatus {
	return ProviderStatus{
		Type:       m.TypeVal,
		Connected:  m.Connected,
		Interface:  m.InterfaceVal,
		LastUpdate: time.Now(),
	}
}

func (m *MockProvider) IsConnected() bool {
	return m.Connected
}

func (m *MockProvider) Interface() string {
	return m.InterfaceVal
}

func (m *MockProvider) ManagementAccess() bool {
	return m.ManagementAccessVal
}

func TestInterfaceExists(t *testing.T) {
	// Loopback usually exists
	if !InterfaceExists("lo") && !InterfaceExists("lo0") {
		// Just skip
	}
	if InterfaceExists("nonexistent_xyz") {
		t.Error("Found non-existent interface")
	}
}

func TestManager_Lifecycle(t *testing.T) {
	p1 := &MockProvider{TypeVal: TypeWireGuard, InterfaceVal: "wg0"}
	p2 := &MockProvider{TypeVal: TypeTailscale, InterfaceVal: "tailscale0"}

	// Create a discard logger
	discardHandler := slog.NewTextHandler(io.Discard, nil)
	logger := &logging.Logger{Logger: slog.New(discardHandler)}

	m := &Manager{
		providers: []Provider{p1, p2},
		logger:    logger,
	}

	ctx := context.Background()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !p1.StartCalled || !p2.StartCalled {
		t.Error("Providers not started")
	}

	m.Stop()

	if !p1.StopCalled || !p2.StopCalled {
		t.Error("Providers not stopped")
	}
}
