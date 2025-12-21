package orca

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mdlayher/vsock"
	"grimm.is/glacic/internal/toolbox/harness"
)

// AgentPort is the vsock port the agent listens on (Linux only)
const AgentPort = 5000

// TestJob represents a test to be run
type TestJob struct {
	ScriptPath string        // Relative to project root (e.g., "t/01-sanity/sanity_test.sh")
	Timeout    time.Duration // Per-test timeout (parsed from TEST_TIMEOUT comment)
}

// Default timeout for tests that don't specify one
const DefaultTestTimeout = 30 * time.Second

// TestResult represents the outcome of running a test
type TestResult struct {
	Job       TestJob
	Suite     *harness.TestSuite
	Duration  time.Duration
	Error     error
	RawOutput string // Raw TAP output for logging
}

// Pod manages multiple VMs for parallel test execution
type Pod struct {
	config       Config
	warmSize     int            // Number of persistent warm workers
	maxSize      int            // Maximum total workers (warm + overflow)
	idleTimeout  time.Duration  // Auto-shutdown warm pool after idle period (0 = no timeout)
	lastActivity time.Time      // Time of last job completion
	workers      []*worker      // Active workers
	workersMu    sync.Mutex     // Protects workers slice and lastActivity
	nextWorkerID int            // Next worker ID to assign
	jobs         chan TestJob
	results      chan TestResult
	ctx          context.Context
	cancel       context.CancelFunc
	vmWg         sync.WaitGroup // Tracks VM goroutines
	workerWg     sync.WaitGroup // Tracks worker goroutines
	stopOnce     sync.Once
	resultsOnce  sync.Once
}

// worker represents a single VM in the pod
type worker struct {
	id          int
	vm          *VM
	conn        net.Conn
	mux         *Multiplexer // For macOS socket multiplexing
	clientPath  string       // Path clients connect to (multiplexer or direct)
	config      Config
	isOverflow  bool      // True if this is a transient overflow worker
	busy        bool      // True if currently running a test
	lastActivity time.Time // Time when last job finished
	stopping    int32     // Atomic flag (1 if intentionally stopping)
}

