// Package upgrade implements seamless zero-downtime upgrades with socket handoff.
//
// The upgrade process:
// 1. Old process serializes state (config, DHCP leases, DNS cache, conntrack)
// 2. New process starts in "standby" mode, loads state, validates config
// 3. Old process passes listener file descriptors via Unix socket
// 4. New process takes over listeners, signals ready
// 5. Old process exits cleanly
//
// This allows upgrades without dropping connections or losing state.
package upgrade

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"grimm.is/glacic/internal/brand"
	"grimm.is/glacic/internal/clock"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/scheduler"
)

const (
	// UpgradeSocketPath is the Unix socket for upgrade coordination
	UpgradeSocketPath = "/run/firewall/upgrade.sock"
)

var (
	// StateFilePath is where state is serialized during upgrade
	StateFilePath = "/run/firewall/upgrade-state.gob"
)

const (

	// HandoffTimeout is how long to wait for handoff completion
	HandoffTimeout = 30 * time.Second

	// ReadySignal is sent by new process when ready to take over
	ReadySignal = syscall.SIGUSR1

	// HandoffSignal is sent by old process to initiate handoff
	HandoffSignal = syscall.SIGUSR2
)

// State represents the serializable state passed during upgrade.
type State struct {
	// Configuration
	ConfigPath string         `json:"config_path"`
	Config     *config.Config `json:"config"`

	// DHCP state
	DHCPLeases []DHCPLease `json:"dhcp_leases"`

	// DNS cache (simplified - just the hot entries)
	DNSCache []DNSCacheEntry `json:"dns_cache"`

	// Connection tracking state (for stateful failover)
	ConntrackEntries []ConntrackEntry `json:"conntrack_entries,omitempty"`

	// Listener info for socket handoff
	Listeners []ListenerInfo `json:"listeners"`

	// Scheduler state
	SchedulerState []scheduler.TaskStatus `json:"scheduler_state"`

	// Checkpoint for delta sync
	CheckpointID uint64 `json:"checkpoint_id"`

	// Metadata
	Version     string    `json:"version"`
	UpgradeTime time.Time `json:"upgrade_time"`
	PID         int       `json:"pid"`
}

// StateDelta represents changes since the last checkpoint.
type StateDelta struct {
	CheckpointID uint64 `json:"checkpoint_id"`

	// New or updated DHCP leases
	DHCPAdded   []DHCPLease `json:"dhcp_added,omitempty"`
	DHCPRemoved []string    `json:"dhcp_removed,omitempty"` // MACs

	// New DNS cache entries
	DNSAdded []DNSCacheEntry `json:"dns_added,omitempty"`

	// Timestamp
	Timestamp time.Time `json:"timestamp"`
}

// DHCPLease represents a DHCP lease to preserve across upgrade.
type DHCPLease struct {
	MAC       string    `json:"mac"`
	IP        string    `json:"ip"`
	Hostname  string    `json:"hostname"`
	Expires   time.Time `json:"expires"`
	Interface string    `json:"interface"`
}

// DNSCacheEntry represents a cached DNS record.
type DNSCacheEntry struct {
	Name    string    `json:"name"`
	Type    uint16    `json:"type"`
	TTL     uint32    `json:"ttl"`
	Data    []byte    `json:"data"`
	Expires time.Time `json:"expires"`
}

// ConntrackEntry represents a connection tracking entry.
type ConntrackEntry struct {
	Protocol string `json:"protocol"`
	SrcIP    string `json:"src_ip"`
	DstIP    string `json:"dst_ip"`
	SrcPort  uint16 `json:"src_port"`
	DstPort  uint16 `json:"dst_port"`
	State    string `json:"state"`
	Timeout  uint32 `json:"timeout"`
}

// ListenerInfo describes a listener to hand off.
type ListenerInfo struct {
	Network string `json:"network"` // "tcp", "udp", "unix"
	Address string `json:"address"` // ":8080", "/run/firewall.sock"
	Name    string `json:"name"`    // "api", "dns", "dhcp"
}

