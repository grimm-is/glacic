// Package harness provides TAP (Test Anything Protocol) parsing for the test orchestrator.
// This is a minimal implementation since we control both the test output and the parser.
package harness

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// TestResult represents the outcome of a single test
type TestResult struct {
	Number      int
	Description string
	Passed      bool
	Skipped     bool
	TODO        bool
	Diagnostics []string
}

// TestSuite represents the results of running a test script
type TestSuite struct {
	Name        string
	PlanCount   int // Expected test count from "1..N"
	Results     []TestResult
	Diagnostics []string // Script-level diagnostics (### prefixed)
	ExitCode    int
}

// Summary returns pass/fail counts
func (s *TestSuite) Summary() (passed, failed, skipped int) {
	for _, r := range s.Results {
		if r.Skipped {
			skipped++
		} else if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	return
}

// Success returns true if all tests passed
func (s *TestSuite) Success() bool {
	_, failed, _ := s.Summary()
	return failed == 0 && s.ExitCode == 0
}

// TAP line regexes
var (
	planRe     = regexp.MustCompile(`^1\.\.(\d+)`)
	resultRe   = regexp.MustCompile(`^(ok|not ok)\s+(\d+)?\s*-?\s*(.*)`)
	diagRe     = regexp.MustCompile(`^#\s*(.*)`)
	skipRe     = regexp.MustCompile(`(?i)#\s*skip`)
	todoRe     = regexp.MustCompile(`(?i)#\s*todo`)
	tapStartRe = regexp.MustCompile(`^TAP_START\s+(.+)`)
	tapEndRe   = regexp.MustCompile(`^TAP_END\s+(\S+)\s+exit=(\d+)`)
)

// Parser reads TAP output and produces TestSuite results
type Parser struct {
	reader io.Reader
}

// NewParser creates a TAP parser
func NewParser(r io.Reader) *Parser {
	return &Parser{reader: r}
}

// Parse reads TAP output and returns a TestSuite
func (p *Parser) Parse() (*TestSuite, error) {
	suite := &TestSuite{}
	scanner := bufio.NewScanner(p.reader)

	var currentDiags []string
	testNum := 0

	for scanner.Scan() {
		line := scanner.Text()

		// TAP_START marker
		if m := tapStartRe.FindStringSubmatch(line); m != nil {
			suite.Name = m[1]
			continue
		}

		// TAP_END marker
		if m := tapEndRe.FindStringSubmatch(line); m != nil {
			if code, err := strconv.Atoi(m[2]); err == nil {
				suite.ExitCode = code
			}
			continue
		}

		// Plan line: 1..N
		if m := planRe.FindStringSubmatch(line); m != nil {
			if count, err := strconv.Atoi(m[1]); err == nil {
				suite.PlanCount = count
			}
			continue
		}

		// Test result: ok N - description
		if m := resultRe.FindStringSubmatch(line); m != nil {
			testNum++
			num := testNum
			if m[2] != "" {
				if n, err := strconv.Atoi(m[2]); err == nil {
					num = n
					testNum = n
				}
			}

			result := TestResult{
				Number:      num,
				Description: strings.TrimSpace(m[3]),
				Passed:      m[1] == "ok",
				Skipped:     skipRe.MatchString(m[3]),
				TODO:        todoRe.MatchString(m[3]),
				Diagnostics: currentDiags,
			}
			suite.Results = append(suite.Results, result)
			currentDiags = nil
			continue
		}

		// Diagnostic: # comment
		if m := diagRe.FindStringSubmatch(line); m != nil {
			currentDiags = append(currentDiags, m[1])
			continue
		}

		// Infrastructure output (### prefixed)
		if strings.HasPrefix(line, "###") {
			suite.Diagnostics = append(suite.Diagnostics, strings.TrimPrefix(line, "### "))
		}
	}

	if err := scanner.Err(); err != nil {
		return suite, fmt.Errorf("scan error: %w", err)
	}

	return suite, nil
}

// FormatSummary returns a human-readable summary
func (s *TestSuite) FormatSummary() string {
	passed, failed, skipped := s.Summary()
	total := len(s.Results)

	var status string
	if s.Success() {
		status = "✅ PASS"
	} else {
		status = "❌ FAIL"
	}

	return fmt.Sprintf("%s %s: %d/%d passed, %d failed, %d skipped (exit=%d)",
		status, s.Name, passed, total, failed, skipped, s.ExitCode)
}