// NewPod creates a new VM pod with warm and overflow capacity
// NewPod creates a new VM pod with warm and overflow capacity
func NewPod(cfg Config, warmSize, maxSize int) *Pod {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Default idle timeout: 30 minutes for warm pools, 0 (disabled) for transient
	idleTimeout := time.Duration(0)
	if warmSize > 0 {
		idleTimeout = 30 * time.Minute
	}
	
	return &Pod{
		config:       cfg,
		warmSize:     warmSize,
		maxSize:      maxSize,
		idleTimeout:  idleTimeout,
		lastActivity: time.Now(),
		nextWorkerID: 1,
		jobs:         make(chan TestJob, 100),
		results:      make(chan TestResult, 100),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// SetIdleTimeout configures the warm pool idle timeout (0 to disable)
func (p *Pod) SetIdleTimeout(d time.Duration) {
	p.workersMu.Lock()
	p.idleTimeout = d
	p.workersMu.Unlock()
}

// Start launches VMs and begins processing jobs
func (p *Pod) Start() error {
	if p.config.Verbose {
		if p.warmSize > 0 {
			fmt.Printf("Starting VM pod with %d warm workers (max %d)...\n", p.warmSize, p.maxSize)
		} else {
			fmt.Printf("Starting VM pod with %d workers...\n", p.maxSize)
		}
	}

	// Determine how many workers to start now
	initialWorkers := p.maxSize
	if p.warmSize > 0 {
		initialWorkers = p.warmSize // Start just warm workers, overflow on demand
	}

	// Start initial workers in parallel
	for i := 0; i < initialWorkers; i++ {
		p.workerWg.Add(1)
		go func() {
			defer p.workerWg.Done()
			isOverflow := p.warmSize == 0 // All transient if no warm pool
			_, err := p.spawnWorker(isOverflow)
			if err != nil {
				fmt.Printf("Failed to start worker: %v\n", err)
			}
		}()
	}

	// Always start dispatcher to manage job assignment
	p.workerWg.Add(1)
	go func() {
		defer p.workerWg.Done()
		p.dispatchJobs()
	}()

	// If we allow overflow, start reaper
	if p.maxSize > p.warmSize {
		// Start background reaper for overflow workers
		go func() {
			p.reapIdleWorkers()
		}()
	}

	// Close results channel when all workers are done
	go func() {
		p.workerWg.Wait()
		p.closeResults()
	}()

	return nil
}

func (p *Pod) closeResults() {
	p.resultsOnce.Do(func() {
		close(p.results)
	})
}

// Stop gracefully shuts down all VMs
func (p *Pod) Stop() {
	p.stopOnce.Do(func() {
		// Cancel context - signals workers to stop
		p.cancel()

		// Kill all VMs - causes VM goroutines to exit
		for _, w := range p.workers {
			w.stop()
		}

		// Wait for VM goroutines
		p.vmWg.Wait()
	})
}

// Submit adds a test job to the queue
func (p *Pod) Submit(job TestJob) {
	p.jobs <- job
}

// Results returns the results channel
func (p *Pod) Results() <-chan TestResult {
	return p.results
}

// CloseJobs signals no more jobs will be submitted
func (p *Pod) CloseJobs() {
	close(p.jobs)
}

// spawnWorker creates and starts a new worker VM
func (p *Pod) spawnWorker(isOverflow bool) (*worker, error) {
	p.workersMu.Lock()
	id := p.nextWorkerID
	p.nextWorkerID++
	p.workersMu.Unlock()

	vm, err := NewVM(p.config, id)
	if err != nil {
		return nil, err
	}

	w := &worker{
		id:         id,
		vm:         vm,
		config:       p.config,
		isOverflow:   isOverflow,
		lastActivity: time.Now(),
	}

	// Start VM in background
	p.vmWg.Add(1)
	go func() {
		defer p.vmWg.Done()
		err := vm.Start(p.ctx)
		if err != nil && p.ctx.Err() == nil {
			isStopping := atomic.LoadInt32(&w.stopping) == 1
			isKilled := strings.Contains(err.Error(), "signal: killed")

			if !isStopping || !isKilled {
				fmt.Printf("[Worker %d] VM exited with error: %v\n", id, err)
			}
		}
	}()

	// Wait for agent to be reachable
	if err := w.waitForAgent(30 * time.Second); err != nil {
		vm.Stop()
		return nil, fmt.Errorf("agent not ready: %w", err)
	}

	// Set up multiplexer on macOS
	if runtime.GOOS != "linux" && vm.SocketPath != "" && w.conn != nil {
		clientPath := fmt.Sprintf("/tmp/glacic-vm%d-mux.sock", id)
		mux, err := NewMultiplexerWithConn(w.conn, clientPath)
		if err != nil {
			w.conn.Close()
			vm.Stop()
			return nil, fmt.Errorf("failed to start multiplexer: %w", err)
		}
		mux.Start()
		w.mux = mux
		w.clientPath = clientPath
		w.conn = nil

		conn, err := net.Dial("unix", clientPath)
		if err != nil {
			mux.Close()
			vm.Stop()
			return nil, fmt.Errorf("failed to connect to multiplexer: %w", err)
		}
		bufio.NewReader(conn).ReadString('\n')
		w.conn = conn
	}

	p.workersMu.Lock()
	p.workers = append(p.workers, w)
	p.workersMu.Unlock()

	workerType := "warm"
	if isOverflow {
		workerType = "overflow"
	}
	if p.config.Verbose {
		if runtime.GOOS == "linux" {
			fmt.Printf("[Worker %d] %s ready (CID %d)\n", id, workerType, vm.CID)
		} else {
			fmt.Printf("[Worker %d] %s ready (%s)\n", id, workerType, w.clientPath)
		}
	}

	return w, nil
}



// dispatchJobs handles job dispatch with elastic scaling
func (p *Pod) dispatchJobs() {
	for job := range p.jobs {
		// Find an idle worker or spawn overflow
		w := p.findIdleWorker()
		if w == nil {
			// Check if we can spawn more
			p.workersMu.Lock()
			canSpawn := len(p.workers) < p.maxSize
			p.workersMu.Unlock()

			if canSpawn {
				newWorker, err := p.spawnWorker(true)
				if err != nil {
					fmt.Printf("Failed to spawn overflow worker: %v\n", err)
					// Try to find worker again or wait
					w = p.waitForIdleWorker()
				} else {
					w = newWorker
				}
			} else {
				// At capacity, wait for a worker
				w = p.waitForIdleWorker()
			}
		}

		if w != nil {
			w.busy = true
			// Run test in goroutine for parallel execution
			p.workerWg.Add(1)
			go func(worker *worker, testJob TestJob) {
				defer p.workerWg.Done()
				result := worker.executeTest(testJob)
				worker.busy = false
				
				// Update activity timestamp
				p.workersMu.Lock()
				worker.lastActivity = time.Now()
				p.workersMu.Unlock()
				
				p.results <- result
			}(w, job)
		}
	}
}

// findIdleWorker returns an idle worker or nil
func (p *Pod) findIdleWorker() *worker {
	p.workersMu.Lock()
	defer p.workersMu.Unlock()
	for _, w := range p.workers {
		if !w.busy {
			return w
		}
	}
	return nil
}

// waitForIdleWorker blocks until a worker becomes available
func (p *Pod) waitForIdleWorker() *worker {
	for {
		select {
		case <-p.ctx.Done():
			return nil
		default:
			if w := p.findIdleWorker(); w != nil {
				return w
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// removeWorker removes a worker from the pool
func (p *Pod) removeWorker(w *worker) {
	p.workersMu.Lock()
	defer p.workersMu.Unlock()
	for i, worker := range p.workers {
		if worker.id == w.id {
			p.workers = append(p.workers[:i], p.workers[i+1:]...)
			return
		}
	}
}

// reapIdleWorkers monitors overflow workers and shuts them down if idle
func (p *Pod) reapIdleWorkers() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Overflow workers are kept alive for a short grace period
	// to allow for reuse during bursts of tests
	idleGracePeriod := 5 * time.Second

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.workersMu.Lock()
			now := time.Now()
			var toRemove []*worker

			for _, w := range p.workers {
				if w.isOverflow && !w.busy && now.Sub(w.lastActivity) > idleGracePeriod {
					toRemove = append(toRemove, w)
				}
			}
			p.workersMu.Unlock()

			for _, w := range toRemove {
				if p.config.Verbose {
					fmt.Printf("[Worker %d] Overflow idle for %v, shutting down\n", w.id, idleGracePeriod)
				}
				p.removeWorker(w)
				w.stop()
			}
		}
	}
}

func (p *Pod) newWorker(id int) (*worker, error) {
	vm, err := NewVM(p.config, id)
	if err != nil {
		return nil, err
	}

	w := &worker{
		id:     id,
		vm:     vm,
		config: p.config,
	}

	// Start VM in background
	p.vmWg.Add(1)
	go func() {
		defer p.vmWg.Done()
		err := vm.Start(p.ctx)
		if err != nil && p.ctx.Err() == nil {
			isStopping := atomic.LoadInt32(&w.stopping) == 1
			isKilled := strings.Contains(err.Error(), "signal: killed")

			if !isStopping || !isKilled {
				fmt.Printf("[Worker %d] VM exited with error: %v\n", id, err)
			}
		}
	}()

	// Wait for agent to be reachable
	if err := w.waitForAgent(30 * time.Second); err != nil {
		vm.Stop()
		return nil, fmt.Errorf("agent not ready: %w", err)
	}

	// On macOS, start a multiplexer to allow concurrent connections
	// We hand off the probe connection (w.conn) to the multiplexer
	if runtime.GOOS != "linux" && vm.SocketPath != "" && w.conn != nil {
		clientPath := fmt.Sprintf("/tmp/glacic-vm%d-mux.sock", id)
		
		// Hand off the existing connection to the multiplexer
		// (HELLO was already consumed during waitForAgent)
		mux, err := NewMultiplexerWithConn(w.conn, clientPath)
		if err != nil {
			w.conn.Close()
			vm.Stop()
			return nil, fmt.Errorf("failed to start multiplexer: %w", err)
		}
		mux.Start()
		w.mux = mux
		w.clientPath = clientPath
		w.conn = nil // Mux now owns this connection

		// Connect to the multiplexer for our worker connection
		conn, err := net.Dial("unix", clientPath)
		if err != nil {
			mux.Close()
			vm.Stop()
			return nil, fmt.Errorf("failed to connect to multiplexer: %w", err)
		}
		// Read and discard HELLO from multiplexer
		bufio.NewReader(conn).ReadString('\n')
		w.conn = conn

		if p.config.Verbose {
			fmt.Printf("[Worker %d] VM ready (%s via mux)\n", id, clientPath)
		}
	} else if runtime.GOOS == "linux" {
		if p.config.Verbose {
			fmt.Printf("[Worker %d] VM ready (CID %d)\n", id, vm.CID)
		}
	} else {
		w.clientPath = vm.SocketPath
		if p.config.Verbose {
			fmt.Printf("[Worker %d] VM ready (%s)\n", id, vm.SocketPath)
		}
	}
	return w, nil
}

func (w *worker) waitForAgent(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		var conn net.Conn
		var err error

		if runtime.GOOS == "linux" && w.vm.CID != 0 {
			conn, err = vsock.Dial(w.vm.CID, AgentPort, nil)
		} else {
			conn, err = net.Dial("unix", w.vm.SocketPath)
		}

		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		scanner := bufio.NewScanner(conn)
		if scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "HELLO") {
				// Keep the connection - on Linux for direct use, on macOS to hand to multiplexer
				w.conn = conn
				return nil
			}
		}
		conn.Close()
		time.Sleep(500 * time.Millisecond)
	}

	if runtime.GOOS == "linux" {
		return fmt.Errorf("timeout waiting for agent on CID %d", w.vm.CID)
	}
	return fmt.Errorf("timeout waiting for agent on %s", w.vm.SocketPath)
}



