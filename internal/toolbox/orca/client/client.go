package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"grimm.is/glacic/internal/protocol"
)

// jobState tracks per-job state for log accumulation
type jobState struct {
	Name      string
	StartTime time.Time
	LogFile   *os.File
	LogPath   string
	Lines     int
	Started   bool // True once we've received first output
	Timeout   time.Duration
	Buffer    []byte // Buffer for partial lines
	Skipped     int                    // Count of skipped tests
	Todo        bool                   // Test is marked as TODO (allow failure)
	InYaml      bool                   // Are we inside a YAML block?
	YamlBuffer  []string               // Buffer for YAML lines
	Diagnostics map[string]interface{} // Parsed diagnostics
}

// TestInfo contains the path and timeout for a test
type TestInfo struct {
	Path    string
	Timeout time.Duration
}

// RunTests submits tests to the orca server and streams results via callback
func RunTests(runID string, tests []TestInfo, logDir string, onStart func(string, string), onResult func(protocol.TestResult)) error {
	socketPath := "/tmp/glacic-orca.sock"
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to orca server: %w (is it running?)", err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	var wg sync.WaitGroup
	jobs := make(map[string]*jobState) // ID -> state
	var mu sync.Mutex

	// Submit all jobs
	for _, t := range tests {
		id := uuid.New().String()

		timeout := t.Timeout
		if timeout == 0 {
			timeout = 90 * time.Second // Default timeout
		}

		job := protocol.Job{
			ID:         id,
			ScriptPath: t.Path,
			Timeout:    timeout,
			Env:        map[string]string{"TEST_NAME": t.Path},
		}

		req := protocol.ClientRequest{
			Type: "submit_job",
			Job:  job,
		}

		if err := enc.Encode(req); err != nil {
			return err
		}

		mu.Lock()
		jobs[id] = &jobState{
			Name:      t.Path,
			StartTime: time.Now(),
			Timeout:   timeout,
		}
		mu.Unlock()
		wg.Add(1)
	}

	// Result loop
	go func() {
		for {
			var msg protocol.Message
			if err := dec.Decode(&msg); err != nil {
				return // Disconnected
			}

			mu.Lock()
			state := jobs[msg.Ref]
			mu.Unlock()

			if state == nil {
				continue
			}

			switch msg.Type {
			case protocol.MsgStdout, protocol.MsgStderr:
				// Create log file on first output (lazy initialization)
				if !state.Started && logDir != "" {
					state.LogFile, state.LogPath, _ = prepLogFile(logDir, state.Name, runID)
					state.Started = true
					state.StartTime = time.Now() // Reset timer to actual start of execution

					// Inject metadata header
					if state.LogFile != nil && msg.WorkerID != "" {
						fmt.Fprintf(state.LogFile, "### Test Metadata ###\n")
						fmt.Fprintf(state.LogFile, "Test: %s\n", state.Name)
						fmt.Fprintf(state.LogFile, "Worker: %s\n", msg.WorkerID)
						fmt.Fprintf(state.LogFile, "Start: %s\n", state.StartTime.Format(time.RFC3339))

						// Fetch worker history/status
						status, err := GetStatus()
						if err == nil {
							for _, vm := range status.VMs {
								if vm.ID == msg.WorkerID {
									fmt.Fprintf(state.LogFile, "History: %v\n", vm.JobHistory)
									fmt.Fprintf(state.LogFile, "ActiveJobs: %d\n", vm.ActiveJobs)
									break
								}
							}
						}
						fmt.Fprintf(state.LogFile, "---------------------\n\n")
					}

					if onStart != nil {
						onStart(state.Name, state.LogPath)
					}
				}
				if state.LogFile != nil {
					state.LogFile.Write(msg.Data)
				}

				// LINE BUFFERING & TAP PARSING
				// Append new data to buffer
				state.Buffer = append(state.Buffer, msg.Data...)

				// Process complete lines
				for {
					idx := bytes.IndexByte(state.Buffer, '\n')
					if idx == -1 {
						break
					}

					// Extract line (excluding newline)
					line := state.Buffer[:idx]
					// Advance buffer
					state.Buffer = state.Buffer[idx+1:]

					state.Lines++

					// TAP Parsing: Look for "ok <num> - # SKIP <reason>"
					// Simplistic check: line matches "^ok .*# SKIP"
					lineStr := string(line)
					if strings.Contains(lineStr, "# SKIP") {
						// Ensure it's a passing TAP result (starts with "ok")
						trimmed := strings.TrimSpace(lineStr)
						if strings.HasPrefix(trimmed, "ok") {
							state.Skipped++
						} else if strings.HasPrefix(trimmed, "skip") {
							// Sometimes people just write "skip"
							state.Skipped++
						}
					}

					// TODO Parsing: Look for "# TODO:" mechanism to allow failure
					if strings.Contains(lineStr, "# TODO:") {
						state.Todo = true
					}

					// YAML Diagnostics Parsing
					// State machine:
					// 0. Default
					// 1. Inside YAML block (saw "  ---")

					trimmed := strings.TrimSpace(lineStr)
					if trimmed == "---" {
						state.InYaml = true
						state.YamlBuffer = []string{}
					} else if trimmed == "..." && state.InYaml {
						state.InYaml = false
						// Parse the buffer
						if state.Diagnostics == nil {
							state.Diagnostics = make(map[string]interface{})
						}
						for _, yLine := range state.YamlBuffer {
							// Simple "key: value" parser
							parts := strings.SplitN(yLine, ":", 2)
							if len(parts) == 2 {
								key := strings.TrimSpace(parts[0])
								val := strings.TrimSpace(parts[1])
								// Strip quotes if present
								val = strings.Trim(val, "\"")
								state.Diagnostics[key] = val
							}
						}
					} else if state.InYaml {
						state.YamlBuffer = append(state.YamlBuffer, lineStr)
					}
				}

			case protocol.MsgExit:
				duration := time.Since(state.StartTime)
				passed := msg.ExitCode == 0

				// Allow failure if marked as TODO
				if state.Todo {
					passed = true
				}

				timedOut := msg.ExitCode == 124 || duration > 85*time.Second

				if state.LogFile != nil {
					state.LogFile.Close()
				}

				result := protocol.TestResult{
					ID:            msg.Ref,
					Name:          state.Name,
					Passed:        passed,
					ExitCode:      msg.ExitCode,
					Duration:      duration,
					LogPath:       state.LogPath,
					TimedOut:      timedOut,
					LinesCaptured: state.Lines,
					WorkerID:      msg.WorkerID,
					Skipped:       state.Skipped,
					Todo:          state.Todo,
					Diagnostics:   state.Diagnostics,
				}

				if onResult != nil {
					onResult(result)
				}

				mu.Lock()
				delete(jobs, msg.Ref)
				mu.Unlock()
				wg.Done()
			}
		}
	}()

	wg.Wait()
	return nil
}

