package config

import "encoding/json"

// VPNConfig configures VPN integrations.
// Supports multiple connections per provider, each with its own zone or combined.
type VPNConfig struct {
	// Tailscale/Headscale connections (multiple allowed)
	Tailscale []TailscaleConfig `hcl:"tailscale,block" json:"tailscale,omitempty"`

	// WireGuard connections (multiple allowed)
	WireGuard []WireGuardConfig `hcl:"wireguard,block" json:"wireguard,omitempty"`

	// 6to4 Tunnels (multiple allowed, usually one)
	SixToFour []SixToFourConfig `hcl:"six_to_four,block" json:"6to4,omitempty"`

	// Interface prefix matching for zones (like firehol's "wg+" syntax)
	// Maps interface prefix to zone name
	// Example: {"wg": "vpn", "tailscale": "tailscale"} means wg0, wg1 -> vpn zone
	InterfacePrefixZones map[string]string `hcl:"interface_prefix_zones,optional" json:"interface_prefix_zones,omitempty"`
}

// GetAllInterfaces returns all configured VPN interface names.
func (c *VPNConfig) GetAllInterfaces() []string {
	var interfaces []string
	for _, ts := range c.Tailscale {
		if ts.Enabled && ts.Interface != "" {
			interfaces = append(interfaces, ts.Interface)
		}
	}
	for _, wg := range c.WireGuard {
		if wg.Enabled && wg.Interface != "" {
			interfaces = append(interfaces, wg.Interface)
		}
	}
	return interfaces
}

// GetManagementInterfaces returns VPN interfaces that should bypass firewall.
func (c *VPNConfig) GetManagementInterfaces() []string {
	var interfaces []string
	for _, ts := range c.Tailscale {
		if ts.Enabled && ts.ManagementAccess {
			iface := ts.Interface
			if iface == "" {
				iface = "tailscale0"
			}
			interfaces = append(interfaces, iface)
		}
	}
	for _, wg := range c.WireGuard {
		if wg.Enabled && wg.ManagementAccess {
			iface := wg.Interface
			if iface == "" {
				iface = "wg0"
			}
			interfaces = append(interfaces, iface)
		}
	}
	return interfaces
}

// GetInterfaceZone returns the zone for a given interface, checking both
// explicit interface->zone mappings and prefix matching.
func (c *VPNConfig) GetInterfaceZone(iface string) string {
	// Check explicit Tailscale configs
	for _, ts := range c.Tailscale {
		if ts.Interface == iface && ts.Zone != "" {
			return ts.Zone
		}
	}
	// Check explicit WireGuard configs
	for _, wg := range c.WireGuard {
		if wg.Interface == iface && wg.Zone != "" {
			return wg.Zone
		}
	}
	// Check prefix matching (like firehol's "wg+" syntax)
	for prefix, zone := range c.InterfacePrefixZones {
		if len(iface) > len(prefix) && iface[:len(prefix)] == prefix {
			return zone
		}
	}
	return ""
}

// TailscaleConfig configures a Tailscale/Headscale connection.
type TailscaleConfig struct {
	// Connection name (label for multiple connections)
	Name string `hcl:"name,label" json:"name,omitempty"`

	// Enable this Tailscale connection
	Enabled bool `hcl:"enabled,optional" json:"enabled"`

	// Interface name (default: tailscale0, or tailscale1, etc. for multiple)
	Interface string `hcl:"interface,optional" json:"interface,omitempty"`

	// Auth key for unattended setup (or use AuthKeyEnv)
	AuthKey string `hcl:"auth_key,optional" json:"auth_key,omitempty"`

	// Environment variable containing auth key
	AuthKeyEnv string `hcl:"auth_key_env,optional" json:"auth_key_env,omitempty"`

	// Control server URL (for Headscale)
	ControlURL string `hcl:"control_url,optional" json:"control_url,omitempty"`

	// Always allow management access via Tailscale (lockout protection)
	// This inserts accept rules BEFORE all other firewall rules
	ManagementAccess bool `hcl:"management_access,optional" json:"management_access"`

	// Zone name for this interface (default: tailscale)
	// Use same zone name across multiple connections to combine them
	Zone string `hcl:"zone,optional" json:"zone,omitempty"`

	// Routes to advertise to Tailscale network
	AdvertiseRoutes []string `hcl:"advertise_routes,optional" json:"advertise_routes,omitempty"`

	// Accept routes from other Tailscale nodes
	AcceptRoutes bool `hcl:"accept_routes,optional" json:"accept_routes"`

	// Advertise this node as an exit node
	AdvertiseExitNode bool `hcl:"advertise_exit_node,optional" json:"advertise_exit_node"`

	// Use a specific exit node (Tailscale IP or hostname)
	ExitNode string `hcl:"exit_node,optional" json:"exit_node,omitempty"`
}

