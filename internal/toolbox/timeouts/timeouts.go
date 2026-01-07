package timeouts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	// factor is the multiplier applied to all timeouts.
	factor float64 = 1.0
	once   sync.Once
)

const (
	// calibrationIterations is the work volume.
	// Tuned to take ~100-200ms on a modern fast CPU.
	calibrationIterations = 500_000

	// ReferenceDuration is the time it takes on your PRIMARY dev machine.
	// You will update this value after running the calibration tool below.
	// Tuned to 32ms based on M1 Max performance (calibration gave ~0.42x with 75ms).
	ReferenceDuration = 32 * time.Millisecond
)

// Scale accepts a base duration and returns the adjusted duration
// based on the system's performance relative to the reference machine.
func Scale(base time.Duration) time.Duration {
	ensureCalibrated()
	return time.Duration(float64(base) * factor)
}

// GetFactor returns the current scaling multiplier for logging.
func GetFactor() float64 {
	ensureCalibrated()
	return factor
}

// GetFactorString returns the factor as a string suitable for env vars.
func GetFactorString() string {
	return fmt.Sprintf("%.2f", GetFactor())
}

func ensureCalibrated() {
	once.Do(func() {
		// 1. Allow manual override via ENV (Crucial for debugging/CI forcing)
		if env := os.Getenv("ORCA_TIMEOUT_FACTOR"); env != "" {
			if f, err := strconv.ParseFloat(env, 64); err == nil && f > 0 {
				factor = f
				return
			}
		}

		// 2. Run the Benchmark
		start := time.Now()
		cpuBenchmark(calibrationIterations)
		elapsed := time.Since(start)

		// 3. Calculate Factor
		factor = float64(elapsed) / float64(ReferenceDuration)

		// 4. Sanity Clamps (0.25x to 10x)
		// We rarely want to go faster than 0.25x (risk of race conditions)
		// We rarely want to go slower than 10x (test is effectively hung)
		if factor < 1.0 {
			factor = 1.0
		} else if factor > 10.0 {
			factor = 10.0
		}
	})
}

// cpuBenchmark hashes a small dataset repeatedly.
// This stresses integer performance and small memory allocations.
func cpuBenchmark(n int) {
	data := []byte("orca-calibration-workload-string")
	for i := 0; i < n; i++ {
		h := sha256.New()
		h.Write(data)
		h.Write([]byte{byte(i)}) // Ensure input changes
		_ = h.Sum(nil)
	}
}
