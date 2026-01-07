package protocol

import "time"

// Job represents a test execution request
type Job struct {
	ID         string            `json:"id"`
	ScriptPath string            `json:"script_path,omitempty"`
	Command    []string          `json:"command,omitempty"`
	Timeout    time.Duration     `json:"timeout"`
	Env        map[string]string `json:"env,omitempty"`
	Tty        bool              `json:"tty,omitempty"`
}

// JobResult represents the outcome of a job
type JobResult struct {
	JobID    string        `json:"job_id"`
	Status   string        `json:"status"` // "PASS", "FAIL", "SKIP", "ERROR"
	ExitCode int           `json:"exit_code"`
	Output   string        `json:"output"`
	Duration time.Duration `json:"duration"`
	WorkerID string        `json:"worker_id"`
	Error    string        `json:"error,omitempty"`
}

// ClientRequest defines the envelope for CLI->Server communication
type ClientRequest struct {
	Type     string            `json:"type"`                // "submit_job", "status", "shutdown", "exec", "shell"
	TargetVM string            `json:"target_vm,omitempty"` // Specific VM ID to target
	Job      Job               `json:"job,omitempty"`
	Command  []string          `json:"command,omitempty"` // For "exec"
	Env      map[string]string `json:"env,omitempty"`
	Tty      bool              `json:"tty,omitempty"`
}

// StatusResponse defines the payload for "status" requests
type StatusResponse struct {
	VMs      []VMInfo `json:"vms"`
	WarmSize int      `json:"warmSize"`
	MaxSize  int      `json:"maxSize"`
}

// VMInfo provides details about a single worker
type VMInfo struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Busy       bool     `json:"busy"`
	ActiveJobs int      `json:"active_jobs"`
	LastHealth string   `json:"last_health,omitempty"`
	LastJob    string   `json:"last_job,omitempty"`
	JobHistory []string `json:"job_history,omitempty"` // List of executed job scripts/commands
}

// TestResult represents the outcome of a single test for streaming to the client
type TestResult struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Passed        bool          `json:"passed"`
	ExitCode      int           `json:"exit_code"`
	Duration      time.Duration `json:"duration"`
	LogPath       string        `json:"log_path"`
	TimedOut      bool          `json:"timed_out"`
	LinesCaptured int           `json:"lines_captured"`
	WorkerID      string        `json:"worker_id"`
	Skipped       int                    `json:"skipped"`
	Todo          bool                   `json:"todo"`
	Diagnostics   map[string]interface{} `json:"diagnostics,omitempty"`
}
