package upgrade

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"
)

// TestGenericSocketHandoff tests passing both Listeners and PacketConns
func TestGenericSocketHandoff(t *testing.T) {
	// Setup generic logger
	logger := logging.WithComponent("test-upgrade")

	// Create two managers: "old" (sender) and "new" (receiver)
	oldMgr := NewManager(logger)
	newMgr := NewManager(logger)

	// 1. Create dummy listeners
	// TCP Listener
	tcpL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create TCP listener: %v", err)
	}
	defer tcpL.Close()

	// UDP PacketConn
	udpPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create UDP listener: %v", err)
	}
	defer udpPC.Close()

	// 2. Register them with old manager
	oldMgr.RegisterListener("test-tcp", tcpL)
	oldMgr.RegisterPacketConn("test-udp", udpPC)

	// 3. Simulate Handoff Connection (Unix Socket Pair)
	// We can't easily use socketpair in Go via stdlib for net.Conn,
	// so we'll use a real Unix socket.
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "handoff.sock")

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to create unix listener: %v", err)
	}
	defer l.Close()

	errCh := make(chan error, 2)

	// Receiver Goroutine (New Process)
	go func() {
		defer close(errCh)

		conn, err := net.Dial("unix", sockPath)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		// Call receiveListeners directly
		if err := newMgr.receiveListeners(context.Background(), conn); err != nil {
			errCh <- err
			return
		}
	}()

	// Sender Goroutine (Old Process)
	conn, err := l.Accept()
	if err != nil {
		t.Fatalf("Accept failed: %v", err)
	}
	defer conn.Close()

	// Call handoffListeners directly
	if err := oldMgr.handoffListeners(context.Background(), conn); err != nil {
		t.Fatalf("Handoff failed: %v", err)
	}
	conn.Close() // Explicitly close to signal EOF to receiver

	// Wait for receiver
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Receiver failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for receiver")
	}

	// 4. Verification
	// Verify TCP Listener
	retrievedL, ok := newMgr.GetListener("test-tcp")
	if !ok {
		t.Fatal("Failed to retrieve generic TCP listener")
	}
	if retrievedL.Addr().String() != tcpL.Addr().String() {
		t.Errorf("TCP Addr mismatch: got %v, want %v", retrievedL.Addr(), tcpL.Addr())
	}
	retrievedL.Close()

	// Verify PacketConn
	retrievedPC, ok := newMgr.GetPacketConn("test-udp")
	if !ok {
		t.Fatal("Failed to retrieve generic UDP PacketConn")
	}
	if retrievedPC.LocalAddr().String() != udpPC.LocalAddr().String() {
		t.Errorf("UDP Addr mismatch: got %v, want %v", retrievedPC.LocalAddr(), udpPC.LocalAddr())
	}
	retrievedPC.Close()
}