// Manager handles the upgrade process.
type Manager struct {
	logger    *logging.Logger
	state     *State
	listeners map[string]interface{}
	mu        sync.RWMutex

	// Checkpoint tracking
	checkpointID   uint64
	pendingDeltas  []StateDelta
	deltaCollector *DeltaCollector
	upgradeActive  bool

	// Callbacks for state collection
	collectDHCPLeases func() []DHCPLease
	collectDNSCache   func() []DNSCacheEntry
	collectConntrack  func() []ConntrackEntry
	collectScheduler  func() []scheduler.TaskStatus

	// Callbacks for state restoration
	restoreDHCPLeases func([]DHCPLease) error
	restoreDNSCache   func([]DNSCacheEntry) error
	restoreConntrack  func([]ConntrackEntry) error
	restoreScheduler  func([]scheduler.TaskStatus) error
}

// DeltaCollector accumulates state changes during upgrade.
type DeltaCollector struct {
	mu           sync.Mutex
	checkpointID uint64
	dhcpAdded    map[string]DHCPLease // keyed by MAC
	dhcpRemoved  map[string]bool
	dnsAdded     []DNSCacheEntry
	active       bool
}

// NewDeltaCollector creates a new delta collector.
func NewDeltaCollector(checkpointID uint64) *DeltaCollector {
	return &DeltaCollector{
		checkpointID: checkpointID,
		dhcpAdded:    make(map[string]DHCPLease),
		dhcpRemoved:  make(map[string]bool),
		dnsAdded:     make([]DNSCacheEntry, 0),
		active:       true,
	}
}

// RecordDHCPLease records a new or updated DHCP lease.
func (dc *DeltaCollector) RecordDHCPLease(lease DHCPLease) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if !dc.active {
		return
	}
	delete(dc.dhcpRemoved, lease.MAC)
	dc.dhcpAdded[lease.MAC] = lease
}

// RecordDHCPRelease records a released DHCP lease.
func (dc *DeltaCollector) RecordDHCPRelease(mac string) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if !dc.active {
		return
	}
	delete(dc.dhcpAdded, mac)
	dc.dhcpRemoved[mac] = true
}

// RecordDNSCache records a new DNS cache entry.
func (dc *DeltaCollector) RecordDNSCache(entry DNSCacheEntry) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if !dc.active {
		return
	}
	dc.dnsAdded = append(dc.dnsAdded, entry)
}

// Flush returns the accumulated delta and resets the collector.
func (dc *DeltaCollector) Flush() StateDelta {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	delta := StateDelta{
		CheckpointID: dc.checkpointID,
		Timestamp:    clock.Now(),
	}

	for _, lease := range dc.dhcpAdded {
		delta.DHCPAdded = append(delta.DHCPAdded, lease)
	}
	for mac := range dc.dhcpRemoved {
		delta.DHCPRemoved = append(delta.DHCPRemoved, mac)
	}
	delta.DNSAdded = dc.dnsAdded

	// Reset
	dc.checkpointID++
	dc.dhcpAdded = make(map[string]DHCPLease)
	dc.dhcpRemoved = make(map[string]bool)
	dc.dnsAdded = make([]DNSCacheEntry, 0)

	return delta
}

// Stop deactivates the collector.
func (dc *DeltaCollector) Stop() {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.active = false
}

// IsEmpty returns true if no changes have been recorded.
func (dc *DeltaCollector) IsEmpty() bool {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return len(dc.dhcpAdded) == 0 && len(dc.dhcpRemoved) == 0 && len(dc.dnsAdded) == 0
}

// NewManager creates a new upgrade manager.
func NewManager(logger *logging.Logger) *Manager {
	return &Manager{
		logger:    logger,
		listeners: make(map[string]interface{}),
	}
}

// SetStateCollectors sets the callbacks for collecting state during upgrade.
func (m *Manager) SetStateCollectors(
	dhcp func() []DHCPLease,
	dns func() []DNSCacheEntry,
	conntrack func() []ConntrackEntry,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectDHCPLeases = dhcp
	m.collectDNSCache = dns
	m.collectConntrack = conntrack
}

// SetSchedulerCollector sets the callback for collecting scheduler state.
func (m *Manager) SetSchedulerCollector(fn func() []scheduler.TaskStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectScheduler = fn
}

// SetStateRestorers sets the callbacks for restoring state after upgrade.
func (m *Manager) SetStateRestorers(
	dhcp func([]DHCPLease) error,
	dns func([]DNSCacheEntry) error,
	conntrack func([]ConntrackEntry) error,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restoreDHCPLeases = dhcp
	m.restoreDNSCache = dns
	m.restoreConntrack = conntrack
}

