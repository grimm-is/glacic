package vpn

import (
	"context"
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/logging"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// WireGuardConfig represents WireGuard VPN configuration.
type WireGuardConfig struct {
	Enabled          bool            `hcl:"enabled" json:"enabled"`
	Interface        string          `hcl:"interface" json:"interface"`
	ManagementAccess bool            `hcl:"management_access" json:"management_access"`
	Zone             string          `hcl:"zone" json:"zone,omitempty"`
	PrivateKey       string          `hcl:"private_key" json:"private_key,omitempty"`
	PrivateKeyFile   string          `hcl:"private_key_file" json:"private_key_file,omitempty"`
	ListenPort       int             `hcl:"listen_port" json:"listen_port,omitempty"`
	Address          []string        `hcl:"address" json:"address,omitempty"`
	DNS              []string        `hcl:"dns" json:"dns,omitempty"`
	MTU              int             `hcl:"mtu" json:"mtu,omitempty"`
	FWMark           int             `hcl:"fwmark" json:"fwmark,omitempty"`
	Peers            []WireGuardPeer `hcl:"peer,block" json:"peers,omitempty"`
}

// WireGuardPeer represents a WireGuard peer configuration.
type WireGuardPeer struct {
	Name                string   `hcl:"name,label" json:"name"`
	PublicKey           string   `hcl:"public_key" json:"public_key"`
	PresharedKey        string   `hcl:"preshared_key" json:"preshared_key,omitempty"`
	Endpoint            string   `hcl:"endpoint" json:"endpoint,omitempty"`
	AllowedIPs          []string `hcl:"allowed_ips" json:"allowed_ips"`
	PersistentKeepalive int      `hcl:"persistent_keepalive" json:"persistent_keepalive,omitempty"`
}

// WireGuardStatus represents the current WireGuard status.
type WireGuardStatus struct {
	Running    bool                  `json:"running"`
	Interface  string                `json:"interface"`
	PublicKey  string                `json:"public_key"`
	ListenPort int                   `json:"listen_port"`
	Peers      []WireGuardPeerStatus `json:"peers"`
	LastUpdate time.Time             `json:"last_update"`
}

// WireGuardPeerStatus represents a peer's current status.
type WireGuardPeerStatus struct {
	PublicKey           string    `json:"public_key"`
	Endpoint            string    `json:"endpoint,omitempty"`
	AllowedIPs          []string  `json:"allowed_ips"`
	LatestHandshake     time.Time `json:"latest_handshake"`
	TransferRx          uint64    `json:"transfer_rx"`
	TransferTx          uint64    `json:"transfer_tx"`
	PersistentKeepalive int       `json:"persistent_keepalive,omitempty"`
}

// MarshalJSON masks the private key in API responses.
// Mitigation: CWE-200: Exposure of Sensitive Information
func (c WireGuardConfig) MarshalJSON() ([]byte, error) {
	type Alias WireGuardConfig
	// Create a temporary struct with the same fields
	aux := &struct {
		Alias
		PrivateKey string `json:"private_key,omitempty"`
	}{
		Alias: (Alias)(c),
	}

	// Mask the private key if it exists
	if c.PrivateKey != "" {
		aux.PrivateKey = "******"
	}

	return json.Marshal(aux)
}

// MarshalJSON masks the preshared key in API responses.
// Mitigation: CWE-200: Exposure of Sensitive Information
func (p WireGuardPeer) MarshalJSON() ([]byte, error) {
	type Alias WireGuardPeer
	// Create a temporary struct with the same fields
	aux := &struct {
		Alias
		PresharedKey string `json:"preshared_key,omitempty"`
	}{
		Alias: (Alias)(p),
	}

	// Mask the preshared key if it exists
	if p.PresharedKey != "" {
		aux.PresharedKey = "******"
	}

	return json.Marshal(aux)
}

// DefaultWireGuardConfig returns sensible defaults.
func DefaultWireGuardConfig() WireGuardConfig {
	return WireGuardConfig{
		Enabled:          false,
		Interface:        "wg0",
		ManagementAccess: true,
		Zone:             "vpn",
		ListenPort:       51820,
		MTU:              1420,
	}
}

// WireGuardManager handles WireGuard VPN integration.
type WireGuardManager struct {
	config WireGuardConfig
	logger *logging.Logger
	status *WireGuardStatus
	mu     sync.RWMutex

	wgClient *wgctrl.Client
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewWireGuardManager creates a new WireGuard manager.
func NewWireGuardManager(config WireGuardConfig, logger *logging.Logger) *WireGuardManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &WireGuardManager{
		config: config,
		logger: logger,
		status: &WireGuardStatus{},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins monitoring WireGuard status.
func (m *WireGuardManager) Start(ctx context.Context) error {
	if !m.config.Enabled {
		m.logger.Info("WireGuard integration disabled")
		return nil
	}

	// Note: We no longer check for 'wg' binary since we use native Go libraries,
	// but the kernel module must be loaded.

	// Bring up interface
	if err := m.Up(); err != nil {
		return fmt.Errorf("failed to bring up wireguard interface: %w", err)
	}

	// Initial status check
	if err := m.updateStatus(); err != nil {
		m.logger.Warn("Failed to get initial WireGuard status", "error", err)
	}

	// Start status monitoring
	go m.monitorLoop()

	m.logger.Info("WireGuard integration started",
		"interface", m.config.Interface,
		"management_access", m.config.ManagementAccess,
	)

	return nil
}

// Stop stops the WireGuard manager.
func (m *WireGuardManager) Stop() error {
	m.cancel()
	if m.wgClient != nil {
		m.wgClient.Close()
	}
	return nil
}

// Status returns the current connection status.
func (m *WireGuardManager) Status() ProviderStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return ProviderStatus{
		Type:       TypeWireGuard,
		Connected:  m.status.Running,
		Interface:  m.status.Interface,
		PublicKey:  m.status.PublicKey,
		LastUpdate: m.status.LastUpdate,
		Details:    m.status,
	}
}

// GetConfig returns the current configuration.
func (m *WireGuardManager) GetConfig() WireGuardConfig {
	return m.config
}

// IsEnabled returns true if WireGuard integration is enabled.
func (m *WireGuardManager) IsEnabled() bool {
	return m.config.Enabled
}

// IsConnected returns true if WireGuard interface is up.
func (m *WireGuardManager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status.Running
}

// Interface returns the WireGuard interface name.
func (m *WireGuardManager) Interface() string {
	return m.config.Interface
}

// ManagementAccess returns true if this VPN should bypass firewall rules.
func (m *WireGuardManager) ManagementAccess() bool {
	return m.config.ManagementAccess
}

// Type returns the VPN provider type.
func (m *WireGuardManager) Type() Type {
	return TypeWireGuard
}

// monitorLoop periodically updates WireGuard status.
func (m *WireGuardManager) monitorLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if err := m.updateStatus(); err != nil {
				m.logger.Debug("Failed to update WireGuard status", "error", err)
			}
		}
	}
}

