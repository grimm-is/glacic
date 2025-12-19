package ctlplane

import (
	"testing"
	"time"
)

func TestNotificationHub_Publish(t *testing.T) {
	hub := NewNotificationHub(10)

	hub.Publish(NotifySuccess, "Test", "Message")

	all := hub.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(all))
	}

	if all[0].ID != 1 {
		t.Errorf("expected ID 1, got %d", all[0].ID)
	}
	if all[0].Type != NotifySuccess {
		t.Errorf("expected type success, got %s", all[0].Type)
	}
	if all[0].Title != "Test" {
		t.Errorf("expected title 'Test', got %s", all[0].Title)
	}
}

func TestNotificationHub_GetSince(t *testing.T) {
	hub := NewNotificationHub(10)

	hub.Publish(NotifyInfo, "First", "msg1")
	hub.Publish(NotifyInfo, "Second", "msg2")
	hub.Publish(NotifyInfo, "Third", "msg3")

	// Get all since 0
	all := hub.GetSince(0)
	if len(all) != 3 {
		t.Fatalf("expected 3 notifications since 0, got %d", len(all))
	}

	// Get since ID 1 (should get 2 and 3)
	since1 := hub.GetSince(1)
	if len(since1) != 2 {
		t.Fatalf("expected 2 notifications since ID 1, got %d", len(since1))
	}
	if since1[0].ID != 2 || since1[1].ID != 3 {
		t.Errorf("expected IDs 2,3, got %d,%d", since1[0].ID, since1[1].ID)
	}

	// Get since ID 3 (should get nothing)
	since3 := hub.GetSince(3)
	if len(since3) != 0 {
		t.Fatalf("expected 0 notifications since ID 3, got %d", len(since3))
	}
}

func TestNotificationHub_RingBuffer(t *testing.T) {
	hub := NewNotificationHub(3) // Only holds 3

	hub.Publish(NotifyInfo, "1", "")
	hub.Publish(NotifyInfo, "2", "")
	hub.Publish(NotifyInfo, "3", "")
	hub.Publish(NotifyInfo, "4", "") // Should evict "1"

	all := hub.GetAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 notifications (ring buffer), got %d", len(all))
	}

	// First should now be ID 2 (ID 1 was evicted)
	if all[0].ID != 2 {
		t.Errorf("expected first ID to be 2 after eviction, got %d", all[0].ID)
	}
	if all[2].ID != 4 {
		t.Errorf("expected last ID to be 4, got %d", all[2].ID)
	}
}

func TestNotificationHub_LastID(t *testing.T) {
	hub := NewNotificationHub(10)

	if hub.LastID() != 0 {
		t.Errorf("expected LastID 0 for empty hub, got %d", hub.LastID())
	}

	hub.Publish(NotifyInfo, "Test", "")
	if hub.LastID() != 1 {
		t.Errorf("expected LastID 1, got %d", hub.LastID())
	}

	hub.Publish(NotifyInfo, "Test2", "")
	if hub.LastID() != 2 {
		t.Errorf("expected LastID 2, got %d", hub.LastID())
	}
}

func TestNotificationHub_TimeSet(t *testing.T) {
	hub := NewNotificationHub(10)
	before := time.Now()
	hub.Publish(NotifyWarning, "Test", "msg")
	after := time.Now()

	all := hub.GetAll()
	if all[0].Time.Before(before) || all[0].Time.After(after) {
		t.Errorf("notification time not in expected range")
	}
}