// SetSchedulerRestorer sets the callback for restoring scheduler state.
func (m *Manager) SetSchedulerRestorer(fn func([]scheduler.TaskStatus) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restoreScheduler = fn
}

// RegisterListener registers a listener for handoff during upgrade.
func (m *Manager) RegisterListener(name string, listener net.Listener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners[name] = listener
	m.logger.Info("Registered listener for upgrade handoff", "name", name, "type", "listener")
}

// RegisterPacketConn registers a packet connection (UDP) for handoff during upgrade.
func (m *Manager) RegisterPacketConn(name string, conn net.PacketConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners[name] = conn
	m.logger.Info("Registered packet conn for upgrade handoff", "name", name, "type", "packet_conn")
}

// UnregisterListener removes a listener from handoff tracking.
func (m *Manager) UnregisterListener(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.listeners, name)
}

// CollectState gathers all state for serialization.
func (m *Manager) CollectState(cfg *config.Config, configPath string) *State {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state := &State{
		ConfigPath:  configPath,
		Config:      cfg,
		Version:     brand.Version,
		UpgradeTime: clock.Now(),
		PID:         os.Getpid(),
		Listeners:   make([]ListenerInfo, 0),
	}

	// Collect DHCP leases
	if m.collectDHCPLeases != nil {
		state.DHCPLeases = m.collectDHCPLeases()
		m.logger.Info("Collected DHCP leases", "count", len(state.DHCPLeases))
	}

	// Collect DNS cache
	if m.collectDNSCache != nil {
		state.DNSCache = m.collectDNSCache()
		m.logger.Info("Collected DNS cache entries", "count", len(state.DNSCache))
	}

	// Collect conntrack (optional, can be large)
	if m.collectConntrack != nil {
		state.ConntrackEntries = m.collectConntrack()
		m.logger.Info("Collected conntrack entries", "count", len(state.ConntrackEntries))
	}

	// Collect scheduler state
	if m.collectScheduler != nil {
		state.SchedulerState = m.collectScheduler()
		m.logger.Info("Collected scheduler state", "count", len(state.SchedulerState))
	}

	// Record listener info
	for name, l := range m.listeners {
		var network, address string
		if listener, ok := l.(net.Listener); ok {
			network = listener.Addr().Network()
			address = listener.Addr().String()
		} else if pc, ok := l.(net.PacketConn); ok {
			network = pc.LocalAddr().Network()
			address = pc.LocalAddr().String()
		}

		state.Listeners = append(state.Listeners, ListenerInfo{
			Network: network,
			Address: address,
			Name:    name,
		})
	}

	return state
}

// SaveState serializes state to disk.
func (m *Manager) SaveState(state *State) error {
	// Ensure directory exists
	dir := filepath.Dir(StateFilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Write state file
	f, err := os.Create(StateFilePath)
	if err != nil {
		return fmt.Errorf("failed to create state file: %w", err)
	}
	defer f.Close()

	encoder := gob.NewEncoder(f)
	if err := encoder.Encode(state); err != nil {
		return fmt.Errorf("failed to encode state: %w", err)
	}

	m.logger.Info("Saved upgrade state", "path", StateFilePath)
	return nil
}

// LoadState deserializes state from disk.
func (m *Manager) LoadState() (*State, error) {
	f, err := os.Open(StateFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open state file: %w", err)
	}
	defer f.Close()

	var state State
	decoder := gob.NewDecoder(f)
	if err := decoder.Decode(&state); err != nil {
		return nil, fmt.Errorf("failed to decode state: %w", err)
	}

	m.state = &state
	m.logger.Info("Loaded upgrade state",
		"version", state.Version,
		"pid", state.PID,
		"leases", len(state.DHCPLeases),
		"dns_cache", len(state.DNSCache),
	)

	return &state, nil
}

// RestoreState applies loaded state to the running services.
func (m *Manager) RestoreState(state *State) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Restore DHCP leases
	if m.restoreDHCPLeases != nil && len(state.DHCPLeases) > 0 {
		if err := m.restoreDHCPLeases(state.DHCPLeases); err != nil {
			m.logger.Warn("Failed to restore DHCP leases", "error", err)
		} else {
			m.logger.Info("Restored DHCP leases", "count", len(state.DHCPLeases))
		}
	}

	// Restore DNS cache
	if m.restoreDNSCache != nil && len(state.DNSCache) > 0 {
		if err := m.restoreDNSCache(state.DNSCache); err != nil {
			m.logger.Warn("Failed to restore DNS cache", "error", err)
		} else {
			m.logger.Info("Restored DNS cache", "count", len(state.DNSCache))
		}
	}

	// Restore conntrack entries
	if m.restoreConntrack != nil && len(state.ConntrackEntries) > 0 {
		if err := m.restoreConntrack(state.ConntrackEntries); err != nil {
			m.logger.Warn("Failed to restore conntrack entries", "error", err)
		} else {
			m.logger.Info("Restored conntrack entries", "count", len(state.ConntrackEntries))
		}
	}

	// Restore scheduler state
	if m.restoreScheduler != nil && len(state.SchedulerState) > 0 {
		if err := m.restoreScheduler(state.SchedulerState); err != nil {
			m.logger.Warn("Failed to restore scheduler state", "error", err)
		} else {
			m.logger.Info("Restored scheduler state", "count", len(state.SchedulerState))
		}
	}

	return nil
}

