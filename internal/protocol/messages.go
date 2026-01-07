package protocol

// MessageType defines the kind of JSON message
type MessageType string

const (
	// Controller -> Agent
	MsgExec   MessageType = "exec"   // Execute a command
	MsgStdin  MessageType = "stdin"  // Input data for a running process
	MsgSignal MessageType = "signal" // Send signal (SIGINT/SIGTERM)
	MsgResize MessageType = "resize" // Resize PTY (if applicable)

	// Agent -> Controller
	MsgStdout    MessageType = "stdout"    // Output data from process
	MsgStderr    MessageType = "stderr"    // Error data from process
	MsgExit      MessageType = "exit"      // Process exit code
	MsgHeartbeat MessageType = "heartbeat" // Agent alive signal
	MsgError     MessageType = "error"     // Protocol or system error
)

// Message is the generic container for all JSONL lines.
// It uses a discrimintator field `Type` to determine `Payload` structure.
type Message struct {
	Type     MessageType `json:"type"`                // Message type
	ID       string      `json:"id,omitempty"`        // Unique Request ID (for Exec)
	Ref      string      `json:"ref,omitempty"`       // Reference ID (for output/exit associated with a req)
	Payload  interface{} `json:"payload,omitempty"`   // Start payload (for Exec)
	Data     []byte      `json:"data,omitempty"`      // Raw data (for Stdin/Stdout/Stderr)
	ExitCode int         `json:"exit_code,omitempty"` // For Exit messages
	Signal   int         `json:"signal,omitempty"`    // For Signal messages
	Error    string      `json:"error,omitempty"`     // For Error messages
	WorkerID string      `json:"worker_id,omitempty"` // ID of the worker VM (injected by server)
}

// ... existing structs ...

// VMInfo describes a single worker VM state

// ExecPayload defines the parameters for starting a process
type ExecPayload struct {
	Command []string          `json:"cmd"`
	Env     map[string]string `json:"env,omitempty"`
	Dir     string            `json:"dir,omitempty"`
	Tty     bool              `json:"tty,omitempty"`     // Allocate a PTY?
	Timeout int               `json:"timeout,omitempty"` // Timeout in seconds (0 = no timeout)
}