// updateStatus fetches current status from wgctrl.
func (m *WireGuardManager) updateStatus() error {
	// Initialize client if needed
	if m.wgClient == nil {
		c, err := wgctrl.New()
		if err != nil {
			return fmt.Errorf("failed to open wgctrl: %w", err)
		}
		m.wgClient = c
	}

	// Get device info
	device, err := m.wgClient.Device(m.config.Interface)
	if err != nil {
		if strings.Contains(err.Error(), "no such device") || strings.Contains(err.Error(), "not found") {
			m.mu.Lock()
			m.status.Running = false
			m.status.LastUpdate = clock.Now()
			m.mu.Unlock()
			return nil
		}
		return fmt.Errorf("failed to get device info: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.status.Running = true
	m.status.Interface = device.Name
	m.status.PublicKey = device.PublicKey.String()
	m.status.ListenPort = device.ListenPort
	m.status.LastUpdate = clock.Now()

	// Parse peers
	peers := make([]WireGuardPeerStatus, 0, len(device.Peers))
	for _, p := range device.Peers {
		allowedIPs := make([]string, len(p.AllowedIPs))
		for i, ip := range p.AllowedIPs {
			allowedIPs[i] = ip.String()
		}

		peers = append(peers, WireGuardPeerStatus{
			PublicKey:           p.PublicKey.String(),
			Endpoint:            p.Endpoint.String(),
			AllowedIPs:          allowedIPs,
			LatestHandshake:     p.LastHandshakeTime,
			TransferRx:          uint64(p.ReceiveBytes),
			TransferTx:          uint64(p.TransmitBytes),
			PersistentKeepalive: int(p.PersistentKeepaliveInterval.Seconds()),
		})
	}
	m.status.Peers = peers

	return nil
}

// Up brings WireGuard interface up using netlink and wgctrl.
// Replaces wg-quick dependency.
func (m *WireGuardManager) Up() error {
	// 1. Create Interface
	linkAttr := netlink.NewLinkAttrs()
	linkAttr.Name = m.config.Interface
	link := &netlink.Wireguard{LinkAttrs: linkAttr}

	// Check if exists
	if existing, err := netlink.LinkByName(m.config.Interface); err == nil {
		// If it exists but is not wireguard, that's an issue
		if existing.Type() != "wireguard" {
			return fmt.Errorf("interface %s exists but is not wireguard (type: %s)", m.config.Interface, existing.Type())
		}
		link = existing.(*netlink.Wireguard)
	} else {
		// Create it
		if err := netlink.LinkAdd(link); err != nil {
			return fmt.Errorf("failed to create wireguard interface: %w", err)
		}
	}

	// Refresh link handle
	l, err := netlink.LinkByName(m.config.Interface)
	if err != nil {
		return fmt.Errorf("failed to get link after creation: %w", err)
	}

	// 2. Configure Device (Crypto/Peers)
	if m.wgClient == nil {
		c, err := wgctrl.New()
		if err != nil {
			return fmt.Errorf("failed to open wgctrl: %w", err)
		}
		m.wgClient = c
	}

	conf := wgtypes.Config{
		ReplacePeers: true,
	}

	// Set Private Key
	if m.config.PrivateKey != "" {
		key, err := wgtypes.ParseKey(m.config.PrivateKey)
		if err != nil {
			return fmt.Errorf("invalid private key: %w", err)
		}
		conf.PrivateKey = &key
	} else if m.config.PrivateKeyFile != "" {
		// Read private key from file
		keyData, err := os.ReadFile(m.config.PrivateKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read private key file: %w", err)
		}
		keyStr := strings.TrimSpace(string(keyData))
		key, err := wgtypes.ParseKey(keyStr)
		if err != nil {
			return fmt.Errorf("invalid private key in file: %w", err)
		}
		conf.PrivateKey = &key
	}

	// Set Listen Port
	if m.config.ListenPort != 0 {
		port := m.config.ListenPort
		conf.ListenPort = &port
	}

	// Set Firewall Mark
	if m.config.FWMark != 0 {
		mark := m.config.FWMark
		conf.FirewallMark = &mark
	}

	// Set Peers
	var peers []wgtypes.PeerConfig
	for _, p := range m.config.Peers {
		pubKey, err := wgtypes.ParseKey(p.PublicKey)
		if err != nil {
			m.logger.Warn("Invalid peer public key, skipping", "peer", p.Name, "error", err)
			continue
		}

		peerConf := wgtypes.PeerConfig{
			PublicKey:         pubKey,
			AllowedIPs:        []net.IPNet{},
			ReplaceAllowedIPs: true,
		}

		if p.PresharedKey != "" {
			psk, err := wgtypes.ParseKey(p.PresharedKey)
			if err != nil {
				m.logger.Warn("Invalid peer preshared key", "peer", p.Name, "error", err)
			} else {
				peerConf.PresharedKey = &psk
			}
		}

		if p.Endpoint != "" {
			udpAddr, err := net.ResolveUDPAddr("udp", p.Endpoint)
			if err != nil {
				m.logger.Warn("Invalid peer endpoint", "peer", p.Name, "error", err)
			} else {
				peerConf.Endpoint = udpAddr
			}
		}

		if p.PersistentKeepalive != 0 {
			ka := time.Duration(p.PersistentKeepalive) * time.Second
			peerConf.PersistentKeepaliveInterval = &ka
		}

		for _, cidr := range p.AllowedIPs {
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				m.logger.Warn("Invalid allowed ip", "peer", p.Name, "cidr", cidr)
				continue
			}
			peerConf.AllowedIPs = append(peerConf.AllowedIPs, *ipnet)
		}

		peers = append(peers, peerConf)
	}
	conf.Peers = peers

	if err := m.wgClient.ConfigureDevice(m.config.Interface, conf); err != nil {
		return fmt.Errorf("failed to configure wireguard device: %w", err)
	}

	// 3. Assign IP Addresses
	currentAddrs, err := netlink.AddrList(l, 0)
	if err != nil {
		return fmt.Errorf("failed to list addresses: %w", err)
	}
	// Flush existing? Or just add. Flushing is safer for 'conf sync'.
	// For simple Up(), usually we just add.
	// But to match wg-quick idempotency, we might want to ensure exact match.
	// For now, let's just add missing ones.

	for _, addrStr := range m.config.Address {
		addr, err := netlink.ParseAddr(addrStr)
		if err != nil {
			return fmt.Errorf("invalid address %s: %w", addrStr, err)
		}

		// Check if exists
		exists := false
		for _, cur := range currentAddrs {
			if cur.Equal(*addr) {
				exists = true
				break
			}
		}

		if !exists {
			if err := netlink.AddrAdd(l, addr); err != nil {
				if strings.Contains(err.Error(), "file exists") {
					continue
				}
				return fmt.Errorf("failed to add address %s: %w", addrStr, err)
			}
		}
	}

	// 4. Set MTU
	if m.config.MTU > 0 {
		if err := netlink.LinkSetMTU(l, m.config.MTU); err != nil {
			m.logger.Warn("Failed to set MTU", "error", err)
		}
	}

	// 5. Bring Up
	if err := netlink.LinkSetUp(l); err != nil {
		return fmt.Errorf("failed to bring interface up: %w", err)
	}

	// 6. Add Routes
	// Iterate over peers and add routes for AllowedIPs if they route to this interface
	// Note: WireGuard kernel module generally handles routing for AllowedIPs if the interface route exists.
	// However, we usually need explicit routes for the allowed IP ranges to the dev.
	for _, p := range m.config.Peers {
		for _, ipRange := range p.AllowedIPs {
			_, dst, err := net.ParseCIDR(ipRange)
			if err != nil {
				continue
			}

			route := netlink.Route{
				LinkIndex: l.Attrs().Index,
				Dst:       dst,
			}
			if err := netlink.RouteAdd(&route); err != nil {
				if !strings.Contains(err.Error(), "file exists") {
					m.logger.Warn("Failed to add route", "dst", ipRange, "error", err)
				}
			}
		}
	}

	m.logger.Info("WireGuard up completed via netlink", "interface", m.config.Interface)
	return m.updateStatus()
}

// Down brings WireGuard interface down.
func (m *WireGuardManager) Down() error {
	link, err := netlink.LinkByName(m.config.Interface)
	if err != nil {
		// If not found, it's already down
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("failed to get link: %w", err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete interface: %w", err)
	}

	m.logger.Info("WireGuard down completed", "interface", m.config.Interface)
	return m.updateStatus()
}

// GenerateKeyPair generates a new WireGuard key pair.
func GenerateKeyPair() (privateKey, publicKey string, err error) {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %w", err)
	}
	return key.String(), key.PublicKey().String(), nil
}

// AddPeer adds a new peer to the WireGuard interface dynamically.
// This updates the running interface but does NOT persist to config.
func (m *WireGuardManager) AddPeer(peer WireGuardPeer) error {
	if m.wgClient == nil {
		c, err := wgctrl.New()
		if err != nil {
			return fmt.Errorf("failed to open wgctrl: %w", err)
		}
		m.wgClient = c
	}

	pubKey, err := wgtypes.ParseKey(peer.PublicKey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	peerConf := wgtypes.PeerConfig{
		PublicKey:         pubKey,
		ReplaceAllowedIPs: true,
		AllowedIPs:        []net.IPNet{},
	}

	// Parse preshared key
	if peer.PresharedKey != "" {
		psk, err := wgtypes.ParseKey(peer.PresharedKey)
		if err != nil {
			return fmt.Errorf("invalid preshared key: %w", err)
		}
		peerConf.PresharedKey = &psk
	}

	// Parse endpoint
	if peer.Endpoint != "" {
		udpAddr, err := net.ResolveUDPAddr("udp", peer.Endpoint)
		if err != nil {
			return fmt.Errorf("invalid endpoint: %w", err)
		}
		peerConf.Endpoint = udpAddr
	}

	// Parse keepalive
	if peer.PersistentKeepalive != 0 {
		ka := time.Duration(peer.PersistentKeepalive) * time.Second
		peerConf.PersistentKeepaliveInterval = &ka
	}

	// Parse allowed IPs
	for _, cidr := range peer.AllowedIPs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid allowed IP %s: %w", cidr, err)
		}
		peerConf.AllowedIPs = append(peerConf.AllowedIPs, *ipnet)
	}

	// Apply peer configuration
	conf := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerConf},
	}

	if err := m.wgClient.ConfigureDevice(m.config.Interface, conf); err != nil {
		return fmt.Errorf("failed to add peer: %w", err)
	}

	// Add routes for allowed IPs
	link, err := netlink.LinkByName(m.config.Interface)
	if err != nil {
		m.logger.Warn("Failed to get interface for routing", "error", err)
	} else {
		for _, ipnet := range peerConf.AllowedIPs {
			route := netlink.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       &ipnet,
			}
			if err := netlink.RouteAdd(&route); err != nil {
				if !strings.Contains(err.Error(), "file exists") {
					m.logger.Warn("Failed to add route", "dst", ipnet.String(), "error", err)
				}
			}
		}
	}

	// Add to local config for status tracking
	m.mu.Lock()
	m.config.Peers = append(m.config.Peers, peer)
	m.mu.Unlock()

	m.logger.Info("Peer added", "public_key", peer.PublicKey[:8]+"...")
	return m.updateStatus()
}