// CleanupState removes the state file after successful upgrade.
func (m *Manager) CleanupState() error {
	if err := os.Remove(StateFilePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// InitiateUpgrade starts the upgrade process from the old process.
func (m *Manager) InitiateUpgrade(ctx context.Context, newBinaryPath string, cfg *config.Config, configPath string) error {
	// Mark this process as the old one in logs
	logging.SetPrefix("GLACIC-OLD")

	m.logger.Info("Initiating seamless upgrade", "new_binary", newBinaryPath)

	// 1. Start delta collection BEFORE serializing state
	m.mu.Lock()
	m.checkpointID++
	m.deltaCollector = NewDeltaCollector(m.checkpointID)
	m.upgradeActive = true
	m.mu.Unlock()

	m.logger.Info("Started delta collection", "checkpoint_id", m.checkpointID)

	// 2. Collect and save initial state
	state := m.CollectState(cfg, configPath)
	state.CheckpointID = m.checkpointID
	if err := m.SaveState(state); err != nil {
		m.stopDeltaCollection()
		return fmt.Errorf("failed to save state: %w", err)
	}

	// 3. Create upgrade coordination socket
	if err := os.MkdirAll(filepath.Dir(UpgradeSocketPath), 0755); err != nil {
		m.stopDeltaCollection()
		return fmt.Errorf("failed to create socket directory: %w", err)
	}
	os.Remove(UpgradeSocketPath) // Remove stale socket

	listener, err := net.Listen("unix", UpgradeSocketPath)
	if err != nil {
		m.stopDeltaCollection()
		return fmt.Errorf("failed to create upgrade socket: %w", err)
	}
	defer listener.Close()

	// 4. Start new process in standby mode
	// We use exec.Command instead of CommandContext effectively "detaching" the lifecycle
	// from the RPC context. We must manually kill it if the handshake fails.
	cmd := exec.Command(newBinaryPath, "--config", configPath)

	// Override argv[0] so 'ps' shows reasonable name (e.g. /usr/sbin/glacic) not .../glacic_new
	cmd.Args[0] = os.Args[0]

	cmd.Stdout = logging.OriginalStdout
	// CRITICAL: We MUST use the original file descriptor directly (no io.MultiWriter or Pipes).
	// If we use io.MultiWriter(OriginalStderr, buf), Go creates a pipe to capture the output.
	// When this parent process (glacic-old) exits, that pipe is closed.
	// If the child (glacic-new) tries to write to it (e.g. log output), it receives SIGPIPE and crashes.
	// By assigning the *os.File directly, the child inherits the file descriptor which remains valid
	// even after the parent exits, ensuring log continuity and stability.
	cmd.Stderr = logging.OriginalStderr
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("FIREWALL_UPGRADE_PID=%d", os.Getpid()),
		"GLACIC_UPGRADE_STANDBY=1",
	)

	// Create a localized context for the startup/handshake phase effectively
	// behaving like CommandContext but only until we decide it's a success

	if err := cmd.Start(); err != nil {
		m.stopDeltaCollection()
		return fmt.Errorf("failed to start new process: %w", err)
	}

	newPID := cmd.Process.Pid
	m.logger.Info("Started new process in standby mode", "new_pid", newPID, "old_pid", os.Getpid())

	// Monitor for early exit of the new process
	processExitCh := make(chan error, 1)
	go func() {
		processExitCh <- cmd.Wait()
	}()

	// Helper to kill new process on failure
	killNewProcess := func() {
		if cmd.Process != nil {
			// Check if process is already dead to avoid redundant kill log/action?
			// But Kill is idempotent-ish (returns error if dead).
			m.logger.Info("Killing new process due to upgrade failure", "pid", newPID)
			cmd.Process.Kill()
		}
	}

	// 5. Wait for new process to connect and signal ready
	connCh := make(chan net.Conn)
	errCh := make(chan error, 1)
	go func() {
		// Accept loop
		for {
			conn, err := listener.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					errCh <- err
				}
				return
			}
			connCh <- conn
		}
	}()

	var upgradeConn net.Conn
	select {
	case conn := <-connCh:
		upgradeConn = conn
	case exitErr := <-processExitCh:
		m.stopDeltaCollection()
		// Process exited before connecting. Return stdout/stderr details.
		return fmt.Errorf("new process exited prematurely: %v. Check log file for details.", exitErr)
	case <-time.After(HandoffTimeout):
		killNewProcess()
		m.stopDeltaCollection()
		return fmt.Errorf("timeout waiting for new process connection. Check log file for details.")
	}

	// Handle Phase 1: Handshake and Deltas
	err = func() error {
		defer upgradeConn.Close()

		// 6. Wait for ready message
		if err := m.waitForReadyMessage(upgradeConn, cmd.Process.Pid); err != nil {
			return fmt.Errorf("new process failed to become ready: %w", err)
		}

		// 7. Send accumulated deltas
		if err := m.sendDeltas(upgradeConn); err != nil {
			return fmt.Errorf("failed to send deltas: %w", err)
		}

		// 8. Stop delta collection and send final delta
		m.stopDeltaCollection()
		if err := m.sendFinalDelta(upgradeConn); err != nil {
			return fmt.Errorf("failed to send final delta: %w", err)
		}
		return nil
	}()

	if err != nil {
		killNewProcess()
		return err
	}

	m.logger.Info("Delta sync complete. Waiting for Phase 2 connection...")

	// Phase 2: Listener Handoff (New connection)
	var listenerConn net.Conn
	select {
	case conn := <-connCh:
		listenerConn = conn
	case err := <-errCh:
		killNewProcess()
		return fmt.Errorf("failed to accept listener connection: %w", err)
	case <-ctx.Done():
		killNewProcess()
		return ctx.Err()
	case <-time.After(HandoffTimeout):
		killNewProcess()
		return fmt.Errorf("timeout waiting for listener connection")
	}
	defer listenerConn.Close()

	// 9. Hand off listeners
	if err := m.handoffListeners(ctx, listenerConn); err != nil {
		killNewProcess()
		return fmt.Errorf("failed to hand off listeners: %w", err)
	}

	// 10. Hand off complete
	m.logger.Info("Upgrade complete, exiting old process")
	return nil
}

