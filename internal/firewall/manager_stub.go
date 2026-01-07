//go:build !linux
// +build !linux

package firewall

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/services"
)

// ErrNotSupported is returned when firewall operations are attempted on non-Linux systems.
var ErrNotSupported = fmt.Errorf("firewall operations not supported on %s", runtime.GOOS)

// Manager handles firewall rules (stub for non-Linux).
type Manager struct{}

// NewManager creates a new firewall manager (stub for non-Linux).
func NewManager(logger *logging.Logger, cacheDir string) (*Manager, error) {
	return &Manager{}, nil
}

// NewManagerWithConn creates a new firewall manager with injected connection (stub for non-Linux).
func NewManagerWithConn(conn interface{}, logger *logging.Logger, cacheDir string) *Manager {
	return &Manager{}
}

// ApplyConfig applies the firewall configuration (stub for non-Linux).
func (m *Manager) ApplyConfig(cfg *Config) error {
	return ErrNotSupported
}

// Name returns the service name.
func (m *Manager) Name() string {
	return "Firewall"
}

// Start starts the service.
func (m *Manager) Start(ctx context.Context) error {
	return ErrNotSupported
}

// Stop stops the service.
func (m *Manager) Stop(ctx context.Context) error {
	return nil
}

// Reload reloads the service configuration.
func (m *Manager) Reload(cfg *config.Config) (bool, error) {
	return false, ErrNotSupported
}

// IsRunning returns whether the service is running.
func (m *Manager) IsRunning() bool {
	return false
}

// Status returns the current status of the service.
func (m *Manager) Status() services.ServiceStatus {
	return services.ServiceStatus{
		Name:    m.Name(),
		Running: false,
	}
}

// AddDynamicNATRule helper for UPnP (stub).
func (m *Manager) AddDynamicNATRule(rule config.NATRule) error {
	return ErrNotSupported
}

// RemoveDynamicNATRule helper for UPnP (stub).
func (m *Manager) RemoveDynamicNATRule(match func(config.NATRule) bool) error {
	return ErrNotSupported
}

// GenerateRules generates the nftables ruleset script (Stub).
func (m *Manager) GenerateRules(cfg *Config) (string, error) {
	// 1. Build filter table script (no metadata on non-Linux)
	filterScript, err := BuildFilterTableScript(cfg, cfg.VPN, "glacic", "")
	if err != nil {
		return "", fmt.Errorf("failed to build filter table script: %w", err)
	}

	// 2. Build NAT table script (if needed)
	natScript, err := BuildNATTableScript(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to build NAT table script: %w", err)
	}

	// 3. Build Mangle table script (Management Routing)
	mangleScript, err := BuildMangleTableScript(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to build mangle table script: %w", err)
	}

	// 4. Combine scripts with atomic flush + rebuild
	var combinedScript strings.Builder

	// Flush entire ruleset first - this is atomic with the new rules
	combinedScript.WriteString("flush ruleset\n")

	// Add filter table
	combinedScript.WriteString(filterScript.Build())

	// Add NAT table if present
	if natScript != nil {
		combinedScript.WriteString(natScript.Build())
	}

	// Add Mangle table if present
	if mangleScript != nil {
		combinedScript.WriteString(mangleScript.Build())
	}

	return combinedScript.String(), nil
}

// ApplyScheduledRule stub.
func (m *Manager) ApplyScheduledRule(rule config.ScheduledRule, enabled bool) error {
	return ErrNotSupported
}

// MonitorIntegrity is a stub for non-Linux systems.
func (m *Manager) MonitorIntegrity(ctx context.Context, cfg *config.Config) {
	// No-op
}

