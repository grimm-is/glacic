package config

// DHCPServer configuration.
type DHCPServer struct {
	Enabled bool        `hcl:"enabled,optional" json:"enabled"`
	Scopes  []DHCPScope `hcl:"scope,block" json:"scopes"`
	// Mode specifies how DHCP server is managed:
	//   - "builtin" (default): Use Glacic's built-in DHCP server
	//   - "external": External DHCP server (dnsmasq, isc-dhcp, kea) is in use
	//   - "import": Import leases from external server for visibility
	Mode string `hcl:"mode,optional" json:"mode,omitempty"`
	// ExternalLeaseFile is the path to external DHCP server's lease file (for import mode)
	ExternalLeaseFile string `hcl:"external_lease_file,optional" json:"external_lease_file,omitempty"`
}

// DHCPScope defines a DHCP pool.
type DHCPScope struct {
	Name       string   `hcl:"name,label" json:"name"`
	Interface  string   `hcl:"interface" json:"interface"`
	RangeStart string   `hcl:"range_start" json:"range_start"`
	RangeEnd   string   `hcl:"range_end" json:"range_end"`
	Router     string   `hcl:"router" json:"router"`
	DNS        []string `hcl:"dns,optional" json:"dns"`
	LeaseTime  string   `hcl:"lease_time,optional" json:"lease_time"` // e.g. "24h"
	Domain     string   `hcl:"domain,optional" json:"domain,omitempty"`
	// Custom DHCP options using named options or numeric codes (1-255)
	// Named options: dns_server = "8.8.8.8", tftp_server = "tftp.boot", mtu = "1500"
	// Numeric codes MUST have type prefix: "66" = "str:tftp.boot", "150" = "ip:192.168.1.10"
	// Available prefixes: ip, str/text, hex, u8, u16, u32, bool
	Options      map[string]string `hcl:"options,optional" json:"options,omitempty"`
	Reservations []DHCPReservation `hcl:"reservation,block" json:"reservations,omitempty"`

	// IPv6 Support (SLAAC/DHCPv6)
	RangeStartV6 string   `hcl:"range_start_v6,optional" json:"range_start_v6,omitempty"` // For Stateful DHCPv6
	RangeEndV6   string   `hcl:"range_end_v6,optional" json:"range_end_v6,omitempty"`
	DNSServersV6 []string `hcl:"dns_v6,optional" json:"dns_v6,omitempty"`
}

// DHCPReservation defines a static IP assignment for a MAC address.
type DHCPReservation struct {
	MAC         string            `hcl:"mac,label" json:"mac"`
	IP          string            `hcl:"ip" json:"ip"`
	Hostname    string            `hcl:"hostname,optional" json:"hostname,omitempty"`
	Description string            `hcl:"description,optional" json:"description,omitempty"`
	Options     map[string]string `hcl:"options,optional" json:"options,omitempty"` // Per-host custom DHCP options (same format as scope options)
	// DNS integration
	RegisterDNS bool `hcl:"register_dns,optional" json:"register_dns"` // Auto-register in DNS
}

