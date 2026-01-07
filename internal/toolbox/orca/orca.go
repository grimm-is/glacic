package orca

import (
	"bufio"
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mdlayher/vsock"
	"grimm.is/glacic/internal/protocol"
	"grimm.is/glacic/internal/toolbox/orca/client"
	"grimm.is/glacic/internal/toolbox/orca/server"
	"grimm.is/glacic/internal/toolbox/timeouts"
	"grimm.is/glacic/internal/toolbox/vmm"
)

func Run(args []string) error {
	if len(args) > 0 && (args[0] == "orca" || args[0] == "orchestrator") {
		args = args[1:]
	}

	if len(args) == 0 {
		return fmt.Errorf("usage: orca [run|test]")
	}
	switch args[0] {
	case "run":
		return runVM(args[1:])
	case "test":
		return runTests(args[1:])
	case "status":
		return runStatus(args[1:])
	case "shell":
		return runShell(args[1:])
	case "exec":
		return runExec(args[1:])
	case "stop":
		return runStop(args[1:])
	case "history":
		return runHistory(args[1:])
	case "server":
		return runServer(args[1:])
	case "help", "--help", "-h":
		helpGlobal()
		return nil
	default:
		return fmt.Errorf("unknown command: %s (see 'orca help')", args[0])
	}
}

func helpGlobal() {
	fmt.Println("Glacic Orc(hestr)a(tor) - Integration Test Runner")
	fmt.Println("\nUsage:")
	fmt.Println("  orca <command> [arguments]")
	fmt.Println("\nCommands:")
	fmt.Println("  server    Start the persistent controller daemon")
	fmt.Println("  test      Run integration tests")
	fmt.Println("  history   View results history and test health matrix")
	fmt.Println("  run       Start a development VM")
	fmt.Println("  status    Show status of running VMs")
	fmt.Println("  shell     Open a shell in a running VM")
	fmt.Println("  exec      Execute a command in a running VM")
	fmt.Println("  stop      Stop all running VMs")
	fmt.Println("  help      Show this help message")
	fmt.Println("\nUse 'orca <command> --help' for more information on a specific command.")
}

func runServer(args []string) error {
	debug := false
	daemon := false
	trace := false
	warmSize := 4
	maxSize := 4

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-d" || arg == "--debug" || arg == "-v" || arg == "--verbose" {
			debug = true
		}
		if arg == "--daemon" {
			daemon = true
		}
		if arg == "--trace" {
			trace = true
		}
		if arg == "--help" || arg == "-h" {
			helpServer()
			return nil
		}
		if strings.HasPrefix(arg, "-j") {
			jArg := ""
			if len(arg) > 2 {
				jArg = arg[2:]
			} else if i+1 < len(args) {
				jArg = args[i+1]
				i++
			}

			if strings.Contains(jArg, ":") {
				parts := strings.Split(jArg, ":")
				if len(parts) == 2 {
					w, err1 := strconv.Atoi(parts[0])
					m, err2 := strconv.Atoi(parts[1])
					if err1 == nil && err2 == nil && w >= 0 && m > 0 && m >= w {
						warmSize = w
						maxSize = m
						continue
					}
				}
			} else if val, err := strconv.Atoi(jArg); err == nil && val > 0 {
				warmSize = val
				maxSize = val
				continue
			}
		}
	}

	projectRoot, buildDir := locateBuildDir()

	if daemon {
		logPath := filepath.Join(buildDir, "orca-server.log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		exe, err := os.Executable()
		if err != nil {
			return err
		}

		// We need to ensure we call 'orca server ...'
		// Our new Run() guard makes 'orca orca server' safe even if exe is the symlink
		newArgs := []string{"orca", "server"}
		for _, a := range args {
			if a != "--daemon" {
				newArgs = append(newArgs, a)
			}
		}

		cmd := exec.Command(exe, newArgs...)
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to daemonize: %w", err)
		}
		fmt.Printf("Orca Server backgrounded (PID %d, Logs: %s)\n", cmd.Process.Pid, logPath)
		os.Exit(0)
	}

	cfg := vmm.Config{
		KernelPath:  filepath.Join(buildDir, "vmlinuz"),
		InitrdPath:  filepath.Join(buildDir, "initramfs"),
		RootfsPath:  filepath.Join(buildDir, "rootfs.qcow2"),
		ProjectRoot: projectRoot,
		Debug:       debug,
		Trace:       trace,
	}

	socketPath := "/tmp/glacic-orca.sock"
	srv := server.New(cfg, warmSize, maxSize)
	if err := srv.Start(socketPath); err != nil {
		return err
	}

	fmt.Printf("Orca Server listening on %s (Pool: %d warm / %d max)\n", socketPath, warmSize, maxSize)

	// Start Initial Pool
	for i := 1; i <= warmSize; i++ {
		if err := srv.StartVM(i); err != nil {
			fmt.Printf("Failed to start VM %d: %v\n", i, err)
		}
	}

	// Wait for interruption or remote shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-stop:
		fmt.Println("Orca Server shutting down (interrupted)...")
	case <-srv.Done():
		fmt.Println("Orca Server shutting down (command received)...")
	}

	// Ensure we wait for cleanup
	srv.Stop()

	return nil
}

