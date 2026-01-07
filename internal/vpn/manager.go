package vpn

import (
	"context"
	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
)

// Manager handles the lifecycle of all VPN providers.
type Manager struct {
	providers []Provider
	logger    *logging.Logger
}

// NewManager creates a new VPN manager from configuration.
func NewManager(cfg *config.VPNConfig, logger *logging.Logger) (*Manager, error) {
	m := &Manager{
		logger: logger,
	}

	if cfg == nil {
		return m, nil
	}

	// Initialize WireGuard providers
	for _, wgCfg := range cfg.WireGuard {
		if !wgCfg.Enabled {
			continue
		}

		// Map config to internal vpn type
		internalCfg := WireGuardConfig{
			Enabled:          wgCfg.Enabled,
			Interface:        wgCfg.Interface,
			ManagementAccess: wgCfg.ManagementAccess,
			Zone:             wgCfg.Zone,
			PrivateKey:       wgCfg.PrivateKey,
			PrivateKeyFile:   wgCfg.PrivateKeyFile,
			ListenPort:       wgCfg.ListenPort,
			Address:          wgCfg.Address,
			DNS:              wgCfg.DNS,
			MTU:              wgCfg.MTU,
			FWMark:           wgCfg.FWMark,
		}

		for _, p := range wgCfg.Peers {
			internalPeer := WireGuardPeer{
				Name:                p.Name,
				PublicKey:           p.PublicKey,
				PresharedKey:        p.PresharedKey,
				Endpoint:            p.Endpoint,
				AllowedIPs:          p.AllowedIPs,
				PersistentKeepalive: p.PersistentKeepalive,
			}
			internalCfg.Peers = append(internalCfg.Peers, internalPeer)
		}

		provider := NewWireGuardManager(internalCfg, logger)
		m.providers = append(m.providers, provider)
	}

	// Initialize Tailscale providers
	for _, tsCfg := range cfg.Tailscale {
		if !tsCfg.Enabled {
			continue
		}

		internalCfg := TailscaleConfig{
			Enabled:           tsCfg.Enabled,
			Interface:         tsCfg.Interface,
			AuthKey:           tsCfg.AuthKey,
			AuthKeyEnv:        tsCfg.AuthKeyEnv,
			ControlURL:        tsCfg.ControlURL,
			ManagementAccess:  tsCfg.ManagementAccess,
			Zone:              tsCfg.Zone,
			AdvertiseRoutes:   tsCfg.AdvertiseRoutes,
			AcceptRoutes:      tsCfg.AcceptRoutes,
			AdvertiseExitNode: tsCfg.AdvertiseExitNode,
			ExitNode:          tsCfg.ExitNode,
		}

		provider := NewTailscaleManager(internalCfg, logger)
		m.providers = append(m.providers, provider)
	}

	return m, nil
}

// Start starts all managed VPN providers.
func (m *Manager) Start(ctx context.Context) error {
	for _, p := range m.providers {
		m.logger.Info("Starting VPN provider", "type", p.Type(), "interface", p.Interface())
		if err := p.Start(ctx); err != nil {
			m.logger.Warn("Failed to start VPN provider", "interface", p.Interface(), "error", err)
			// Continue trying others? Or fail?
			// For robustness, likely continue but log error.
		}
	}
	return nil
}

// Stop stops all managed VPN providers.
func (m *Manager) Stop() {
	for _, p := range m.providers {
		m.logger.Info("Stopping VPN provider", "interface", p.Interface())
		p.Stop()
	}
}
