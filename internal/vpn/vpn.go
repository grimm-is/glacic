// Package vpn provides VPN integrations including Tailscale, WireGuard, and others.
// These provide secure remote access that survives firewall misconfigurations.
package vpn

import (
	"context"
	"net"
	"time"
)

// Type represents the VPN provider type.
type Type string

const (
	TypeTailscale Type = "tailscale"
	TypeWireGuard Type = "wireguard"
	TypeOpenVPN   Type = "openvpn" // Future
)

// Provider is the interface that all VPN implementations must satisfy.
type Provider interface {
	// Type returns the VPN provider type.
	Type() Type

	// Start begins the VPN connection/monitoring.
	Start(ctx context.Context) error

	// Stop gracefully stops the VPN.
	Stop() error

	// Status returns the current connection status.
	Status() ProviderStatus

	// IsConnected returns true if the VPN is connected.
	IsConnected() bool

	// Interface returns the VPN interface name.
	Interface() string

	// ManagementAccess returns true if this VPN should bypass firewall rules.
	ManagementAccess() bool
}

// ProviderStatus is the common status interface for all VPN providers.
type ProviderStatus struct {
	Type       Type      `json:"type"`
	Connected  bool      `json:"connected"`
	Interface  string    `json:"interface"`
	LocalIP    string    `json:"local_ip,omitempty"`
	LocalIPv6  string    `json:"local_ipv6,omitempty"`
	PublicKey  string    `json:"public_key,omitempty"`
	Endpoint   string    `json:"endpoint,omitempty"`
	LastUpdate time.Time `json:"last_update"`

	// Provider-specific details
	Details interface{} `json:"details,omitempty"`
}

// InterfaceExists checks if a network interface exists.
func InterfaceExists(name string) bool {
	_, err := net.InterfaceByName(name)
	return err == nil
}