// stopDeltaCollection stops the delta collector.
func (m *Manager) stopDeltaCollection() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deltaCollector != nil {
		m.deltaCollector.Stop()
	}
	m.upgradeActive = false
}

// sendDeltas sends accumulated deltas to the new process.
func (m *Manager) sendDeltas(conn net.Conn) error {
	m.mu.RLock()
	dc := m.deltaCollector
	m.mu.RUnlock()

	if dc == nil || dc.IsEmpty() {
		m.logger.Info("No deltas to send")
		return nil
	}

	delta := dc.Flush()
	m.logger.Info("Sending delta",
		"checkpoint", delta.CheckpointID,
		"dhcp_added", len(delta.DHCPAdded),
		"dhcp_removed", len(delta.DHCPRemoved),
		"dns_added", len(delta.DNSAdded),
	)

	encoder := json.NewEncoder(conn)
	msg := upgradeMessage{
		Type:  "delta",
		Delta: &delta,
	}
	return encoder.Encode(msg)
}

// sendFinalDelta sends the final delta after stopping collection.
func (m *Manager) sendFinalDelta(conn net.Conn) error {
	m.mu.RLock()
	dc := m.deltaCollector
	m.mu.RUnlock()

	if dc == nil {
		return nil
	}

	// Get any remaining changes
	delta := dc.Flush()
	if len(delta.DHCPAdded) == 0 && len(delta.DHCPRemoved) == 0 && len(delta.DNSAdded) == 0 {
		m.logger.Info("No final delta to send")
		// Send empty final marker
		encoder := json.NewEncoder(conn)
		return encoder.Encode(upgradeMessage{Type: "delta_complete"})
	}

	m.logger.Info("Sending final delta",
		"dhcp_added", len(delta.DHCPAdded),
		"dhcp_removed", len(delta.DHCPRemoved),
		"dns_added", len(delta.DNSAdded),
	)

	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(upgradeMessage{Type: "delta", Delta: &delta}); err != nil {
		return err
	}
	return encoder.Encode(upgradeMessage{Type: "delta_complete"})
}

