package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewCrashTracker(t *testing.T) {
	ct := NewCrashTracker("/tmp/test-crash")
	if ct == nil {
		t.Fatal("NewCrashTracker returned nil")
	}
	if ct.stateDir != "/tmp/test-crash" {
		t.Errorf("Expected stateDir /tmp/test-crash, got %s", ct.stateDir)
	}
}

func TestCrashTracker_CheckCrashLoop_FirstRun(t *testing.T) {
	tmpDir := t.TempDir()
	ct := NewCrashTracker(tmpDir)

	// First run should not trigger safe mode
	safeMode, err := ct.CheckCrashLoop()
	if err != nil {
		t.Fatalf("CheckCrashLoop failed: %v", err)
	}
	if safeMode {
		t.Error("First run should not trigger safe mode")
	}

	// Verify state was saved
	statePath := filepath.Join(tmpDir, StateFileName)
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Error("State file was not created")
	}

	// Verify crash count is 1
	if ct.state.ConsecutiveCrashes != 1 {
		t.Errorf("Expected ConsecutiveCrashes=1, got %d", ct.state.ConsecutiveCrashes)
	}
}

func TestCrashTracker_CheckCrashLoop_TriggersAfterThreshold(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate multiple rapid restarts by pre-populating state
	initialState := CrashState{
		ConsecutiveCrashes: CrashThreshold - 1, // One more will trigger
		LastStartTime:      time.Now(),         // Recent
	}
	statePath := filepath.Join(tmpDir, StateFileName)
	data, _ := json.Marshal(initialState)
	os.WriteFile(statePath, data, 0644)

	ct := NewCrashTracker(tmpDir)
	safeMode, err := ct.CheckCrashLoop()
	if err != nil {
		t.Fatalf("CheckCrashLoop failed: %v", err)
	}
	if !safeMode {
		t.Error("Should trigger safe mode after reaching threshold")
	}
	if ct.state.ConsecutiveCrashes != CrashThreshold {
		t.Errorf("Expected ConsecutiveCrashes=%d, got %d", CrashThreshold, ct.state.ConsecutiveCrashes)
	}
}

func TestCrashTracker_CheckCrashLoop_ResetsOutsideWindow(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate a crash that happened long ago (outside window)
	oldState := CrashState{
		ConsecutiveCrashes: CrashThreshold - 1,
		LastStartTime:      time.Now().Add(-CrashWindow - time.Minute), // Outside window
	}
	statePath := filepath.Join(tmpDir, StateFileName)
	data, _ := json.Marshal(oldState)
	os.WriteFile(statePath, data, 0644)

	ct := NewCrashTracker(tmpDir)
	safeMode, err := ct.CheckCrashLoop()
	if err != nil {
		t.Fatalf("CheckCrashLoop failed: %v", err)
	}
	if safeMode {
		t.Error("Should NOT trigger safe mode - last crash was outside window")
	}
	// Count should reset to 1
	if ct.state.ConsecutiveCrashes != 1 {
		t.Errorf("Expected ConsecutiveCrashes=1 (reset), got %d", ct.state.ConsecutiveCrashes)
	}
}

func TestCrashTracker_Reset(t *testing.T) {
	tmpDir := t.TempDir()
	ct := NewCrashTracker(tmpDir)

	// Simulate some crashes
	ct.state.ConsecutiveCrashes = 5
	ct.state.LastStartTime = time.Now()

	// Reset
	err := ct.Reset()
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	if ct.state.ConsecutiveCrashes != 0 {
		t.Errorf("Expected ConsecutiveCrashes=0 after reset, got %d", ct.state.ConsecutiveCrashes)
	}

	// Verify persisted
	data, _ := os.ReadFile(filepath.Join(tmpDir, StateFileName))
	var loadedState CrashState
	json.Unmarshal(data, &loadedState)
	if loadedState.ConsecutiveCrashes != 0 {
		t.Errorf("Persisted state should have ConsecutiveCrashes=0, got %d", loadedState.ConsecutiveCrashes)
	}
}

func TestCrashTracker_CorruptState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, StateFileName)

	// Write corrupt JSON
	os.WriteFile(statePath, []byte("not valid json{{{"), 0644)

	ct := NewCrashTracker(tmpDir)
	safeMode, err := ct.CheckCrashLoop()
	if err != nil {
		t.Fatalf("CheckCrashLoop should handle corrupt state gracefully: %v", err)
	}
	if safeMode {
		t.Error("Corrupt state should be treated as fresh start, not safe mode")
	}
	// Should have reset to 1
	if ct.state.ConsecutiveCrashes != 1 {
		t.Errorf("Expected ConsecutiveCrashes=1 after corrupt state reset, got %d", ct.state.ConsecutiveCrashes)
	}
}

func TestCrashTracker_StateDirectory_Created(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentDir := filepath.Join(tmpDir, "nested", "dir")

	ct := NewCrashTracker(nonExistentDir)
	_, err := ct.CheckCrashLoop()
	if err != nil {
		t.Fatalf("CheckCrashLoop should create nested directories: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(nonExistentDir); os.IsNotExist(err) {
		t.Error("State directory was not created")
	}
}

func TestCrashTracker_Constants(t *testing.T) {
	if CrashThreshold < 2 {
		t.Errorf("CrashThreshold should be at least 2, got %d", CrashThreshold)
	}
	if CrashWindow < time.Minute {
		t.Errorf("CrashWindow should be at least 1 minute, got %v", CrashWindow)
	}
	if StabilityDuration < time.Minute {
		t.Errorf("StabilityDuration should be at least 1 minute, got %v", StabilityDuration)
	}
}

func TestCrashState_JSONMarshaling(t *testing.T) {
	original := CrashState{
		ConsecutiveCrashes: 5,
		LastStartTime:      time.Now().Truncate(time.Second), // Truncate for comparison
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var loaded CrashState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if loaded.ConsecutiveCrashes != original.ConsecutiveCrashes {
		t.Errorf("ConsecutiveCrashes mismatch: %d vs %d", loaded.ConsecutiveCrashes, original.ConsecutiveCrashes)
	}
}
