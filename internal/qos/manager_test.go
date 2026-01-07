//go:build linux
// +build linux

package qos

import (
	"testing"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		mbps     int
		expected uint64
	}{
		{1, 125000},
		{10, 1250000},
		{0, 0},
	}

	for _, tt := range tests {
		if got := parseRate(tt.mbps); got != tt.expected {
			t.Errorf("parseRate(%d) = %d; want %d", tt.mbps, got, tt.expected)
		}
	}
}

func TestParseRateStr(t *testing.T) {
	parent := uint64(1000000) // 1MB/s
	tests := []struct {
		input    string
		expected uint64
	}{
		{"50%", 500000},
		{"10%", 100000},
		{"1mbit", 125000}, // 1mbit = 1 mbps
		{"invalid", 0},
		{"", 0},
	}

	for _, tt := range tests {
		if got := parseRateStr(tt.input, parent); got != tt.expected {
			t.Errorf("parseRateStr(%q) = %d; want %d", tt.input, got, tt.expected)
		}
	}
}

// Future enhancement: Add ApplyConfig tests that mock netlink or use a namespace.
// Current unit tests cover rate parsing; integration tests cover actual QoS application.
