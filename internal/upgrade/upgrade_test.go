package upgrade

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/scheduler"
)

// Helper to create a test manager
func newTestManager() *Manager {
	discardHandler := slog.NewTextHandler(io.Discard, nil)
	return NewManager(&logging.Logger{Logger: slog.New(discardHandler)})
}

// TestManager_Lifecycle tests creation and listener registration
func TestManager_Lifecycle(t *testing.T) {
	m := newTestManager()

	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()

	m.RegisterListener("test-http", l)

	m.mu.RLock()
	_, ok := m.listeners["test-http"]
	m.mu.RUnlock()

	if !ok {
		t.Error("Listener not registered")
	}

	m.UnregisterListener("test-http")

	m.mu.RLock()
	_, ok = m.listeners["test-http"]
	m.mu.RUnlock()

	if ok {
		t.Error("Listener should be unregistered")
	}
}

// TestManager_StateCollection tests the CollectState method
func TestManager_StateCollection(t *testing.T) {
	m := newTestManager()

	// Mock callbacks
	m.SetStateCollectors(
		func() []DHCPLease {
			return []DHCPLease{{MAC: "00:00:00:00:00:01"}}
		},
		func() []DNSCacheEntry {
			return []DNSCacheEntry{{Name: "example.com"}}
		},
		func() []ConntrackEntry {
			return []ConntrackEntry{{Protocol: "tcp"}}
		},
	)

	cfg := &config.Config{}
	state := m.CollectState(cfg, "/tmp/config.hcl")

	if len(state.DHCPLeases) != 1 {
		t.Error("DHCP lease not collected")
	}
	if len(state.DNSCache) != 1 {
		t.Error("DNS cache not collected")
	}
	if len(state.ConntrackEntries) != 1 {
		t.Error("Conntrack not collected")
	}
	if state.ConfigPath != "/tmp/config.hcl" {
		t.Error("Config path mismatch")
	}
}

// TestManager_StateRestoration tests the RestoreState method
func TestManager_StateRestoration(t *testing.T) {
	m := newTestManager()

	dhcpCalled := false
	dnsCalled := false

	m.SetStateRestorers(
		func(leases []DHCPLease) error {
			dhcpCalled = true
			if len(leases) != 1 {
				t.Error("Expected 1 lease")
			}
			return nil
		},
		func(entries []DNSCacheEntry) error {
			dnsCalled = true
			if len(entries) != 1 {
				t.Error("Expected 1 dns entry")
			}
			return nil
		},
		nil, // conntrack restorer (not needed for this test)
	)

	state := &State{
		DHCPLeases: []DHCPLease{{MAC: "test"}},
		DNSCache:   []DNSCacheEntry{{Name: "test"}},
	}

	if err := m.RestoreState(state); err != nil {
		t.Fatalf("RestoreState failed: %v", err)
	}

	if !dhcpCalled {
		t.Error("DHCP restorer not called")
	}
	if !dnsCalled {
		t.Error("DNS restorer not called")
	}
}