// WireGuardConfig configures a WireGuard VPN connection.
type WireGuardConfig struct {
	// Connection name (label for multiple connections)
	Name string `hcl:"name,label" json:"name,omitempty"`

	// Enable this WireGuard connection
	Enabled bool `hcl:"enabled,optional" json:"enabled"`

	// Interface name (default: wg0, or wg1, etc. for multiple)
	Interface string `hcl:"interface,optional" json:"interface,omitempty"`

	// Always allow management access via WireGuard (lockout protection)
	ManagementAccess bool `hcl:"management_access,optional" json:"management_access"`

	// Zone name for this interface (default: vpn)
	// Use same zone name across multiple connections to combine them
	Zone string `hcl:"zone,optional" json:"zone,omitempty"`

	// Private key (or use PrivateKeyFile)
	PrivateKey string `hcl:"private_key,optional" json:"private_key,omitempty"`

	// Path to private key file
	PrivateKeyFile string `hcl:"private_key_file,optional" json:"private_key_file,omitempty"`

	// Listen port (default: 51820)
	ListenPort int `hcl:"listen_port,optional" json:"listen_port,omitempty"`

	// Interface addresses
	Address []string `hcl:"address,optional" json:"address,omitempty"`

	// DNS servers to use when connected
	DNS []string `hcl:"dns,optional" json:"dns,omitempty"`

	// MTU (default: 1420)
	MTU int `hcl:"mtu,optional" json:"mtu,omitempty"`

	// Peer configurations
	Peers []WireGuardPeerConfig `hcl:"peer,block" json:"peers,omitempty"`

	// Firewall Mark (fwmark) for routing
	FWMark int `hcl:"fwmark,optional" json:"fwmark,omitempty"`
}

// MarshalJSON masks the private key in API responses.
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
		aux.PrivateKey = "(hidden)"
	}

	return json.Marshal(aux)
}

// WireGuardPeerConfig configures a WireGuard peer.
type WireGuardPeerConfig struct {
	// Peer name (label)
	Name string `hcl:"name,label" json:"name"`

	// Peer's public key
	PublicKey string `hcl:"public_key" json:"public_key"`

	// Optional preshared key for additional security
	PresharedKey string `hcl:"preshared_key,optional" json:"preshared_key,omitempty"`

	// Peer's endpoint (host:port)
	Endpoint string `hcl:"endpoint,optional" json:"endpoint,omitempty"`

	// Allowed IP ranges for this peer
	AllowedIPs []string `hcl:"allowed_ips" json:"allowed_ips"`

	// Keepalive interval in seconds (useful for NAT traversal)
	PersistentKeepalive int `hcl:"persistent_keepalive,optional" json:"persistent_keepalive,omitempty"`
}

// MarshalJSON masks the preshared key in API responses.
func (p WireGuardPeerConfig) MarshalJSON() ([]byte, error) {
	type Alias WireGuardPeerConfig
	// Create a temporary struct with the same fields
	aux := &struct {
		Alias
		PresharedKey string `json:"preshared_key,omitempty"`
	}{
		Alias: (Alias)(p),
	}

	// Mask the preshared key if it exists
	if p.PresharedKey != "" {
		aux.PresharedKey = "(hidden)"
	}

	return json.Marshal(aux)
}

