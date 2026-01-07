package flowdb

import (
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestFlowDB_CRUD(t *testing.T) {
	db, err := Open(":memory:", nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// 1. Upsert Flow
	flow := &Flow{
		SrcMAC:    "00:11:22:33:44:55",
		SrcIP:     "192.168.1.100",
		Protocol:  "tcp",
		DstPort:   80,
		State:     StatePending,
		FirstSeen: time.Now(),
	}

	if err := db.UpsertFlow(flow); err != nil {
		t.Fatalf("UpsertFlow failed: %v", err)
	}
	if flow.ID == 0 {
		t.Error("ID not set after upsert")
	}

	// 2. Find FindFlow
	found, err := db.FindFlow("00:11:22:33:44:55", "tcp", 80)
	if err != nil {
		t.Fatalf("FindFlow failed: %v", err)
	}
	if found == nil {
		t.Fatal("Flow not found")
	}
	if found.ID != flow.ID {
		t.Errorf("ID mismatch: got %d want %d", found.ID, flow.ID)
	}

	// 3. Add Hint
	hint := &DomainHint{
		FlowID:     flow.ID,
		Domain:     "example.com",
		Confidence: 80,
		Source:     SourceDNSSnoop,
	}
	if err := db.AddHint(hint); err != nil {
		t.Fatalf("AddHint failed: %v", err)
	}

	// 4. Get Hints
	hints, err := db.GetHints(flow.ID)
	if err != nil {
		t.Fatalf("GetHints failed: %v", err)
	}
	if len(hints) != 1 {
		t.Errorf("Expected 1 hint, got %d", len(hints))
	}
	if hints[0].Domain != "example.com" {
		t.Errorf("Hint domain mismatch")
	}

	// 5. GetFlowWithHints
	fwh, err := db.GetFlowWithHints(flow.ID)
	if err != nil {
		t.Fatalf("GetFlowWithHints failed: %v", err)
	}
	if fwh.BestHint != "example.com" {
		t.Errorf("BestHint mismatch")
	}

	// 6. Update State
	if err := db.UpdateState(flow.ID, StateAllowed); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}

	// 7. GetStats
	stats, err := db.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.AllowedFlows != 1 {
		t.Errorf("Expected 1 allowed flow, got %d", stats.AllowedFlows)
	}
	if stats.TotalDomainHints != 1 {
		t.Errorf("Expected 1 hint total")
	}

	// 8. Delete
	if err := db.DeleteFlow(flow.ID); err != nil {
		t.Fatalf("DeleteFlow failed: %v", err)
	}
	found, _ = db.GetFlow(flow.ID)
	if found != nil {
		t.Error("Flow should be deleted")
	}
}

func TestFlowDB_Replication(t *testing.T) {
	db, err := Open(":memory:", nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Check initial version
	v, err := db.GetVersion()
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	if v != 0 {
		t.Errorf("Expected initial version 0, got %d", v)
	}

	// Use unexported logChange ?? No, use public methods and see if they log changes (they don't seem to calls logChange automatically in the current code? upsertFlow doesn't seem to call logChange... wait, checking flowdb.go again)
}

func TestFlowDB_Cleanup(t *testing.T) {
	db, err := Open(":memory:", nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Manually insert old flow
	_, err = db.db.Exec(`
		INSERT INTO learned_flows (src_mac, proto, dst_port, state, last_seen)
		VALUES ('old', 'tcp', 80, 'pending', '2000-01-01 00:00:00')
	`)
	if err != nil {
		t.Fatalf("Manual insert failed: %v", err)
	}

	count, err := db.Cleanup(30)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 deleted row, got %d", count)
	}
}
