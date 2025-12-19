package learning

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"

	"grimm.is/glacic/internal/learning/flowdb"
)

func TestEngine_Lifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engine-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "flow.db")

	cfg := EngineConfig{
		DBPath:       dbPath,
		Logger:       logging.Default(),
		LearningMode: true,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Stop()

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !engine.IsLearningMode() {
		t.Error("Expected learning mode to be true")
	}

	engine.SetLearningMode(false)
	if engine.IsLearningMode() {
		t.Error("Expected learning mode to be false")
	}
}

func TestEngine_ProcessPacket(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engine-test-proc")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := EngineConfig{
		DBPath:       filepath.Join(tmpDir, "flow.db"),
		LearningMode: true,
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Stop()

	pkt := &PacketInfo{
		SrcMAC:   "00:11:22:33:44:55",
		SrcIP:    "192.168.1.100",
		DstIP:    "1.1.1.1",
		DstPort:  80,
		Protocol: "tcp",
	}

	// 1. Process new packet in learning mode -> Should allow
	allowed, err := engine.ProcessPacket(pkt)
	if err != nil {
		t.Fatalf("ProcessPacket failed: %v", err)
	}
	if !allowed {
		t.Error("Expected packet to be allowed in learning mode")
	}

	// Verify flow created
	flows, err := engine.GetAllFlows("allowed", 10)
	if err != nil {
		t.Fatalf("GetAllFlows failed: %v", err)
	}
	if len(flows) != 1 {
		t.Errorf("Expected 1 allowed flow, got %d", len(flows))
	}

	// 2. Disable learning mode
	engine.SetLearningMode(false)

	pkt2 := &PacketInfo{
		SrcMAC:   "AA:BB:CC:DD:EE:FF",
		SrcIP:    "10.0.0.1",
		DstIP:    "8.8.8.8",
		DstPort:  53,
		Protocol: "udp",
	}

	// Process new packet -> Should return false (pending)
	allowed, err = engine.ProcessPacket(pkt2)
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Error("Expected packet to be blocked/pending when learning mode off")
	}

	// Check pending
	pending, err := engine.GetPendingFlows(10)
	if err != nil {
		t.Fatalf("GetPendingFlows failed: %v", err)
	}
	if len(pending) != 1 {
		t.Error("Expected 1 pending flow")
	}

	// 3. Process existing allowed packet -> Should allow (even if learning off)
	allowed, err = engine.ProcessPacket(pkt)
	if !allowed {
		t.Error("Expected existing allowed flow to be allowed")
	}
}

func TestEngine_FlowOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engine-test-ops")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	cfg := EngineConfig{DBPath: filepath.Join(tmpDir, "flow.db")}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Stop()

	// Initial pending flow
	pkt := &PacketInfo{
		SrcMAC:   "00:00:00:00:00:01",
		SrcIP:    "192.168.1.1",
		DstIP:    "1.1.1.1",
		DstPort:  80,
		Protocol: "tcp",
	}
	engine.SetLearningMode(false)
	engine.ProcessPacket(pkt)

	flows, _ := engine.GetPendingFlows(10)
	if len(flows) != 1 {
		t.Fatal("Setup failed: need 1 pending flow")
	}
	flowID := flows[0].ID

	// Test AllowFlow
	called := false
	engine.SetFlowCallbacks(func(f *flowdb.Flow) error {
		called = true
		return nil
	}, nil)

	if err := engine.AllowFlow(flowID); err != nil {
		t.Fatalf("AllowFlow failed: %v", err)
	}

	if !called {
		t.Error("OnFlowAllowed callback not triggered")
	}

	f, _ := engine.GetFlow(flowID)
	if f.State != flowdb.StateAllowed {
		t.Error("Flow state mismatch")
	}

	// Test DenyFlow
	if err := engine.DenyFlow(flowID); err != nil {
		t.Fatal(err)
	}
	f, _ = engine.GetFlow(flowID)
	if f.State != flowdb.StateDenied {
		t.Error("Flow state denied mismatch")
	}

	// Test DeleteFlow
	if err := engine.DeleteFlow(flowID); err != nil {
		t.Fatal(err)
	}
	f, err = engine.GetFlow(flowID)
	// GetFlow returns nil if not found (sql.ErrNoRows -> nil)
	if err != nil {
		t.Errorf("GetFlow error: %v", err)
	}
	if f != nil {
		t.Error("Expected nil flow after deletion")
	}
}

func TestEngine_DNSCorrelation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engine-test-dns")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	cfg := EngineConfig{DBPath: filepath.Join(tmpDir, "flow.db")}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Stop()

	// Snoop DNS first
	ip := net.ParseIP("93.184.216.34")
	engine.HandleDNSResponse("example.com", ip, 3600)

	// Generate flow to that IP
	pkt := &PacketInfo{
		SrcMAC:   "00:00:00:00:00:01",
		SrcIP:    "192.168.1.1",
		DstIP:    "93.184.216.34",
		DstPort:  80,
		Protocol: "tcp",
	}

	engine.ProcessPacket(pkt)

	// Check hints - loop retry
	for i := 0; i < 20; i++ {
		flows, _ := engine.GetAllFlows("pending", 10)
		if len(flows) == 1 {
			if flows[0].BestHint == "example.com" {
				return // Success
			}
			// Fetch full flow to check hints detail if needed
			f, _ := engine.GetFlow(flows[0].ID)
			if f != nil && len(f.DomainHints) > 0 {
				if f.DomainHints[0].Domain == "example.com" {
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("Timeout waiting for DNS enrichment")
}