func prepLogFile(logDir, testName, runID string) (*os.File, string, error) {
	// Create directory structure: logDir/testName/
	testDir := filepath.Join(logDir, testName)
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return nil, "", err
	}

	// Generate filename from runID
	filename := fmt.Sprintf("%s.log", runID)
	logPath := filepath.Join(testDir, filename)

	f, err := os.Create(logPath)
	if err != nil {
		return nil, "", err
	}

	// Return absolute or relative path as appropriate
	displayPath := logPath
	// If it's in the project root's build dir, make it a bit nicer?
	// For now just return the path we have.
	return f, displayPath, nil
}

// RunExec executes a command on a worker VM, potentially with interactivity
func RunExec(command []string, tty bool, vmid string) error {
	socketPath := "/tmp/glacic-orca.sock"
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to orca server: %w", err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	jobID := uuid.New().String()
	req := protocol.ClientRequest{
		Type:     "exec",
		TargetVM: vmid,
		Command:  command,
		Tty:      tty,
		Job: protocol.Job{
			ID: jobID,
		},
	}

	if err := enc.Encode(req); err != nil {
		return err
	}

	// Stdin loop
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			data := make([]byte, n)
			copy(data, buf[:n])
			msg := protocol.Message{
				Type: protocol.MsgStdin,
				Ref:  jobID,
				Data: data,
			}
			enc.Encode(msg)
		}
	}()

	// Output loop
	for {
		var msg protocol.Message
		if err := dec.Decode(&msg); err != nil {
			return nil // Disconnected
		}

		switch msg.Type {
		case protocol.MsgStdout:
			os.Stdout.Write(msg.Data)
		case protocol.MsgStderr:
			os.Stderr.Write(msg.Data)
		case protocol.MsgExit:
			if msg.ExitCode != 0 {
				return fmt.Errorf("exit status %d", msg.ExitCode)
			}
			return nil
		case protocol.MsgError:
			return fmt.Errorf("server error: %s", msg.Error)
		}
	}
}

// RunShell starts an interactive shell on a worker VM
func RunShell(vmid string) error {
	return RunExec([]string{"/bin/sh"}, true, vmid)
}

// EnsureServer checks if the orca server is running and starts it if not.
// Returns true if a transient server was started.
func EnsureServer(trace bool, warm, max int) (bool, error) {
	socketPath := "/tmp/glacic-orca.sock"
	conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err == nil {
		conn.Close()
		return false, nil
	}

	fmt.Println("Orca Server not found, starting transient controller...")
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}

	args := []string{"orca", "server", "--daemon"}
	if warm > 0 || max > 0 {
		if warm == max {
			args = append(args, fmt.Sprintf("-j%d", max))
		} else {
			args = append(args, fmt.Sprintf("-j%d:%d", warm, max))
		}
	}
	if trace {
		args = append(args, "--trace")
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("failed to start transient server: %w", err)
	}

	// Wait for socket
	for i := 0; i < 100; i++ {
		conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return false, fmt.Errorf("timeout waiting for transient orca server to start")
}

// ShutdownServer sends a shutdown command to the orca server
func ShutdownServer() error {
	socketPath := "/tmp/glacic-orca.sock"
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	return json.NewEncoder(conn).Encode(protocol.ClientRequest{Type: "shutdown"})
}

// GetStatus fetches the current state of the orca server
func GetStatus() (*protocol.StatusResponse, error) {
	socketPath := "/tmp/glacic-orca.sock"
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := protocol.ClientRequest{Type: "status"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}

	var resp protocol.StatusResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
