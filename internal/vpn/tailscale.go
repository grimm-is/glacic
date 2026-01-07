// Package vpn provides VPN integrations including Tailscale, WireGuard, and others.
// These provide secure remote access that survives firewall misconfigurations.
package vpn

import (
	"context"
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/logging"
)

// TailscaleStatus represents the current Tailscale connection status.
type TailscaleStatus struct {
	Running          bool            `json:"running"`
	BackendState     string          `json:"backend_state"` // Running, Stopped, NeedsLogin, etc.
	TailscaleIP      string          `json:"tailscale_ip"`
	TailscaleIPv6    string          `json:"tailscale_ipv6,omitempty"`
	Hostname         string          `json:"hostname"`
	DNSName          string          `json:"dns_name"`
	MagicDNSSuffix   string          `json:"magic_dns_suffix"`
	ControlURL       string          `json:"control_url"`
	Online           bool            `json:"online"`
	ExitNode         bool            `json:"exit_node"`
	ExitNodeActive   bool            `json:"exit_node_active"`
	AdvertisedRoutes []string        `json:"advertised_routes"`
	Peers            []TailscalePeer `json:"peers"`
	LastUpdate       time.Time       `json:"last_update"`
}

// TailscalePeer represents a connected Tailscale peer.
type TailscalePeer struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	DNSName      string    `json:"dns_name"`
	TailscaleIP  string    `json:"tailscale_ip"`
	Online       bool      `json:"online"`
	LastSeen     time.Time `json:"last_seen"`
	ExitNode     bool      `json:"exit_node"`
	ExitNodeUsed bool      `json:"exit_node_used"`
	OS           string    `json:"os"`
	Relay        string    `json:"relay,omitempty"`
}

// TailscaleConfig represents Tailscale configuration.
type TailscaleConfig struct {
	Enabled           bool     `hcl:"enabled" json:"enabled"`
	Interface         string   `hcl:"interface" json:"interface"`
	AuthKeyEnv        string   `hcl:"auth_key_env" json:"auth_key_env,omitempty"`
	AuthKey           string   `hcl:"auth_key" json:"auth_key,omitempty"`
	ControlURL        string   `hcl:"control_url" json:"control_url,omitempty"`
	ManagementAccess  bool     `hcl:"management_access" json:"management_access"`
	Zone              string   `hcl:"zone" json:"zone,omitempty"`
	AdvertiseRoutes   []string `hcl:"advertise_routes" json:"advertise_routes,omitempty"`
	AcceptRoutes      bool     `hcl:"accept_routes" json:"accept_routes"`
	ExitNode          string   `hcl:"exit_node" json:"exit_node,omitempty"`
	AdvertiseExitNode bool     `hcl:"advertise_exit_node" json:"advertise_exit_node"`
}

// DefaultTailscaleConfig returns sensible defaults.
func DefaultTailscaleConfig() TailscaleConfig {
	return TailscaleConfig{
		Enabled:          false,
		Interface:        "tailscale0",
		ManagementAccess: true,
		Zone:             "tailscale",
		AcceptRoutes:     true,
	}
}

// TailscaleManager handles Tailscale integration.
type TailscaleManager struct {
	config TailscaleConfig
	logger *logging.Logger
	status *TailscaleStatus
	mu     sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
}