// RemovePeer removes a peer from the WireGuard interface dynamically.
// This updates the running interface but does NOT persist to config.
func (m *WireGuardManager) RemovePeer(publicKey string) error {
	if m.wgClient == nil {
		c, err := wgctrl.New()
		if err != nil {
			return fmt.Errorf("failed to open wgctrl: %w", err)
		}
		m.wgClient = c
	}

	pubKey, err := wgtypes.ParseKey(publicKey)
	if err != nil {
		return fmt.Errorf("invalid public key: %w", err)
	}

	// Find peer to get allowed IPs for route removal
	var allowedIPs []string
	m.mu.RLock()
	for _, p := range m.config.Peers {
		if p.PublicKey == publicKey {
			allowedIPs = p.AllowedIPs
			break
		}
	}
	m.mu.RUnlock()

	// Remove from WireGuard
	peerConf := wgtypes.PeerConfig{
		PublicKey: pubKey,
		Remove:    true,
	}

	conf := wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerConf},
	}

	if err := m.wgClient.ConfigureDevice(m.config.Interface, conf); err != nil {
		return fmt.Errorf("failed to remove peer: %w", err)
	}

	// Remove routes
	link, err := netlink.LinkByName(m.config.Interface)
	if err == nil {
		for _, cidr := range allowedIPs {
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			route := netlink.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       ipnet,
			}
			if err := netlink.RouteDel(&route); err != nil {
				if !strings.Contains(err.Error(), "no such process") {
					m.logger.Warn("Failed to remove route", "dst", cidr, "error", err)
				}
			}
		}
	}

	// Remove from local config
	m.mu.Lock()
	newPeers := make([]WireGuardPeer, 0, len(m.config.Peers))
	for _, p := range m.config.Peers {
		if p.PublicKey != publicKey {
			newPeers = append(newPeers, p)
		}
	}
	m.config.Peers = newPeers
	m.mu.Unlock()

	m.logger.Info("Peer removed", "public_key", publicKey[:8]+"...")
	return m.updateStatus()
}

// GetStatus returns the current WireGuard status.
func (m *WireGuardManager) GetStatus() *WireGuardStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}
