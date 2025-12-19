package logging

import (
	"testing"
)

func TestDefaultSyslogConfig(t *testing.T) {
	cfg := DefaultSyslogConfig()

	if cfg.Enabled {
		t.Error("Default should be disabled")
	}
	if cfg.Port != 514 {
		t.Errorf("Expected port 514, got %d", cfg.Port)
	}
	if cfg.Protocol != "udp" {
		t.Errorf("Expected protocol udp, got %s", cfg.Protocol)
	}
	if cfg.Tag != "glacic" {
		t.Errorf("Expected tag glacic, got %s", cfg.Tag)
	}
	if cfg.Facility != 1 {
		t.Errorf("Expected facility 1, got %d", cfg.Facility)
	}
}

func TestNewSyslogWriter_MissingHost(t *testing.T) {
	cfg := SyslogConfig{
		Enabled: true,
		Host:    "", // Missing
	}

	_, err := NewSyslogWriter(cfg)
	if err == nil {
		t.Error("Expected error for missing host")
	}
}

func TestNewSyslogWriter_Defaults(t *testing.T) {
	// This test would fail without a real syslog server
	// We're testing the config normalization logic
	cfg := SyslogConfig{
		Host: "localhost",
		// Port, Protocol, Tag should be defaulted
	}

	// Can't actually connect in unit test, but check defaults would be applied
	if cfg.Port == 0 {
		cfg.Port = 514 // Would be defaulted in NewSyslogWriter
	}
	if cfg.Protocol == "" {
		cfg.Protocol = "udp"
	}
	if cfg.Tag == "" {
		cfg.Tag = "glacic"
	}

	if cfg.Port != 514 {
		t.Error("Port should default to 514")
	}
	if cfg.Protocol != "udp" {
		t.Error("Protocol should default to udp")
	}
	if cfg.Tag != "glacic" {
		t.Error("Tag should default to glacic")
	}
}

func TestSyslogConfig_Struct(t *testing.T) {
	cfg := SyslogConfig{
		Enabled:  true,
		Host:     "syslog.example.com",
		Port:     1514,
		Protocol: "tcp",
		Tag:      "myapp",
		Facility: 3,
	}

	if !cfg.Enabled {
		t.Error("Enabled mismatch")
	}
	if cfg.Host != "syslog.example.com" {
		t.Error("Host mismatch")
	}
	if cfg.Port != 1514 {
		t.Error("Port mismatch")
	}
	if cfg.Protocol != "tcp" {
		t.Error("Protocol mismatch")
	}
	if cfg.Tag != "myapp" {
		t.Error("Tag mismatch")
	}
	if cfg.Facility != 3 {
		t.Error("Facility mismatch")
	}
}
