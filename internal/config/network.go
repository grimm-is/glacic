package config

// Interface represents a physical interface configuration.
type Interface struct {
	Name        string   `hcl:"name,label" json:"name"`
	Description string   `hcl:"description,optional" json:"description,omitempty"`
	Disabled    bool     `hcl:"disabled,optional" json:"disabled,omitempty"` // Temporarily disable this interface (bring it down)
	Zone        string   `hcl:"zone,optional" json:"zone,omitempty"`
	NewZone     *Zone    `hcl:"new_zone,block" json:"new_zone,omitempty"` // Create zone inline
	IPv4        []string `hcl:"ipv4,optional" json:"ipv4,omitempty"`
	IPv6        []string `hcl:"ipv6,optional" json:"ipv6,omitempty"` // IPv6 addresses (static)
	DHCP        bool     `hcl:"dhcp,optional" json:"dhcp"`
	DHCPv6      bool     `hcl:"dhcp_v6,optional" json:"dhcp_v6"` // Enable DHCPv6 Client (WAN)
	RA          bool     `hcl:"ra,optional" json:"ra"`           // Enable Router Advertisements (Server)
	// DHCPClient specifies how DHCP client is managed:
	//   - "builtin" (default): Use Glacic's built-in DHCP client
	//   - "external": External DHCP client (udhcpc, dhclient, etc.) manages this interface
	//   - "monitor": Don't manage DHCP, but monitor interface for IP changes
	DHCPClient string `hcl:"dhcp_client,optional" json:"dhcp_client,omitempty"`

	// Table specifies the routing table ID for this interface.
	// If set to > 0 (and not 254/main), Glacic will enable split routing:
	// - A separate routing table (ID=Table) is created with a default route via Gateway.
	// - Incoming connections are marked with the table ID.
	// - Reply packets are restored with the mark and routed via the table.
	// - Locally initiated traffic marked for this table (e.g. via specific IP) uses the table.
	Table int `hcl:"table,optional" json:"table,omitempty"`

	Gateway   string `hcl:"gateway,optional" json:"gateway,omitempty"`       // Default gateway for static config
	GatewayV6 string `hcl:"gateway_v6,optional" json:"gateway_v6,omitempty"` // Default gateway for static IPv6
	MTU       int    `hcl:"mtu,optional" json:"mtu,omitempty"`
	Bond      *Bond  `hcl:"bond,block" json:"bond,omitempty"`
	VLANs     []VLAN `hcl:"vlan,block" json:"vlans,omitempty"`

	// Anti-Lockout protection (sandbox mode only)
	// When true, implicit accept rules are created for this interface in the
	// high-priority glacic-lockout table. This ensures control plane access
	// is preserved even if the main firewall ruleset is broken/flushed.
	// Defaults to false (protection enabled by default if AccessWebUI is true)
	DisableAntiLockout bool `hcl:"disable_anti_lockout,optional" json:"disable_anti_lockout,omitempty"`

	// Web UI / API Access
	// Deprecated: Use Management block instead
	AccessWebUI bool `hcl:"access_web_ui,optional" json:"access_web_ui,omitempty"`
	// Deprecated: Use Management block instead
	WebUIPort int `hcl:"web_ui_port,optional" json:"web_ui_port,omitempty"` // Port to map (external)

	// Management Access (Interface specific overrides)
	Management *ZoneManagement `hcl:"management,block" json:"management,omitempty"`

	TLS *TLSConfig `hcl:"tls,block" json:"tls,omitempty"` // TLS/Certificate configuration for this interface
}

// Bond represents the configuration for a bonding interface.
type Bond struct {
	Mode       string   `hcl:"mode,optional" json:"mode,omitempty"`
	Interfaces []string `hcl:"interfaces,optional" json:"interfaces,omitempty"`
}

// VLAN represents a VLAN configuration nested within an interface.
type VLAN struct {
	ID          string   `hcl:"id,label" json:"id"`
	Description string   `hcl:"description,optional" json:"description,omitempty"`
	Zone        string   `hcl:"zone,optional" json:"zone,omitempty"`
	NewZone     *Zone    `hcl:"new_zone,block" json:"new_zone,omitempty"` // Create zone inline
	IPv4        []string `hcl:"ipv4,optional" json:"ipv4,omitempty"`
	IPv6        []string `hcl:"ipv6,optional" json:"ipv6,omitempty"`
}

// Route represents a static route configuration.
type Route struct {
	Name        string `hcl:"name,label" json:"name"`
	Destination string `hcl:"destination" json:"destination"`
	Gateway     string `hcl:"gateway,optional" json:"gateway,omitempty"`
	Interface   string `hcl:"interface,optional" json:"interface,omitempty"`
	MonitorIP   string `hcl:"monitor_ip,optional" json:"monitor_ip,omitempty"`
	Table       int    `hcl:"table,optional" json:"table,omitempty"`   // Routing table ID (default: main)
	Metric      int    `hcl:"metric,optional" json:"metric,omitempty"` // Route metric/priority
}