// ThreatIntel configures threat intelligence feeds.
type ThreatIntel struct {
	Enabled  bool           `hcl:"enabled,optional" json:"enabled"`
	Interval string         `hcl:"interval,optional" json:"interval"` // e.g. "1h"
	Sources  []ThreatSource `hcl:"source,block" json:"sources,omitempty"`
}

type ThreatSource struct {
	Name         string `hcl:"name,label" json:"name"`
	URL          string `hcl:"url" json:"url"`
	Format       string `hcl:"format,optional" json:"format"` // "taxii", "text", "json"
	CollectionID string `hcl:"collection_id,optional" json:"collection_id,omitempty"`
	Username     string `hcl:"username,optional" json:"username,omitempty"`
	Password     string `hcl:"password,optional" json:"password,omitempty"`
}

// IPSet defines a named set of IPs/networks for use in firewall rules.
type IPSet struct {
	Name        string   `hcl:"name,label" json:"name"`
	Description string   `hcl:"description,optional" json:"description,omitempty"`
	Type        string   `hcl:"type,optional" json:"type"` // ipv4_addr (default), ipv6_addr, inet_service, or dns
	Entries     []string `hcl:"entries,optional" json:"entries,omitempty"`

	// Domains for dynamic resolution (only for type="dns")
	Domains []string `hcl:"domains,optional" json:"domains,omitempty"`

	// Refresh interval for DNS resolution (e.g., "5m", "1h") - only for type="dns"
	RefreshInterval string `hcl:"refresh_interval,optional" json:"refresh_interval,omitempty"`

	// Optimization: Pre-allocated size for dynamic sets to prevent resizing (suggested: 65535)
	Size int `hcl:"size,optional" json:"size,omitempty"`

	// FireHOL import
	FireHOLList   string `hcl:"firehol_list,optional" json:"firehol_list,omitempty"`   // e.g., "firehol_level1", "spamhaus_drop"
	URL           string `hcl:"url,optional" json:"url,omitempty"`                     // Custom URL for IP list
	RefreshHours  int    `hcl:"refresh_hours,optional" json:"refresh_hours,omitempty"` // How often to refresh (default: 24)
	AutoUpdate    bool   `hcl:"auto_update,optional" json:"auto_update"`               // Enable automatic updates
	Action        string `hcl:"action,optional" json:"action,omitempty"`               // drop, reject, log (for auto-generated rules)
	ApplyTo       string `hcl:"apply_to,optional" json:"apply_to,omitempty"`           // input, forward, both (for auto-generated rules)
	MatchOnSource bool   `hcl:"match_on_source,optional" json:"match_on_source"`       // Match source IP (default: true)
	MatchOnDest   bool   `hcl:"match_on_dest,optional" json:"match_on_dest,omitempty"` // Match destination IP
}