// waitForReadyMessage reads the ready message from the new process.
func (m *Manager) waitForReadyMessage(conn net.Conn, newPID int) error {
	var msg upgradeMessage
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&msg); err != nil {
		return fmt.Errorf("failed to read ready message: %w", err)
	}

	if msg.Type != "ready" {
		return fmt.Errorf("unexpected message type: %s (error: %s)", msg.Type, msg.Error)
	}

	if msg.PID != newPID {
		return fmt.Errorf("PID mismatch: expected %d, got %d", newPID, msg.PID)
	}

	m.logger.Info("New process is ready", "pid", newPID)
	return nil
}

// waitForReady waits for the new process to signal it's ready.
func (m *Manager) waitForReady(ctx context.Context, listener net.Listener, newPID int) error {
	// Accept connection from new process
	conn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("failed to accept upgrade connection: %w", err)
	}
	defer conn.Close()

	// Read ready message
	var msg upgradeMessage
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&msg); err != nil {
		return fmt.Errorf("failed to read ready message: %w", err)
	}

	if msg.Type != "ready" {
		return fmt.Errorf("unexpected message type: %s (error: %s)", msg.Type, msg.Error)
	}

	if msg.PID != newPID {
		return fmt.Errorf("PID mismatch: expected %d, got %d", newPID, msg.PID)
	}

	m.logger.Info("New process is ready", "pid", newPID)
	return nil
}

// handoffListeners passes listener file descriptors to the new process.
func (m *Manager) handoffListeners(ctx context.Context, conn net.Conn) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a unix connection")
	}

	// Send each listener's file descriptor
	for name, listener := range m.listeners {
		// Get the file descriptor from the listener
		file, err := getListenerFile(listener)
		if err != nil {
			m.logger.Warn("Failed to get listener file", "name", name, "error", err)
			continue
		}

		// Send the file descriptor
		rights := syscall.UnixRights(int(file.Fd()))

		// Encode type in name for receiver: "name|type"
		// type: "L" (Listener), "P" (PacketConn)
		typeCode := "L"
		if _, ok := listener.(net.PacketConn); ok {
			typeCode = "P"
		}

		msg := []byte(fmt.Sprintf("%s|%s", name, typeCode))
		_, _, err = unixConn.WriteMsgUnix(msg, rights, nil)
		file.Close()

		if err != nil {
			return fmt.Errorf("failed to send listener %s: %w", name, err)
		}

		m.logger.Info("Handed off listener", "name", name, "type", typeCode)
	}

	// No completion message needed. Closing the socket signals EOF.
	return nil
}

// getListenerFile extracts the file descriptor from a listener or packet conn.
func getListenerFile(l interface{}) (*os.File, error) {
	switch v := l.(type) {
	case *net.TCPListener:
		return v.File()
	case *net.UnixListener:
		return v.File()
	case *net.UDPConn:
		return v.File()
	case *net.UnixConn:
		return v.File()
	default:
		return nil, fmt.Errorf("unsupported listener type: %T", l)
	}
}

