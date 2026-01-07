package config

// migrate_dns.go migrates legacy dns_server{} config to new dns{} structure.

func migrateDNSServer(c *Config) error {
	if c.DNSServer == nil {
		return nil
	}

	// Skip if new dns{} is already configured
	if c.DNS != nil && (len(c.DNS.Serve) > 0 || len(c.DNS.Forwarders) > 0) {
		return nil
	}

	// Initialize DNS if needed
	if c.DNS == nil {
		c.DNS = &DNS{}
	}

	// Migrate upstream settings to top-level DNS
	c.DNS.Mode = c.DNSServer.Mode
	c.DNS.Forwarders = c.DNSServer.Forwarders
	c.DNS.ConditionalForwarders = c.DNSServer.ConditionalForwarders
	c.DNS.UpstreamTimeout = c.DNSServer.UpstreamTimeout
	c.DNS.UpstreamDoH = c.DNSServer.UpstreamDoH
	c.DNS.UpstreamDoT = c.DNSServer.UpstreamDoT
	c.DNS.UpstreamDNSCrypt = c.DNSServer.UpstreamDNSCrypt
	c.DNS.Recursive = c.DNSServer.Recursive
	c.DNS.DNSSEC = c.DNSServer.DNSSEC

	// Only create serve block if server was enabled
	if c.DNSServer.Enabled {
		serve := DNSServe{
			Zone:             "*",
			ListenPort:       c.DNSServer.ListenPort,
			LocalDomain:      c.DNSServer.LocalDomain,
			ExpandHosts:      c.DNSServer.ExpandHosts,
			DHCPIntegration:  c.DNSServer.DHCPIntegration,
			AuthoritativeFor: c.DNSServer.AuthoritativeFor,
			RebindProtection: c.DNSServer.RebindProtection,
			QueryLogging:     c.DNSServer.QueryLogging,
			RateLimitPerSec:  c.DNSServer.RateLimitPerSec,
			Blocklists:       c.DNSServer.Blocklists,
			Allowlist:        c.DNSServer.Allowlist,
			BlockedTTL:       c.DNSServer.BlockedTTL,
			BlockedAddress:   c.DNSServer.BlockedAddress,
			CacheEnabled:     c.DNSServer.CacheEnabled,
			CacheSize:        c.DNSServer.CacheSize,
			CacheMinTTL:      c.DNSServer.CacheMinTTL,
			CacheMaxTTL:      c.DNSServer.CacheMaxTTL,
			NegativeCacheTTL: c.DNSServer.NegativeCacheTTL,
			DoHServer:        c.DNSServer.DoHServer,
			DoTServer:        c.DNSServer.DoTServer,
			DNSCryptServer:   c.DNSServer.DNSCryptServer,
			Hosts:            c.DNSServer.Hosts,
			Zones:            c.DNSServer.Zones,
		}
		c.DNS.Serve = append(c.DNS.Serve, serve)
	}

	return nil
}

func init() {
	RegisterPostLoadMigration(PostLoadMigration{
		Name:        "dns_server",
		Description: "Migrate legacy dns_server{} to dns{} structure",
		Migrate:     migrateDNSServer,
	})
}