// InterfaceProtection defines security protection settings for an interface.
type InterfaceProtection struct {
	Name      string `hcl:"name,label" json:"name"`
	Interface string `hcl:"interface" json:"interface"` // Interface name or "*" for all
	Enabled   bool   `hcl:"enabled,optional" json:"enabled"`

	// AntiSpoofing drops packets with spoofed source IPs (recommended for WAN)
	AntiSpoofing bool `hcl:"anti_spoofing,optional" json:"anti_spoofing"`
	// BogonFiltering drops packets from reserved/invalid IP ranges
	BogonFiltering bool `hcl:"bogon_filtering,optional" json:"bogon_filtering"`
	// PrivateFiltering drops packets from private IP ranges on WAN (RFC1918)
	PrivateFiltering bool `hcl:"private_filtering,optional" json:"private_filtering"`
	// InvalidPackets drops malformed/invalid packets
	InvalidPackets bool `hcl:"invalid_packets,optional" json:"invalid_packets"`

	// SynFloodProtection limits SYN packets to prevent SYN floods
	SynFloodProtection bool `hcl:"syn_flood_protection,optional" json:"syn_flood_protection"`
	SynFloodRate       int  `hcl:"syn_flood_rate,optional" json:"syn_flood_rate"`   // packets/sec (default: 25)
	SynFloodBurst      int  `hcl:"syn_flood_burst,optional" json:"syn_flood_burst"` // burst allowance (default: 50)

	// ICMPRateLimit limits ICMP packets to prevent ping floods
	ICMPRateLimit bool `hcl:"icmp_rate_limit,optional" json:"icmp_rate_limit"`
	ICMPRate      int  `hcl:"icmp_rate,optional" json:"icmp_rate"`   // packets/sec (default: 10)
	ICMPBurst     int  `hcl:"icmp_burst,optional" json:"icmp_burst"` // burst (default: 20)

	// NewConnRateLimit limits new connections per second
	NewConnRateLimit bool `hcl:"new_conn_rate_limit,optional" json:"new_conn_rate_limit"`
	NewConnRate      int  `hcl:"new_conn_rate,optional" json:"new_conn_rate"`   // per second (default: 100)
	NewConnBurst     int  `hcl:"new_conn_burst,optional" json:"new_conn_burst"` // burst (default: 200)

	// PortScanProtection detects and blocks port scanning
	PortScanProtection bool `hcl:"port_scan_protection,optional" json:"port_scan_protection"`
	PortScanThreshold  int  `hcl:"port_scan_threshold,optional" json:"port_scan_threshold"` // ports/sec (default: 10)

	// GeoBlocking blocks traffic from specific countries (requires GeoIP database)
	GeoBlocking      bool     `hcl:"geo_blocking,optional" json:"geo_blocking"`
	BlockedCountries []string `hcl:"blocked_countries,optional" json:"blocked_countries,omitempty"` // ISO country codes
	AllowedCountries []string `hcl:"allowed_countries,optional" json:"allowed_countries,omitempty"` // If set, only these allowed
}

// ProtectionConfig is an alias for InterfaceProtection for backwards compatibility.
type ProtectionConfig = InterfaceProtection

// Zone defines a network security zone.
// Zones can match traffic by interface, source/destination IP, VLAN, or combinations.
// Simple zones use top-level fields, complex zones use match blocks.
type Zone struct {
	Name        string `hcl:"name,label" json:"name"`
	Color       string `hcl:"color,optional" json:"color"`
	Description string `hcl:"description,optional" json:"description"`

	// Simple match criteria (use for single-interface zones)
	// These are effectively a single implicit match rule
	// Interface can be exact ("eth0") or prefix with + or * suffix ("wg+" or "wg*" matches wg0, wg1...)
	Interface string `hcl:"interface,optional" json:"interface,omitempty"`
	Src       string `hcl:"src,optional" json:"src,omitempty"`   // Source IP/network (e.g., "192.168.1.0/24")
	Dst       string `hcl:"dst,optional" json:"dst,omitempty"`   // Destination IP/network
	VLAN      int    `hcl:"vlan,optional" json:"vlan,omitempty"` // VLAN tag

	// Complex match criteria (OR logic between matches, AND logic within each match)
	// Global fields above apply to ALL matches as defaults
	Matches []ZoneMatch `hcl:"match,block" json:"matches,omitempty"`

	// DEPRECATED: Use Interface or Matches instead
	// Will be auto-converted to Matches with warning
	Interfaces []string `hcl:"interfaces,optional" json:"interfaces,omitempty"`

	// Legacy fields (kept for backwards compat)
	IPSets   []string `hcl:"ipsets,optional" json:"ipsets,omitempty"`     // IPSet names for IP-based membership
	Networks []string `hcl:"networks,optional" json:"networks,omitempty"` // Direct CIDR ranges

	// Zone behavior
	// Action for intra-zone traffic: "accept", "drop", "reject" (default: accept)
	Action string `hcl:"action,optional" json:"action,omitempty"`

	// External marks this as an external/WAN zone (used for auto-masquerade detection)
	// If not set, detected from: DHCP client enabled, non-RFC1918 address, or "wan"/"external" in name
	External *bool `hcl:"external,optional" json:"external,omitempty"`

	// Services provided TO this zone (firewall auto-generates rules)
	// These define what the firewall offers to clients in this zone
	Services *ZoneServices `hcl:"services,block" json:"services,omitempty"`

	// Management access FROM this zone to the firewall
	Management *ZoneManagement `hcl:"management,block" json:"management,omitempty"`

	// IP assignment for simple zones (shorthand - assigns to the interface)
	IPv4 []string `hcl:"ipv4,optional" json:"ipv4,omitempty"`
	IPv6 []string `hcl:"ipv6,optional" json:"ipv6,omitempty"`
	DHCP bool     `hcl:"dhcp,optional" json:"dhcp,omitempty"` // Use DHCP client on this interface
}