// DNSServer configuration.
type DNSServer struct {
	Enabled    bool     `hcl:"enabled,optional" json:"enabled"`
	ListenOn   []string `hcl:"listen_on,optional" json:"listen_on"`
	ListenPort int      `hcl:"listen_port,optional" json:"listen_port"` // Default 53

	// Local domain configuration
	LocalDomain      string `hcl:"local_domain,optional" json:"local_domain"`           // e.g., "lan", "home.arpa"
	ExpandHosts      bool   `hcl:"expand_hosts,optional" json:"expand_hosts"`           // Append local domain to simple hostnames
	DHCPIntegration  bool   `hcl:"dhcp_integration,optional" json:"dhcp_integration"`   // Auto-register DHCP hostnames
	AuthoritativeFor string `hcl:"authoritative_for,optional" json:"authoritative_for"` // Return NXDOMAIN for unknown local hosts

	// Resolution mode:
	//   - "forward" (default): Forward queries to upstream DNS servers
	//   - "recursive": Full recursive resolver with root hints
	//   - "external": External DNS server (dnsmasq, bind, unbound) is in use
	Mode string `hcl:"mode,optional" json:"mode"`

	// Upstream DNS (for forwarding mode)
	Forwarders            []string             `hcl:"forwarders,optional" json:"forwarders"`
	ConditionalForwarders []ConditionalForward `hcl:"conditional_forward,block" json:"conditional_forwarders"`
	UpstreamTimeout       int                  `hcl:"upstream_timeout,optional" json:"upstream_timeout"` // seconds

	// Encrypted DNS - Upstream (client mode)
	UpstreamDoH      []DNSOverHTTPS     `hcl:"upstream_doh,block" json:"upstream_doh,omitempty"`           // DNS-over-HTTPS upstreams
	UpstreamDoT      []DNSOverTLS       `hcl:"upstream_dot,block" json:"upstream_dot,omitempty"`           // DNS-over-TLS upstreams
	UpstreamDNSCrypt []DNSCryptUpstream `hcl:"upstream_dnscrypt,block" json:"upstream_dnscrypt,omitempty"` // DNSCrypt upstreams

	// Encrypted DNS - Server (serve DoH/DoT/DNSCrypt to clients)
	DoHServer      *DoHServerConfig      `hcl:"doh_server,block" json:"doh_server,omitempty"`           // Serve DNS-over-HTTPS
	DoTServer      *DoTServerConfig      `hcl:"dot_server,block" json:"dot_server,omitempty"`           // Serve DNS-over-TLS
	DNSCryptServer *DNSCryptServerConfig `hcl:"dnscrypt_server,block" json:"dnscrypt_server,omitempty"` // Serve DNSCrypt

	// Recursive resolver settings (when mode = "recursive")
	Recursive *RecursiveConfig `hcl:"recursive,block" json:"recursive,omitempty"`

	// Security
	DNSSEC           bool `hcl:"dnssec,optional" json:"dnssec"`                       // Validate DNSSEC
	RebindProtection bool `hcl:"rebind_protection,optional" json:"rebind_protection"` // Block private IPs in public responses
	QueryLogging     bool `hcl:"query_logging,optional" json:"query_logging"`
	RateLimitPerSec  int  `hcl:"rate_limit_per_sec,optional" json:"rate_limit_per_sec"` // Per-client rate limit

	// Filtering
	Blocklists     []DNSBlocklist `hcl:"blocklist,block" json:"blocklists"`
	Allowlist      []string       `hcl:"allowlist,optional" json:"allowlist"`             // Domains that bypass blocklists
	BlockedTTL     int            `hcl:"blocked_ttl,optional" json:"blocked_ttl"`         // TTL for blocked responses
	BlockedAddress string         `hcl:"blocked_address,optional" json:"blocked_address"` // IP to return for blocked (default 0.0.0.0)

	// Caching
	CacheEnabled     bool `hcl:"cache_enabled,optional" json:"cache_enabled"`
	CacheSize        int  `hcl:"cache_size,optional" json:"cache_size"`                 // Max entries
	CacheMinTTL      int  `hcl:"cache_min_ttl,optional" json:"cache_min_ttl"`           // Minimum TTL to cache
	CacheMaxTTL      int  `hcl:"cache_max_ttl,optional" json:"cache_max_ttl"`           // Maximum TTL to cache
	NegativeCacheTTL int  `hcl:"negative_cache_ttl,optional" json:"negative_cache_ttl"` // TTL for NXDOMAIN

	// Static entries and zones
	Hosts []DNSHostEntry `hcl:"host,block" json:"hosts"` // Static /etc/hosts style entries
	Zones []DNSZone      `hcl:"zone,block" json:"zones"`
}

// DNSOverHTTPS configures a DNS-over-HTTPS upstream server.
type DNSOverHTTPS struct {
	Name       string `hcl:"name,label" json:"name"`
	URL        string `hcl:"url" json:"url"`                                    // e.g., "https://cloudflare-dns.com/dns-query"
	Bootstrap  string `hcl:"bootstrap,optional" json:"bootstrap,omitempty"`     // IP to use for initial connection
	ServerName string `hcl:"server_name,optional" json:"server_name,omitempty"` // SNI override
	Enabled    bool   `hcl:"enabled,optional" json:"enabled"`
	Priority   int    `hcl:"priority,optional" json:"priority,omitempty"` // Lower = preferred
}

// DNSOverTLS configures a DNS-over-TLS upstream server.
type DNSOverTLS struct {
	Name       string `hcl:"name,label" json:"name"`
	Server     string `hcl:"server" json:"server"`                              // IP:port or hostname:port
	ServerName string `hcl:"server_name,optional" json:"server_name,omitempty"` // For TLS verification
	Enabled    bool   `hcl:"enabled,optional" json:"enabled"`
	Priority   int    `hcl:"priority,optional" json:"priority,omitempty"`
}

// DoHServerConfig configures the DNS-over-HTTPS server.
type DoHServerConfig struct {
	Enabled        bool   `hcl:"enabled,optional" json:"enabled"`
	ListenAddr     string `hcl:"listen_addr,optional" json:"listen_addr,omitempty"` // Default: :443
	Path           string `hcl:"path,optional" json:"path,omitempty"`               // Default: /dns-query
	CertFile       string `hcl:"cert_file,optional" json:"cert_file,omitempty"`
	KeyFile        string `hcl:"key_file,optional" json:"key_file,omitempty"`
	UseLetsEncrypt bool   `hcl:"use_letsencrypt,optional" json:"use_letsencrypt,omitempty"`
	Domain         string `hcl:"domain,optional" json:"domain,omitempty"` // For Let's Encrypt
}