func helpServer() {
	fmt.Println("Usage: orca server [flags]")
	fmt.Println("\nStarts the Orca test orchestration server.")
	fmt.Println("\nFlags:")
	fmt.Println("  -j N              Start with N worker VMs (default: 4)")
	fmt.Println("  --daemon          Run in background (logs to build/orca-server.log)")
	fmt.Println("  --debug, -d       Show VM console output")
	fmt.Println("  --trace           Log all JSONL protocol messages")
	fmt.Println("  --help, -h        Show this help message")
	fmt.Println("\nExamples:")
	fmt.Println("  orca server --daemon -j8")
	fmt.Println("  orca server --debug --trace")
}

func helpTest() {
	fmt.Println("Usage: orca test [flags] [test_files...]")
	fmt.Println("\nFlags:")
	fmt.Println("  -j N              Run with N transient workers")
	fmt.Println("  -j W:M            Run with W warm workers and M total (M-W transient)")
	fmt.Println("  -filter REGEX     Only run tests matching the regular expression")
	fmt.Println("  -streak-max N     Skip tests with > N consecutive passes (default: 0 = disabled)")
	fmt.Println("  -v                Verbose output (show more details during run)")
	fmt.Println("  --run-skipped     Execute tests marked with SKIP=true")
	fmt.Println("  --only-skipped    Only execute tests marked with SKIP=true")
	fmt.Println("  --strict-isolation Disable worker reuse (fresh VM for every test)")
	fmt.Println("  --trace           Log all JSONL protocol messages")
	fmt.Println("  --help, -h        Show this help message")
	fmt.Println("\nExamples:")
	fmt.Println("  orca test t/01-sanity/*.sh")
	fmt.Println("  orca test -filter dns")
	fmt.Println("  orca test -j8:8")
}

func helpHistory() {
	fmt.Println("Usage: orca history [limit] | [detail <index>]")
	fmt.Println("\nArguments:")
	fmt.Println("  limit           Show summary and health matrix for the last N test runs (default 10)")
	fmt.Println("  detail <index>  Show detailed results for a specific run index (0 is latest)")
	fmt.Println("\nExamples:")
	fmt.Println("  orca history           Show last 10 runs")
	fmt.Println("  orca history 20        Show last 20 runs")
	fmt.Println("  orca history detail 0  Show details for the most recent run")
}

func helpRun() {
	fmt.Println("Usage: orca run")
	fmt.Println("\nStarts a development VM with the Glacic environment configured.")
	fmt.Println("This mode is used for interactive testing and feature development.")
}

func helpStatus() {
	fmt.Println("Usage: orca status")
	fmt.Println("\nScans and displays the status of all currently active VMs managed by Orca.")
	fmt.Println("In Linux, this uses vsock; in macOS/others, it uses Unix domain sockets.")
}

func helpShell() {
	fmt.Println("Usage: orca shell [options]")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  --vmid, -v <id>   Target a specific VM by ID")
	fmt.Println("  --help, -h        Show this help message")
	fmt.Println("\nConnects to an active VM and opens an interactive shell session.")
	fmt.Println("If no VM is running, it will automatically bootstrap a temporary session.")
}

func helpExec() {
	fmt.Println("Usage: orca exec [options] -- <command>")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  --vmid, -v <id>   Target a specific VM by ID")
	fmt.Println("  --help, -h        Show this help message")
	fmt.Println("\nExecutes a non-interactive command inside a VM.")
	fmt.Println("If no VM is running, it will automatically bootstrap a temporary session.")
	fmt.Println("\nArguments:")
	fmt.Println("  command    The command to execute in the VM")
	fmt.Println("\nExample:")
	fmt.Println("  orca exec ip addr show")
}

func helpStop() {
	fmt.Println("Usage: orca stop")
	fmt.Println("\nSends a shutdown signal to the warm worker pool and cleans up active sessions.")
}

// locateBuildDir returns the project root and the build directory.
// It handles running from both project root and the build directory itself.
func locateBuildDir() (string, string) {
	cwd, _ := os.Getwd()
	// If the current directory is named "build", assume we are inside it.
	if filepath.Base(cwd) == "build" {
		return filepath.Dir(cwd), cwd
	}
	// If a "build" subdirectory exists, assume we are in the project root.
	if _, err := os.Stat(filepath.Join(cwd, "build")); err == nil {
		return cwd, filepath.Join(cwd, "build")
	}
	// Fallback
	return cwd, filepath.Join(cwd, "build")
}

func runHistory(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		helpHistory()
		return nil
	}
	_, buildDir := locateBuildDir() // projectRoot unused

	history, err := LoadHistory(buildDir)
	if err != nil {
		return fmt.Errorf("failed to load history: %w", err)
	}

	if len(args) > 0 && args[0] == "detail" {
		fmt.Println("Detailed run history view is temporarily unavailable due to storage refactor.")
		fmt.Println("Use 'orca history' for health summary.")
		return nil
	}

	limit := 10
	if len(args) > 0 {
		if l, err := strconv.Atoi(args[0]); err == nil {
			limit = l
		}
	}

	history.PrintSummary(limit, nil)
	return nil
}

