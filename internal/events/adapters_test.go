package events

import (
	"net"
	"testing"
	"time"
)

func TestDHCPAdapter_OnLease(t *testing.T) {
	hub := NewHub()
	adapter := NewDHCPAdapter(hub, nil) // No device manager for test

	// Subscribe to DHCP events
	ch := hub.Subscribe(10, EventDHCPLease)

	// Trigger lease event
	adapter.OnLease("aa:bb:cc:dd:ee:ff", net.ParseIP("192.168.1.100"), "my-laptop")

	// Verify event
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
		if data.IP != "192.168.1.100" {
			t.Errorf("expected IP 192.168.1.100, got %s", data.IP)
		}
		if data.Hostname != "my-laptop" {
			t.Errorf("expected hostname my-laptop, got %s", data.Hostname)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestDHCPAdapter_OnLeaseExpired(t *testing.T) {
	hub := NewHub()
	adapter := NewDHCPAdapter(hub, nil)

	ch := hub.Subscribe(10, EventDHCPExpire)

	adapter.OnLeaseExpired("aa:bb:cc:dd:ee:ff", net.ParseIP("192.168.1.100"), "my-laptop")

	select {
	case e := <-ch:
		if e.Type != EventDHCPExpire {
			t.Errorf("expected EventDHCPExpire, got %s", e.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestNFLogAdapter_HandleEntry(t *testing.T) {
	hub := NewHub()
	adapter := NewNFLogAdapter(hub)

	ch := hub.Subscribe(10, EventNFTMatch)

	adapter.HandleEntry("rule-1", "192.168.1.1", "8.8.8.8", "tcp", "accept", 54321, 443, 1500)

	select {
	case e := <-ch:
		if e.Type != EventNFTMatch {
			t.Errorf("expected EventNFTMatch, got %s", e.Type)
		}
		data, ok := e.Data.(NFTMatchData)
		if !ok {
			t.Fatal("expected NFTMatchData")
		}
		if data.RuleID != "rule-1" {
			t.Errorf("expected rule-1, got %s", data.RuleID)
		}
		if data.DstPort != 443 {
			t.Errorf("expected port 443, got %d", data.DstPort)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestStatsAdapter_OnCounterUpdate(t *testing.T) {
	hub := NewHub()
	adapter := NewStatsAdapter(hub)

	ch := hub.Subscribe(10, EventNFTCounter)

	adapter.OnCounterUpdate("rule-1", 1000, 50000)

	select {
	case e := <-ch:
		if e.Type != EventNFTCounter {
			t.Errorf("expected EventNFTCounter, got %s", e.Type)
		}
		data, ok := e.Data.(NFTCounterData)
		if !ok {
			t.Fatal("expected NFTCounterData")
		}
		if data.Packets != 1000 {
			t.Errorf("expected 1000 packets, got %d", data.Packets)
		}
		if data.Bytes != 50000 {
			t.Errorf("expected 50000 bytes, got %d", data.Bytes)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestWSBridge_TopicMapping(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventDHCPLease, "leases"},
		{EventDHCPExpire, "leases"},
		{EventDeviceSeen, "devices"},
		{EventNFTCounter, "stats"},
		{EventFlowNew, "learning"},
		{EventDNSQuery, ""}, // Not mapped
	}

	for _, tt := range tests {
		got := eventTypeToWSTopic(tt.eventType)
		if got != tt.expected {
			t.Errorf("eventTypeToWSTopic(%s) = %s, want %s", tt.eventType, got, tt.expected)
		}
	}
}

func TestWSBridge_Integration(t *testing.T) {
	hub := NewHub()

	// Mock WS publisher
	published := make(chan struct {
		topic string
		data  any
	}, 10)

	mockPublish := func(topic string, data any) {
		published <- struct {
			topic string
			data  any
		}{topic, data}
	}

	bridge := NewWSBridge(hub, mockPublish)
	bridge.Start()
	defer bridge.Stop()

	// Wait for subscription to be set up
	time.Sleep(10 * time.Millisecond)

	// Emit event
	hub.Publish(Event{Type: EventDHCPLease, Source: "test"})

	// Verify it was forwarded
	select {
	case msg := <-published:
		if msg.topic != "leases" {
			t.Errorf("expected topic 'leases', got %s", msg.topic)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for WS publish")
	}
}