// DoTServerConfig configures the DNS-over-TLS server.
type DoTServerConfig struct {
	Enabled    bool   `hcl:"enabled,optional" json:"enabled"`
	ListenAddr string `hcl:"listen_addr,optional" json:"listen_addr,omitempty"` // Default: :853
	CertFile   string `hcl:"cert_file,optional" json:"cert_file,omitempty"`
	KeyFile    string `hcl:"key_file,optional" json:"key_file,omitempty"`
}

// DNSCryptUpstream configures a DNSCrypt upstream server.
type DNSCryptUpstream struct {
	Name         string `hcl:"name,label" json:"name"`
	Stamp        string `hcl:"stamp,optional" json:"stamp,omitempty"`                 // DNS stamp (sdns://...)
	ProviderName string `hcl:"provider_name,optional" json:"provider_name,omitempty"` // Provider name for manual config
	ServerAddr   string `hcl:"server_addr,optional" json:"server_addr,omitempty"`     // Server address (IP:port)
	PublicKey    string `hcl:"public_key,optional" json:"public_key,omitempty"`       // Server public key (hex)
	Enabled      bool   `hcl:"enabled,optional" json:"enabled"`
	Priority     int    `hcl:"priority,optional" json:"priority,omitempty"`
}

// DNSCryptServerConfig configures the DNSCrypt server (serve DNSCrypt to clients).
type DNSCryptServerConfig struct {
	Enabled       bool   `hcl:"enabled,optional" json:"enabled"`
	ListenAddr    string `hcl:"listen_addr,optional" json:"listen_addr,omitempty"`     // Default: :5443
	ProviderName  string `hcl:"provider_name,optional" json:"provider_name,omitempty"` // e.g., "2.dnscrypt-cert.example.com"
	PublicKeyFile string `hcl:"public_key_file,optional" json:"public_key_file,omitempty"`
	SecretKeyFile string `hcl:"secret_key_file,optional" json:"secret_key_file,omitempty"`
	CertFile      string `hcl:"cert_file,optional" json:"cert_file,omitempty"`   // DNSCrypt certificate
	CertTTL       int    `hcl:"cert_ttl,optional" json:"cert_ttl,omitempty"`     // Certificate TTL in hours
	ESVersion     int    `hcl:"es_version,optional" json:"es_version,omitempty"` // Encryption suite: 1=XSalsa20Poly1305, 2=XChacha20Poly1305
}

// RecursiveConfig configures recursive DNS resolution.
type RecursiveConfig struct {
	// Root hints for recursive resolution
	RootHintsFile       string `hcl:"root_hints_file,optional" json:"root_hints_file,omitempty"` // Path to root hints file
	AutoUpdateRootHints bool   `hcl:"auto_update_root_hints,optional" json:"auto_update_root_hints,omitempty"`

	// Query settings
	MaxDepth      int `hcl:"max_depth,optional" json:"max_depth,omitempty"`           // Max recursion depth (default 30)
	QueryTimeout  int `hcl:"query_timeout,optional" json:"query_timeout,omitempty"`   // Per-query timeout in ms
	MaxConcurrent int `hcl:"max_concurrent,optional" json:"max_concurrent,omitempty"` // Max concurrent outbound queries

	// Hardening
	HardenGlue           bool `hcl:"harden_glue,optional" json:"harden_glue,omitempty"`                       // Validate glue records
	HardenDNSSECStripped bool `hcl:"harden_dnssec_stripped,optional" json:"harden_dnssec_stripped,omitempty"` // Require DNSSEC if expected
	HardenBelowNXDomain  bool `hcl:"harden_below_nxdomain,optional" json:"harden_below_nxdomain,omitempty"`   // RFC 8020 compliance
	HardenReferralPath   bool `hcl:"harden_referral_path,optional" json:"harden_referral_path,omitempty"`     // Validate referral path

	// Privacy
	QNameMinimisation bool `hcl:"qname_minimisation,optional" json:"qname_minimisation,omitempty"` // RFC 7816
	AggressiveNSEC    bool `hcl:"aggressive_nsec,optional" json:"aggressive_nsec,omitempty"`       // RFC 8198

	// Prefetching
	Prefetch    bool `hcl:"prefetch,optional" json:"prefetch,omitempty"`         // Prefetch expiring entries
	PrefetchKey bool `hcl:"prefetch_key,optional" json:"prefetch_key,omitempty"` // Prefetch DNSKEY records
}