// RoutingTable represents a custom routing table configuration.
type RoutingTable struct {
	Name   string  `hcl:"name,label" json:"name"`
	ID     int     `hcl:"id" json:"id"`              // Table ID (1-252 for custom tables)
	Routes []Route `hcl:"route,block" json:"routes"` // Routes in this table
}

// PolicyRoute represents a policy-based routing rule.
// Policy routes use firewall marks to direct traffic to specific routing tables.
type PolicyRoute struct {
	Name     string `hcl:"name,label" json:"name"`
	Priority int    `hcl:"priority,optional" json:"priority,omitempty"` // Rule priority (lower = higher priority)

	// Match criteria (combined with AND)
	Mark       string `hcl:"mark,optional" json:"mark,omitempty"`           // Firewall mark to match (hex: 0x10)
	MarkMask   string `hcl:"mark_mask,optional" json:"mark_mask,omitempty"` // Mask for mark matching
	FromSource string `hcl:"from,optional" json:"from,omitempty"`           // Source IP/CIDR
	To         string `hcl:"to,optional" json:"to,omitempty"`               // Destination IP/CIDR
	IIF        string `hcl:"iif,optional" json:"iif,omitempty"`             // Input interface
	OIF        string `hcl:"oif,optional" json:"oif,omitempty"`             // Output interface
	FWMark     string `hcl:"fwmark,optional" json:"fwmark,omitempty"`       // Alternative mark syntax

	// Action
	Table     int  `hcl:"table,optional" json:"table,omitempty"`         // Routing table to use
	Blackhole bool `hcl:"blackhole,optional" json:"blackhole,omitempty"` // Drop matching packets
	Prohibit  bool `hcl:"prohibit,optional" json:"prohibit,omitempty"`   // Return ICMP prohibited

	Enabled bool   `hcl:"enabled,optional" json:"enabled,omitempty"` // Default true
	Comment string `hcl:"comment,optional" json:"comment,omitempty"`
}

// MarkRule represents a rule for setting routing marks on packets.
// Marks are set in nftables and matched by ip rule for routing decisions.
type MarkRule struct {
	Name string `hcl:"name,label" json:"name"`
	Mark string `hcl:"mark" json:"mark"`                    // Mark value to set (hex: 0x10)
	Mask string `hcl:"mask,optional" json:"mask,omitempty"` // Mask for mark operations

	// Match criteria
	Protocol     string   `hcl:"proto,optional" json:"proto,omitempty"` // tcp, udp, icmp, all
	SrcIP        string   `hcl:"src_ip,optional" json:"src_ip,omitempty"`
	DstIP        string   `hcl:"dst_ip,optional" json:"dst_ip,omitempty"`
	SrcPort      int      `hcl:"src_port,optional" json:"src_port,omitempty"`
	DstPort      int      `hcl:"dst_port,optional" json:"dst_port,omitempty"`
	DstPorts     []int    `hcl:"dst_ports,optional" json:"dst_ports,omitempty"` // Multiple ports
	InInterface  string   `hcl:"in_interface,optional" json:"in_interface,omitempty"`
	OutInterface string   `hcl:"out_interface,optional" json:"out_interface,omitempty"`
	SrcZone      string   `hcl:"src_zone,optional" json:"src_zone,omitempty"`
	DstZone      string   `hcl:"dst_zone,optional" json:"dst_zone,omitempty"`
	IPSet        string   `hcl:"ipset,optional" json:"ipset,omitempty"`           // Match against IPSet
	ConnState    []string `hcl:"conn_state,optional" json:"conn_state,omitempty"` // NEW, ESTABLISHED, etc.

	// Mark behavior
	SaveMark    bool `hcl:"save_mark,optional" json:"save_mark,omitempty"`       // Save to conntrack
	RestoreMark bool `hcl:"restore_mark,optional" json:"restore_mark,omitempty"` // Restore from conntrack

	Enabled bool   `hcl:"enabled,optional" json:"enabled,omitempty"`
	Comment string `hcl:"comment,optional" json:"comment,omitempty"`
}

// MultiWAN represents multi-WAN configuration for failover and load balancing.
type MultiWAN struct {
	Enabled     bool       `hcl:"enabled,optional" json:"enabled,omitempty"`
	Mode        string     `hcl:"mode,optional" json:"mode,omitempty"` // "failover", "loadbalance", "both"
	Connections []WANLink  `hcl:"wan,block" json:"connections"`
	HealthCheck *WANHealth `hcl:"health_check,block" json:"health_check,omitempty"`
}