// NewTailscaleManager creates a new Tailscale manager.
func NewTailscaleManager(config TailscaleConfig, logger *logging.Logger) *TailscaleManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &TailscaleManager{
		config: config,
		logger: logger,
		status: &TailscaleStatus{},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins monitoring Tailscale status.
func (m *TailscaleManager) Start(ctx context.Context) error {
	if !m.config.Enabled {
		m.logger.Info("Tailscale integration disabled")
		return nil
	}

	// Check if tailscaled is available
	if !m.isTailscaledRunning() {
		m.logger.Warn("tailscaled not running, Tailscale features unavailable")
		return nil
	}

	// Bring up interface
	if err := m.Up(); err != nil {
		m.logger.Warn("Failed to bring up Tailscale interface (may need auth)", "error", err)
	}

	// Initial status check
	if err := m.updateStatus(); err != nil {
		m.logger.Warn("Failed to get initial Tailscale status", "error", err)
	}

	// Start status monitoring
	go m.monitorLoop()

	m.logger.Info("Tailscale integration started",
		"interface", m.config.Interface,
		"management_access", m.config.ManagementAccess,
	)

	return nil
}

// Stop stops the Tailscale manager.
func (m *TailscaleManager) Stop() error {
	m.cancel()
	return nil
}

// Status returns the current connection status.
func (m *TailscaleManager) Status() ProviderStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return ProviderStatus{
		Type:       TypeTailscale,
		Connected:  m.status.Running && m.status.Online,
		Interface:  m.config.Interface,
		LocalIP:    m.status.TailscaleIP,
		LocalIPv6:  m.status.TailscaleIPv6,
		LastUpdate: m.status.LastUpdate,
		Details:    m.status,
	}
}

// GetStatus returns the current Tailscale status.
func (m *TailscaleManager) GetStatus() *TailscaleStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// GetConfig returns the current configuration.
func (m *TailscaleManager) GetConfig() TailscaleConfig {
	return m.config
}

// IsEnabled returns true if Tailscale integration is enabled.
func (m *TailscaleManager) IsEnabled() bool {
	return m.config.Enabled
}

// IsConnected returns true if Tailscale is connected.
func (m *TailscaleManager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status.Running && m.status.Online
}

// Interface returns the Tailscale interface name.
func (m *TailscaleManager) Interface() string {
	return m.config.Interface
}

// ManagementAccess returns true if this VPN should bypass firewall rules.
func (m *TailscaleManager) ManagementAccess() bool {
	return m.config.ManagementAccess
}

// Type returns the VPN provider type.
func (m *TailscaleManager) Type() Type {
	return TypeTailscale
}

// GetTailscaleIP returns the Tailscale IP address.
func (m *TailscaleManager) GetTailscaleIP() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status.TailscaleIP
}

// isTailscaledRunning checks if the tailscaled daemon is running.
func (m *TailscaleManager) isTailscaledRunning() bool {
	// Check if tailscale command exists
	_, err := exec.LookPath("tailscale")
	if err != nil {
		return false
	}

	// Check if interface exists
	_, err = net.InterfaceByName(m.config.Interface)
	return err == nil
}

// monitorLoop periodically updates Tailscale status.
func (m *TailscaleManager) monitorLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if err := m.updateStatus(); err != nil {
				m.logger.Debug("Failed to update Tailscale status", "error", err)
			}
		}
	}
}

