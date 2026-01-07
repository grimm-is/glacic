package api

import (
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"
)

// mockIPBlocker implements IPBlocker interface for testing
type mockIPBlocker struct {
	added   []string
	removed []string
	checks  []string
	exists  map[string]bool
}

func (m *mockIPBlocker) AddToIPSet(name, ip string) error {
	m.added = append(m.added, name+":"+ip)
	return nil
}

func (m *mockIPBlocker) RemoveFromIPSet(name, ip string) error {
	m.removed = append(m.removed, name+":"+ip)
	return nil
}

func (m *mockIPBlocker) CheckIPSet(name, ip string) (bool, error) {
	m.checks = append(m.checks, name+":"+ip)
	val := m.exists[ip]
	return val, nil
}

func TestSecurityManager_BlockIP(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	mock := &mockIPBlocker{
		exists: make(map[string]bool),
	}

	// Test Nil Blocker
	smNil := NewSecurityManager(nil, logger)
	err := smNil.BlockIP("1.2.3.4", "test")
	if err == nil {
		t.Error("Expected error when blocker is nil")
	} else if err.Error() != "IPBlocker not available" {
		t.Errorf("Unexpected error: %v", err)
	}

	// Test Happy Path
	sm := NewSecurityManager(mock, logger)

	err = sm.BlockIP("1.2.3.4", "test")
	if err != nil {
		t.Errorf("BlockIP failed: %v", err)
	}

	// Verify call
	if len(mock.added) != 1 {
		t.Errorf("Expected 1 addition, got %d", len(mock.added))
	} else if mock.added[0] != "blocked_ips:1.2.3.4" {
		t.Errorf("Unexpected addition: %s", mock.added[0])
	}
}

func TestSecurityManager_AutoBlock(t *testing.T) {
	logger := logging.New(logging.DefaultConfig())
	mock := &mockIPBlocker{
		exists: make(map[string]bool),
	}
	sm := NewSecurityManager(mock, logger)

	// Threshold 3, Window 1m
	// 1st attempt
	err := sm.RecordFailedAttempt("10.0.0.1", "bad_pass", 3, time.Minute)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(mock.added) != 0 {
		t.Errorf("Should not block yet")
	}

	// 2nd attempt
	sm.RecordFailedAttempt("10.0.0.1", "bad_pass", 3, time.Minute)
	if len(mock.added) != 0 {
		t.Errorf("Should not block yet")
	}

	// 3rd attempt - Should Block
	sm.RecordFailedAttempt("10.0.0.1", "bad_pass", 3, time.Minute)
	if len(mock.added) != 1 {
		t.Errorf("Should have blocked after 3 attempts")
	} else if mock.added[0] != "blocked_ips:10.0.0.1" {
		t.Errorf("Unexpected addition: %s", mock.added[0])
	}
}