// ConditionalForward routes specific domains to specific DNS servers.
type ConditionalForward struct {
	Domain  string   `hcl:"domain,label" json:"domain"` // e.g., "corp.example.com"
	Servers []string `hcl:"servers" json:"servers"`     // DNS servers for this domain
}

// DNSBlocklist defines a domain blocklist source.
type DNSBlocklist struct {
	Name         string `hcl:"name,label" json:"name"`
	URL          string `hcl:"url,optional" json:"url"`       // URL to fetch blocklist
	File         string `hcl:"file,optional" json:"file"`     // Local file path
	Format       string `hcl:"format,optional" json:"format"` // hosts, domains, adblock
	Enabled      bool   `hcl:"enabled,optional" json:"enabled"`
	RefreshHours int    `hcl:"refresh_hours,optional" json:"refresh_hours"`
}

// DNSHostEntry is a static host entry (like /etc/hosts).
type DNSHostEntry struct {
	IP        string   `hcl:"ip,label" json:"ip"`
	Hostnames []string `hcl:"hostnames" json:"hostnames"`
}

// DNSZone configuration for authoritative zones.
type DNSZone struct {
	Name    string      `hcl:"name,label" json:"name"`
	Records []DNSRecord `hcl:"record,block" json:"records"`
}

// DNSRecord configuration.
type DNSRecord struct {
	Name     string `hcl:"name,label" json:"name"`
	Type     string `hcl:"type" json:"type"`   // A, AAAA, CNAME, MX, TXT, PTR, SRV
	Value    string `hcl:"value" json:"value"` // IP or target
	TTL      int    `hcl:"ttl,optional" json:"ttl"`
	Priority int    `hcl:"priority,optional" json:"priority"` // For MX, SRV
}

// SyslogConfig configures remote syslog logging.
type SyslogConfig struct {
	Enabled  bool   `hcl:"enabled,optional" json:"enabled"`
	Host     string `hcl:"host" json:"host"`                            // Remote syslog server hostname/IP
	Port     int    `hcl:"port,optional" json:"port,omitempty"`         // Default: 514
	Protocol string `hcl:"protocol,optional" json:"protocol,omitempty"` // udp or tcp (default: udp)
	Tag      string `hcl:"tag,optional" json:"tag,omitempty"`           // Syslog tag (default: glacic)
	Facility int    `hcl:"facility,optional" json:"facility,omitempty"` // Syslog facility (default: 1)
}

// DDNSConfig configures dynamic DNS updates.
type DDNSConfig struct {
	Enabled   bool   `hcl:"enabled,optional" json:"enabled"`
	Provider  string `hcl:"provider" json:"provider"`                      // duckdns, cloudflare, noip
	Hostname  string `hcl:"hostname" json:"hostname"`                      // Hostname to update
	Token     string `hcl:"token,optional" json:"token,omitempty"`         // API token/password
	Username  string `hcl:"username,optional" json:"username,omitempty"`   // For providers requiring username
	ZoneID    string `hcl:"zone_id,optional" json:"zone_id,omitempty"`     // For Cloudflare
	RecordID  string `hcl:"record_id,optional" json:"record_id,omitempty"` // For Cloudflare
	Interface string `hcl:"interface,optional" json:"interface,omitempty"` // Interface to get IP from
	Interval  int    `hcl:"interval,optional" json:"interval,omitempty"`   // Update interval in minutes (default: 5)
}

// MDNSConfig configures the mDNS reflector service.
type MDNSConfig struct {
	Enabled    bool     `hcl:"enabled,optional" json:"enabled"`
	Interfaces []string `hcl:"interfaces,optional" json:"interfaces"` // Interfaces to reflect between
}

// UPnPConfig configures the UPnP IGD service.
type UPnPConfig struct {
	Enabled       bool     `hcl:"enabled,optional" json:"enabled"`
	ExternalIntf  string   `hcl:"external_interface,optional" json:"external_interface"`   // WAN interface
	InternalIntfs []string `hcl:"internal_interfaces,optional" json:"internal_interfaces"` // LAN interfaces
	SecureMode    bool     `hcl:"secure_mode,optional" json:"secure_mode"`                 // Only allow mapping to requesting IP
}

// NTPConfig configures the NTP service (Client & Server).
type NTPConfig struct {
	Enabled  bool     `hcl:"enabled,optional" json:"enabled"`
	Servers  []string `hcl:"servers,optional" json:"servers,omitempty"`   // Upstream servers
	Interval string   `hcl:"interval,optional" json:"interval,omitempty"` // Sync interval (e.g. "4h")
}