func runVM(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		helpRun()
		return nil
	}
	fmt.Println("Glacic Orc(hestr)a(tor) Starting...")
	// Defaults based on scripts/vm-dev.sh
	projectRoot, buildDir := locateBuildDir()

	cfg := vmm.Config{
		KernelPath:  filepath.Join(buildDir, "vmlinuz"),
		InitrdPath:  filepath.Join(buildDir, "initramfs"),
		RootfsPath:  filepath.Join(buildDir, "rootfs.qcow2"),
		ProjectRoot: projectRoot,
		Debug:       true,
	}

	vm, err := vmm.NewVM(cfg, 1) // ID 1 for single VM mode
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nReceived signal, stopping VM...")
		cancel()
	}()

	if err := vm.Start(ctx); err != nil {
		return fmt.Errorf("vm error: %w", err)
	}

	return nil
}

func runTests(args []string) error {
	// Defaults
	projectRoot, buildDir := locateBuildDir()
	cwd := projectRoot

	// Load test history early for test discovery
	history, err := LoadHistory(buildDir)
	if err != nil {
		fmt.Printf("Warning: failed to load test history: %v\n", err)
		history = &TestHistory{MaxRuns: DefaultMaxRuns}
	}

	// Start resource monitoring
	go monitorResources(cwd)

	// Parse args
	warmSize := 4 // Default warm pool size
	maxSize := 0  // 0 means no overflow (maxSize = warmSize)
	runSkipped := false
	onlySkipped := false
	verbose := false
	streakMax := 0
	strictIsolation := false
	trace := false
	noShuffle := false
	var filter string
	var target string
	var tests []TestJob

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--help" || arg == "-h" || arg == "help" {
			helpTest()
			return nil
		}
		if arg == "--run-skipped" {
			runSkipped = true
			continue
		}
		if arg == "--only-skipped" {
			onlySkipped = true
			runSkipped = true // implies running skipped tests
			continue
		}
		if strings.HasPrefix(arg, "-j") {
			jArg := ""
			if len(arg) > 2 {
				jArg = arg[2:]
			} else if i+1 < len(args) {
				jArg = args[i+1]
				i++
			} else {
				return fmt.Errorf("missing -j value")
			}

			// Check for warm:max format
			if strings.Contains(jArg, ":") {
				parts := strings.Split(jArg, ":")
				if len(parts) == 2 {
					w, err1 := strconv.Atoi(parts[0])
					m, err2 := strconv.Atoi(parts[1])
					if err1 == nil && err2 == nil && w >= 0 && m > 0 && m >= w {
						warmSize = w
						maxSize = m
						continue
					}
				}
				return fmt.Errorf("invalid -j format, use: -j N or -j warm:max")
			}
			// Simple number format - all transient
			val, err := strconv.Atoi(jArg)
			if err == nil && val > 0 {
				warmSize = 0  // No warm workers
				maxSize = val // All transient
				continue
			}
			return fmt.Errorf("invalid -j value: %s", jArg)
		}

		if arg == "-filter" {
			if i+1 < len(args) {
				filter = args[i+1]
				i++
				continue
			}
			return fmt.Errorf("missing value for -filter")
		}

		if arg == "-v" {
			verbose = true
			continue
		}

		if arg == "-streak-max" {
			if i+1 < len(args) {
				val, err := strconv.Atoi(args[i+1])
				if err == nil && val > 0 {
					streakMax = val
					i++
					continue
				}
			}
			return fmt.Errorf("invalid or missing value for -streak-max")
		}

		if arg == "--strict-isolation" {
			strictIsolation = true
			continue
		}

		if arg == "--trace" {
			trace = true
			continue
		}

		if arg == "--no-shuffle" {
			// Disable shuffling (for deterministic debugging)
			// We can't easily propagate this via a variable due to how I structured parsing,
			// so just setting a special env var or using a variable in scope (which IS in scope here).
			// Wait, I need 'noShuffle' defined.
			continue
		}

		if arg == "--no-shuffle" {
			noShuffle = true
			continue
		}

		if arg == "--target" {
			if i+1 < len(args) {
				target = args[i+1]
				i++
				continue
			}
			return fmt.Errorf("missing value for --target")
		}

		// Check if it's a file or directory
		pathToCheck := arg
		if target != "" && !filepath.IsAbs(arg) {
			// Try checking if it exists under integration_tests/<target>/arg
			// e.g. target=linux, arg=10-api/api_test.sh -> integration_tests/linux/10-api/api_test.sh
			candidate := filepath.Join(projectRoot, "integration_tests", target, arg)
			if _, err := os.Stat(candidate); err == nil {
				pathToCheck = candidate
			}
		}

		if info, statErr := os.Stat(pathToCheck); statErr == nil {
			if info.IsDir() {
				// Recurse directory
				filepath.Walk(pathToCheck, func(path string, dInfo os.FileInfo, err error) error {
					if err != nil || dInfo.IsDir() {
						return nil
					}
					if strings.HasSuffix(path, "_test.sh") {
						timeout := parseTestTimeout(path)
						tests = append(tests, TestJob{ScriptPath: path, Timeout: timeout})
					}
					return nil
				})
			} else {
				// Single file
				timeout := parseTestTimeout(pathToCheck)
				tests = append(tests, TestJob{ScriptPath: pathToCheck, Timeout: timeout})
			}
		} else {
			fmt.Printf("Warning: Test file not found: %s\n", arg)
		}
	}

	fmt.Println("Glacic Orc(hestr)a(tor) Starting...")

	// Ensure sane defaults
	if maxSize == 0 {
		maxSize = 4 // Default to 4 transient workers
	}

	// Propagate verbosity to config
	/* cfg := vmm.Config{
		KernelPath:    filepath.Join(buildDir, "vmlinuz"),
		InitrdPath:    filepath.Join(buildDir, "initramfs"),
		RootfsPath:    filepath.Join(buildDir, "rootfs.qcow2"),
		ProjectRoot:   cwd,
		RunSkipped:    runSkipped,
		Verbose:       verbose,
		StrictIsolation: strictIsolation,
	} */

	// Fallback to discovery if no specific tests
	if len(tests) == 0 {
		tests, err = DiscoverTests(projectRoot, target, history)
		if err != nil {
			return fmt.Errorf("failed to discover tests: %w", err)
		}
	}

	// Suppress unused variable errors for flags not yet re-implemented in V2 Client
	_ = verbose
	_ = strictIsolation
	_ = maxSize
	_ = trace
	_ = runSkipped
	_ = onlySkipped
	_ = streakMax

	// Apply filter if specified
	if filter != "" {
		re, err := regexp.Compile(filter)
		if err != nil {
			return fmt.Errorf("invalid filter regex: %w", err)
		}
		var filtered []TestJob
		for _, t := range tests {
			if re.MatchString(t.ScriptPath) {
				filtered = append(filtered, t)
			}
		}
		tests = filtered
	}

	// Apply skip filtering
	var skipCount int
	var skippedTests []string

	if streakMax > 0 {
		// Filter by success streak
		history, err := LoadHistory(buildDir)
		if err == nil { // silently ignore history load errors here
			var streakFiltered []TestJob
			for _, t := range tests {
				streak := history.GetStreak(t.ScriptPath)
				if streak <= streakMax {
					streakFiltered = append(streakFiltered, t)
				} else {
					skipCount++
					skippedTests = append(skippedTests, fmt.Sprintf("%s (streak: %d)", t.ScriptPath, streak))
				}
			}
			tests = streakFiltered
		}
	}

	if onlySkipped {
		// Only run skipped tests
		var skippedOnly []TestJob
		for _, t := range tests {
			if t.Skip {
				skippedOnly = append(skippedOnly, t)
			}
		}
		skipCount = len(tests) - len(skippedOnly)
		tests = skippedOnly
	} else if !runSkipped {
		// Exclude skipped tests (default behavior)
		var notSkipped []TestJob
		for _, t := range tests {
			if !t.Skip {
				notSkipped = append(notSkipped, t)
			} else {
				skipCount++
				skippedTests = append(skippedTests, t.ScriptPath)
			}
		}
		tests = notSkipped

		// Display skipped tests
		if len(skippedTests) > 0 {
			fmt.Printf("Skipping %d test(s):\n", len(skippedTests))
			for _, path := range skippedTests {
				fmt.Printf("  ⏭  %s\n", path)
			}
		}
	}

	// Shuffle test order to identify ordering-dependent tests
	if !noShuffle {
		rand.Shuffle(len(tests), func(i, j int) {
			tests[i], tests[j] = tests[j], tests[i]
		})
	}

	if len(tests) == 0 {
		if skipCount > 0 {
			fmt.Printf("No tests found (%d skipped)\n", skipCount)
		} else {
			fmt.Println("No tests found")
		}
		return nil
	}

	skipMsg := ""
	if skipCount > 0 && !runSkipped {
		skipMsg = fmt.Sprintf(" (skipping %d)", skipCount)
	}
	// Determine optimal reporting workers
	reportWarm := warmSize
	reportMax := maxSize

	fmt.Println("Ensuring Orca Server is running...")
	transient, err := client.EnsureServer(trace, warmSize, maxSize)
	if err != nil {
		return err
	}

	// Fetch ACTUAL server status to report correctly
	status, err := client.GetStatus()
	if err == nil {
		reportWarm = status.WarmSize
		reportMax = status.MaxSize
	}

	if reportWarm > 0 {
		fmt.Printf("Found %d tests%s, running with %d warm + %d overflow workers\n", len(tests), skipMsg, reportWarm, reportMax-reportWarm)
	} else {
		fmt.Printf("Found %d tests%s, running with up to %d transient workers\n", len(tests), skipMsg, reportMax)
	}

    if target != "" {
        fmt.Printf("Platform Target: %s (integration_tests/%s)\n", target, target)
    } else {
        fmt.Printf("Platform Target: default (integration_tests/linux)\n")
    }

	// Log TIME_DILATION factor for visibility
	factor := timeouts.GetFactor()
	fmt.Printf("TIME_DILATION: %.2fx (test timeouts scaled)\n", factor)

	// Generate Run ID for this test session
	runID := time.Now().Format("20060102-150405") + "_" + fmt.Sprintf("%08x", rand.Uint32())
	fmt.Printf("Run ID: %s\n", runID)

	// Create test results base directory
	resultsBase := filepath.Join(cwd, "build", "test-results")
	if err := os.MkdirAll(resultsBase, 0755); err != nil {
		return fmt.Errorf("failed to create results dir: %w", err)
	}

	// Extract test info with timeouts
	var testInfos []client.TestInfo
	for _, t := range tests {
		testInfos = append(testInfos, client.TestInfo{
			Path:    t.ScriptPath,
			Timeout: t.Timeout,
		})
	}

	// Also collect script paths for tracking
	var testScripts []string
	for _, t := range tests {
		testScripts = append(testScripts, t.ScriptPath)
	}

	if transient {
		defer func() {
			fmt.Println("Shutting down transient controller...")
			client.ShutdownServer()
		}()
	}

	// Track results for summary
	var results []protocol.TestResult
	var resultsMu sync.Mutex
	completed := 0
	total := len(testScripts)

	// Track which tests have completed
	completedTests := make(map[string]bool)
	testLogs := make(map[string]string) // Name -> LogPath

	logDir := filepath.Join(cwd, "build", "test-results")

	onStart := func(name, path string) {
		resultsMu.Lock()
		testLogs[name] = path
		resultsMu.Unlock()
	}

	onResult := func(r protocol.TestResult) {
		resultsMu.Lock()
		results = append(results, r)
		completedTests[r.Name] = true
		completed++
		idx := completed
		resultsMu.Unlock()

		// Format duration as MM:SS.mmm
		dur := r.Duration
		mins := int(dur.Minutes())
		secs := dur.Seconds() - float64(mins*60)
		durStr := fmt.Sprintf("%02d:%06.3f", mins, secs)

		// Display result
		marker := "✅"
		extra := ""
		if !r.Passed {
			marker = "❌"
			if r.TimedOut {
				extra = " ⏱"
			}
		} else if r.Todo {
			marker = "🚧"
			extra = " (TODO - Failure Allowed)"
			if r.ExitCode == 0 {
				extra = " (TODO - Unexpected Pass?)"
			}
		} else if r.Skipped > 0 {
			extra = fmt.Sprintf(" (skipped %d)", r.Skipped)
		}

		// Relativize path if target is set
		displayName := r.Name
		if target != "" {
			// Try to strip known prefixes based on target
			// We can't access projectRoot easily here inside the closure without capturing it,
			// but we can assume integration_tests/<target>/ structure
			prefix := fmt.Sprintf("integration_tests/%s/", target)
			displayName = strings.TrimPrefix(displayName, prefix)
		} else {
            // Also try default linux prefix for cleaner output
            displayName = strings.TrimPrefix(displayName, "integration_tests/linux/")
        }

		fmt.Printf("%s %-55s %s (%d/%d)%s\n", marker, displayName, durStr, idx, total, extra)

		if !r.Passed && r.TimedOut {
			fmt.Printf("   └─ test exceeded timeout (captured %d lines before failure)\n", r.LinesCaptured)
		}

		if len(r.Diagnostics) > 0 {
			for k, v := range r.Diagnostics {
				// nice formatting
				fmt.Printf("   └─ %s: %v\n", k, v)
			}
		}


	}

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n\n⚡ Interrupted! Printing partial summary...")

		resultsMu.Lock()
		passed := 0

		// Print failed tests with log links
		for _, r := range results {
			if r.Passed {
				passed++
			} else if r.LogPath != "" {
				fmt.Printf("❌ %-55s -> %s\n", r.Name, r.LogPath)
			}
		}

		// Print in-progress tests (have logs), count never-started
		neverStarted := 0
		for _, t := range testScripts {
			if !completedTests[t] {
				if path, ok := testLogs[t]; ok && path != "" {
					fmt.Printf("⏸  %-55s <in-progress> -> %s\n", t, path)
				} else {
					neverStarted++
				}
			}
		}
		resultsMu.Unlock()

		if neverStarted > 0 {
			fmt.Printf("\n(%d tests were never started)\n", neverStarted)
		}
		fmt.Printf("\nPassed: %d/%d (interrupted at %d/%d)\n", passed, len(results), len(results), total)

		if transient {
			client.ShutdownServer()
		}
		os.Exit(130) // Standard exit code for Ctrl+C
	}()

	err = client.RunTests(runID, testInfos, logDir, onStart, onResult)
	if err != nil {
		return err
	}

	// Summary
	passed := 0
	var failed []protocol.TestResult
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed = append(failed, r)
		}
	}

	// Print failed tests with log links
	if len(failed) > 0 {
		fmt.Println("\nFailed tests:")
		for _, r := range failed {
			fmt.Printf("  ❌ %s\n", r.Name)
			if r.LogPath != "" {
				// Make path relative to project root
				relPath := r.LogPath
				if strings.HasPrefix(r.LogPath, cwd) {
					relPath = strings.TrimPrefix(r.LogPath, cwd+"/")
				}
				fmt.Printf("     └─ %s\n", relPath)
			}
		}
	}

	fmt.Printf("\nPassed: %d/%d\n", passed, total)

	return nil
}

