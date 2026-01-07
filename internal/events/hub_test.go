package events

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestHub_PublishSubscribe(t *testing.T) {
	hub := NewHub()

	// Subscribe to specific event type
	ch := hub.Subscribe(10, EventDHCPLease)

	// Publish event
	hub.Publish(Event{
		Type:   EventDHCPLease,
		Source: "test",
		Data:   DHCPLeaseData{MAC: "aa:bb:cc:dd:ee:ff", IP: "192.168.1.100"},
	})

	// Should receive
	select {
	case e := <-ch:
		if e.Type != EventDHCPLease {
			t.Errorf("expected EventDHCPLease, got %s", e.Type)
		}
		data, ok := e.Data.(DHCPLeaseData)
		if !ok {
			t.Fatal("expected DHCPLeaseData")
		}
		if data.MAC != "aa:bb:cc:dd:ee:ff" {
			t.Errorf("expected MAC aa:bb:cc:dd:ee:ff, got %s", data.MAC)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestHub_GlobalSubscription(t *testing.T) {
	hub := NewHub()

	// Global subscription (no types specified)
	ch := hub.Subscribe(10)

	// Publish different event types
	hub.Publish(Event{Type: EventDHCPLease, Source: "test"})
	hub.Publish(Event{Type: EventDNSQuery, Source: "test"})
	hub.Publish(Event{Type: EventNFTMatch, Source: "test"})

	// Should receive all 3
	received := 0
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
			received++
		case <-time.After(100 * time.Millisecond):
			break
		}
	}

	if received != 3 {
		t.Errorf("expected 3 events, got %d", received)
	}
}

func TestHub_TypeFiltering(t *testing.T) {
	hub := NewHub()

	// Subscribe only to DNS events
	ch := hub.Subscribe(10, EventDNSQuery, EventDNSBlock)

	// Publish various types
	hub.Publish(Event{Type: EventDHCPLease, Source: "test"})
	hub.Publish(Event{Type: EventDNSQuery, Source: "test"})
	hub.Publish(Event{Type: EventNFTMatch, Source: "test"})
	hub.Publish(Event{Type: EventDNSBlock, Source: "test"})

	// Should only receive 2 DNS events
	received := 0
	for {
		select {
		case <-ch:
			received++
		case <-time.After(50 * time.Millisecond):
			goto done
		}
	}
done:

	if received != 2 {
		t.Errorf("expected 2 DNS events, got %d", received)
	}
}

func TestHub_NonBlocking(t *testing.T) {
	hub := NewHub()

	// Subscribe with buffer of 1
	ch := hub.Subscribe(1, EventNFTCounter)
	_ = ch // Consume to avoid unused error

	// Publish more events than buffer
	for i := 0; i < 10; i++ {
		hub.Publish(Event{Type: EventNFTCounter, Source: "test"})
	}

	// Should not block - just drop overflows
	published, dropped := hub.Stats()
	if published != 10 {
		t.Errorf("expected 10 published, got %d", published)
	}
	if dropped < 9 {
		t.Errorf("expected at least 9 dropped, got %d", dropped)
	}
}

func TestHub_Concurrent(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe(1000, EventNFTCounter)

	var wg sync.WaitGroup
	const numPublishers = 10
	const eventsPerPublisher = 100

	// Concurrent publishers
	for i := 0; i < numPublishers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerPublisher; j++ {
				hub.Publish(Event{Type: EventNFTCounter, Source: "test"})
			}
		}()
	}

	wg.Wait()

	// Drain channel
	received := 0
	for {
		select {
		case <-ch:
			received++
		default:
			goto done
		}
	}
done:

	if received < numPublishers*eventsPerPublisher/2 {
		t.Errorf("expected at least %d events, got %d", numPublishers*eventsPerPublisher/2, received)
	}
}

func TestAggregator_Schema(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	hub := NewHub()
	agg, err := NewAggregator(db, hub)
	if err != nil {
		t.Fatalf("failed to create aggregator: %v", err)
	}
	_ = agg

	// Verify tables exist
	tables := []string{"stats_raw", "stats_hourly", "stats_daily"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}
}

func TestAggregator_WriteAndQuery(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	hub := NewHub()
	agg, err := NewAggregator(db, hub)
	if err != nil {
		t.Fatalf("failed to create aggregator: %v", err)
	}

	// Start with short flush interval
	cfg := DefaultAggregatorConfig()
	cfg.FlushInterval = 50 * time.Millisecond
	agg.Start(cfg)
	defer agg.Stop()

	// Emit some counter events
	hub.EmitNFTCounter("rule-1", 100, 1000)
	hub.EmitNFTCounter("rule-1", 200, 2000)
	hub.EmitNFTCounter("rule-2", 50, 500)

	// Wait for flush
	time.Sleep(100 * time.Millisecond)

	// Query recent stats
	points, err := agg.GetRecentStats("rule-1", 5*time.Minute)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(points) != 2 {
		t.Errorf("expected 2 points for rule-1, got %d", len(points))
	}
}
