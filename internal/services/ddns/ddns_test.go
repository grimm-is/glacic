package ddns

import (
	"testing"
)

func TestNewProvider_DuckDNS(t *testing.T) {
	cfg := Config{
		Provider: "duckdns",
		Token:    "test-token",
		Hostname: "myhost.duckdns.org",
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to create DuckDNS provider: %v", err)
	}

	if provider.Name() != "DuckDNS" {
		t.Errorf("Expected name DuckDNS, got %s", provider.Name())
	}
}

func TestNewProvider_Cloudflare(t *testing.T) {
	cfg := Config{
		Provider: "cloudflare",
		Token:    "cf-token",
		ZoneID:   "zone123",
		RecordID: "record456",
		Hostname: "example.com",
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to create Cloudflare provider: %v", err)
	}

	if provider.Name() != "Cloudflare" {
		t.Errorf("Expected name Cloudflare, got %s", provider.Name())
	}
}

func TestNewProvider_NoIP(t *testing.T) {
	cfg := Config{
		Provider: "noip",
		Username: "user",
		Token:    "password",
		Hostname: "myhost.no-ip.org",
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to create No-IP provider: %v", err)
	}

	if provider.Name() != "No-IP" {
		t.Errorf("Expected name No-IP, got %s", provider.Name())
	}
}

func TestNewProvider_NoIPAlternate(t *testing.T) {
	// Test "no-ip" variant
	cfg := Config{
		Provider: "no-ip",
		Username: "user",
		Token:    "password",
		Hostname: "myhost.no-ip.org",
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to create No-IP provider: %v", err)
	}

	if provider.Name() != "No-IP" {
		t.Errorf("Expected name No-IP, got %s", provider.Name())
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	cfg := Config{
		Provider: "unknown",
	}

	_, err := NewProvider(cfg)
	if err == nil {
		t.Error("Expected error for unknown provider")
	}
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  "duckdns",
		Hostname:  "test.duckdns.org",
		Token:     "secret",
		Interface: "eth0",
		Interval:  10,
	}

	if !cfg.Enabled {
		t.Error("Enabled mismatch")
	}
	if cfg.Provider != "duckdns" {
		t.Error("Provider mismatch")
	}
	if cfg.Hostname != "test.duckdns.org" {
		t.Error("Hostname mismatch")
	}
	if cfg.Token != "secret" {
		t.Error("Token mismatch")
	}
	if cfg.Interface != "eth0" {
		t.Error("Interface mismatch")
	}
	if cfg.Interval != 10 {
		t.Error("Interval mismatch")
	}
}

// Note: Actual Update() methods require network access and real credentials
// Integration tests should be done manually or with mocked HTTP clients
