package orca

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"grimm.is/glacic/internal/toolbox/harness"
	"grimm.is/glacic/internal/toolbox/timeouts"
)

// AgentPort is the vsock port the agent listens on (Linux only)
const AgentPort = 5000

// TestJob represents a test to be run
type TestJob struct {
	ScriptPath string        // Relative to project root (e.g., "t/01-sanity/sanity_test.sh")
	Timeout    time.Duration // Per-test timeout (parsed from TEST_TIMEOUT comment)
	Skip       bool          // If true, test should be skipped (parsed from SKIP=true)
	SkipReason string        // Reason for skipping (from comment after SKIP=true)
}

// Default timeout for tests that don't specify one
const DefaultTestTimeout = 90 * time.Second

// TestResult represents the outcome of running a test
type TestResult struct {
	Job       TestJob
	Result    string
	Duration  time.Duration
	Error     error
	RawOutput string
	Suite     *harness.TestSuite
	WorkerID  string
	StartTime time.Time
}

// Regex to match TEST_TIMEOUT comment in scripts
var testTimeoutRe = regexp.MustCompile(`(?m)^#?\s*TEST_TIMEOUT[=:]\s*(\d+)`)

// DiscoverTests finds all test scripts in t/ and parses their timeouts
func DiscoverTests(projectRoot string, target string, history *TestHistory) ([]TestJob, error) {
	var jobs []TestJob

	var testDir string
	if target != "" {
		testDir = filepath.Join(projectRoot, "integration_tests", target)
	} else {
		// Default to integration_tests/linux, fallback to t/ if not found (backwards compatibility)
		testDir = filepath.Join(projectRoot, "integration_tests", "linux")
		if _, err := os.Stat(testDir); os.IsNotExist(err) {
			testDir = filepath.Join(projectRoot, "t")
		}
	}

	err := filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, "_test.sh") {
			relPath, _ := filepath.Rel(projectRoot, path)

			// 1. Parse static timeout from file (or default 30s)
			staticTimeout := parseTestTimeout(path) // This is already Scaled

			// 2. Check history for dynamic timeout
			var finalTimeout = staticTimeout
			if history != nil {
				avg := history.GetExpectedDuration(relPath)
				if avg > 0 {
					// Found history! Calculate dynamic timeout.
					// Average * 2.5 safety margin
					baseDynamic := time.Duration(float64(avg) * 2.5)

					// Apply scaling (machine load)
					dynamic := timeouts.Scale(baseDynamic)

					// Floor of 5s
					if dynamic < 5*time.Second {
						dynamic = 5 * time.Second
					}

					// Respect static timeout if it's longer than dynamic
					if staticTimeout > dynamic {
						finalTimeout = staticTimeout
					} else {
						finalTimeout = dynamic
					}
				}
			}

			skip, skipReason := parseTestSkip(path)

			jobs = append(jobs, TestJob{
				ScriptPath: relPath,
				Timeout:    finalTimeout,
				Skip:       skip,
				SkipReason: skipReason,
			})
		}

		return nil
	})

	return jobs, err
}

func parseTestTimeout(scriptPath string) time.Duration {
	file, err := os.Open(scriptPath)
	if err != nil {
		return timeouts.Scale(DefaultTestTimeout)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() && lineCount < 50 {
		line := scanner.Text()
		lineCount++

		if match := testTimeoutRe.FindStringSubmatch(line); match != nil {
			if seconds, err := strconv.Atoi(match[1]); err == nil {
				// Apply TIME_DILATION scaling to the timeout
				return timeouts.Scale(time.Duration(seconds) * time.Second)
			}
		}
	}

	return timeouts.Scale(DefaultTestTimeout)
}

// testSkipRe matches SKIP=true with optional comment
var testSkipRe = regexp.MustCompile(`(?m)^\s*SKIP=(true|1|yes)(?:\s*#\s*(.*))?`)

func parseTestSkip(scriptPath string) (bool, string) {
	file, err := os.Open(scriptPath)
	if err != nil {
		return false, ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() && lineCount < 50 {
		line := scanner.Text()
		lineCount++

		if match := testSkipRe.FindStringSubmatch(line); match != nil {
			reason := ""
			if len(match) > 2 {
				reason = strings.TrimSpace(match[2])
			}
			return true, reason
		}
	}

	return false, ""
}