// TrafficStats holds traffic counters for an interface or zone.
type TrafficStats struct {
	Name    string `json:"name"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

// AccountingStats holds all traffic accounting data.
type AccountingStats struct {
	Interfaces []TrafficStats `json:"interfaces"`
	Zones      []TrafficStats `json:"zones"`
}

// Accounting manages traffic accounting rules and counters (stub for non-Linux).
type Accounting struct{}

// NewAccounting creates a new accounting manager (stub for non-Linux).
func NewAccounting(conn interface{}) *Accounting {
	return &Accounting{}
}

// SetupAccounting creates accounting chains and rules (stub for non-Linux).
func (a *Accounting) SetupAccounting(interfaces []string, zones map[string][]string) error {
	return ErrNotSupported
}

// GetStats retrieves current traffic accounting statistics (stub for non-Linux).
func (a *Accounting) GetStats() (*AccountingStats, error) {
	return nil, ErrNotSupported
}

// QoSConfig defines Quality of Service configuration.
type QoSConfig struct {
	Enabled    bool       `hcl:"enabled,optional" json:"enabled"`
	Interfaces []QoSIface `hcl:"interface,block" json:"interfaces"`
	Classes    []QoSClass `hcl:"class,block" json:"classes"`
	Rules      []QoSRule  `hcl:"rule,block" json:"rules"`
}

// QoSIface defines QoS settings for an interface.
type QoSIface struct {
	Name         string `hcl:"name,label" json:"name"`
	DownloadRate string `hcl:"download,optional" json:"download"`
	UploadRate   string `hcl:"upload,optional" json:"upload"`
}

// QoSClass defines a traffic class with guaranteed/max bandwidth.
type QoSClass struct {
	Name     string `hcl:"name,label" json:"name"`
	Priority int    `hcl:"priority,optional" json:"priority"`
	Rate     string `hcl:"rate,optional" json:"rate"`
	Ceil     string `hcl:"ceil,optional" json:"ceil"`
	Burst    string `hcl:"burst,optional" json:"burst"`
}

// QoSRule maps traffic to a class.
type QoSRule struct {
	Name     string   `hcl:"name,label" json:"name"`
	Class    string   `hcl:"class" json:"class"`
	Services []string `hcl:"services,optional" json:"services"`
	Ports    []int    `hcl:"ports,optional" json:"ports"`
	Protocol string   `hcl:"proto,optional" json:"proto"`
	SrcIP    string   `hcl:"src_ip,optional" json:"src_ip"`
	DstIP    string   `hcl:"dst_ip,optional" json:"dst_ip"`
	DSCP     string   `hcl:"dscp,optional" json:"dscp"`
}

// QoSManager manages traffic shaping (stub for non-Linux).
type QoSManager struct {
	config *QoSConfig
}

// NewQoSManager creates a new QoS manager (stub for non-Linux).
func NewQoSManager() *QoSManager {
	return &QoSManager{}
}

// Apply applies QoS configuration (stub for non-Linux).
func (m *QoSManager) Apply(cfg *QoSConfig) error {
	return ErrNotSupported
}

// PredefinedQoSProfiles contains predefined QoS profiles.
var PredefinedQoSProfiles = map[string]*QoSConfig{
	"gaming": {
		Enabled: true,
		Classes: []QoSClass{
			{Name: "realtime", Priority: 1, Rate: "30%", Ceil: "100%"},
			{Name: "interactive", Priority: 2, Rate: "40%", Ceil: "100%"},
			{Name: "bulk", Priority: 5, Rate: "20%", Ceil: "90%"},
		},
	},
	"voip": {
		Enabled: true,
		Classes: []QoSClass{
			{Name: "voice", Priority: 1, Rate: "20%", Ceil: "100%"},
			{Name: "signaling", Priority: 2, Rate: "10%", Ceil: "50%"},
			{Name: "default", Priority: 4, Rate: "60%", Ceil: "100%"},
		},
	},
	"balanced": {
		Enabled: true,
		Classes: []QoSClass{
			{Name: "high", Priority: 2, Rate: "40%", Ceil: "100%"},
			{Name: "normal", Priority: 4, Rate: "40%", Ceil: "100%"},
			{Name: "low", Priority: 6, Rate: "20%", Ceil: "80%"},
		},
	},
}

// dscpValues maps DSCP names to values.
var dscpValues = map[string]int{
	"ef":   46,
	"af11": 10, "af12": 12, "af13": 14,
	"af21": 18, "af22": 20, "af23": 22,
	"af31": 26, "af32": 28, "af33": 30,
	"af41": 34, "af42": 36, "af43": 38,
	"cs0": 0, "cs1": 8, "cs2": 16, "cs3": 24,
	"cs4": 32, "cs5": 40, "cs6": 48, "cs7": 56,
}

// parseRate parses a rate string like "100mbit" to bytes per second.
func parseRate(rate string) uint64 {
	// Stub implementation for non-Linux
	return 0
}

// privateNetworks contains RFC1918 private address ranges.
var privateNetworks = []struct {
	ip   []byte
	mask []byte
}{
	{[]byte{10, 0, 0, 0}, []byte{255, 0, 0, 0}},
	{[]byte{172, 16, 0, 0}, []byte{255, 240, 0, 0}},
	{[]byte{192, 168, 0, 0}, []byte{255, 255, 0, 0}},
}

// bogonNetworks contains reserved/invalid address ranges.
var bogonNetworks = []struct {
	ip   []byte
	mask []byte
}{
	{[]byte{0, 0, 0, 0}, []byte{255, 0, 0, 0}},
	{[]byte{127, 0, 0, 0}, []byte{255, 0, 0, 0}},
	{[]byte{169, 254, 0, 0}, []byte{255, 255, 0, 0}},
	{[]byte{192, 0, 0, 0}, []byte{255, 255, 255, 0}},
	{[]byte{192, 0, 2, 0}, []byte{255, 255, 255, 0}},
	{[]byte{198, 51, 100, 0}, []byte{255, 255, 255, 0}},
	{[]byte{203, 0, 113, 0}, []byte{255, 255, 255, 0}},
	{[]byte{224, 0, 0, 0}, []byte{240, 0, 0, 0}},
	{[]byte{240, 0, 0, 0}, []byte{240, 0, 0, 0}},
}

// CommandRunner abstracts shell command execution.
type CommandRunner interface {
	Run(name string, args ...string) error
	RunInput(input string, name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

// RealCommandRunner executes actual shell commands (stub for non-Linux).
type RealCommandRunner struct{}

// Run returns an error on non-Linux systems.
func (r *RealCommandRunner) Run(name string, args ...string) error {
	return ErrNotSupported
}

// Output returns an error on non-Linux systems.
func (r *RealCommandRunner) Output(name string, args ...string) ([]byte, error) {
	return nil, ErrNotSupported
}

// RunInput returns an error on non-Linux systems.
func (r *RealCommandRunner) RunInput(input string, name string, args ...string) error {
	return ErrNotSupported
}

// DefaultCommandRunner is the default command runner.
var DefaultCommandRunner CommandRunner = &RealCommandRunner{}

// PreRenderSafeMode generates and caches safe mode ruleset (stub for non-Linux).
func (m *Manager) PreRenderSafeMode(cfg *Config) {
	// No-op on non-Linux
}

// ApplySafeMode applies safe mode ruleset (stub for non-Linux).
func (m *Manager) ApplySafeMode() error {
	return ErrNotSupported
}

// ExitSafeMode exits safe mode (stub for non-Linux).
func (m *Manager) ExitSafeMode() error {
	return ErrNotSupported
}

// IsInSafeMode returns safe mode status (stub for non-Linux).
func (m *Manager) IsInSafeMode() bool {
	return false
}