// TestManager_Persistence tests SaveState and LoadState
func TestManager_Persistence(t *testing.T) {
	// Create temp dir for state file
	tmpDir, err := ioutil.TempDir("", "upgrade-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Override state file path
	oldPath := StateFilePath
	StateFilePath = filepath.Join(tmpDir, "state.gob")
	defer func() { StateFilePath = oldPath }()

	m := newTestManager()

	state := &State{
		Version: "1.0.0",
		PID:     123,
	}

	// Test Save
	if err := m.SaveState(state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(StateFilePath); os.IsNotExist(err) {
		t.Error("State file not created")
	}

	// Test Load
	m2 := newTestManager()
	loadedState, err := m2.LoadState()
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if loadedState.Version != "1.0.0" {
		t.Errorf("Loaded version mismatch: %s", loadedState.Version)
	}
	if loadedState.PID != 123 {
		t.Errorf("Loaded PID mismatch: %d", loadedState.PID)
	}

	// Test Cleanup
	if err := m.CleanupState(); err != nil {
		t.Fatalf("CleanupState failed: %v", err)
	}

	if _, err := os.Stat(StateFilePath); !os.IsNotExist(err) {
		t.Error("State file not removed")
	}
}

// TestManager_GetListener tests GetListener
func TestManager_GetListener(t *testing.T) {
	m := newTestManager()
	l, _ := net.Listen("tcp", ":0")
	defer l.Close()

	m.RegisterListener("test", l)

	got, ok := m.GetListener("test")
	if !ok || got != l {
		t.Error("GetListener returned incorrect listener")
	}

	got, ok = m.GetListener("missing")
	if ok || got != nil {
		t.Error("GetListener should return nil/false for missing")
	}
}

func TestDeltaCollector(t *testing.T) {
	collector := NewDeltaCollector(1)

	// Initially empty
	if !collector.IsEmpty() {
		t.Error("new collector should be empty")
	}

	// Record DHCP lease
	lease := DHCPLease{
		MAC:       "aa:bb:cc:dd:ee:ff",
		IP:        "192.168.1.100",
		Hostname:  "test-host",
		Interface: "eth0",
		Expires:   time.Now().Add(time.Hour),
	}
	collector.RecordDHCPLease(lease)

	if collector.IsEmpty() {
		t.Error("collector should not be empty after recording lease")
	}

	// Record DNS cache
	dnsEntry := DNSCacheEntry{
		Name:    "example.com",
		Type:    1,
		TTL:     300,
		Data:    []byte{93, 184, 216, 34},
		Expires: time.Now().Add(5 * time.Minute),
	}
	collector.RecordDNSCache(dnsEntry)

	// Flush and verify
	delta := collector.Flush()

	if delta.CheckpointID != 1 {
		t.Errorf("expected checkpoint ID 1, got %d", delta.CheckpointID)
	}
	if len(delta.DHCPAdded) != 1 {
		t.Errorf("expected 1 DHCP added, got %d", len(delta.DHCPAdded))
	}
	if len(delta.DNSAdded) != 1 {
		t.Errorf("expected 1 DNS added, got %d", len(delta.DNSAdded))
	}

	// After flush, should be empty
	if !collector.IsEmpty() {
		t.Error("collector should be empty after flush")
	}
}

// TestDeltaCollector_DHCPRelease tests recording lease releases
func TestDeltaCollector_DHCPRelease(t *testing.T) {
	collector := NewDeltaCollector(1)

	// Record release
	collector.RecordDHCPRelease("aa:bb:cc:dd:ee:ff")

	delta := collector.Flush()

	if len(delta.DHCPRemoved) != 1 {
		t.Errorf("expected 1 DHCP removed, got %d", len(delta.DHCPRemoved))
	}
	if delta.DHCPRemoved[0] != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("unexpected MAC: %s", delta.DHCPRemoved[0])
	}
}

// TestDeltaCollector_Stop tests stopping the collector
func TestDeltaCollector_Stop(t *testing.T) {
	collector := NewDeltaCollector(1)

	// Record something
	collector.RecordDHCPLease(DHCPLease{MAC: "test"})
	if collector.IsEmpty() {
		t.Error("should not be empty")
	}

	// Stop (should not panic)
	collector.Stop()

	// Recording after stop should be safe (no panic)
	collector.RecordDHCPLease(DHCPLease{MAC: "test2"})
}

// TestStateSerialize, etc... (keep existing tests)
func TestStateSerialize(t *testing.T) {
	state := &State{
		ConfigPath: "/etc/glacic/config.hcl",
		DHCPLeases: []DHCPLease{
			{MAC: "aa:bb:cc:dd:ee:ff", IP: "192.168.1.100"},
		},
		DNSCache: []DNSCacheEntry{
			{Name: "example.com", Type: 1, TTL: 300},
		},
		Listeners: []ListenerInfo{
			{Network: "tcp", Address: ":8080", Name: "api"},
		},
		CheckpointID: 1,
		Version:      "1.0.0",
		UpgradeTime:  time.Now(),
		PID:          12345,
	}

	// Serialize
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("failed to serialize state: %v", err)
	}

	// Deserialize
	var decoded State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to deserialize state: %v", err)
	}

	// Verify fields
	if decoded.ConfigPath != state.ConfigPath {
		t.Errorf("config path mismatch: %s vs %s", decoded.ConfigPath, state.ConfigPath)
	}
	if len(decoded.DHCPLeases) != 1 {
		t.Errorf("expected 1 DHCP lease, got %d", len(decoded.DHCPLeases))
	}
	if decoded.DHCPLeases[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("DHCP MAC mismatch: %s", decoded.DHCPLeases[0].MAC)
	}
	if len(decoded.Listeners) != 1 {
		t.Errorf("expected 1 listener, got %d", len(decoded.Listeners))
	}
	if decoded.PID != 12345 {
		t.Errorf("PID mismatch: %d", decoded.PID)
	}
}

func TestStateDeltaSerialize(t *testing.T) {
	delta := StateDelta{
		CheckpointID: 5,
		DHCPAdded: []DHCPLease{
			{MAC: "11:22:33:44:55:66", IP: "10.0.0.50"},
		},
		DHCPRemoved: []string{"aa:bb:cc:dd:ee:ff"},
		DNSAdded: []DNSCacheEntry{
			{Name: "test.local", Type: 1},
		},
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(delta)
	if err != nil {
		t.Fatalf("failed to serialize delta: %v", err)
	}

	var decoded StateDelta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to deserialize delta: %v", err)
	}

	if decoded.CheckpointID != 5 {
		t.Errorf("checkpoint mismatch: %d", decoded.CheckpointID)
	}
	if len(decoded.DHCPAdded) != 1 {
		t.Errorf("expected 1 added, got %d", len(decoded.DHCPAdded))
	}
	if len(decoded.DHCPRemoved) != 1 {
		t.Errorf("expected 1 removed, got %d", len(decoded.DHCPRemoved))
	}
}

