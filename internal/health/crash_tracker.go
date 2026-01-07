package health

import (
	"encoding/json"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"os"
	"path/filepath"
	"time"
)

const (
	// CrashThreshold is the number of consecutive crashes before entering safe mode
	CrashThreshold = 3
	// CrashWindow is the duration to consider a restart as a "crash" (if restarted within this time)
	CrashWindow = 300 * time.Second // 5 minutes
	// StabilityDuration is the time after which we consider the system stable and reset the counter
	StabilityDuration = 300 * time.Second
	// StateFileName is the name of the state file
	StateFileName = "crash.state"
)

type CrashState struct {
	ConsecutiveCrashes int       `json:"consecutive_crashes"`
	LastStartTime      time.Time `json:"last_start_time"`
}

// CrashTracker manages boot loop detection
type CrashTracker struct {
	stateDir string
	state    CrashState
}

// NewCrashTracker creates a new tracker
func NewCrashTracker(stateDir string) *CrashTracker {
	return &CrashTracker{
		stateDir: stateDir,
	}
}

// CheckCrashLoop checks if we are in a crash loop
// Returns true if safe mode should be enabled
func (ct *CrashTracker) CheckCrashLoop() (bool, error) {
	statePath := filepath.Join(ct.stateDir, StateFileName)

	// Ensure directory exists
	if err := os.MkdirAll(ct.stateDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create state dir: %w", err)
	}

	// Read existing state
	data, err := os.ReadFile(statePath)
	if err == nil {
		if err := json.Unmarshal(data, &ct.state); err != nil {
			// Corrupt state, reset
			ct.state = CrashState{}
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to read state file: %w", err)
	}

	// Logic
	now := clock.Now()

	// If last start was recent, increment crash count
	if !ct.state.LastStartTime.IsZero() && now.Sub(ct.state.LastStartTime) < CrashWindow {
		ct.state.ConsecutiveCrashes++
	} else {
		// Reset if outside window (or first run / very old run)
		ct.state.ConsecutiveCrashes = 1 // This is the first "crash" candidate
	}
	ct.state.LastStartTime = now

	// Save new state
	if err := ct.saveState(statePath); err != nil {
		return false, err
	}

	// Check threshold
	if ct.state.ConsecutiveCrashes >= CrashThreshold {
		return true, nil
	}

	return false, nil
}

// StartStabilityTimer starts a background timer to reset the crash count if the system stays up
func (ct *CrashTracker) StartStabilityTimer() {
	go func() {
		time.Sleep(StabilityDuration)
		ct.Reset()
	}()
}

// Reset clears the crash count
func (ct *CrashTracker) Reset() error {
	statePath := filepath.Join(ct.stateDir, StateFileName)
	ct.state.ConsecutiveCrashes = 0
	return ct.saveState(statePath)
}

func (ct *CrashTracker) saveState(path string) error {
	data, err := json.Marshal(ct.state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