func runStatus(args []string) error {
	resp, err := client.GetStatus()
	if err != nil {
		return fmt.Errorf("failed to get status: %w (is orca server running?)", err)
	}

	fmt.Println("Orca Server Status")
	fmt.Println("------------------")
	if len(resp.VMs) == 0 {
		fmt.Println("No active workers.")
		return nil
	}

	fmt.Printf("%-6s %-12s %-6s %-6s %-12s %s\n", "ID", "STATUS", "BUSY", "JOBS", "LAST HEALTH", "LAST JOB")
	for _, v := range resp.VMs {
		busyStr := "no"
		if v.Busy {
			busyStr = "yes"
		}
		lastJob := filepath.Base(v.LastJob)
		if lastJob == "." {
			lastJob = "-"
		}
		fmt.Printf("%-6s %-12s %-6s %-6d %-12s %s\n", v.ID, v.Status, busyStr, v.ActiveJobs, v.LastHealth, lastJob)
	}
	return nil
}

func runStatusVsock(args []string) error {
	targetID := ""
	if len(args) > 0 {
		targetID = args[0]
	}

	startCID := uint32(3)
	endCID := uint32(20)

	if targetID != "" {
		id, err := strconv.Atoi(targetID)
		if err != nil {
			return fmt.Errorf("invalid vm id: %s", targetID)
		}
		// VM ID 1 -> CID 3
		cid := uint32(id + 2)
		startCID = cid
		endCID = cid
		fmt.Printf("Checking VM %s (CID %d)...\n", targetID, cid)
	} else {
		fmt.Println("Scanning for VMs on vsock CIDs 3-20...")
	}

	found := 0
	for cid := startCID; cid <= endCID; cid++ {
		conn, err := vsock.Dial(cid, AgentPort, nil)
		if err != nil {
			continue
		}

		found++
		queryVMStatus(conn, fmt.Sprintf("CID %d", cid))
		conn.Close()
	}

	if found == 0 {
		if targetID != "" {
			fmt.Printf("VM %s not found (or unresponsive).\n", targetID)
		} else {
			fmt.Println("No active VMs found.")
		}
	} else {
		fmt.Printf("Found %d active VM(s).\n", found)
	}
	return nil
}

