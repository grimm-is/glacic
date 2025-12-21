package agent

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mdlayher/vsock"
)

// AgentPort is the vsock port the agent listens on (Linux only)
const AgentPort = 5000

// AgentState tracks what the agent is currently doing
type AgentState struct {
	mu          sync.Mutex
	currentTask string
	taskStart   time.Time
}

func (s *AgentState) TryAcquire(task string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentTask != "" {
		return false
	}
	s.currentTask = task
	s.taskStart = time.Now()
	return true
}

func (s *AgentState) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentTask = ""
}

func (s *AgentState) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentTask != "" {
		elapsed := time.Since(s.taskStart).Round(time.Second)
		return fmt.Sprintf("BUSY: %s (%v)", s.currentTask, elapsed)
	}
	return "IDLE"
}

var state = &AgentState{}

// Run is the entrypoint for the agent subcommand
func Run(args []string) error {
	fmt.Println("Glacic Agent Starting...")
	_ = os.WriteFile("/mnt/glacic/agent_alive.txt", []byte("I AM ALIVE\n"), 0666)

	// Try vsock first (Linux), fall back to virtio-serial (macOS)
	if runtime.GOOS == "linux" {
		// Try vsock
		listener, err := vsock.Listen(AgentPort, nil)
		if err == nil {
			fmt.Printf("Listening on vsock port %d\n", AgentPort)
			return acceptLoop(listener)
		}
		fmt.Printf("vsock not available: %v, falling back to virtio-serial\n", err)
	}

	// Fall back to virtio-serial
	return runVirtioSerial()
}

// acceptLoop handles vsock connections (Linux)
func acceptLoop(listener net.Listener) error {
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("Accept error: %v\n", err)
			continue
		}
		go handleConnection(conn)
	}
}

// runVirtioSerial handles the single virtio-serial connection (macOS)
func runVirtioSerial() error {
	portPath, err := findVirtioPort()
	if err != nil {
		return fmt.Errorf("failed to find virtio-serial port: %w", err)
	}
	fmt.Printf("Found orca port: %s\n", portPath)

	port, err := openPort(portPath)
	if err != nil {
		return err
	}
	defer port.Close()

	fmt.Println("Connected to orca!")

	// Send handshake
	_, err = port.WriteString("HELLO AGENT_v1\n")
	if err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	// Single-connection command loop
	return commandLoop(port)
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	fmt.Printf("New connection from %s\n", remoteAddr)

	// Send handshake
	fmt.Fprintln(conn, "HELLO AGENT_v1")

	scanner := bufio.NewScanner(conn)

	for scanner.Scan() {
		line := scanner.Text()

		parts := strings.SplitN(line, " ", 2)
		cmd := strings.ToUpper(parts[0])
		var arg string
		if len(parts) > 1 {
			arg = parts[1]
		}

		switch cmd {
		case "PING":
			writeResponse(conn, "PONG")

		case "STATUS":
			writeResponse(conn, state.Status())

		case "EXEC":
			if arg == "" {
				writeResponse(conn, "ERR: EXEC requires a command")
				continue
			}
			handleExec(conn, arg)

		case "TEST":
			if arg == "" {
				writeResponse(conn, "ERR: TEST requires a script path")
				continue
			}
			if !state.TryAcquire(fmt.Sprintf("TEST: %s", filepath.Base(arg))) {
				writeResponse(conn, fmt.Sprintf("ERR: Agent busy - %s", state.Status()))
				continue
			}
			handleTest(conn, arg)
			state.Release()

		case "SHELL":
			if !state.TryAcquire("SHELL session") {
				writeResponse(conn, fmt.Sprintf("ERR: Agent busy - %s", state.Status()))
				continue
			}
			handleShell(conn)
			state.Release()

		case "EXIT", "QUIT":
			writeResponse(conn, "BYE")
			return

		default:
			writeResponse(conn, fmt.Sprintf("ERR: Unknown command: %s", cmd))
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Connection %s read error: %v\n", remoteAddr, err)
	}
	fmt.Printf("Connection %s closed\n", remoteAddr)
}

func commandLoop(port *os.File) error {
	scanner := bufio.NewScanner(port)

	for {
		port.SetReadDeadline(time.Now().Add(10 * time.Minute))

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				if errors.Is(err, os.ErrDeadlineExceeded) {
					fmt.Println("Idle timeout reached (10m). Powering off...")
					writeResponse(port, "TIMEOUT: Idle for 10m. Shutting down.")
					exec.Command("poweroff", "-f").Run()
					return fmt.Errorf("idle timeout")
				}
				fmt.Printf("Read error: %v\n", err)
				return err
			}
			break
		}

		line := scanner.Text()

		parts := strings.SplitN(line, " ", 2)
		cmd := strings.ToUpper(parts[0])
		var arg string
		if len(parts) > 1 {
			arg = parts[1]
		}

		switch cmd {
		case "PING":
			writeResponse(port, "PONG")

		case "STATUS":
			writeResponse(port, state.Status())

		case "EXEC":
			if arg == "" {
				writeResponse(port, "ERR: EXEC requires a command")
				continue
			}
			handleExec(port, arg)

		case "TEST":
			if arg == "" {
				writeResponse(port, "ERR: TEST requires a script path")
				continue
			}
			if !state.TryAcquire(fmt.Sprintf("TEST: %s", filepath.Base(arg))) {
				writeResponse(port, fmt.Sprintf("ERR: Agent busy - %s", state.Status()))
				continue
			}
			handleTest(port, arg)
			state.Release()

		case "SHELL":
			if !state.TryAcquire("SHELL session") {
				writeResponse(port, fmt.Sprintf("ERR: Agent busy - %s", state.Status()))
				continue
			}
			handleShell(port)
			state.Release()

		case "EXIT", "QUIT":
			writeResponse(port, "BYE")
			return nil

		default:
			writeResponse(port, fmt.Sprintf("ERR: Unknown command: %s", cmd))
		}
	}

	fmt.Println("Agent exiting (EOF).")
	return nil
}