func (w *worker) executeTest(job TestJob) TestResult {
	start := time.Now()
	result := TestResult{Job: job}

	if w.conn == nil {
		result.Error = fmt.Errorf("no connection to agent")
		result.Duration = time.Since(start)
		return result
	}

	cmd := fmt.Sprintf("TEST %s\n", job.ScriptPath)
	_, err := w.conn.Write([]byte(cmd))
	if err != nil {
		result.Error = fmt.Errorf("failed to send command: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	timeout := job.Timeout
	if timeout == 0 {
		timeout = DefaultTestTimeout
	}
	w.conn.SetReadDeadline(time.Now().Add(timeout + 10*time.Second))

	var output strings.Builder
	scanner := bufio.NewScanner(w.conn)
	completedNormally := false
	for scanner.Scan() {
		line := scanner.Text()
		output.WriteString(line)
		output.WriteString("\n")

		if strings.HasPrefix(line, "TAP_END") {
			completedNormally = true
			break
		}
	}

	readErr := scanner.Err()
	elapsed := time.Since(start)
	result.Duration = elapsed

	// Try to parse whatever output we got, even if incomplete
	if output.Len() > 0 {
		parser := harness.NewParser(strings.NewReader(output.String()))
		suite, parseErr := parser.Parse()
		if parseErr == nil {
			result.Suite = suite
		}
	}

	// If we didn't complete normally, diagnose why
	if !completedNormally {
		if readErr != nil {
			// Classify the error
			errStr := readErr.Error()
			if strings.Contains(errStr, "i/o timeout") {
				if elapsed > timeout {
					result.Error = fmt.Errorf("test exceeded timeout (%v > %v)", elapsed.Round(time.Second), timeout)
				} else {
					result.Error = fmt.Errorf("connection timeout (possible agent/mux crash)")
				}
			} else if strings.Contains(errStr, "connection reset") || strings.Contains(errStr, "broken pipe") {
				result.Error = fmt.Errorf("connection lost (agent may have crashed)")
			} else {
				result.Error = fmt.Errorf("read error: %w", readErr)
			}
			
			// Append partial output info if we have any
			if output.Len() > 0 {
				lines := strings.Count(output.String(), "\n")
				result.Error = fmt.Errorf("%w (captured %d lines before failure)", result.Error, lines)
			}
		} else if output.Len() == 0 {
			result.Error = fmt.Errorf("no output received from test")
		} else {
			result.Error = fmt.Errorf("test output incomplete (no TAP_END marker)")
		}
		return result
	}

	result.RawOutput = output.String()
	result.Duration = time.Since(start)
	return result
}

func (w *worker) stop() {
	atomic.StoreInt32(&w.stopping, 1)
	if w.conn != nil {
		w.conn.Write([]byte("EXIT\n"))
		w.conn.Close()
	}
	if w.mux != nil {
		w.mux.Close()
	}
	if w.vm != nil {
		w.vm.Stop()
	}
	if w.vm != nil && w.vm.SocketPath != "" {
		_ = os.Remove(w.vm.SocketPath)
	}
	if w.clientPath != "" {
		_ = os.Remove(w.clientPath)
	}
}

// Regex to match TEST_TIMEOUT comment in scripts
var testTimeoutRe = regexp.MustCompile(`(?m)^#?\s*TEST_TIMEOUT[=:]\s*(\d+)`)

// DiscoverTests finds all test scripts in t/ and parses their timeouts
func DiscoverTests(projectRoot string) ([]TestJob, error) {
	var jobs []TestJob

	testDir := filepath.Join(projectRoot, "t")
	err := filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, "_test.sh") {
			relPath, _ := filepath.Rel(projectRoot, path)
			timeout := parseTestTimeout(path)

			jobs = append(jobs, TestJob{
				ScriptPath: relPath,
				Timeout:    timeout,
			})
		}

		return nil
	})

	return jobs, err
}

func parseTestTimeout(scriptPath string) time.Duration {
	file, err := os.Open(scriptPath)
	if err != nil {
		return DefaultTestTimeout
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() && lineCount < 50 {
		line := scanner.Text()
		lineCount++

		if match := testTimeoutRe.FindStringSubmatch(line); match != nil {
			if seconds, err := strconv.Atoi(match[1]); err == nil {
				return time.Duration(seconds) * time.Second
			}
		}
	}

	return DefaultTestTimeout
}
