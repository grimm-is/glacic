package scanner

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"
)

func testLogger() *logging.Logger {
	return &logging.Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestScanner_IsPortOpen(t *testing.T) {
	// Start a test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	scanner := New(testLogger(), DefaultConfig())
	ctx := context.Background()

	// Test open port
	if !scanner.isPortOpen(ctx, "127.0.0.1", port) {
		t.Error("expected port to be open")
	}

	// Test closed port
	if scanner.isPortOpen(ctx, "127.0.0.1", 65534) {
		t.Error("expected port 65534 to be closed")
	}
}

func TestScanner_ScanHost(t *testing.T) {
	// Start test servers on two ports
	listener1, _ := net.Listen("tcp", "127.0.0.1:0")
	defer listener1.Close()
	go func() {
		for {
			conn, err := listener1.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	cfg := Config{
		Timeout:     500 * time.Millisecond,
		Concurrency: 10,
	}
	scanner := New(testLogger(), cfg)
	ctx := context.Background()

	result, err := scanner.ScanHost(ctx, "127.0.0.1")
	if err != nil {
		t.Fatalf("ScanHost failed: %v", err)
	}

	if result.IP != "127.0.0.1" {
		t.Errorf("expected IP 127.0.0.1, got %s", result.IP)
	}

	// May or may not find the test server depending on port
	t.Logf("Found %d open ports on localhost", len(result.OpenPorts))
}

func TestScanner_InvalidIP(t *testing.T) {
	scanner := New(testLogger(), DefaultConfig())
	ctx := context.Background()

	_, err := scanner.ScanHost(ctx, "not-an-ip")
	if err == nil {
		t.Error("expected error for invalid IP")
	}
}

func TestScanner_ConcurrentScanPrevention(t *testing.T) {
	scanner := New(testLogger(), Config{
		Timeout:     100 * time.Millisecond,
		Concurrency: 1,
	})

	// Start a slow scan
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go scanner.ScanNetwork(ctx, "127.0.0.1/24")

	// Give it time to start
	time.Sleep(10 * time.Millisecond)

	// Try to start another scan
	_, err := scanner.ScanNetwork(context.Background(), "127.0.0.1/30")
	if err == nil {
		t.Error("expected error when scan already in progress")
	}
}

func TestCommonPorts(t *testing.T) {
	ports := GetCommonPorts()
	if len(ports) == 0 {
		t.Error("expected common ports list to be non-empty")
	}

	// Check for expected ports
	hasHTTP := false
	hasSSH := false
	for _, p := range ports {
		if p.Number == 80 {
			hasHTTP = true
		}
		if p.Number == 22 {
			hasSSH = true
		}
	}

	if !hasHTTP {
		t.Error("expected HTTP port 80 in common ports")
	}
	if !hasSSH {
		t.Error("expected SSH port 22 in common ports")
	}
}

func TestScanner_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Timeout == 0 {
		t.Error("default timeout should not be zero")
	}
	if cfg.Concurrency == 0 {
		t.Error("default concurrency should not be zero")
	}
}

func TestIncIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1", "192.168.1.2"},
		{"192.168.1.255", "192.168.2.0"},
		{"10.0.0.254", "10.0.0.255"},
	}

	for _, tc := range tests {
		ip := net.ParseIP(tc.input).To4()
		incIP(ip)
		if ip.String() != tc.expected {
			t.Errorf("incIP(%s) = %s, expected %s", tc.input, ip.String(), tc.expected)
		}
	}
}