func writeResponse(w io.Writer, msg string) {
	fmt.Fprintln(w, msg)
}

func handleExec(w io.Writer, cmdLine string) {
	writeResponse(w, "--- BEGIN OUTPUT ---")

	cmd := exec.Command("/bin/sh", "-c", cmdLine)
	cmd.Stdout = w
	cmd.Stderr = w

	err := cmd.Run()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			writeResponse(w, fmt.Sprintf("--- END OUTPUT (exit=%d) ---", exitErr.ExitCode()))
		} else {
			writeResponse(w, fmt.Sprintf("--- END OUTPUT (error: %v) ---", err))
		}
	} else {
		writeResponse(w, "--- END OUTPUT (exit=0) ---")
	}
}

func handleTest(w io.Writer, scriptPath string) {
	fullPath := scriptPath
	if !strings.HasPrefix(scriptPath, "/") {
		fullPath = "/mnt/glacic/" + scriptPath
	}

	if _, err := os.Stat(fullPath); err != nil {
		writeResponse(w, fmt.Sprintf("TAP_ERROR: Script not found: %s", fullPath))
		return
	}

	writeResponse(w, fmt.Sprintf("TAP_START %s", filepath.Base(scriptPath)))

	cmd := exec.Command("/bin/sh", fullPath)
	cmd.Dir = "/mnt/glacic"
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.Env = append(os.Environ(),
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"TERM=dumb",
	)

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	writeResponse(w, fmt.Sprintf("TAP_END %s exit=%d", filepath.Base(scriptPath), exitCode))
}

func handleShell(w io.Writer) {
	writeResponse(w, "--- SHELL START (type 'exit' to end) ---")

	// Need to cast back to get stdin
	var stdin io.Reader
	if f, ok := w.(*os.File); ok {
		stdin = f
	} else if conn, ok := w.(net.Conn); ok {
		stdin = conn
	}

	cmd := exec.Command("/bin/sh", "-i")
	cmd.Stdin = stdin
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.Env = append(os.Environ(),
		"TERM=dumb",
		"PS1=glacic$ ",
	)

	err := cmd.Run()
	if err != nil {
		writeResponse(w, fmt.Sprintf("--- SHELL END (error: %v) ---", err))
	} else {
		writeResponse(w, "--- SHELL END ---")
	}
}

func openPort(portPath string) (*os.File, error) {
	var file *os.File
	var err error
	for i := 0; i < 5; i++ {
		file, err = os.OpenFile(portPath, os.O_RDWR, 0600)
		if err == nil {
			return file, nil
		}
		if errors.Is(err, syscall.EBUSY) {
			fmt.Fprintf(os.Stderr, "Error: Port %s is already in use by another process.\n", portPath)
			fmt.Fprintf(os.Stderr, "Run 'fuser -v %s' to identify the owner.\n", portPath)
			return nil, fmt.Errorf("port busy: %w", err)
		}
		fmt.Printf("Waiting for device... (%v)\n", err)
		time.Sleep(1 * time.Second)
	}
	return nil, fmt.Errorf("failed to open port %s: %w", portPath, err)
}

func findVirtioPort() (string, error) {
	candidates := []string{
		"/dev/virtio-ports/glacic.agent",
		"/dev/vport0p1",
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	matches, _ := filepath.Glob("/dev/vport*")
	if len(matches) > 0 {
		return matches[0], nil
	}

	return "", fmt.Errorf("no suitable port found in /dev/virtio-ports/ or /dev/vport*")
}