// WANLink represents a WAN connection for multi-WAN.
type WANLink struct {
	Name      string `hcl:"name,label" json:"name"`
	Interface string `hcl:"interface" json:"interface"`
	Gateway   string `hcl:"gateway" json:"gateway"`
	Weight    int    `hcl:"weight,optional" json:"weight,omitempty"`     // For load balancing (1-100)
	Priority  int    `hcl:"priority,optional" json:"priority,omitempty"` // For failover (lower = preferred)
	Enabled   bool   `hcl:"enabled,optional" json:"enabled,omitempty"`
}

// WANHealth configures health checking for multi-WAN.
type WANHealth struct {
	Interval  int      `hcl:"interval,optional" json:"interval,omitempty"`     // Check interval in seconds
	Timeout   int      `hcl:"timeout,optional" json:"timeout,omitempty"`       // Timeout per check
	Threshold int      `hcl:"threshold,optional" json:"threshold,omitempty"`   // Failures before marking down
	Targets   []string `hcl:"targets,optional" json:"targets,omitempty"`       // IPs to ping
	HTTPCheck string   `hcl:"http_check,optional" json:"http_check,omitempty"` // URL for HTTP health check
}

// UplinkGroup configures a group of uplinks (WAN, VPN, etc.) with failover/load balancing.
// This enables dynamic switching between uplinks while preserving existing connections.
type UplinkGroup struct {
	Name             string      `hcl:"name,label" json:"name"`
	Uplinks          []UplinkDef `hcl:"uplink,block" json:"uplinks"`
	SourceNetworks   []string    `hcl:"source_networks" json:"source_networks"`                        // CIDRs that use this group
	SourceInterfaces []string    `hcl:"source_interfaces,optional" json:"source_interfaces,omitempty"` // Interfaces for connmark restore
	SourceZones      []string    `hcl:"source_zones,optional" json:"source_zones,omitempty"`           // Zones that use this group

	// Failover configuration
	FailoverMode  string `hcl:"failover_mode,optional" json:"failover_mode,omitempty"`   // "immediate", "graceful", "manual", "programmatic"
	FailbackMode  string `hcl:"failback_mode,optional" json:"failback_mode,omitempty"`   // "immediate", "graceful", "manual", "never"
	FailoverDelay int    `hcl:"failover_delay,optional" json:"failover_delay,omitempty"` // Seconds before failover
	FailbackDelay int    `hcl:"failback_delay,optional" json:"failback_delay,omitempty"` // Seconds before failback

	// Load balancing configuration
	LoadBalanceMode   string `hcl:"load_balance_mode,optional" json:"load_balance_mode,omitempty"` // "none", "roundrobin", "weighted", "latency"
	StickyConnections bool   `hcl:"sticky_connections,optional" json:"sticky_connections,omitempty"`

	HealthCheck *WANHealth `hcl:"health_check,block" json:"health_check,omitempty"`
	Enabled     bool       `hcl:"enabled,optional" json:"enabled,omitempty"`
}

// UplinkDef defines an uplink within a group.
type UplinkDef struct {
	Name      string `hcl:"name,label" json:"name"`                      // e.g., "wg0", "wan1", "primary-vpn"
	Type      string `hcl:"type,optional" json:"type,omitempty"`         // "wan", "wireguard", "tailscale", "openvpn", "ipsec", "custom"
	Interface string `hcl:"interface" json:"interface"`                  // Network interface name
	Gateway   string `hcl:"gateway,optional" json:"gateway,omitempty"`   // Gateway IP (for WANs)
	LocalIP   string `hcl:"local_ip,optional" json:"local_ip,omitempty"` // Local IP for SNAT
	Tier      int    `hcl:"tier,optional" json:"tier,omitempty"`         // Failover tier (0 = primary, 1 = secondary, etc.)
	Weight    int    `hcl:"weight,optional" json:"weight,omitempty"`     // Weight within tier for load balancing (1-100)
	Enabled   bool   `hcl:"enabled,optional" json:"enabled,omitempty"`
	Comment   string `hcl:"comment,optional" json:"comment,omitempty"`

	// Custom health check (optional)
	HealthCheckCmd string `hcl:"health_check_cmd,optional" json:"health_check_cmd,omitempty"` // Custom command for health check
}

// VPNLinkGroup is an alias for UplinkGroup for backward compatibility.
// Deprecated: Use UplinkGroup instead.
type VPNLinkGroup = UplinkGroup

// VPNLinkDef is an alias for UplinkDef for backward compatibility.
// Deprecated: Use UplinkDef instead.
type VPNLinkDef = UplinkDef

