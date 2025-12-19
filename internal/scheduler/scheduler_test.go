package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"
)

// usageSchedule always returns now (immediate)
type immediateSchedule struct{}

func (s immediateSchedule) Next(t time.Time) time.Time {
	return t
}

// futureSchedule returns time + 1 hour
type futureSchedule struct{}

func (s futureSchedule) Next(t time.Time) time.Time {
	return t.Add(time.Hour)
}

func TestScheduler_CRUD(t *testing.T) {
	s := New(nil)

	task := &Task{
		ID:       "test-1",
		Name:     "Test Task",
		Enabled:  true,
		Schedule: futureSchedule{},
		Func: func(ctx context.Context) error {
			return nil
		},
	}

	// Add
	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	if _, exists := s.GetTaskStatus("test-1"); !exists {
		t.Error("Task not found after add")
	}

	// Duplicate Add
	if err := s.AddTask(task); err == nil {
		t.Error("Expected error adding duplicate task")
	}

	// Enable/Disable
	if err := s.EnableTask("test-1", false); err != nil {
		t.Errorf("Disable failed: %v", err)
	}
	stat, _ := s.GetTaskStatus("test-1")
	if stat.Enabled {
		t.Error("Task should be disabled")
	}

	if err := s.EnableTask("test-1", true); err != nil {
		t.Errorf("Enable failed: %v", err)
	}
	stat, _ = s.GetTaskStatus("test-1")
	if !stat.Enabled {
		t.Error("Task should be enabled")
	}

	// GetStatus list
	all := s.GetStatus()
	if len(all) != 1 {
		t.Errorf("Expected 1 task status, got %d", len(all))
	}

	// Remove
	if err := s.RemoveTask("test-1"); err != nil {
		t.Errorf("RemoveTask failed: %v", err)
	}

	if _, exists := s.GetTaskStatus("test-1"); exists {
		t.Error("Task should be gone after remove")
	}
}

func TestScheduler_Execution(t *testing.T) {
	s := New(nil)
	s.Start()
	defer s.Stop()

	// Wait for start
	time.Sleep(10 * time.Millisecond)
	if !s.IsRunning() {
		t.Error("Scheduler should be running")
	}

	// Test manual run
	ran := make(chan struct{})
	task := &Task{
		ID:       "manual-run",
		Name:     "Manual Run",
		Enabled:  false, // Disabled, but run manually
		Schedule: futureSchedule{},
		Func: func(ctx context.Context) error {
			close(ran)
			return nil
		},
	}
	s.AddTask(task)

	if err := s.RunTask("manual-run"); err != nil {
		t.Fatalf("RunTask failed: %v", err)
	}

	select {
	case <-ran:
		// Success
	case <-time.After(time.Second):
		t.Error("Timeout waiting for manual task run")
	}
}

func TestScheduler_RunOnStart(t *testing.T) {
	s := New(nil)

	var mu sync.Mutex
	ran := false

	task := &Task{
		ID:         "start-run",
		Name:       "Start Run",
		Enabled:    true,
		RunOnStart: true, // Key flag
		Schedule:   futureSchedule{},
		Func: func(ctx context.Context) error {
			mu.Lock()
			ran = true
			mu.Unlock()
			return nil
		},
	}
	s.AddTask(task)

	s.Start()
	defer s.Stop()

	// Give it a moment to run
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	wasRan := ran
	mu.Unlock()

	if !wasRan {
		t.Error("Task with RunOnStart did not run on start")
	}
}
