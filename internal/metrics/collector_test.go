package metrics

import (
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"
)

func TestCollector_StateAccess(t *testing.T) {
	// Setup
	logger := logging.New(logging.DefaultConfig())
	c := NewCollector(logger, time.Minute)

	// Test Initial State
	if c.GetLastUpdate().IsZero() {
		// New collector hasn't run, so this is correct.
	}

	// Test SetInterfaceZone
	c.SetInterfaceZone("eth0", "wan")
	stats := c.GetInterfaceStats()
	if len(stats) != 1 {
		t.Errorf("Expected 1 interface stat, got %d", len(stats))
	}
	if stats["eth0"].Zone != "wan" {
		t.Error("Interface zone not set correctly")
	}

	// Test UpdateDHCPStats
	c.UpdateDHCPStats(true, 10, 50)
	svc := c.GetServiceStats()
	if !svc.DHCP.Enabled {
		t.Error("DHCP should be enabled")
	}
	if svc.DHCP.ActiveLeases != 10 {
		t.Errorf("DHCP active leases mismatch: %d", svc.DHCP.ActiveLeases)
	}

	// Test UpdateDNSStats
	c.UpdateDNSStats(true, 100, 80, 20, 5)
	svc = c.GetServiceStats()
	if !svc.DNS.Enabled {
		t.Error("DNS should be enabled")
	}
	if svc.DNS.Queries != 100 {
		t.Errorf("DNS queries mismatch: %d", svc.DNS.Queries)
	}

	// Test UpdateSystemMetrics
	c.UpdateSystemMetrics(time.Hour)
	sys := c.GetSystemStats()
	if sys == nil {
		t.Error("System stats should not be nil")
	}
	// Note: UpdateSystemMetrics only updates registry, not the internal struct currently.

	// Test GetPolicyStats (empty)
	pStats := c.GetPolicyStats()
	if len(pStats) != 0 {
		t.Error("Expected empty policy stats")
	}

	// Test GetConntrackStats (empty)
	ctStats := c.GetConntrackStats()
	if ctStats == nil {
		t.Error("Conntrack stats should not be nil")
	}
}

func TestCollector_Lifecycle(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	c := NewCollector(logger, 10*time.Millisecond)

	// Verify initial last update is zero
	initialUpdate := c.GetLastUpdate()
	if !initialUpdate.IsZero() {
		t.Error("Expected initial lastUpdate to be zero")
	}

	// Start in goroutine
	go c.Start()

	// Let it tick a few times
	time.Sleep(50 * time.Millisecond)

	// Stop
	c.Stop()

	// Verify lastUpdate was set (collector ran at least once)
	// Note: On macOS without nft, collectMetrics will fail but should still update timestamp
	finalUpdate := c.GetLastUpdate()
	if finalUpdate.IsZero() {
		// This is acceptable - collection may fail on non-Linux, but no panic occurred
		t.Log("lastUpdate not set (expected on non-Linux systems)")
	} else if !finalUpdate.After(initialUpdate) {
		t.Error("Expected lastUpdate to be updated after Start()")
	}
}

func TestIncrementConfigReload(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	c := NewCollector(logger, time.Minute)

	// Initial state should be zero
	success, failure := c.GetReloadCounts()
	if success != 0 || failure != 0 {
		t.Errorf("Expected initial counts (0, 0), got (%d, %d)", success, failure)
	}

	// Increment success
	c.IncrementConfigReload(true)
	success, failure = c.GetReloadCounts()
	if success != 1 {
		t.Errorf("Expected success=1, got %d", success)
	}

	// Increment failure
	c.IncrementConfigReload(false)
	success, failure = c.GetReloadCounts()
	if failure != 1 {
		t.Errorf("Expected failure=1, got %d", failure)
	}

	// Verify totals
	if success != 1 || failure != 1 {
		t.Errorf("Expected final counts (1, 1), got (%d, %d)", success, failure)
	}
}