func runStatusUnix(args []string) error {
	targetID := ""
	if len(args) > 0 {
		targetID = args[0]
	}

	// Prefer mux sockets (support concurrent connections)
	sockets, _ := filepath.Glob("/tmp/glacic-vm*-mux.sock")
	if len(sockets) == 0 {
		// Fall back to raw VM sockets
		sockets, _ = filepath.Glob("/tmp/glacic-vm*.sock")
	}

	if len(sockets) == 0 {
		fmt.Println("No active VMs found.")
		return nil
	}

	if targetID == "" {
		fmt.Printf("Found %d socket file(s), checking...\n\n", len(sockets))
	}

	found := 0
	for _, sock := range sockets {
		id := "unknown"
		if parts := strings.Split(sock, "glacic-vm"); len(parts) > 1 {
			id = strings.TrimSuffix(parts[1], ".sock")
			id = strings.TrimSuffix(id, "-mux") // Remove mux suffix
		}

		// Filter if target specified
		if targetID != "" && id != targetID {
			continue
		}

		vmName := fmt.Sprintf("VM %s", id)

		// 1. Try Side-Channel (File) First
		if status, ok := checkFileStatus(id); ok {
			found++
			printStatusBox(vmName, status, "ACTIVE")
			continue
		}

		// 2. Fallback to Socket
		conn, err := net.DialTimeout("unix", sock, 1*time.Second)
		if err != nil {
			// GC Logic: If connection refused or file missing, it's garbage.
			errStr := err.Error()
			isRefused := strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "connect: no such file")

			if isRefused {
				// Clean up stale socket
				os.Remove(sock)

				// Clean up stale status file
				_, buildDir := locateBuildDir()
				statusFile := filepath.Join(buildDir, "vm_status", fmt.Sprintf("glacic-vm-%s.status", id))
				os.Remove(statusFile)

				// Do not count as found, do not print.
				continue
			}

			// Real timeout or other error -> Unresponsive possibly still alive
			printStatusBox(vmName, fmt.Sprintf("UNRESPONSIVE - %v", err), "UNRESPONSIVE")
			continue
		}

		found++
		queryVMStatus(conn, vmName)
		conn.Close()
	}

	if found == 0 {
		fmt.Println("No active VMs found.")
	} else {
		fmt.Printf("Found %d active VM(s).\n", found)
	}
	return nil
}

