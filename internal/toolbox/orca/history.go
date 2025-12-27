package orca

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	DefaultMaxRuns   = 20
	HistoryFileName  = "test-history.json"
	FlakyThreshold   = 0.9 // Tests passing < 90% are considered flaky
)

// TestResult represents a single test's result
type TestRunResult struct {
	TestPath string        `json:"test"`
	Status   string        `json:"status"`   // "pass", "fail", "skip"
	Duration time.Duration `json:"duration"` // Test duration
}

// WorkerRun represents all tests run by a single worker in order
type WorkerRun struct {
	WorkerID int             `json:"worker_id"`
	Tests    []TestRunResult `json:"tests"` // Ordered by execution
}

// TestRun represents a single test run, organized by worker
type TestRun struct {
	Timestamp time.Time   `json:"timestamp"`
	RunID     string      `json:"run_id"`    // Unique ID for this run
	LogFiles  []string    `json:"log_files"` // Paths to log files generated in this run
	Workers   []WorkerRun `json:"workers"`   // Results by worker
	Passed    int         `json:"passed"`
	Failed    int         `json:"failed"`
	Skipped   int         `json:"skipped"`
}

// TestHistory tracks test results across multiple runs
type TestHistory struct {
	Runs    []TestRun `json:"runs"`
	MaxRuns int       `json:"maxRuns"`
}

// FlakyStats represents statistics for a single test
type FlakyStats struct {
	TestPath  string
	PassCount int
	FailCount int
	TotalRuns int
	PassRate  float64
}

// LoadHistory loads test history from disk
func LoadHistory(buildDir string) (*TestHistory, error) {
	path := filepath.Join(buildDir, HistoryFileName)
	
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &TestHistory{MaxRuns: DefaultMaxRuns}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read history: %w", err)
	}
	
	var history TestHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("failed to parse history: %w", err)
	}
	
	if history.MaxRuns == 0 {
		history.MaxRuns = DefaultMaxRuns
	}
	
	return &history, nil
}

// Save writes history to disk
func (h *TestHistory) Save(buildDir string) error {
	// Trim to max runs and delete old logs
	if len(h.Runs) > h.MaxRuns {
		toRemove := h.Runs[:len(h.Runs)-h.MaxRuns]
		for _, run := range toRemove {
			for _, logFile := range run.LogFiles {
				if err := os.Remove(logFile); err != nil && !os.IsNotExist(err) {
					fmt.Printf("Warning: failed to remove old log file %s: %v\n", logFile, err)
				}
			}
		}
		h.Runs = h.Runs[len(h.Runs)-h.MaxRuns:]
	}
	
	path := filepath.Join(buildDir, HistoryFileName)
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}
	
	return os.WriteFile(path, data, 0644)
}

// AddRun adds a new test run to history
func (h *TestHistory) AddRun(runID string, passed, failed, skipped int, workers []WorkerRun, logFiles []string) {
	run := TestRun{
		Timestamp: time.Now(),
		RunID:     runID,
		LogFiles:  logFiles,
		Workers:   workers,
		Passed:    passed,
		Failed:    failed,
		Skipped:   skipped,
	}
	h.Runs = append(h.Runs, run)
}

// GetStreak returns the number of consecutive passes for a test
func (h *TestHistory) GetStreak(testPath string) int {
	streak := 0
	// Iterate through runs from newest to oldest
	for i := len(h.Runs) - 1; i >= 0; i-- {
		run := h.Runs[i]
		foundInRun := false
		for _, worker := range run.Workers {
			for _, result := range worker.Tests {
				if result.TestPath == testPath {
					if result.Status == "pass" {
						streak++
					} else {
						// Found a non-pass (fail or skip), broken streak
						return streak
					}
					foundInRun = true
					break
				}
			}
			if foundInRun {
				break
			}
		}
		// If test wasn't run in this execution, we skip it (doesn't break streak)
		// This handles the "filtered out" case mentioned by user
	}
	return streak
}

