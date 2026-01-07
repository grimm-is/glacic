//go:build linux

package clock

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"time"
)

const clockAnchorPath = "/var/lib/glacic/clock_anchor"

// EnsureSaneTime checks if system time is reasonable.
// If not, it sets the system clock from the saved anchor file.
// This should be called early in boot before TLS, DHCP, etc.
func EnsureSaneTime() error {
	if IsReasonableTime(time.Now()) {
		return nil // System time is already sane
	}

	// Load saved anchor
	anchor, err := loadAnchor()
	if err != nil {
		return fmt.Errorf("system time is 1970 and no anchor available: %w", err)
	}

	// Set system time
	if err := setSystemTime(anchor); err != nil {
		return fmt.Errorf("failed to set system time: %w", err)
	}

	slog.Info("System time was unreasonable, set to anchor", "anchor", anchor.Format(time.RFC3339))
	return nil
}

// SaveAnchor saves the current time as an anchor for future boots.
// Called lazily when other state changes.
func SaveAnchor() error {
	data, err := time.Now().MarshalText()
	if err != nil {
		return err
	}
	return os.WriteFile(clockAnchorPath, data, 0644)
}

// loadAnchor reads the saved anchor time from disk.
func loadAnchor() (time.Time, error) {
	data, err := os.ReadFile(clockAnchorPath)
	if err != nil {
		return time.Time{}, err
	}

	var t time.Time
	if err := t.UnmarshalText(data); err != nil {
		return time.Time{}, err
	}

	return t, nil
}

// setSystemTime sets the system clock using settimeofday.
// Requires CAP_SYS_TIME (root).
func setSystemTime(t time.Time) error {
	tv := syscall.Timeval{
		Sec:  t.Unix(),
		Usec: int64(t.Nanosecond() / 1000),
	}
	return syscall.Settimeofday(&tv)
}