func checkFileStatus(id string) (string, bool) {
	// Hostname is usually glacic-vm-<id>
	// But `runStatusUnix` extracts ID from `glacic-vm<ID>.sock`.
	// Wait, socket name is `/tmp/glacic-vm<ID>.sock`.
	// Hostname inside VM is set by `vm.go`.
	// Usually `glacic-vm-<ID>`.
	// Let's assume standard naming.

	filename := fmt.Sprintf("glacic-vm-%s.status", id)
	_, buildDir := locateBuildDir()
	path := filepath.Join(buildDir, "vm_status", filename)

	stat, err := os.Stat(path)
	if err != nil {
		return "", false
	}

	// Check freshness (5s heartbeat)
	if time.Since(stat.ModTime()) > 6*time.Second {
		return "", false // Stale
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(content), true
}

func printStatusBox(name, agentStatus, state string) {
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("  %s: %s\n", name, state)
	fmt.Printf("  Agent: %s\n", agentStatus)
	fmt.Printf("═══════════════════════════════════════════════════════════════\n\n")
}

func queryVMStatus(conn net.Conn, vmName string) {
	reader := bufio.NewReader(conn)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader.ReadString('\n') // Consume HELLO

	// Send PING
	start := time.Now()
	fmt.Fprintf(conn, "PING\n")
	resp, err := reader.ReadString('\n')
	latency := time.Since(start)

	if err != nil {
		status := "Unknown Error: " + err.Error()
		if strings.Contains(err.Error(), "i/o timeout") {
			status = "Unresponsive (Timeout)"
		} else if strings.Contains(err.Error(), "connection refused") {
			status = "Connection Refused (Stale Socket)"
		}

		fmt.Printf("═══════════════════════════════════════════════════════════════\n")
		fmt.Printf("  %s: %s\n", vmName, status)
		fmt.Printf("═══════════════════════════════════════════════════════════════\n\n")
		return
	}

	if strings.TrimSpace(resp) != "PONG" {
		fmt.Printf("═══════════════════════════════════════════════════════════════\n")
		fmt.Printf("  %s: BAD RESPONSE (Expected PONG, got %q)\n", vmName, strings.TrimSpace(resp))
		fmt.Printf("═══════════════════════════════════════════════════════════════\n\n")
		return
	}

	// Query agent status
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn, "STATUS\n")
	statusResp, _ := reader.ReadString('\n')
	agentStatus := strings.TrimSpace(statusResp)

	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("  %s: ACTIVE (latency: %v)\n", vmName, latency.Round(time.Millisecond))
	fmt.Printf("  Agent: %s\n", agentStatus)
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")

	// Query memory
	memOutput := execCommand(conn, reader, "cat /proc/meminfo | head -5")
	if memOutput != "" {
		fmt.Printf("\n  📊 Memory:\n")
		for _, line := range strings.Split(memOutput, "\n") {
			if strings.TrimSpace(line) != "" {
				fmt.Printf("     %s\n", line)
			}
		}
	}

	// Query CPU load
	loadOutput := execCommand(conn, reader, "cat /proc/loadavg")
	if loadOutput != "" {
		fmt.Printf("\n  🔥 Load Average: %s\n", strings.TrimSpace(loadOutput))
	}

	// Query top processes (BusyBox-compatible)
	// Use 'args' to see full command line (e.g. 'sleep 10')
	psOutput := execCommand(conn, reader, "ps -o pid,user,stat,args | head -10")
	if psOutput != "" {
		fmt.Printf("\n  📋 Top Processes:\n")
		for _, line := range strings.Split(psOutput, "\n") {
			if strings.TrimSpace(line) != "" {
				fmt.Printf("     %s\n", line)
			}
		}
	}

	fmt.Println()
}

