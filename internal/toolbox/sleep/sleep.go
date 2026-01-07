package sleep

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Run executes the sleep command with time dilation
func Run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: sleep <seconds> (supports floats)")
	}

	base, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		return fmt.Errorf("invalid duration: %v", err)
	}

	dilation := 1.0
	if d := os.Getenv("TIME_DILATION"); d != "" {
		if val, err := strconv.ParseFloat(d, 64); err == nil && val > 0 {
			dilation = val
		}
	}

	// Calculate duration in nanoseconds
	totalSecs := base * dilation
	d := time.Duration(totalSecs * float64(time.Second))

	time.Sleep(d)
	return nil
}
