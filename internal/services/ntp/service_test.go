package ntp

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"

	"grimm.is/glacic/internal/upgrade"
)

func TestNTPService(t *testing.T) {
	// Setup generic logger
	logger := logging.New(logging.DefaultConfig())

	// Upgrade manager mock (not needed for this test, can be nil or empty)
	upgMgr := upgrade.NewManager(logger)

	svc := NewService(logger)
	svc.SetUpgradeManager(upgMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start service
	// Note: We can't bind :123 in test without root usually.
	// We need to modify Start to accept addr or just test helper directly?
	// But Start calls startListener which binds :123.
	// Let's refactor Service to allow custom address for testing?
	// Or we can mock the listener inheritance?
	// Wait, inheritance logic checks upgradeMgr.
	// If I put a listener in upgradeMgr, it uses it?

	// Better: Use `upgradeMgr.ReceivePacketConn` to inject a loopback conn before calling Start?
	// But `Start` only calls `GetPacketConn`.
	// So I can pre-populate upgradeMgr with a random port listener.

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer pc.Close()

	t.Logf("Listening on %s", pc.LocalAddr())

	// Inject into UpgradeManager
	upgMgr.RegisterPacketConn("ntp-udp", pc)

	// Start service
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}
	defer svc.Stop(context.Background())

	// Send NTP Mode 3 Query
	clientConn, err := net.Dial("udp", pc.LocalAddr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	req := make([]byte, 48)
	req[0] = 0x1B // LI=0, VN=3, Mode=3 (Client)

	// Set Transmit Timestamp (random)
	ts := func(t time.Time) []byte {
		b := make([]byte, 8)
		nt := toNtpTime(t)
		binary.BigEndian.PutUint32(b[0:4], nt.Sec)
		binary.BigEndian.PutUint32(b[4:8], nt.Frac)
		return b
	}
	now := time.Now()
	copy(req[40:48], ts(now))

	if _, err := clientConn.Write(req); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Read Response
	resp := make([]byte, 48)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := clientConn.Read(resp)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if n != 48 {
		t.Fatalf("short response: %d", n)
	}

	// Validate Response (Mode 4)
	mode := resp[0] & 0x07
	if mode != ModeServer {
		t.Errorf("expected mode 4 (server), got %d", mode)
	}

	// Validate Stratum (should be 2)
	strat := resp[1]
	if strat != 2 {
		t.Errorf("expected stratum 2, got %d", strat)
	}

	// Validate Originate Timestamp == Request Transmit Timestamp
	// Originate is bytes 24-32
	// Request Transmit is bytes 40-48 of REQ (which we sent)
	// We stored it in req[40:48]
	if string(resp[24:32]) != string(req[40:48]) {
		t.Errorf("originate timestamp mismatch")
	}
}
