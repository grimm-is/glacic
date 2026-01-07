package config

// CurrentSchemaVersion defines the current schema version of the configuration.
const CurrentSchemaVersion = "1.0"

// Config is the top-level structure for the firewall configuration.
type Config struct {
	// Schema version for backward compatibility (e.g., "1.0", "2.0")
	// If empty, defaults to "1.0" for legacy configs
	SchemaVersion string `hcl:"schema_version,optional" json:"schema_version,omitempty"`

	IPForwarding      bool           `hcl:"ip_forwarding,optional" json:"ip_forwarding"`
	MSSClamping       bool           `hcl:"mss_clamping,optional" json:"mss_clamping"` // Enable TCP MSS clamping to PMTU
	EnableFlowOffload bool           `hcl:"enable_flow_offload,optional" json:"enable_flow_offload"`
	Interfaces        []Interface    `hcl:"interface,block" json:"interfaces"`
	Routes            []Route        `hcl:"route,block" json:"routes"`
	RoutingTables     []RoutingTable `hcl:"routing_table,block" json:"routing_tables,omitempty"`
	PolicyRoutes      []PolicyRoute  `hcl:"policy_route,block" json:"policy_routes,omitempty"`
	MarkRules         []MarkRule     `hcl:"mark_rule,block" json:"mark_rules,omitempty"`
	MultiWAN          *MultiWAN      `hcl:"multi_wan,block" json:"multi_wan,omitempty"`
	UplinkGroups      []UplinkGroup  `hcl:"uplink_group,block" json:"uplink_groups,omitempty"`
	UIDRouting        []UIDRouting   `hcl:"uid_routing,block" json:"uid_routing,omitempty"`
	FRR               *FRRConfig     `hcl:"frr,block" json:"frr"`
	Policies          []Policy       `hcl:"policy,block" json:"policies"`
	NAT               []NATRule      `hcl:"nat,block" json:"nat"`
	DHCP              *DHCPServer    `hcl:"dhcp,block" json:"dhcp"`
	DNSServer         *DNSServer     `hcl:"dns_server,block" json:"dns_server"` // Deprecated: use DNS
	DNS               *DNS           `hcl:"dns,block" json:"dns,omitempty"`     // New consolidated DNS config
	ThreatIntel       *ThreatIntel   `hcl:"threat_intel,block" json:"threat_intel,omitempty"`

	IPSets         []IPSet          `hcl:"ipset,block" json:"ipsets"`
	Zones          []Zone           `hcl:"zone,block" json:"zones"`
	Scheduler      *SchedulerConfig `hcl:"scheduler,block" json:"scheduler,omitempty"`
	ScheduledRules []ScheduledRule  `hcl:"scheduled_rule,block" json:"scheduled_rules,omitempty"`

	// Per-interface settings (first-class)
	QoSPolicies []QoSPolicy           `hcl:"qos_policy,block" json:"qos_policies,omitempty"`
	Protections []InterfaceProtection `hcl:"protection,block" json:"protections,omitempty"`

	// Rule learning and notifications
	RuleLearning  *RuleLearningConfig  `hcl:"rule_learning,block" json:"rule_learning,omitempty"`
	AnomalyConfig *AnomalyConfig       `hcl:"anomaly_detection,block" json:"anomaly_detection,omitempty"`
	Notifications *NotificationsConfig `hcl:"notifications,block" json:"notifications,omitempty"`

	// State Replication configuration
	Replication *ReplicationConfig `hcl:"replication,block" json:"replication,omitempty"`

	// VPN integrations (Tailscale, WireGuard, etc.) for secure remote access
	VPN *VPNConfig `hcl:"vpn,block" json:"vpn,omitempty"`

	// API configuration
	API *APIConfig `hcl:"api,block" json:"api,omitempty"`

	// Web Server configuration (previously part of API)
	Web *WebConfig `hcl:"web,block" json:"web,omitempty"`

	// mDNS Reflector configuration
	MDNS *MDNSConfig `hcl:"mdns,block" json:"mdns,omitempty"`

	// UPnP IGD configuration
	UPnP *UPnPConfig `hcl:"upnp,block" json:"upnp,omitempty"`

	// NTP configuration
	NTP *NTPConfig `hcl:"ntp,block" json:"ntp,omitempty"`

	// Feature Flags
	Features *Features `hcl:"features,block" json:"features,omitempty"`

	// Syslog remote logging
	Syslog *SyslogConfig `hcl:"syslog,block" json:"syslog,omitempty"`

	// Dynamic DNS
	DDNS *DDNSConfig `hcl:"ddns,block" json:"ddns,omitempty"`

	// System tuning and settings
	System *SystemConfig `hcl:"system,block" json:"system,omitempty"`

	// Audit logging configuration
	Audit *AuditConfig `hcl:"audit,block" json:"audit,omitempty"`

	// GeoIP configuration for country-based filtering
	GeoIP *GeoIPConfig `hcl:"geoip,block" json:"geoip,omitempty"`

	// State Directory (overrides default /var/lib/glacic)
	StateDir string `hcl:"state_dir,optional" json:"state_dir,omitempty"`
}
