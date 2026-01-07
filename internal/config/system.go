package config

// Features defines feature flags for the application
type Features struct {
	ThreatIntel         bool `hcl:"threat_intel,optional" json:"threat_intel"`                 // Phase 5: Threat Intelligence
	NetworkLearning     bool `hcl:"network_learning,optional" json:"network_learning"`         // Automated rule learning
	QoS                 bool `hcl:"qos,optional" json:"qos"`                                   // Traffic Shaping
	IntegrityMonitoring bool `hcl:"integrity_monitoring,optional" json:"integrity_monitoring"` // Detect and revert external changes
}

// APIConfig configures the REST API server.
type APIConfig struct {
	Enabled             bool   `hcl:"enabled,optional" json:"enabled,omitempty"`
	DisableSandbox      bool   `hcl:"disable_sandbox,optional" json:"disable_sandbox,omitempty"`             // Default: false (Sandbox Enabled)
	Listen              string `hcl:"listen,optional" json:"listen,omitempty"`                               // Deprecated: use web.listen
	TLSListen           string `hcl:"tls_listen,optional" json:"tls_listen,omitempty"`                       // Deprecated: use web.tls_listen
	TLSCert             string `hcl:"tls_cert,optional" json:"tls_cert,omitempty"`                           // Deprecated: use web.tls_cert
	TLSKey              string `hcl:"tls_key,optional" json:"tls_key,omitempty"`                             // Deprecated: use web.tls_key
	DisableHTTPRedirect bool   `hcl:"disable_http_redirect,optional" json:"disable_http_redirect,omitempty"` // Deprecated: use web.disable_redirect
	RequireAuth         bool   `hcl:"require_auth,optional" json:"require_auth,omitempty"`                   // Require API key auth

	// Bootstrap key (for initial setup, should be removed after creating real keys)
	BootstrapKey string `hcl:"bootstrap_key,optional" json:"bootstrap_key,omitempty"`

	// API key storage
	KeyStorePath string `hcl:"key_store_path,optional" json:"key_store_path,omitempty"` // Path to key store file

	// Predefined API keys (for config-based key management)
	Keys []APIKeyConfig `hcl:"key,block" json:"keys,omitempty"`

	// CORS settings
	CORSOrigins []string `hcl:"cors_origins,optional" json:"cors_origins,omitempty"`

	// Let's Encrypt automatic TLS
	LetsEncrypt *LetsEncryptConfig `hcl:"letsencrypt,block" json:"letsencrypt,omitempty"`
}

// APIKeyConfig defines an API key in the config file.
type APIKeyConfig struct {
	Name         string   `hcl:"name,label" json:"name"`
	Key          string   `hcl:"key" json:"key"`                 // The actual key value
	Permissions  []string `hcl:"permissions" json:"permissions"` // Permission strings
	AllowedIPs   []string `hcl:"allowed_ips,optional" json:"allowed_ips,omitempty"`
	AllowedPaths []string `hcl:"allowed_paths,optional" json:"allowed_paths,omitempty"`
	RateLimit    int      `hcl:"rate_limit,optional" json:"rate_limit,omitempty"`
	Enabled      bool     `hcl:"enabled,optional" json:"enabled,omitempty"`
	Description  string   `hcl:"description,optional" json:"description,omitempty"`
}

// LetsEncryptConfig configures automatic TLS certificate provisioning.
type LetsEncryptConfig struct {
	Enabled  bool   `hcl:"enabled,optional" json:"enabled"`
	Email    string `hcl:"email" json:"email"`                            // Contact email for certificate
	Domain   string `hcl:"domain" json:"domain"`                          // Domain name for certificate
	CacheDir string `hcl:"cache_dir,optional" json:"cache_dir,omitempty"` // Certificate cache directory
	Staging  bool   `hcl:"staging,optional" json:"staging,omitempty"`     // Use staging server for testing
}

// TLSConfig defines TLS/certificate configuration for an interface.
// The certificate presented depends on which interface the client connects through.
type TLSConfig struct {
	Mode     string `hcl:"mode,optional" json:"mode,omitempty"`         // "self-signed", "acme", "tailscale", "manual"
	Hostname string `hcl:"hostname,optional" json:"hostname,omitempty"` // For Tailscale mode

	// ACME (Let's Encrypt) settings
	Email   string   `hcl:"email,optional" json:"email,omitempty"`
	Domains []string `hcl:"domains,optional" json:"domains,omitempty"`

	// Manual certificate (bring your own)
	CertFile string `hcl:"cert_file,optional" json:"cert_file,omitempty"`
	KeyFile  string `hcl:"key_file,optional" json:"key_file,omitempty"`
}

// ReplicationConfig configures state replication between nodes.
type ReplicationConfig struct {
	// Mode: "primary" or "replica"
	Mode string `hcl:"mode" json:"mode"`

	// Listen address for replication traffic (e.g. ":9000")
	ListenAddr string `hcl:"listen_addr,optional" json:"listen_addr,omitempty"`

	// Address of the primary node (only for replica mode)
	PrimaryAddr string `hcl:"primary_addr,optional" json:"primary_addr,omitempty"`

	// Secret key for authentication (optional)
	SecretKey string `hcl:"secret_key,optional" json:"secret_key,omitempty"`
}

// SchedulerConfig defines scheduler settings.
type SchedulerConfig struct {
	Enabled             bool   `hcl:"enabled,optional" json:"enabled"`
	IPSetRefreshHours   int    `hcl:"ipset_refresh_hours,optional" json:"ipset_refresh_hours"`     // Default: 24
	DNSRefreshHours     int    `hcl:"dns_refresh_hours,optional" json:"dns_refresh_hours"`         // Default: 24
	BackupEnabled       bool   `hcl:"backup_enabled,optional" json:"backup_enabled"`               // Enable auto backups
	BackupSchedule      string `hcl:"backup_schedule,optional" json:"backup_schedule,omitempty"`   // Cron expression, default: "0 2 * * *"
	BackupRetentionDays int    `hcl:"backup_retention_days,optional" json:"backup_retention_days"` // Default: 7
	BackupDir           string `hcl:"backup_dir,optional" json:"backup_dir,omitempty"`             // Default: /var/lib/firewall/backups
}

// ScheduledRule defines a firewall rule that activates on a schedule.
type ScheduledRule struct {
	Name        string     `hcl:"name,label" json:"name"`
	Description string     `hcl:"description,optional" json:"description,omitempty"`
	PolicyName  string     `hcl:"policy" json:"policy"`                                // Which policy to modify
	Rule        PolicyRule `hcl:"rule,block" json:"rule"`                              // The rule to add/remove
	Schedule    string     `hcl:"schedule" json:"schedule"`                            // Cron expression for when to enable
	EndSchedule string     `hcl:"end_schedule,optional" json:"end_schedule,omitempty"` // Cron expression for when to disable
	Enabled     bool       `hcl:"enabled,optional" json:"enabled"`
}

// SystemConfig contains system-level tuning and preferences.
type SystemConfig struct {
	// SysctlProfile selects a preset sysctl tuning profile
	// Options: "default", "performance", "low-memory", "security"
	SysctlProfile string `hcl:"sysctl_profile,optional" json:"sysctl_profile,omitempty"`

	// Sysctl allows manual override of sysctl parameters
	// Applied after profile tuning
	Sysctl map[string]string `hcl:"sysctl,optional" json:"sysctl,omitempty"`
}