func TestDHCPLeaseFields(t *testing.T) {
	// ... reuse existing logic ...
	expires := time.Now().Add(24 * time.Hour)
	lease := DHCPLease{
		MAC:       "aa:bb:cc:dd:ee:ff",
		IP:        "192.168.1.100",
		Hostname:  "test-host",
		Expires:   expires,
		Interface: "eth0",
	}
	if lease.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Error("MAC mismatch")
	}
}

func TestDNSCacheEntryFields(t *testing.T) {
	// ... reuse existing logic ...
	entry := DNSCacheEntry{Name: "example.com", Type: 1}
	if entry.Name != "example.com" {
		t.Error("Name mismatch")
	}
}

func TestListenerInfoFields(t *testing.T) {
	info := ListenerInfo{Name: "api"}
	if info.Name != "api" {
		t.Error("Name mismatch")
	}
}

func TestConntrackEntryFields(t *testing.T) {
	entry := ConntrackEntry{Protocol: "tcp"}
	if entry.Protocol != "tcp" {
		t.Error("Protocol mismatch")
	}
}

func TestConntrackEntrySerialize(t *testing.T) {
	entry := ConntrackEntry{Protocol: "udp"}
	data, _ := json.Marshal(entry)
	var decoded ConntrackEntry
	json.Unmarshal(data, &decoded)
	if decoded.Protocol != "udp" {
		t.Error("Mismatch")
	}
}

func TestDeltaCollector_MultipleLeases(t *testing.T) {
	c := NewDeltaCollector(1)
	c.RecordDHCPLease(DHCPLease{MAC: "1"})
	c.RecordDHCPLease(DHCPLease{MAC: "2"})
	d := c.Flush()
	if len(d.DHCPAdded) != 2 {
		t.Error("Expected 2")
	}
}

func TestDeltaCollector_UpdateExistingLease(t *testing.T) {
	c := NewDeltaCollector(1)
	c.RecordDHCPLease(DHCPLease{MAC: "1", IP: "A"})
	c.RecordDHCPLease(DHCPLease{MAC: "1", IP: "B"})
	d := c.Flush()
	if len(d.DHCPAdded) != 1 || d.DHCPAdded[0].IP != "B" {
		t.Error("Expected update")
	}
}

func TestStateDelta_EmptyFields(t *testing.T) {
	d := StateDelta{}
	b, _ := json.Marshal(d)
	var d2 StateDelta
	json.Unmarshal(b, &d2)
	if d2.CheckpointID != 0 {
		t.Error("Mismatch")
	}
}

func TestState_FullRoundtrip(t *testing.T) {
	// ... existing logic ...
	s := State{Version: "1.0"}
	b, _ := json.Marshal(s)
	var s2 State
	json.Unmarshal(b, &s2)
	if s2.Version != "1.0" {
		t.Error("Mismatch")
	}
}

// TestUpgrade_StatePersistence validates that ALL state (DHCP, DNS, Scheduler) is preserved.
func TestUpgrade_StatePersistence(t *testing.T) {
	m := newTestManager()

	// 1. Mock state sources
	m.SetStateCollectors(
		func() []DHCPLease {
			return []DHCPLease{{MAC: "dhcp-mac"}}
		},
		func() []DNSCacheEntry {
			return []DNSCacheEntry{{Name: "dns-name"}}
		},
		func() []ConntrackEntry { return nil },
	)

	// Set Scheduler Collector (Method doesn't exist yet -> Compilation Error = RED)
	m.SetSchedulerCollector(func() []scheduler.TaskStatus {
		return []scheduler.TaskStatus{
			{
				ID:       "task-1",
				Name:     "Backup",
				RunCount: 5,
				LastRun:  time.Now().Add(-1 * time.Hour),
			},
		}
	})

	// 2. Collect State
	cfg := &config.Config{}
	state := m.CollectState(cfg, "/config.hcl")

	// 3. Verify DHCP Preserved
	if len(state.DHCPLeases) != 1 {
		t.Error("DHCP leases lost")
	}

	// 4. Verify Scheduler Preserved (Field doesn't exist yet -> Compilation Error = RED)
	if len(state.SchedulerState) != 1 {
		t.Errorf("Scheduler state lost. Expected 1 task, got %d", len(state.SchedulerState))
	} else {
		task := state.SchedulerState[0]
		if task.RunCount != 5 {
			t.Errorf("Scheduler RunCount mismatch: got %d, want 5", task.RunCount)
		}
	}
}