// ZoneMatch defines criteria for zone membership.
// Multiple criteria within a match are ANDed together.
// Multiple match blocks are ORed together.
type ZoneMatch struct {
	// Interface can be exact ("eth0") or prefix with + or * suffix ("wg+" matches wg0, wg1...)
	Interface string `hcl:"interface,optional" json:"interface,omitempty"`
	Src       string `hcl:"src,optional" json:"src,omitempty"`
	Dst       string `hcl:"dst,optional" json:"dst,omitempty"`
	VLAN      int    `hcl:"vlan,optional" json:"vlan,omitempty"`
}

// ZoneServices defines which firewall services are available to a zone.
// The firewall automatically generates accept rules for enabled services.
type ZoneServices struct {
	// Network services
	DHCP bool `hcl:"dhcp,optional" json:"dhcp"` // Allow DHCP requests (udp/67-68)
	DNS  bool `hcl:"dns,optional" json:"dns"`   // Allow DNS queries (udp/53, tcp/53)
	NTP  bool `hcl:"ntp,optional" json:"ntp"`   // Allow NTP sync (udp/123)

	// Captive portal / guest access
	CaptivePortal bool `hcl:"captive_portal,optional" json:"captive_portal"` // Redirect HTTP to portal

	// Custom service ports (auto-allow)
	CustomPorts []ZoneServicePort `hcl:"port,block" json:"custom_ports,omitempty"`
}

// ZoneServicePort defines a custom service port to allow from a zone.
type ZoneServicePort struct {
	Name     string `hcl:"name,label" json:"name"`
	Protocol string `hcl:"protocol" json:"protocol"`          // tcp, udp
	Port     int    `hcl:"port" json:"port"`                  // Port number
	EndPort  int    `hcl:"port_end,optional" json:"port_end"` // For port ranges
}

// ZoneManagement defines what management access is allowed from a zone.
type ZoneManagement struct {
	WebUI  bool `hcl:"web_ui,optional" json:"web_ui,omitempty"` // Legacy: Allow Web UI access (tcp/80, tcp/443) -> Use Web
	Web    bool `hcl:"web,optional" json:"web"`                 // Allow Web UI access (tcp/80, tcp/443)
	SSH    bool `hcl:"ssh,optional" json:"ssh"`                 // Allow SSH access (tcp/22)
	API    bool `hcl:"api,optional" json:"api"`                 // Allow API access (used for L7 filtering, implies HTTPS access)
	ICMP   bool `hcl:"icmp,optional" json:"icmp"`               // Allow ping to firewall
	SNMP   bool `hcl:"snmp,optional" json:"snmp"`               // Allow SNMP queries (udp/161)
	Syslog bool `hcl:"syslog,optional" json:"syslog"`           // Allow syslog sending (udp/514)
}

// RuleLearningConfig configures the learning engine.
type RuleLearningConfig struct {
	Enabled       bool     `hcl:"enabled,optional" json:"enabled"`
	LogGroup      int      `hcl:"log_group,optional" json:"log_group"`             // nflog group (default: 100)
	RateLimit     string   `hcl:"rate_limit,optional" json:"rate_limit"`           // e.g., "10/minute"
	AutoApprove   bool     `hcl:"auto_approve,optional" json:"auto_approve"`       // Auto-approve learned rules (legacy)
	IgnoreNets    []string `hcl:"ignore_networks,optional" json:"ignore_networks"` // Networks to ignore from learning
	RetentionDays int      `hcl:"retention_days,optional" json:"retention_days"`   // How long to keep pending rules
	CacheSize     int      `hcl:"cache_size,optional" json:"cache_size"`           // Flow cache size (default: 10000)

	// TOFU (Trust On First Use) mode
	LearningMode bool `hcl:"learning_mode,optional" json:"learning_mode"`

	// InlineMode uses nfqueue instead of nflog for packet inspection.
	// This holds packets until a verdict is returned, fixing the "first packet" problem
	// where the first packet of a new flow would be dropped before an allow rule is added.
	// Trade-off: Adds latency (~microseconds) and requires the engine to be running.
	// Recommended: Enable only during initial learning phase, disable after flows are learned.
	InlineMode bool `hcl:"inline_mode,optional" json:"inline_mode"`

	// NOTE: DNS visibility is now configured via dns { inspect "[zone]" { mode = "passive" } }
}