// RunStandby runs the new process in standby mode during upgrade.
func (m *Manager) RunStandby(ctx context.Context, configPath string) error {
	m.logger.Info("Starting in upgrade standby mode")

	// 1. Load state from old process
	state, err := m.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// 2. Load and validate configuration
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 3. Validate configuration
	if errs := cfg.Validate(); len(errs) > 0 {
		errMsg := fmt.Sprintf("config validation failed: %v", errs)
		m.sendError(errMsg)
		return fmt.Errorf("config validation failed: %v", errs)
	}

	m.logger.Info("Configuration validated successfully")

	// 4. Restore initial state
	if err := m.RestoreState(state); err != nil {
		m.logger.Warn("Failed to restore some state", "error", err)
	}

	// 5. Connect to old process and signal ready
	conn, err := net.Dial("unix", UpgradeSocketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to upgrade socket: %w", err)
	}
	defer conn.Close()

	// Send ready message
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(upgradeMessage{
		Type: "ready",
		PID:  os.Getpid(),
	}); err != nil {
		return fmt.Errorf("failed to send ready message: %w", err)
	}

	// 6. Receive and apply deltas (changes since initial state)
	if err := m.receiveDeltas(conn); err != nil {
		return fmt.Errorf("failed to receive deltas: %w", err)
	}

	// 7. Receive listener file descriptors (Phase 2)
	// Reconnect for raw listener handoff
	conn2, err := net.Dial("unix", UpgradeSocketPath)
	if err != nil {
		return fmt.Errorf("failed to connect for listener handoff: %w", err)
	}
	defer conn2.Close()

	if err := m.receiveListeners(ctx, conn2); err != nil {
		return fmt.Errorf("failed to receive listeners: %w", err)
	}

	// 7. Cleanup state file
	m.CleanupState()

	m.logger.Info("Upgrade standby complete, taking over")
	return nil
}

// receiveListeners receives listener file descriptors from the old process.
func (m *Manager) receiveListeners(ctx context.Context, conn net.Conn) error {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("not a unix connection")
	}

	buf := make([]byte, 1024)
	oob := make([]byte, 1024)

	for {
		n, oobn, _, _, err := unixConn.ReadMsgUnix(buf, oob)
		if err != nil {
			if errors.Is(err, io.EOF) {
				m.logger.Info("Listener handoff complete (EOF)")
				return nil
			}
			return fmt.Errorf("failed to read message: %w", err)
		}

		if n == 0 {
			// EOF means sender finished writing all listeners and closed connection
			m.logger.Info("Listener handoff complete (EOF)")
			return nil
		}

		name := string(buf[:n])

		// Parse the file descriptor from OOB data
		scms, err := syscall.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			m.logger.Warn("Failed to parse control message", "error", err)
			continue
		}

		for _, scm := range scms {
			fds, err := syscall.ParseUnixRights(&scm)
			if err != nil {
				continue
			}

			for _, fd := range fds {
				// Parse name and type: "name|type"
				parts := strings.Split(name, "|")
				realName := name // legacy fallback
				typeName := "L"

				if len(parts) == 2 {
					realName = parts[0]
					typeName = parts[1]
				}

				file := os.NewFile(uintptr(fd), realName)
				var stored interface{}
				var addr net.Addr

				if typeName == "P" {
					// PacketConn (UDP)
					pc, err := net.FilePacketConn(file)
					if err != nil {
						m.logger.Warn("Failed to recover PacketConn", "name", realName, "error", err)
						file.Close()
						continue
					}
					stored = pc
					addr = pc.LocalAddr()
				} else {
					// Listener (TCP/Unix)
					l, err := net.FileListener(file)
					if err != nil {
						m.logger.Warn("Failed to recover Listener", "name", realName, "error", err)
						file.Close()
						continue
					}
					stored = l
					addr = l.Addr()
				}

				file.Close() // FileListener/PacketConn dupes the FD, so we close ours

				m.mu.Lock()
				m.listeners[realName] = stored
				m.mu.Unlock()

				m.logger.Info("Received socket", "name", realName, "type", typeName, "addr", addr)
			}
		}
	}
}