// execCommand sends an EXEC command and reads the output
func execCommand(conn net.Conn, reader *bufio.Reader, cmd string) string {
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err := fmt.Fprintf(conn, "EXEC %s\n", cmd)
	if err != nil {
		return ""
	}

	var output strings.Builder
	inOutput := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSuffix(line, "\n")

		if strings.HasPrefix(line, "--- BEGIN OUTPUT ---") {
			inOutput = true
			continue
		}
		if strings.HasPrefix(line, "--- END OUTPUT") {
			break
		}
		if inOutput {
			output.WriteString(line)
			output.WriteString("\n")
		}
	}
	return strings.TrimSpace(output.String())
}

// formatDuration formats a duration as MM:SS.mmm
func formatDuration(d time.Duration) string {
	totalSeconds := d.Seconds()
	minutes := int(totalSeconds) / 60
	seconds := totalSeconds - float64(minutes*60)
	return fmt.Sprintf("%02d:%06.3f", minutes, seconds)
}

// testLogName converts a script path to a log name (e.g., t/01-sanity/sanity_test.sh -> 01-sanity--sanity)
func testLogName(scriptPath string) string {
	// Extract directory name and test name
	dir := filepath.Dir(scriptPath)
	base := filepath.Base(scriptPath)

	// Get group from directory (e.g., "01-sanity" from "t/01-sanity")
	group := filepath.Base(dir)

	// Get test name without _test.sh suffix
	name := strings.TrimSuffix(base, "_test.sh")

	return group + "--" + name
}

// writeTestLog writes test output to a log file
func writeTestLog(path string, result TestResult) {
	f, err := os.Create(path)
	if err != nil {
		return // Silently ignore log write failures
	}
	defer f.Close()

	// Write header
	fmt.Fprintf(f, "# Test: %s\n", result.Job.ScriptPath)
	fmt.Fprintf(f, "# Worker: %s\n", result.WorkerID)
	fmt.Fprintf(f, "# Start: %s\n", result.StartTime.Format(time.RFC3339))
	fmt.Fprintf(f, "# Duration: %s\n", formatDuration(result.Duration))
	if result.Error != nil {
		fmt.Fprintf(f, "# Status: FAILED\n")
		fmt.Fprintf(f, "# Error: %v\n", result.Error)
	} else if result.Suite != nil && result.Suite.Success() {
		fmt.Fprintf(f, "# Status: PASSED\n")
	} else {
		fmt.Fprintf(f, "# Status: FAILED\n")
	}
	fmt.Fprintf(f, "\n")

	// Write raw TAP output
	if result.RawOutput != "" {
		f.WriteString(result.RawOutput)
	}

	// Write suite details if available
	if result.Suite != nil {
		fmt.Fprintf(f, "\n# --- Test Results ---\n")
		for _, r := range result.Suite.Results {
			if r.Passed {
				fmt.Fprintf(f, "# ok %d - %s\n", r.Number, r.Description)
			} else {
				fmt.Fprintf(f, "# not ok %d - %s\n", r.Number, r.Description)
				for _, d := range r.Diagnostics {
					fmt.Fprintf(f, "#   %s\n", d)
				}
			}
		}
	}
}