// updateStatus fetches current status from tailscale CLI.
func (m *TailscaleManager) updateStatus() error {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tailscale", "status", "--json")
	output, err := cmd.Output()
	if err != nil {
		m.mu.Lock()
		m.status.Running = false
		m.status.LastUpdate = clock.Now()
		m.mu.Unlock()
		return fmt.Errorf("tailscale status failed: %w", err)
	}

	var tsStatus tailscaleStatus
	if err := json.Unmarshal(output, &tsStatus); err != nil {
		return fmt.Errorf("failed to parse tailscale status: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.status.Running = true
	m.status.BackendState = tsStatus.BackendState
	m.status.Online = tsStatus.BackendState == "Running"
	m.status.MagicDNSSuffix = tsStatus.MagicDNSSuffix
	m.status.LastUpdate = clock.Now()

	// Parse self info
	if tsStatus.Self != nil {
		m.status.Hostname = tsStatus.Self.HostName
		m.status.DNSName = tsStatus.Self.DNSName

		for _, ip := range tsStatus.Self.TailscaleIPs {
			if strings.Contains(ip, ":") {
				m.status.TailscaleIPv6 = ip
			} else {
				m.status.TailscaleIP = ip
			}
		}
	}

	// Parse peers
	m.status.Peers = make([]TailscalePeer, 0, len(tsStatus.Peer))
	for _, p := range tsStatus.Peer {
		peer := TailscalePeer{
			ID:       p.ID,
			Hostname: p.HostName,
			DNSName:  p.DNSName,
			Online:   p.Online,
			LastSeen: p.LastSeen,
			OS:       p.OS,
			Relay:    p.Relay,
		}
		if len(p.TailscaleIPs) > 0 {
			peer.TailscaleIP = p.TailscaleIPs[0]
		}
		m.status.Peers = append(m.status.Peers, peer)
	}

	return nil
}

// Up brings Tailscale up with the configured settings.
func (m *TailscaleManager) Up() error {
	args := []string{"up"}

	// Auth key
	authKey := m.config.AuthKey
	if m.config.AuthKeyEnv != "" {
		authKey = os.Getenv(m.config.AuthKeyEnv)
	}
	if authKey != "" {
		args = append(args, "--authkey="+authKey)
	}

	// Control URL (for Headscale)
	if m.config.ControlURL != "" {
		args = append(args, "--login-server="+m.config.ControlURL)
	}

	// Advertise routes
	if len(m.config.AdvertiseRoutes) > 0 {
		routes := strings.Join(m.config.AdvertiseRoutes, ",")
		args = append(args, "--advertise-routes="+routes)
	}

	// Accept routes
	if m.config.AcceptRoutes {
		args = append(args, "--accept-routes")
	}

	// Exit node
	if m.config.AdvertiseExitNode {
		args = append(args, "--advertise-exit-node")
	}

	// Use Exit Node
	if m.config.ExitNode != "" {
		args = append(args, "--exit-node="+m.config.ExitNode)
	}

	ctx, cancel := context.WithTimeout(m.ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tailscale", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tailscale up failed: %w: %s", err, output)
	}

	m.logger.Info("Tailscale up completed")
	return m.updateStatus()
}

// Down brings Tailscale down.
func (m *TailscaleManager) Down() error {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tailscale", "down")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tailscale down failed: %w: %s", err, output)
	}

	m.logger.Info("Tailscale down completed")
	return m.updateStatus()
}

// AdvertiseRoutes updates the advertised routes.
func (m *TailscaleManager) AdvertiseRoutes(routes []string) error {
	args := []string{"set", "--advertise-routes=" + strings.Join(routes, ",")}

	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tailscale", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to advertise routes: %w: %s", err, output)
	}

	m.config.AdvertiseRoutes = routes
	m.logger.Info("Updated advertised routes", "routes", routes)
	return nil
}

// SetExitNode enables or disables exit node mode.
func (m *TailscaleManager) SetExitNode(enable bool) error {
	var args []string
	if enable {
		args = []string{"set", "--advertise-exit-node"}
	} else {
		args = []string{"set", "--advertise-exit-node=false"}
	}

	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tailscale", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set exit node: %w: %s", err, output)
	}

	m.config.AdvertiseExitNode = enable
	m.logger.Info("Updated exit node setting", "enabled", enable)
	return nil
}

// tailscaleStatus is the JSON structure from `tailscale status --json`
type tailscaleStatus struct {
	BackendState   string                    `json:"BackendState"`
	Self           *tailscalePeer            `json:"Self"`
	Peer           map[string]*tailscalePeer `json:"Peer"`
	MagicDNSSuffix string                    `json:"MagicDNSSuffix"`
}

type tailscalePeer struct {
	ID           string    `json:"ID"`
	PublicKey    string    `json:"PublicKey"`
	HostName     string    `json:"HostName"`
	DNSName      string    `json:"DNSName"`
	OS           string    `json:"OS"`
	TailscaleIPs []string  `json:"TailscaleIPs"`
	Relay        string    `json:"Relay"`
	Online       bool      `json:"Online"`
	LastSeen     time.Time `json:"LastSeen"`
	ExitNode     bool      `json:"ExitNode"`
}