// UIDRouting configures per-user routing (for SOCKS proxies, etc.).
type UIDRouting struct {
	Name      string `hcl:"name,label" json:"name"`
	UID       int    `hcl:"uid,optional" json:"uid,omitempty"`             // User ID to match
	Username  string `hcl:"username,optional" json:"username,omitempty"`   // Username (resolved to UID)
	Uplink    string `hcl:"uplink,optional" json:"uplink,omitempty"`       // Uplink to route through
	VPNLink   string `hcl:"vpn_link,optional" json:"vpn_link,omitempty"`   // VPN link to route through
	Interface string `hcl:"interface,optional" json:"interface,omitempty"` // Output interface
	SNATIP    string `hcl:"snat_ip,optional" json:"snat_ip,omitempty"`     // IP to SNAT to
	Enabled   bool   `hcl:"enabled,optional" json:"enabled,omitempty"`
	Comment   string `hcl:"comment,optional" json:"comment,omitempty"`
}

// FRRConfig holds configuration for Free Range Routing (FRR).
type FRRConfig struct {
	Enabled bool  `hcl:"enabled,optional" json:"enabled,omitempty"`
	OSPF    *OSPF `hcl:"ospf,block" json:"ospf,omitempty"`
	BGP     *BGP  `hcl:"bgp,block" json:"bgp,omitempty"`
}

// OSPF configuration.
type OSPF struct {
	RouterID string     `hcl:"router_id,optional" json:"router_id,omitempty"`
	Networks []string   `hcl:"networks,optional" json:"networks,omitempty"` // List of CIDRs to advertise
	Areas    []OSPFArea `hcl:"area,block" json:"areas,omitempty"`
}

// OSPFArea configuration.
type OSPFArea struct {
	ID       string   `hcl:"id,label" json:"id"`
	Networks []string `hcl:"networks,optional" json:"networks,omitempty"`
}

// BGP configuration.
type BGP struct {
	ASN       int        `hcl:"asn,optional" json:"asn,omitempty"`
	RouterID  string     `hcl:"router_id,optional" json:"router_id,omitempty"`
	Neighbors []Neighbor `hcl:"neighbor,block" json:"neighbors,omitempty"`
	Networks  []string   `hcl:"networks,optional" json:"networks,omitempty"`
}

// Neighbor BGP peer configuration.
type Neighbor struct {
	IP        string `hcl:"ip,label" json:"ip"`
	RemoteASN int    `hcl:"remote_asn,optional" json:"remote_asn,omitempty"`
}

// QoSPolicy defines Quality of Service settings for an interface.
type QoSPolicy struct {
	Name         string     `hcl:"name,label" json:"name"`
	Interface    string     `hcl:"interface" json:"interface"` // Interface to apply QoS
	Enabled      bool       `hcl:"enabled,optional" json:"enabled"`
	Direction    string     `hcl:"direction,optional" json:"direction"` // "ingress", "egress", "both" (default: both)
	DownloadMbps int        `hcl:"download_mbps,optional" json:"download_mbps"`
	UploadMbps   int        `hcl:"upload_mbps,optional" json:"upload_mbps"`
	Classes      []QoSClass `hcl:"class,block" json:"classes"`
	Rules        []QoSRule  `hcl:"rule,block" json:"rules"` // Traffic classification rules
}

// QoSClass defines a traffic class for QoS.
type QoSClass struct {
	Name      string `hcl:"name,label" json:"name"`
	Priority  int    `hcl:"priority,optional" json:"priority"`     // 1-7, lower is higher priority
	Rate      string `hcl:"rate,optional" json:"rate"`             // Guaranteed rate e.g., "10mbit" or "10%"
	Ceil      string `hcl:"ceil,optional" json:"ceil"`             // Maximum rate
	Burst     string `hcl:"burst,optional" json:"burst"`           // Burst size
	QueueType string `hcl:"queue_type,optional" json:"queue_type"` // "fq_codel", "sfq", "pfifo" (default: fq_codel)
}

// QoSRule classifies traffic into QoS classes.
type QoSRule struct {
	Name        string       `hcl:"name,label" json:"name"`
	Class       string       `hcl:"class" json:"class"` // Target QoS class
	Protocol    string       `hcl:"proto,optional" json:"proto,omitempty"`
	SrcIP       string       `hcl:"src_ip,optional" json:"src_ip,omitempty"`
	DestIP      string       `hcl:"dest_ip,optional" json:"dest_ip,omitempty"`
	SrcPort     int          `hcl:"src_port,optional" json:"src_port,omitempty"`
	DestPort    int          `hcl:"dest_port,optional" json:"dest_port,omitempty"`
	Services    []string     `hcl:"services,optional" json:"services,omitempty"` // Service names
	ThreatIntel *ThreatIntel `hcl:"threat_intel,block" json:"threat_intel,omitempty"`
	DSCP        string       `hcl:"dscp,optional" json:"dscp,omitempty"`         // Match DSCP value
	SetDSCP     string       `hcl:"set_dscp,optional" json:"set_dscp,omitempty"` // Set DSCP on matching traffic
}