// PolicyLearning configures per-policy learning settings (overrides global).
type PolicyLearning struct {
	Enabled     bool   `hcl:"enabled,optional" json:"enabled"`
	LogGroup    int    `hcl:"log_group,optional" json:"log_group"`
	RateLimit   string `hcl:"rate_limit,optional" json:"rate_limit"`
	AutoApprove bool   `hcl:"auto_approve,optional" json:"auto_approve"`
}

// AnomalyConfig configures traffic anomaly detection.
type AnomalyConfig struct {
	Enabled           bool    `hcl:"enabled,optional" json:"enabled"`
	BaselineWindow    string  `hcl:"baseline_window,optional" json:"baseline_window"`         // e.g., "7d"
	MinSamples        int     `hcl:"min_samples,optional" json:"min_samples"`                 // Min hits before alerting
	SpikeStdDev       float64 `hcl:"spike_stddev,optional" json:"spike_stddev"`               // Alert if > N stddev
	DropStdDev        float64 `hcl:"drop_stddev,optional" json:"drop_stddev"`                 // Alert if < N stddev
	AlertCooldown     string  `hcl:"alert_cooldown,optional" json:"alert_cooldown"`           // e.g., "15m"
	PortScanThreshold int     `hcl:"port_scan_threshold,optional" json:"port_scan_threshold"` // Ports hit before alert
}

// NotificationsConfig configures the notification system.
type NotificationsConfig struct {
	Enabled  bool                  `hcl:"enabled,optional" json:"enabled"`
	Channels []NotificationChannel `hcl:"channel,block" json:"channels"`
}

// NotificationChannel defines a notification destination.
type NotificationChannel struct {
	Name    string `hcl:"name,label" json:"name"`
	Type    string `hcl:"type" json:"type"`            // email, pushover, slack, discord, ntfy, webhook
	Level   string `hcl:"level,optional" json:"level"` // critical, warning, info
	Enabled bool   `hcl:"enabled,optional" json:"enabled"`

	// Email settings
	SMTPHost     string   `hcl:"smtp_host,optional" json:"smtp_host,omitempty"`
	SMTPPort     int      `hcl:"smtp_port,optional" json:"smtp_port,omitempty"`
	SMTPUser     string   `hcl:"smtp_user,optional" json:"smtp_user,omitempty"`
	SMTPPassword string   `hcl:"smtp_password,optional" json:"smtp_password,omitempty"`
	From         string   `hcl:"from,optional" json:"from,omitempty"`
	To           []string `hcl:"to,optional" json:"to,omitempty"`

	// Webhook/Slack/Discord settings
	WebhookURL string `hcl:"webhook_url,optional" json:"webhook_url,omitempty"`
	Channel    string `hcl:"channel,optional" json:"channel,omitempty"`
	Username   string `hcl:"username,optional" json:"username,omitempty"`

	// Pushover settings
	APIToken string `hcl:"api_token,optional" json:"api_token,omitempty"`
	UserKey  string `hcl:"user_key,optional" json:"user_key,omitempty"`
	Priority int    `hcl:"priority,optional" json:"priority,omitempty"`
	Sound    string `hcl:"sound,optional" json:"sound,omitempty"`

	// ntfy settings
	Server string `hcl:"server,optional" json:"server,omitempty"`
	Topic  string `hcl:"topic,optional" json:"topic,omitempty"`

	// Generic auth (for ntfy, webhook)
	Password string            `hcl:"password,optional" json:"password,omitempty"`
	Headers  map[string]string `hcl:"headers,optional" json:"headers,omitempty"`
}
