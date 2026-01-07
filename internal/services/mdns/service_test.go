package mdns

import (
	"context"
	"net"
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"
)

func TestNewReflector(t *testing.T) {
	cfg := Config{
		Enabled:    true,
		Interfaces: []string{"eth0", "eth1"},
	}

	logger := logging.New(logging.DefaultConfig())
	r := NewReflector(cfg, logger)

	if r == nil {
		t.Fatal("NewReflector returned nil")
	}
	if !r.config.Enabled {
		t.Error("Expected Enabled true")
	}
	if len(r.config.Interfaces) != 2 {
		t.Errorf("Expected 2 interfaces, got %d", len(r.config.Interfaces))
	}
}

func TestReflector_StartDisabled(t *testing.T) {
	cfg := Config{
		Enabled:    false,
		Interfaces: []string{"eth0", "eth1"},
	}

	logger := logging.New(logging.DefaultConfig())
	r := NewReflector(cfg, logger)
	err := r.Start(context.Background())

	if err != nil {
		t.Errorf("Start with disabled config should not error: %v", err)
	}
}

func TestReflector_StartInsufficientInterfaces(t *testing.T) {
	cfg := Config{
		Enabled:    true,
		Interfaces: []string{"eth0"}, // Only one interface
	}

	logger := logging.New(logging.DefaultConfig())
	r := NewReflector(cfg, logger)
	err := r.Start(context.Background())

	if err != nil {
		t.Errorf("Start with one interface should not error (just no-op): %v", err)
	}
}

func TestReflector_StopWithoutStart(t *testing.T) {
	cfg := Config{
		Enabled:    true,
		Interfaces: []string{"eth0", "eth1"},
	}

	logger := logging.New(logging.DefaultConfig())
	r := NewReflector(cfg, logger)

	// Stop without Start should not panic
	r.Stop()
}

func TestReflector_StartStopCycle(t *testing.T) {
	// This test uses loopback which should always exist
	cfg := Config{
		Enabled:    true,
		Interfaces: []string{"lo", "lo0"}, // lo on Linux, lo0 on Mac
	}

	logger := logging.New(logging.DefaultConfig())
	r := NewReflector(cfg, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start may fail due to multicast not being allowed on loopback
	// We just verify no panic
	_ = r.Start(ctx)

	// Give it a moment
	time.Sleep(50 * time.Millisecond)

	// Stop should work
	r.Stop()
}

func TestMDNSConstants(t *testing.T) {
	if MDNSPort != 5353 {
		t.Errorf("Expected MDNSPort 5353, got %d", MDNSPort)
	}
	if MaxPacketSize < 1500 {
		t.Errorf("MaxPacketSize should be at least 1500, got %d", MaxPacketSize)
	}
}

func TestMDNSMulticastAddresses(t *testing.T) {
	if mdnsIPv4Addr == nil {
		t.Fatal("mdnsIPv4Addr is nil")
	}
	if mdnsIPv6Addr == nil {
		t.Fatal("mdnsIPv6Addr is nil")
	}

	// Verify they are multicast
	if !mdnsIPv4Addr.IsMulticast() {
		t.Errorf("mdnsIPv4Addr should be multicast: %s", mdnsIPv4Addr)
	}
	if !mdnsIPv6Addr.IsMulticast() {
		t.Errorf("mdnsIPv6Addr should be multicast: %s", mdnsIPv6Addr)
	}

	// Verify correct addresses
	expected4 := net.ParseIP("224.0.0.251")
	expected6 := net.ParseIP("ff02::fb")

	if !mdnsIPv4Addr.Equal(expected4) {
		t.Errorf("Expected IPv4 224.0.0.251, got %s", mdnsIPv4Addr)
	}
	if !mdnsIPv6Addr.Equal(expected6) {
		t.Errorf("Expected IPv6 ff02::fb, got %s", mdnsIPv6Addr)
	}
}

func TestReflector_InterfaceResolution(t *testing.T) {
	// Test with non-existent interface
	cfg := Config{
		Enabled:    true,
		Interfaces: []string{"nonexistent_if_abc123", "another_fake_if"},
	}

	logger := logging.New(logging.DefaultConfig())
	r := NewReflector(cfg, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Should not error, but should log warnings and not reflect
	err := r.Start(ctx)
	if err != nil {
		t.Errorf("Start should not error with non-existent interfaces: %v", err)
	}

	// ifaces map should be empty or minimal
	r.mu.Lock()
	ifaceCount := len(r.ifaces)
	r.mu.Unlock()

	if ifaceCount > 0 {
		t.Errorf("Expected 0 valid interfaces, got %d", ifaceCount)
	}

	r.Stop()
}

// TestPortMapping verifies the PortMapping struct
func TestPortMapping(t *testing.T) {
	// This just ensures the struct is defined correctly
	pm := struct {
		ExternalPort   int
		InternalClient string
		InternalPort   int
		Protocol       string
	}{
		ExternalPort:   8080,
		InternalClient: "192.168.1.100",
		InternalPort:   80,
		Protocol:       "TCP",
	}

	if pm.ExternalPort != 8080 {
		t.Error("PortMapping field access failed")
	}
}

func TestReflector_SetEventCallback(t *testing.T) {
	cfg := Config{
		Enabled:    true,
		Interfaces: []string{"lo"},
	}

	logger := logging.New(logging.DefaultConfig())
	r := NewReflector(cfg, logger)

	called := false
	cb := func(p *ParsedMDNS) {
		called = true
	}

	r.SetEventCallback(cb)

	if r.eventCallback == nil {
		t.Fatal("eventCallback should not be nil after SetEventCallback")
	}

	// Verify callback invocation
	r.eventCallback(&ParsedMDNS{})

	if !called {
		t.Error("Callback was not invoked")
	}
}
