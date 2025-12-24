package orca

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mdlayher/vsock"
)

type Config struct {
	KernelPath    string
	InitrdPath    string
	RootfsPath    string
	ProjectRoot   string
	Debug         bool
	ConsoleOutput bool
	RunSkipped    bool // Force normally-skipped tests to run
	Verbose       bool // Show detailed status messages
}

func Run(args []string) error {
	fmt.Println("Glacic Orc(hestr)a(tor) Starting...")

	if len(args) == 0 {
		return fmt.Errorf("usage: orca [run|test]")
	}

	switch args[0] {
	case "run":
		return runVM()
	case "test":
		return runTests(args[1:])
	case "status":
		return runStatus()
	case "shell":
		return runShell(args[1:])
	case "exec":
		return runExec(args[1:])
	case "stop":
		return runStop()
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runVM() error {
	// Defaults based on scripts/vm-dev.sh
	cwd, _ := os.Getwd()
	buildDir := filepath.Join(cwd, "build")

	cfg := Config{
		KernelPath:  filepath.Join(buildDir, "vmlinuz"),
		InitrdPath:  filepath.Join(buildDir, "initramfs"),
		RootfsPath:  filepath.Join(buildDir, "rootfs.qcow2"),
		ProjectRoot: cwd,
		Debug:       true,
	}

	vm, err := NewVM(cfg, 1) // ID 1 for single VM mode
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
	cwd, _ := os.Getwd()
	buildDir := filepath.Join(cwd, "build")

	// Parse args
	// Parse args
	warmSize := 4   // Default warm pool size
	maxSize := 0    // 0 means no overflow (maxSize = warmSize)
	runSkipped := false
	verbose := false
	var filter string
	var tests []TestJob

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--run-skipped" {
			runSkipped = true
			continue
		}
		if arg == "-j" {
			if i+1 < len(args) {
				jArg := args[i+1]
				// Check for warm:max format
				if strings.Contains(jArg, ":") {
					parts := strings.Split(jArg, ":")
					if len(parts) == 2 {
						w, err1 := strconv.Atoi(parts[0])
						m, err2 := strconv.Atoi(parts[1])
						if err1 == nil && err2 == nil && w >= 0 && m > 0 && m >= w {
							warmSize = w
							maxSize = m
							i++
							continue
						}
					}
					return fmt.Errorf("invalid -j format, use: -j N or -j warm:max")
				}
				// Simple number format - all transient
				val, err := strconv.Atoi(jArg)
				if err == nil && val > 0 {
					warmSize = 0    // No warm workers
					maxSize = val   // All transient
					i++
					continue
				}
			}
			return fmt.Errorf("invalid -j value")
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

		// It's a test file
		if _, statErr := os.Stat(arg); statErr == nil {
			timeout := parseTestTimeout(arg)
			tests = append(tests, TestJob{ScriptPath: arg, Timeout: timeout})
		} else {
			fmt.Printf("Warning: Test file not found: %s\n", arg)
		}
	}

	// Ensure sane defaults
	if maxSize == 0 {
		maxSize = 4 // Default to 4 transient workers
	}

	// Propagate verbosity to config
	cfg := Config{
		KernelPath:    filepath.Join(buildDir, "vmlinuz"),
		InitrdPath:    filepath.Join(buildDir, "initramfs"),
		RootfsPath:    filepath.Join(buildDir, "rootfs.qcow2"),
		ProjectRoot:   cwd,
		RunSkipped:    runSkipped,
		Verbose:       verbose,
	}

	// Fallback to discovery if no specific tests
	var err error
	if len(tests) == 0 {
		tests, err = DiscoverTests(cwd)
		if err != nil {
			return fmt.Errorf("failed to discover tests: %w", err)
		}
	}

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

	if len(tests) == 0 {
		fmt.Println("No tests found")
		return nil
	}

	if warmSize > 0 {
		fmt.Printf("Found %d tests, running with %d warm + %d overflow workers\n", len(tests), warmSize, maxSize-warmSize)
	} else {
		fmt.Printf("Found %d tests, running with up to %d transient workers\n", len(tests), maxSize)
	}

	// Create test results directory
	resultsDir := filepath.Join(cwd, "build", "test-results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("failed to create results dir: %w", err)
	}

	pod := NewPod(cfg, warmSize, maxSize)

	// Write control file for warm pools so `orca stop` can find us
	if warmSize > 0 {
		os.WriteFile(warmPoolControlFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
		defer os.Remove(warmPoolControlFile)
	}

	// Handle signals to ensure cleanup
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nReceived signal, stopping pod...")
		os.Remove(warmPoolControlFile)
		pod.Stop()
		os.Exit(130)
	}()

	if err := pod.Start(); err != nil {
		return fmt.Errorf("failed to start pod: %w", err)
	}
	defer pod.Stop()

	// Submit all jobs
	go func() {
		for _, test := range tests {
			pod.Submit(test)
		}
		pod.CloseJobs()
	}()

	// Collect results
	runStart := time.Now()
	var passed, failed, skipped int
	var failedTests []string
	var totalTestTime time.Duration
	var slowestTest string
	var slowestTime time.Duration
	testCount := 0
	totalTests := len(tests)

	for result := range pod.Results() {
		testCount++
		totalTestTime += result.Duration
		if result.Duration > slowestTime {
			slowestTime = result.Duration
			slowestTest = result.Job.ScriptPath
		}

		// Write log file (format: <group>--<name>.log)
		logName := testLogName(result.Job.ScriptPath)
		logPath := filepath.Join(resultsDir, logName+".log")
		writeTestLog(logPath, result)

		// Check if this was a timeout
		timedOut := result.Error != nil && strings.Contains(result.Error.Error(), "timeout")
		timeoutMarker := ""
		if timedOut {
			timeoutMarker = " ⏱"
		}

		progress := fmt.Sprintf("(%d/%d)", testCount, totalTests)

		if result.Error != nil {
			fmt.Printf("❌ %-55s %s %s%s\n", result.Job.ScriptPath, formatDuration(result.Duration), progress, timeoutMarker)
			fmt.Printf("   └─ %v\n", result.Error)
			failed++
			failedTests = append(failedTests, result.Job.ScriptPath)
			continue
		}

		if result.Suite != nil {
			p, f, s := result.Suite.Summary()
			passed += p
			failed += f
			skipped += s

			if result.Suite.Success() {
				fmt.Printf("✅ %-55s %s %s\n", result.Job.ScriptPath, formatDuration(result.Duration), progress)
			} else {
				fmt.Printf("❌ %-55s %s %s\n", result.Job.ScriptPath, formatDuration(result.Duration), progress)
				fmt.Printf("   └─ %d assertion(s) failed\n", f)
				failedTests = append(failedTests, result.Job.ScriptPath)

				// Print suite diagnostics (often setup logs)
				for _, d := range result.Suite.Diagnostics {
					fmt.Printf("   # %s\n", d)
				}

				// Print failed test details
				for _, r := range result.Suite.Results {
					if !r.Passed {
						fmt.Printf("   not ok %d - %s\n", r.Number, r.Description)
						for _, d := range r.Diagnostics {
							fmt.Printf("   # %s\n", d)
						}
					}
				}
			}
		}
	}

	wallTime := time.Since(runStart)

	// Summary
	fmt.Println("\n--- Summary ---")
	fmt.Printf("Total: %d passed, %d failed, %d skipped\n", passed, failed, skipped)

	// Timing stats
	fmt.Println("\n--- Timing ---")
	fmt.Printf("Wall time:     %v\n", wallTime.Round(time.Millisecond))
	fmt.Printf("Sum of tests:  %v\n", totalTestTime.Round(time.Millisecond))
	if testCount > 0 {
		avgTime := totalTestTime / time.Duration(testCount)
		fmt.Printf("Average test:  %v\n", avgTime.Round(time.Millisecond))
	}
	if slowestTest != "" {
		fmt.Printf("Slowest test:  %s (%v)\n", slowestTest, slowestTime.Round(time.Millisecond))
	}
	if wallTime > 0 {
		parallelism := float64(totalTestTime) / float64(wallTime)
		fmt.Printf("Parallelism:   %.1fx\n", parallelism)
	}

	if len(failedTests) > 0 {
		fmt.Println("\nFailed tests:")
		for _, t := range failedTests {
			logName := testLogName(t)
			relLogPath := filepath.Join("build", "test-results", logName+".log")
			fmt.Printf("  - %s\n    Log: %s\n", t, relLogPath)
		}
		return fmt.Errorf("%d test(s) failed", len(failedTests))
	}

	return nil
}

func runStatus() error {
	if runtime.GOOS == "linux" {
		return runStatusVsock()
	}
	return runStatusUnix()
}

func runStatusVsock() error {
	fmt.Println("Scanning for VMs on vsock CIDs 3-20...")

	found := 0
	for cid := uint32(3); cid <= 20; cid++ {
		conn, err := vsock.Dial(cid, AgentPort, nil)
		if err != nil {
			continue
		}

		found++
		queryVMStatus(conn, fmt.Sprintf("CID %d", cid))
		conn.Close()
	}

	if found == 0 {
		fmt.Println("No active VMs found.")
	} else {
		fmt.Printf("Found %d active VM(s).\n", found)
	}
	return nil
}

func runStatusUnix() error {
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

	fmt.Printf("Found %d socket file(s), checking...\n\n", len(sockets))

	found := 0
	for _, sock := range sockets {
		id := "unknown"
		if parts := strings.Split(sock, "glacic-vm"); len(parts) > 1 {
			id = strings.TrimSuffix(parts[1], ".sock")
			id = strings.TrimSuffix(id, "-mux") // Remove mux suffix for cleaner display
		}

		conn, err := net.DialTimeout("unix", sock, 1*time.Second)
		if err != nil {
			fmt.Printf("═══════════════════════════════════════════════════════════════\n")
			fmt.Printf("  VM %s: UNRESPONSIVE - %v\n", id, err)
			fmt.Printf("═══════════════════════════════════════════════════════════════\n\n")
			continue
		}

		found++
		queryVMStatus(conn, fmt.Sprintf("VM %s", id))
		conn.Close()
	}

	if found == 0 {
		fmt.Println("No active VMs found (all sockets unresponsive).")
	} else {
		fmt.Printf("Found %d active VM(s).\n", found)
	}
	return nil
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
	psOutput := execCommand(conn, reader, "ps -o pid,user,vsz,rss,stat,comm | head -8")
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
	cwd, _ := os.Getwd()
	buildDir := filepath.Join(cwd, "build")
	cfg := Config{
		KernelPath:    filepath.Join(buildDir, "vmlinuz"),
		InitrdPath:    filepath.Join(buildDir, "initramfs"),
		RootfsPath:    filepath.Join(buildDir, "rootfs.qcow2"),
		ProjectRoot:   cwd,
		ConsoleOutput: false, // Keep stdout clean for shell/exec
	}

	// Create a temp VM with ID 99 (unlikely to collide with 1-9)
	vmID := 99
	vm, err := NewVM(cfg, vmID)
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
	conn, cleanup, err := getVMConnection()
	if err != nil {
		return err
	}
	defer cleanup()

	// 1. Handshake
	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		line := scanner.Text()
		fmt.Printf("Connected: %s\n", line)
	}

	// 2. Start Shell
	fmt.Fprintf(conn, "SHELL\n")

	// 3. Proxy IO
	// We handle signals to avoid killing the shell on Ctrl+C from host (if possible)
	// But standard IO copy propagates simple EOF.
	go io.Copy(os.Stdout, conn)
	io.Copy(conn, os.Stdin)
	
	return nil
}

func runExec(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: exec <command>")
	}
	command := strings.Join(args, " ")

	conn, cleanup, err := getVMConnection()
	if err != nil {
		return err
	}
	defer cleanup()

	// 1. Handshake
	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		// fmt.Printf("Connected: %s\n", scanner.Text())
	}

	// 2. Send Command
	fmt.Fprintf(conn, "EXEC %s\n", command)

	// 3. Read Output
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}

	return scanner.Err()
}

// Control file for signaling warm pool shutdown
const warmPoolControlFile = "/tmp/glacic-orca-pool.pid"

func runStop() error {
	// Check if control file exists
	data, err := os.ReadFile(warmPoolControlFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No warm pool is running.")
			return nil
		}
		return fmt.Errorf("failed to read control file: %w", err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("invalid PID in control file: %w", err)
	}

	// Send SIGTERM to the pool process
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Process might already be gone
		fmt.Printf("Pool process %d may have already exited: %v\n", pid, err)
	} else {
		fmt.Printf("Sent shutdown signal to warm pool (PID %d)\n", pid)
	}

	// Clean up control file
	os.Remove(warmPoolControlFile)
	return nil
}