// sendError sends an error message to the old process.
func (m *Manager) sendError(errMsg string) {
	conn, err := net.Dial("unix", UpgradeSocketPath)
	if err != nil {
		return
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	encoder.Encode(upgradeMessage{
		Type:  "error",
		Error: errMsg,
		PID:   os.Getpid(),
	})
}

// GetListener returns a received listener by name.
func (m *Manager) GetListener(name string) (net.Listener, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.listeners[name]
	if !ok {
		return nil, false
	}
	if listener, ok := l.(net.Listener); ok {
		return listener, true
	}
	return nil, false
}

// GetPacketConn returns a received packet connection by name.
func (m *Manager) GetPacketConn(name string) (net.PacketConn, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.listeners[name]
	if !ok {
		return nil, false
	}
	if pc, ok := l.(net.PacketConn); ok {
		return pc, true
	}
	return nil, false
}

// receiveDeltas receives and applies state deltas from the old process.
func (m *Manager) receiveDeltas(conn net.Conn) error {
	decoder := json.NewDecoder(conn)

	for {
		var msg upgradeMessage
		if err := decoder.Decode(&msg); err != nil {
			return fmt.Errorf("failed to decode delta message: %w", err)
		}

		switch msg.Type {
		case "delta":
			if msg.Delta != nil {
				if err := m.applyDelta(msg.Delta); err != nil {
					m.logger.Warn("Failed to apply delta", "error", err)
				}
			}
		case "delta_complete":
			m.logger.Info("Delta sync complete")
			return nil
		case "handoff_complete":
			// No deltas were sent, proceed to listener handoff
			m.logger.Info("No deltas, proceeding to handoff")
			return nil
		default:
			m.logger.Warn("Unexpected message type during delta sync", "type", msg.Type)
		}
	}
}

// applyDelta applies a state delta to the running services.
func (m *Manager) applyDelta(delta *StateDelta) error {
	m.logger.Info("Applying delta",
		"checkpoint", delta.CheckpointID,
		"dhcp_added", len(delta.DHCPAdded),
		"dhcp_removed", len(delta.DHCPRemoved),
		"dns_added", len(delta.DNSAdded),
	)

	// Apply DHCP lease additions
	if m.restoreDHCPLeases != nil && len(delta.DHCPAdded) > 0 {
		if err := m.restoreDHCPLeases(delta.DHCPAdded); err != nil {
			m.logger.Warn("Failed to apply DHCP additions", "error", err)
		}
	}

	// Apply DNS cache additions
	if m.restoreDNSCache != nil && len(delta.DNSAdded) > 0 {
		if err := m.restoreDNSCache(delta.DNSAdded); err != nil {
			m.logger.Warn("Failed to apply DNS additions", "error", err)
		}
	}

	// Note: DHCP removals would need a separate callback
	// For now, we just log them - leases will expire naturally
	if len(delta.DHCPRemoved) > 0 {
		m.logger.Info("DHCP leases released during upgrade", "count", len(delta.DHCPRemoved))
	}

	return nil
}

// RecordDHCPLease records a DHCP lease change during upgrade.
// Call this from the DHCP service when a lease is granted.
func (m *Manager) RecordDHCPLease(lease DHCPLease) {
	m.mu.RLock()
	dc := m.deltaCollector
	active := m.upgradeActive
	m.mu.RUnlock()

	if active && dc != nil {
		dc.RecordDHCPLease(lease)
	}
}

// RecordDHCPRelease records a DHCP lease release during upgrade.
// Call this from the DHCP service when a lease is released.
func (m *Manager) RecordDHCPRelease(mac string) {
	m.mu.RLock()
	dc := m.deltaCollector
	active := m.upgradeActive
	m.mu.RUnlock()

	if active && dc != nil {
		dc.RecordDHCPRelease(mac)
	}
}

// RecordDNSCache records a DNS cache entry during upgrade.
// Call this from the DNS service when a new entry is cached.
func (m *Manager) RecordDNSCache(entry DNSCacheEntry) {
	m.mu.RLock()
	dc := m.deltaCollector
	active := m.upgradeActive
	m.mu.RUnlock()

	if active && dc != nil {
		dc.RecordDNSCache(entry)
	}
}

// IsUpgradeActive returns true if an upgrade is in progress.
func (m *Manager) IsUpgradeActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.upgradeActive
}

// GetDeltaCollector returns the delta collector for direct access.
// This is useful for services that need to record changes efficiently.
func (m *Manager) GetDeltaCollector() *DeltaCollector {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.deltaCollector
}

// upgradeMessage is the protocol message for upgrade coordination.
type upgradeMessage struct {
	Type  string      `json:"type"` // "ready", "error", "delta", "delta_complete", "handoff_complete"
	PID   int         `json:"pid,omitempty"`
	Error string      `json:"error,omitempty"`
	Delta *StateDelta `json:"delta,omitempty"`
}

// SetupSignalHandler sets up signal handling for upgrade coordination.
func (m *Manager) SetupSignalHandler(ctx context.Context, onUpgrade func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, HandoffSignal)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-sigCh:
				if sig == HandoffSignal {
					m.logger.Info("Received upgrade signal")
					if onUpgrade != nil {
						onUpgrade()
					}
				}
			}
		}
	}()
}