// GetFlakyTests returns tests that have inconsistent pass/fail across runs
func (h *TestHistory) GetFlakyTests(runs []TestRun) []FlakyStats {
	if runs == nil {
		runs = h.Runs
	}
	if len(runs) < 2 {
		return nil
	}
	
	// Aggregate results per test
	type testStat struct {
		Path     string
		Statuses []string // Ordered statuses
	}
	statsMap := make(map[string]*testStat)
	
	for _, run := range runs {
		for _, worker := range run.Workers {
			for _, result := range worker.Tests {
				if _, ok := statsMap[result.TestPath]; !ok {
					statsMap[result.TestPath] = &testStat{Path: result.TestPath}
				}
				s := statsMap[result.TestPath]
				// Only track pass/fail for flaky analysis
				if result.Status == "pass" || result.Status == "fail" {
					s.Statuses = append(s.Statuses, result.Status)
				}
			}
		}
	}
	
	// Calculate pass rates and filter
	var flaky []FlakyStats
	for _, s := range statsMap {
		total := len(s.Statuses)
		if total < 2 {
			continue
		}

		passCount := 0
		failCount := 0
		for _, status := range s.Statuses {
			if status == "pass" {
				passCount++
			} else {
				failCount++
			}
		}

		// 1. Must have at least one pass (otherwise it's just broken/failing)
		if passCount == 0 {
			continue
		}

		// 2. Must have at least one fail to be flaky
		if failCount == 0 {
			continue
		}

		// 3. Check for "Run of Failures" at the end (Broken)
		// If last 2 runs are failures, consider it broken, not flaky
		isBroken := false
		if total >= 2 {
			if s.Statuses[total-1] == "fail" && s.Statuses[total-2] == "fail" {
				isBroken = true
			}
		}

		if !isBroken {
			rate := float64(passCount) / float64(total)
			flaky = append(flaky, FlakyStats{
				TestPath:  s.Path,
				PassCount: passCount,
				FailCount: failCount,
				TotalRuns: total,
				PassRate:  rate,
			})
		}
	}
	
	// Sort by pass rate (lowest first = most flaky)
	sort.Slice(flaky, func(i, j int) bool {
		return flaky[i].PassRate < flaky[j].PassRate
	})
	
	return flaky
}

// PrintFlakyReport prints a report of flaky tests
func (h *TestHistory) PrintFlakyReport(runs []TestRun) {
	flaky := h.GetFlakyTests(runs)
	if len(flaky) == 0 {
		return
	}
	
	count := len(runs)
	if runs == nil {
		count = len(h.Runs)
	}

	fmt.Println("\n--- Flaky Tests (last", count, "runs) ---")
	for _, s := range flaky {
		status := "flaky"
		if s.PassRate >= 0.8 {
			status = "occasional fail"
		} else if s.PassRate < 0.5 {
			status = "mostly failing"
		}
		fmt.Printf("  %-55s %d/%d pass (%s)\n", s.TestPath, s.PassCount, s.TotalRuns, status)
	}
}

// PrintSummary prints a summary of the test history
func (h *TestHistory) PrintSummary(limit int) {
	if len(h.Runs) == 0 {
		fmt.Println("No test history found.")
		return
	}

	runsToShow := h.Runs
	if limit > 0 && len(h.Runs) > limit {
		runsToShow = h.Runs[len(h.Runs)-limit:]
	}

	fmt.Printf("\n--- Test History (last %d runs) ---\n", len(runsToShow))
	fmt.Printf("%-25s %-10s %-10s %-10s %-10s\n", "Timestamp", "Passed", "Failed", "Skipped", "Workers")
	for i := len(runsToShow) - 1; i >= 0; i-- {
		run := runsToShow[i]
		fmt.Printf("%-25s %-10d %-10d %-10d %-10d\n",
			run.Timestamp.Format("2006-01-02 15:04:05"),
			run.Passed, run.Failed, run.Skipped, len(run.Workers))
	}

	h.PrintFlakyReport(runsToShow)
}

// PrintRunDetails prints detailed information about a specific run
func (run *TestRun) PrintRunDetails() {
	fmt.Printf("\n--- Test Run Details (%s) ---\n", run.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("Run ID: %s\n", run.RunID)
	fmt.Printf("Summary: %d passed, %d failed, %d skipped across %d workers\n",
		run.Passed, run.Failed, run.Skipped, len(run.Workers))

	for _, w := range run.Workers {
		fmt.Printf("\nWorker %d:\n", w.WorkerID)
		for _, t := range w.Tests {
			status := "✅"
			if t.Status == "fail" {
				status = "❌"
			} else if t.Status == "skip" {
				status = "⏭ "
			}
			fmt.Printf("  %s %-55s (%v)\n", status, t.TestPath, t.Duration.Round(time.Millisecond))
		}
	}
}
