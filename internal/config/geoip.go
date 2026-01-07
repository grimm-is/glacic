package config

// GeoIPConfig configures country-based traffic filtering using MaxMind or DB-IP databases.
// Both MaxMind (GeoLite2-Country.mmdb) and DB-IP (dbip-country-lite.mmdb) formats are supported.
// DB-IP offers free databases at https://db-ip.com/db/lite.php without requiring a license key.
type GeoIPConfig struct {
	// Enabled activates GeoIP matching in firewall rules.
	Enabled bool `hcl:"enabled,optional" json:"enabled"`

	// DatabasePath is the path to the MMDB file (MaxMind or DB-IP format).
	// Default: /var/lib/glacic/geoip/GeoLite2-Country.mmdb
	// For DB-IP: /var/lib/glacic/geoip/dbip-country-lite.mmdb
	DatabasePath string `hcl:"database_path,optional" json:"database_path,omitempty"`

	// AutoUpdate enables automatic database updates (future feature).
	AutoUpdate bool `hcl:"auto_update,optional" json:"auto_update,omitempty"`

	// LicenseKey for premium MaxMind database updates (future feature).
	// Not required for DB-IP or GeoLite2 (free tier).
	LicenseKey string `hcl:"license_key,optional" json:"license_key,omitempty"`
}