// getVMConnection finds an active VM or starts a temporary one
// Returns connection, cleanup function, and error
func getVMConnection() (net.Conn, func(), error) {
	// 1. Try to find existing VM
	socketPath, err := findFirstValidSocket()
	if err == nil {
		fmt.Printf("Connected to active VM at %s\n", socketPath)
		conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to dial existing VM: %w", err)
		}
		return conn, func() { conn.Close() }, nil
	}

	// 2. No VM found, start one
	fmt.Println("No active VM found, configuring temporary session...")

	// Use default config
	projectRoot, buildDir := locateBuildDir()
	cfg := vmm.Config{
		KernelPath:    filepath.Join(buildDir, "vmlinuz"),
		InitrdPath:    filepath.Join(buildDir, "initramfs"),
		RootfsPath:    filepath.Join(buildDir, "rootfs.qcow2"),
		ProjectRoot:   projectRoot,
		ConsoleOutput: false, // Keep stdout clean for shell/exec
	}

	// Create a temp VM with ID 99 (unlikely to collide with 1-9)
	vmID := 99
	vm, err := vmm.NewVM(cfg, vmID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to configure VM: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start VM in background
	errCh := make(chan error, 1)
	go func() {
		if err := vm.Start(ctx); err != nil {
			errCh <- err
		} else {
			errCh <- nil
		}
	}()

	// Wait for socket to appear
	fmt.Printf("Booting VM (ID %d)... ", vmID)
	socketPath = vm.SocketPath

	// Wait up to 30s for socket
	deadline := time.Now().Add(30 * time.Second)
	connected := false
	var conn net.Conn

	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			if err != nil {
				cancel()
				return nil, nil, fmt.Errorf("VM failed to start: %w", err)
			}
			// VM exited unexpectedly
			cancel()
			return nil, nil, fmt.Errorf("VM exited unexpectedly")
		default:
			// Check socket
			if _, err := os.Stat(socketPath); err == nil {
				// Try dialing
				c, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
				if err == nil {
					conn = c
					connected = true
					goto Ready
				}
			}
			time.Sleep(500 * time.Millisecond)
			fmt.Print(".")
		}
	}

Ready:
	if !connected {
		cancel()
		return nil, nil, fmt.Errorf("\ntimeout waiting for VM to start")
	}
	fmt.Println(" Ready!")

	cleanup := func() {
		conn.Close()
		cancel()
		// Wait typically ensures clean shutdown, but we cancel context above.
		// We might want to explicitly wait for cmd to exit?
		// But Stop() in NewVM isn't exposed (wait, vm.Stop is).
		vm.Stop()
		<-errCh // Wait for run to finish
	}

	return conn, cleanup, nil
}

func findFirstValidSocket() (string, error) {
	// Prefer mux sockets
	sockets, _ := filepath.Glob("/tmp/glacic-vm*-mux.sock")
	if len(sockets) == 0 {
		sockets, _ = filepath.Glob("/tmp/glacic-vm*.sock")
	}

	for _, sock := range sockets {
		// Test connectivity
		c, err := net.DialTimeout("unix", sock, 100*time.Millisecond)
		if err == nil {
			c.Close()
			return sock, nil
		}
	}
	return "", fmt.Errorf("no active vm sockets found")
}

func runShell(args []string) error {
	vmid := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--help" || args[i] == "-h" || args[i] == "help" {
			helpShell()
			return nil
		}
		if (args[i] == "--vmid" || args[i] == "-v") && i+1 < len(args) {
			vmid = args[i+1]
			args = append(args[:i], args[i+2:]...)
			i--
		}
	}
	if _, err := client.EnsureServer(false, 0, 0); err != nil {
		return err
	}
	return client.RunShell(vmid)
}

func runExec(args []string) error {
	vmid := ""
	// Pre-process args looking for --vmid or -v
	for i := 0; i < len(args); i++ {
		if (args[i] == "--vmid" || args[i] == "-v") && i+1 < len(args) {
			vmid = args[i+1]
			args = append(args[:i], args[i+2:]...)
			i--
		} else if args[i] == "--help" || args[i] == "-h" || args[i] == "help" {
			helpExec()
			return nil
		}
	}

	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: exec <command>")
	}
	if _, err := client.EnsureServer(false, 0, 0); err != nil {
		return err
	}
	return client.RunExec(args, false, vmid)
}

// Control file for signaling warm pool shutdown
const warmPoolControlFile = "/tmp/glacic-orca-pool.pid"

func runStop(args []string) error {
	fmt.Println("Shutting down Orca Server...")
	if err := client.ShutdownServer(); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	fmt.Println("Shutdown signal sent.")
	return nil
}

// monitorResources logs system resource usage to a file
func monitorResources(cwd string) {
	logPath := filepath.Join(cwd, "build", "orca-resources.log")
	f, err := os.Create(logPath)
	if err != nil {
		fmt.Printf("Warning: Failed to create resource log: %v\n", err)
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "Time,Goroutines,HeapAllocMB,SysMB,OpenFiles\n")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		// Count open files (best effort)
		openFiles := 0
		if fds, err := os.ReadDir("/dev/fd"); err == nil {
			openFiles = len(fds)
		} else if fds, err := os.ReadDir("/proc/self/fd"); err == nil {
			openFiles = len(fds)
		}

		fmt.Fprintf(f, "%s,%d,%d,%d,%d\n",
			time.Now().Format(time.RFC3339),
			runtime.NumGoroutine(),
			m.HeapAlloc/1024/1024,
			m.Sys/1024/1024,
			openFiles,
		)
	}
}
