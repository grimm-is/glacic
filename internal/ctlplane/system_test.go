package ctlplane

import (
	"testing"
)

func TestSystemManager_SafeMode(t *testing.T) {
	sm := NewSystemManager("test.hcl")

	// Initially SafeMode should be false
	status := sm.GetStatus()
	if status.SafeMode {
		t.Error("Expected SafeMode to be false initially")
	}

	// Enable SafeMode
	sm.SetSafeMode(true)

	status = sm.GetStatus()
	if !status.SafeMode {
		t.Error("Expected SafeMode to be true after enabling")
	}

	if !sm.IsSafeMode() {
		t.Error("Expected IsSafeMode() to return true")
	}
}
