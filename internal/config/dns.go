package config

// DNS is the top-level DNS configuration block.
// It consolidates router resolution settings with zone-based serving and inspection.
type DNS struct {
	// Router's own resolution settings (top-level)
	// When a DNS server is running, the router queries localhost automatically.

	// Resolution mode for upstream queries:
	//   - "forward" (default): Forward queries to upstream DNS servers
	//   - "recursive": Full recursive resolver with root hints
	Mode string `hcl:"mode,optional" json:"mode,omitempty"`

	// Upstream DNS servers for forwarding mode
	Forwarders            []string             `hcl:"forwarders,optional" json:"forwarders,omitempty"`
	ConditionalForwarders []ConditionalForward `hcl:"conditional_forward,block" json:"conditional_forwarders,omitempty"`
	UpstreamTimeout       int                  `hcl:"upstream_timeout,optional" json:"upstream_timeout,omitempty"` // seconds

	// Encrypted DNS upstreams
	UpstreamDoH      []DNSOverHTTPS     `hcl:"upstream_doh,block" json:"upstream_doh,omitempty"`
	UpstreamDoT      []DNSOverTLS       `hcl:"upstream_dot,block" json:"upstream_dot,omitempty"`
	UpstreamDNSCrypt []DNSCryptUpstream `hcl:"upstream_dnscrypt,block" json:"upstream_dnscrypt,omitempty"`

	// Recursive resolver settings (when mode = "recursive")
	Recursive *RecursiveConfig `hcl:"recursive,block" json:"recursive,omitempty"`

	// DNSSEC validation for upstream queries
	DNSSEC bool `hcl:"dnssec,optional" json:"dnssec,omitempty"`

	// Egress Filter (DNS Wall)
	// If enabled, firewall blocks outbound traffic to IPs not recently resolved by this DNS server.
	EgressFilter    bool `hcl:"egress_filter,optional" json:"egress_filter,omitempty"`
	EgressFilterTTL int  `hcl:"egress_filter_ttl,optional" json:"egress_filter_ttl,omitempty"` // Seconds (default: matches record TTL)

	// Zone-based serving and inspection
	Serve   []DNSServe   `hcl:"serve,block" json:"serve,omitempty"`
	Inspect []DNSInspect `hcl:"inspect,block" json:"inspect,omitempty"`
}

// DNSServe configures DNS serving for a specific zone.
// The zone label supports wildcards (e.g., "internal-*").
type DNSServe struct {
	// Zone name (label) - can use wildcards
	Zone string `hcl:"zone,label" json:"zone"`

	// Listen configuration
	ListenPort int `hcl:"listen_port,optional" json:"listen_port,omitempty"` // Default 53

	// Local domain configuration
	LocalDomain      string `hcl:"local_domain,optional" json:"local_domain,omitempty"`
	ExpandHosts      bool   `hcl:"expand_hosts,optional" json:"expand_hosts,omitempty"`
	DHCPIntegration  bool   `hcl:"dhcp_integration,optional" json:"dhcp_integration,omitempty"`
	AuthoritativeFor string `hcl:"authoritative_for,optional" json:"authoritative_for,omitempty"`

	// Security
	RebindProtection bool `hcl:"rebind_protection,optional" json:"rebind_protection,omitempty"`
	QueryLogging     bool `hcl:"query_logging,optional" json:"query_logging,omitempty"`
	RateLimitPerSec  int  `hcl:"rate_limit_per_sec,optional" json:"rate_limit_per_sec,omitempty"`

	// Filtering
	Blocklists     []DNSBlocklist `hcl:"blocklist,block" json:"blocklists,omitempty"`
	Allowlist      []string       `hcl:"allowlist,optional" json:"allowlist,omitempty"`
	BlockedTTL     int            `hcl:"blocked_ttl,optional" json:"blocked_ttl,omitempty"`
	BlockedAddress string         `hcl:"blocked_address,optional" json:"blocked_address,omitempty"`

	// Caching
	CacheEnabled     bool `hcl:"cache_enabled,optional" json:"cache_enabled,omitempty"`
	CacheSize        int  `hcl:"cache_size,optional" json:"cache_size,omitempty"`
	CacheMinTTL      int  `hcl:"cache_min_ttl,optional" json:"cache_min_ttl,omitempty"`
	CacheMaxTTL      int  `hcl:"cache_max_ttl,optional" json:"cache_max_ttl,omitempty"`
	NegativeCacheTTL int  `hcl:"negative_cache_ttl,optional" json:"negative_cache_ttl,omitempty"`

	// Encrypted DNS servers (serve DoH/DoT to clients in this zone)
	DoHServer      *DoHServerConfig      `hcl:"doh_server,block" json:"doh_server,omitempty"`
	DoTServer      *DoTServerConfig      `hcl:"dot_server,block" json:"dot_server,omitempty"`
	DNSCryptServer *DNSCryptServerConfig `hcl:"dnscrypt_server,block" json:"dnscrypt_server,omitempty"`

	// Static entries and zones
	Hosts []DNSHostEntry `hcl:"host,block" json:"hosts,omitempty"`
	Zones []DNSZone      `hcl:"zone,block" json:"zones,omitempty"`
}

// DNSInspect configures DNS traffic inspection for a zone.
// Used for transparent interception or passive visibility.
type DNSInspect struct {
	// Zone name (label) - can use wildcards
	Zone string `hcl:"zone,label" json:"zone"`

	// Mode determines what happens to intercepted traffic:
	//   - "redirect": DNAT to local DNS server (requires serve block)
	//   - "passive": Decode & log queries for visibility (like SNI inspection)
	Mode string `hcl:"mode,optional" json:"mode"` // Default: "redirect"

	// ExcludeRouter prevents redirecting the router's own DNS traffic
	ExcludeRouter bool `hcl:"exclude_router,optional" json:"exclude_router"`
}

// MigrateDNSConfig is now handled by migrations.ApplyPostLoadMigrations()
// See internal/config/migrations/dns_server.go
