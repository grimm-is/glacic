package config

import (
	"testing"

	"github.com/hashicorp/hcl/v2/hclsimple"
)

func TestParseDNSServerBasic(t *testing.T) {
	hclContent := `
dns_server {
  enabled      = true
  listen_port  = 53
  local_domain = "home.lan"
  forwarders   = ["1.1.1.1", "8.8.8.8"]
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	dns := cfg.DNSServer
	if dns == nil {
		t.Fatal("DNSServer is nil")
	}
	if !dns.Enabled {
		t.Error("Expected DNS server to be enabled")
	}
	if dns.ListenPort != 53 {
		t.Errorf("Expected listen_port 53, got %d", dns.ListenPort)
	}
	if dns.LocalDomain != "home.lan" {
		t.Errorf("Expected local_domain 'home.lan', got %q", dns.LocalDomain)
	}
	if len(dns.Forwarders) != 2 {
		t.Errorf("Expected 2 forwarders, got %d", len(dns.Forwarders))
	}
}

func TestParseDNSModes(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		wantMode string
	}{
		{"forward mode", "forward", "forward"},
		{"recursive mode", "recursive", "recursive"},
		{"empty defaults to forward", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hclContent := `
dns_server {
  enabled = true
  mode    = "` + tt.mode + `"
}
`
			var cfg Config
			err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
			if err != nil {
				t.Fatalf("Failed to parse HCL: %v", err)
			}
			if cfg.DNSServer.Mode != tt.wantMode {
				t.Errorf("Expected mode %q, got %q", tt.wantMode, cfg.DNSServer.Mode)
			}
		})
	}
}

func TestParseDNSOverHTTPS(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true

  upstream_doh "cloudflare" {
    url       = "https://cloudflare-dns.com/dns-query"
    bootstrap = "1.1.1.1"
    enabled   = true
    priority  = 1
  }

  upstream_doh "google" {
    url         = "https://dns.google/dns-query"
    server_name = "dns.google"
    enabled     = true
    priority    = 2
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.DNSServer.UpstreamDoH) != 2 {
		t.Fatalf("Expected 2 DoH upstreams, got %d", len(cfg.DNSServer.UpstreamDoH))
	}

	cf := cfg.DNSServer.UpstreamDoH[0]
	if cf.Name != "cloudflare" {
		t.Errorf("Expected name 'cloudflare', got %q", cf.Name)
	}
	if cf.URL != "https://cloudflare-dns.com/dns-query" {
		t.Errorf("Expected cloudflare URL, got %q", cf.URL)
	}
	if cf.Bootstrap != "1.1.1.1" {
		t.Errorf("Expected bootstrap '1.1.1.1', got %q", cf.Bootstrap)
	}
	if cf.Priority != 1 {
		t.Errorf("Expected priority 1, got %d", cf.Priority)
	}

	google := cfg.DNSServer.UpstreamDoH[1]
	if google.ServerName != "dns.google" {
		t.Errorf("Expected server_name 'dns.google', got %q", google.ServerName)
	}
}

func TestParseDNSOverTLS(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true

  upstream_dot "cloudflare" {
    server      = "1.1.1.1:853"
    server_name = "cloudflare-dns.com"
    enabled     = true
    priority    = 1
  }

  upstream_dot "quad9" {
    server  = "9.9.9.9:853"
    enabled = true
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.DNSServer.UpstreamDoT) != 2 {
		t.Fatalf("Expected 2 DoT upstreams, got %d", len(cfg.DNSServer.UpstreamDoT))
	}

	cf := cfg.DNSServer.UpstreamDoT[0]
	if cf.Name != "cloudflare" {
		t.Errorf("Expected name 'cloudflare', got %q", cf.Name)
	}
	if cf.Server != "1.1.1.1:853" {
		t.Errorf("Expected server '1.1.1.1:853', got %q", cf.Server)
	}
	if cf.ServerName != "cloudflare-dns.com" {
		t.Errorf("Expected server_name 'cloudflare-dns.com', got %q", cf.ServerName)
	}
}

func TestParseDNSCrypt(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true

  upstream_dnscrypt "quad9" {
    stamp   = "sdns://AQMAAAAAAAAADDkuOS45Ljk6ODQ0MyBnyEe4yHWM0SAkVUO-dWdG3zTfHYTAC4xHA2jfgh2GPhkyLmRuc2NyeXB0LWNlcnQucXVhZDkubmV0"
    enabled = true
  }

  upstream_dnscrypt "adguard" {
    provider_name = "2.dnscrypt.default.ns1.adguard.com"
    server_addr   = "94.140.14.14:5443"
    public_key    = "d12bb6c0f8e0b0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0"
    enabled       = true
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.DNSServer.UpstreamDNSCrypt) != 2 {
		t.Fatalf("Expected 2 DNSCrypt upstreams, got %d", len(cfg.DNSServer.UpstreamDNSCrypt))
	}

	quad9 := cfg.DNSServer.UpstreamDNSCrypt[0]
	if quad9.Name != "quad9" {
		t.Errorf("Expected name 'quad9', got %q", quad9.Name)
	}
	if quad9.Stamp == "" {
		t.Error("Expected stamp to be set")
	}

	adguard := cfg.DNSServer.UpstreamDNSCrypt[1]
	if adguard.ProviderName != "2.dnscrypt.default.ns1.adguard.com" {
		t.Errorf("Expected provider_name, got %q", adguard.ProviderName)
	}
	if adguard.ServerAddr != "94.140.14.14:5443" {
		t.Errorf("Expected server_addr, got %q", adguard.ServerAddr)
	}
}

func TestParseDoHServer(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true

  doh_server {
    enabled         = true
    listen_addr     = ":443"
    path            = "/dns-query"
    use_letsencrypt = true
    domain          = "dns.example.com"
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	doh := cfg.DNSServer.DoHServer
	if doh == nil {
		t.Fatal("DoHServer is nil")
	}
	if !doh.Enabled {
		t.Error("Expected DoH server to be enabled")
	}
	if doh.ListenAddr != ":443" {
		t.Errorf("Expected listen_addr ':443', got %q", doh.ListenAddr)
	}
	if doh.Path != "/dns-query" {
		t.Errorf("Expected path '/dns-query', got %q", doh.Path)
	}
	if !doh.UseLetsEncrypt {
		t.Error("Expected use_letsencrypt to be true")
	}
	if doh.Domain != "dns.example.com" {
		t.Errorf("Expected domain 'dns.example.com', got %q", doh.Domain)
	}
}

func TestParseDoTServer(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true

  dot_server {
    enabled     = true
    listen_addr = ":853"
    cert_file   = "/etc/ssl/dns.crt"
    key_file    = "/etc/ssl/dns.key"
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	dot := cfg.DNSServer.DoTServer
	if dot == nil {
		t.Fatal("DoTServer is nil")
	}
	if !dot.Enabled {
		t.Error("Expected DoT server to be enabled")
	}
	if dot.CertFile != "/etc/ssl/dns.crt" {
		t.Errorf("Expected cert_file, got %q", dot.CertFile)
	}
}

func TestParseDNSCryptServer(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true

  dnscrypt_server {
    enabled         = true
    listen_addr     = ":5443"
    provider_name   = "2.dnscrypt-cert.example.com"
    public_key_file = "/etc/dnscrypt/public.key"
    secret_key_file = "/etc/dnscrypt/secret.key"
    cert_ttl        = 24
    es_version      = 2
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	dnscrypt := cfg.DNSServer.DNSCryptServer
	if dnscrypt == nil {
		t.Fatal("DNSCryptServer is nil")
	}
	if !dnscrypt.Enabled {
		t.Error("Expected DNSCrypt server to be enabled")
	}
	if dnscrypt.ProviderName != "2.dnscrypt-cert.example.com" {
		t.Errorf("Expected provider_name, got %q", dnscrypt.ProviderName)
	}
	if dnscrypt.ESVersion != 2 {
		t.Errorf("Expected es_version 2, got %d", dnscrypt.ESVersion)
	}
}

func TestParseRecursiveConfig(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true
  mode    = "recursive"

  recursive {
    root_hints_file        = "/etc/dns/root.hints"
    auto_update_root_hints = true
    max_depth              = 30
    query_timeout          = 5000
    max_concurrent         = 100
    harden_glue            = true
    harden_dnssec_stripped = true
    harden_below_nxdomain  = true
    qname_minimisation     = true
    aggressive_nsec        = true
    prefetch               = true
    prefetch_key           = true
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if cfg.DNSServer.Mode != "recursive" {
		t.Errorf("Expected mode 'recursive', got %q", cfg.DNSServer.Mode)
	}

	rec := cfg.DNSServer.Recursive
	if rec == nil {
		t.Fatal("Recursive config is nil")
	}
	if rec.RootHintsFile != "/etc/dns/root.hints" {
		t.Errorf("Expected root_hints_file, got %q", rec.RootHintsFile)
	}
	if !rec.AutoUpdateRootHints {
		t.Error("Expected auto_update_root_hints to be true")
	}
	if rec.MaxDepth != 30 {
		t.Errorf("Expected max_depth 30, got %d", rec.MaxDepth)
	}
	if !rec.HardenGlue {
		t.Error("Expected harden_glue to be true")
	}
	if !rec.QNameMinimisation {
		t.Error("Expected qname_minimisation to be true")
	}
	if !rec.AggressiveNSEC {
		t.Error("Expected aggressive_nsec to be true")
	}
	if !rec.Prefetch {
		t.Error("Expected prefetch to be true")
	}
}

func TestParseDNSSecuritySettings(t *testing.T) {
	hclContent := `
dns_server {
  enabled           = true
  dnssec            = true
  rebind_protection = true
  query_logging     = true
  rate_limit_per_sec = 100
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	dns := cfg.DNSServer
	if !dns.DNSSEC {
		t.Error("Expected DNSSEC to be enabled")
	}
	if !dns.RebindProtection {
		t.Error("Expected rebind_protection to be enabled")
	}
	if !dns.QueryLogging {
		t.Error("Expected query_logging to be enabled")
	}
	if dns.RateLimitPerSec != 100 {
		t.Errorf("Expected rate_limit_per_sec 100, got %d", dns.RateLimitPerSec)
	}
}

func TestParseDNSCaching(t *testing.T) {
	hclContent := `
dns_server {
  enabled       = true
  cache_enabled = true
  cache_size    = 10000
  cache_min_ttl = 60
  cache_max_ttl = 86400
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	dns := cfg.DNSServer
	if !dns.CacheEnabled {
		t.Error("Expected cache to be enabled")
	}
	if dns.CacheSize != 10000 {
		t.Errorf("Expected cache_size 10000, got %d", dns.CacheSize)
	}
	if dns.CacheMinTTL != 60 {
		t.Errorf("Expected cache_min_ttl 60, got %d", dns.CacheMinTTL)
	}
	if dns.CacheMaxTTL != 86400 {
		t.Errorf("Expected cache_max_ttl 86400, got %d", dns.CacheMaxTTL)
	}
}

func TestParseDNSBlocklist(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true

  blocklist "ads" {
    url           = "https://example.com/ads.txt"
    format        = "domains"
    enabled       = true
    refresh_hours = 24
  }

  blocklist "malware" {
    url     = "https://example.com/malware.txt"
    format  = "hosts"
    enabled = true
  }

  allowlist = ["safe.example.com", "trusted.example.com"]
  blocked_ttl = 300
  blocked_address = "0.0.0.0"
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	dns := cfg.DNSServer
	if len(dns.Blocklists) != 2 {
		t.Fatalf("Expected 2 blocklists, got %d", len(dns.Blocklists))
	}

	ads := dns.Blocklists[0]
	if ads.Name != "ads" {
		t.Errorf("Expected name 'ads', got %q", ads.Name)
	}
	if ads.Format != "domains" {
		t.Errorf("Expected format 'domains', got %q", ads.Format)
	}
	if ads.RefreshHours != 24 {
		t.Errorf("Expected refresh_hours 24, got %d", ads.RefreshHours)
	}

	if len(dns.Allowlist) != 2 {
		t.Errorf("Expected 2 allowlist entries, got %d", len(dns.Allowlist))
	}
	if dns.BlockedTTL != 300 {
		t.Errorf("Expected blocked_ttl 300, got %d", dns.BlockedTTL)
	}
}

func TestParseDNSZones(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true

  zone "example.lan" {
    record "server" {
      type  = "A"
      value = "10.0.0.10"
      ttl   = 3600
    }

    record "www" {
      type  = "CNAME"
      value = "server.example.lan"
    }
  }

  zone "corp.example.com" {
    record "app" {
      type  = "A"
      value = "10.0.0.20"
    }
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.DNSServer.Zones) != 2 {
		t.Fatalf("Expected 2 zones, got %d", len(cfg.DNSServer.Zones))
	}

	primary := cfg.DNSServer.Zones[0]
	if primary.Name != "example.lan" {
		t.Errorf("Expected zone name 'example.lan', got %q", primary.Name)
	}
	if len(primary.Records) != 2 {
		t.Errorf("Expected 2 records, got %d", len(primary.Records))
	}

	second := cfg.DNSServer.Zones[1]
	if second.Name != "corp.example.com" {
		t.Errorf("Expected zone name 'corp.example.com', got %q", second.Name)
	}
	if len(second.Records) != 1 {
		t.Errorf("Expected 1 record, got %d", len(second.Records))
	}
}

func TestParseConditionalForward(t *testing.T) {
	hclContent := `
dns_server {
  enabled = true

  conditional_forward "corp.example.com" {
    servers = ["10.0.0.1", "10.0.0.2"]
  }

  conditional_forward "internal.example.com" {
    servers = ["192.168.1.1"]
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.DNSServer.ConditionalForwarders) != 2 {
		t.Fatalf("Expected 2 conditional forwarders, got %d", len(cfg.DNSServer.ConditionalForwarders))
	}

	cf := cfg.DNSServer.ConditionalForwarders[0]
	if cf.Domain != "corp.example.com" {
		t.Errorf("Expected domain 'corp.example.com', got %q", cf.Domain)
	}
	if len(cf.Servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(cf.Servers))
	}
}

func TestParseFullDNSConfig(t *testing.T) {
	// Test a complete DNS configuration with all features
	hclContent := `
dns_server {
  enabled           = true
  listen_on         = ["0.0.0.0"]
  listen_port       = 53
  local_domain      = "home.lan"
  dhcp_integration  = true
  mode              = "forward"
  dnssec            = true
  rebind_protection = true
  cache_enabled     = true
  cache_size        = 10000

  forwarders = ["1.1.1.1", "8.8.8.8"]

  upstream_doh "cloudflare" {
    url     = "https://cloudflare-dns.com/dns-query"
    enabled = true
  }

  upstream_dot "cloudflare" {
    server  = "1.1.1.1:853"
    enabled = true
  }

  upstream_dnscrypt "quad9" {
    stamp   = "sdns://test"
    enabled = true
  }

  blocklist "ads" {
    url     = "https://example.com/ads.txt"
    format  = "domains"
    enabled = true
  }

  zone "home.lan" {
    record "router" {
      type  = "A"
      value = "10.0.0.1"
    }
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	dns := cfg.DNSServer
	if dns == nil {
		t.Fatal("DNSServer is nil")
	}

	// Verify all sections are parsed
	if len(dns.Forwarders) != 2 {
		t.Errorf("Expected 2 forwarders, got %d", len(dns.Forwarders))
	}
	if len(dns.UpstreamDoH) != 1 {
		t.Errorf("Expected 1 DoH upstream, got %d", len(dns.UpstreamDoH))
	}
	if len(dns.UpstreamDoT) != 1 {
		t.Errorf("Expected 1 DoT upstream, got %d", len(dns.UpstreamDoT))
	}
	if len(dns.UpstreamDNSCrypt) != 1 {
		t.Errorf("Expected 1 DNSCrypt upstream, got %d", len(dns.UpstreamDNSCrypt))
	}
	if len(dns.Blocklists) != 1 {
		t.Errorf("Expected 1 blocklist, got %d", len(dns.Blocklists))
	}
	if len(dns.Zones) != 1 {
		t.Errorf("Expected 1 zone, got %d", len(dns.Zones))
	}
}

// --- New dns {} syntax tests ---

func TestParseDNSBlock(t *testing.T) {
	hclContent := `
dns {
  forwarders = ["8.8.8.8", "1.1.1.1"]
  mode       = "forward"

  serve "LAN" {
    local_domain     = "home.lan"
    dhcp_integration = true
    cache_enabled    = true
    cache_size       = 10000
  }

  inspect "LAN" {
    mode           = "redirect"
    exclude_router = true
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	dns := cfg.DNS
	if dns == nil {
		t.Fatal("DNS is nil")
	}

	// Check top-level settings
	if len(dns.Forwarders) != 2 {
		t.Errorf("Expected 2 forwarders, got %d", len(dns.Forwarders))
	}
	if dns.Mode != "forward" {
		t.Errorf("Expected mode 'forward', got %q", dns.Mode)
	}

	// Check serve block
	if len(dns.Serve) != 1 {
		t.Fatalf("Expected 1 serve block, got %d", len(dns.Serve))
	}
	serve := dns.Serve[0]
	if serve.Zone != "LAN" {
		t.Errorf("Expected zone 'LAN', got %q", serve.Zone)
	}
	if serve.LocalDomain != "home.lan" {
		t.Errorf("Expected local_domain 'home.lan', got %q", serve.LocalDomain)
	}
	if !serve.DHCPIntegration {
		t.Error("Expected dhcp_integration to be true")
	}
	if !serve.CacheEnabled {
		t.Error("Expected cache_enabled to be true")
	}

	// Check inspect block
	if len(dns.Inspect) != 1 {
		t.Fatalf("Expected 1 inspect block, got %d", len(dns.Inspect))
	}
	inspect := dns.Inspect[0]
	if inspect.Zone != "LAN" {
		t.Errorf("Expected zone 'LAN', got %q", inspect.Zone)
	}
	if inspect.Mode != "redirect" {
		t.Errorf("Expected mode 'redirect', got %q", inspect.Mode)
	}
	if !inspect.ExcludeRouter {
		t.Error("Expected exclude_router to be true")
	}
}

func TestParseDNSMultipleServeAndInspect(t *testing.T) {
	hclContent := `
dns {
  forwarders = ["1.1.1.1"]

  serve "LAN" {
    local_domain = "home.lan"
  }

  serve "Guest" {
    cache_enabled = true
  }

  inspect "LAN" {
    mode = "redirect"
  }

  inspect "Guest" {
    mode = "passive"
  }
}
`
	var cfg Config
	err := hclsimple.Decode("test.hcl", []byte(hclContent), nil, &cfg)
	if err != nil {
		t.Fatalf("Failed to parse HCL: %v", err)
	}

	if len(cfg.DNS.Serve) != 2 {
		t.Errorf("Expected 2 serve blocks, got %d", len(cfg.DNS.Serve))
	}
	if len(cfg.DNS.Inspect) != 2 {
		t.Errorf("Expected 2 inspect blocks, got %d", len(cfg.DNS.Inspect))
	}

	// Verify second serve block
	if cfg.DNS.Serve[1].Zone != "Guest" {
		t.Errorf("Expected second serve zone 'Guest', got %q", cfg.DNS.Serve[1].Zone)
	}

	// Verify inspect modes
	if cfg.DNS.Inspect[0].Mode != "redirect" {
		t.Errorf("Expected first inspect mode 'redirect', got %q", cfg.DNS.Inspect[0].Mode)
	}
	if cfg.DNS.Inspect[1].Mode != "passive" {
		t.Errorf("Expected second inspect mode 'passive', got %q", cfg.DNS.Inspect[1].Mode)
	}
}

func TestMigrateDNSConfig(t *testing.T) {
	// Test that legacy dns_server {} gets migrated to dns {}
	cfg := &Config{
		DNSServer: &DNSServer{
			Enabled:         true,
			ListenPort:      53,
			LocalDomain:     "home.lan",
			Mode:            "forward",
			Forwarders:      []string{"8.8.8.8"},
			DHCPIntegration: true,
			CacheEnabled:    true,
		},
	}

	ApplyPostLoadMigrations(cfg)

	if cfg.DNS == nil {
		t.Fatal("DNS should be populated after migration")
	}

	// Check upstream settings migrated to top-level
	if cfg.DNS.Mode != "forward" {
		t.Errorf("Expected mode 'forward', got %q", cfg.DNS.Mode)
	}
	if len(cfg.DNS.Forwarders) != 1 || cfg.DNS.Forwarders[0] != "8.8.8.8" {
		t.Errorf("Expected forwarders ['8.8.8.8'], got %v", cfg.DNS.Forwarders)
	}

	// Check serve block created
	if len(cfg.DNS.Serve) != 1 {
		t.Fatalf("Expected 1 serve block, got %d", len(cfg.DNS.Serve))
	}
	serve := cfg.DNS.Serve[0]
	if serve.Zone != "*" {
		t.Errorf("Expected zone '*' for legacy, got %q", serve.Zone)
	}
	if serve.LocalDomain != "home.lan" {
		t.Errorf("Expected local_domain 'home.lan', got %q", serve.LocalDomain)
	}
	if !serve.DHCPIntegration {
		t.Error("Expected dhcp_integration to be true")
	}
}

func TestMigrateDNSConfigNoOverwrite(t *testing.T) {
	// Test that migration doesn't overwrite existing dns {} config
	cfg := &Config{
		DNSServer: &DNSServer{
			Enabled:    true,
			Forwarders: []string{"8.8.8.8"},
		},
		DNS: &DNS{
			Forwarders: []string{"1.1.1.1"},
			Serve: []DNSServe{
				{Zone: "LAN", LocalDomain: "custom.lan"},
			},
		},
	}

	ApplyPostLoadMigrations(cfg)

	// Should NOT overwrite existing config
	if len(cfg.DNS.Forwarders) != 1 || cfg.DNS.Forwarders[0] != "1.1.1.1" {
		t.Errorf("Migration should not overwrite existing forwarders, got %v", cfg.DNS.Forwarders)
	}
	if len(cfg.DNS.Serve) != 1 || cfg.DNS.Serve[0].Zone != "LAN" {
		t.Errorf("Migration should not overwrite existing serve blocks")
	}
}
