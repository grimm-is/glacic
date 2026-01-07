package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"grimm.is/glacic/internal/protocol"
)

type ActiveProcess struct {
	Cmd   *exec.Cmd
	Stdin io.WriteCloser
}

// Run is the entrypoint for the V2 agent
func Run(args []string) error {
	// Standardize working directory for relative test paths
	if stat, err := os.Stat("/mnt/glacic"); err == nil && stat.IsDir() {
		os.Chdir("/mnt/glacic")
	}

	port, err := openVirtioPort()
	if err != nil {
		return fmt.Errorf("failed to open serial port: %w", err)
	}
	defer port.Close()

	// Protocol Streams
	dec := json.NewDecoder(port)
	enc := json.NewEncoder(port)
	encMutex := &sync.Mutex{}

	// Sending helper
	send := func(msg protocol.Message) error {
		encMutex.Lock()
		defer encMutex.Unlock()
		return enc.Encode(msg)
	}

	// Active Processes
	procs := make(map[string]*ActiveProcess)
	procsMu := &sync.Mutex{}

	// Hello
	fmt.Fprintf(os.Stderr, "⚡ Agent starting: sending initial heartbeat\n")
	if err := send(protocol.Message{Type: protocol.MsgHeartbeat}); err != nil {
		return err
	}

	// Periodic heartbeats
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			send(protocol.Message{Type: protocol.MsgHeartbeat})
		}
	}()

	// Main Loop
	for {
		var msg protocol.Message
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decode error: %w", err)
		}

		switch msg.Type {
		case protocol.MsgExec:
			go handleExec(msg, procs, procsMu, send)

		case protocol.MsgStdin:
			handleStdin(msg, procs, procsMu)

		case protocol.MsgSignal:
			handleSignal(msg, procs, procsMu)
		}
	}
}

func handleExec(msg protocol.Message, procs map[string]*ActiveProcess, mu *sync.Mutex, send func(protocol.Message) error) {
	// Parse payload
	payloadBytes, _ := json.Marshal(msg.Payload)
	var req protocol.ExecPayload
	json.Unmarshal(payloadBytes, &req)

	cmd := exec.Command(req.Command[0], req.Command[1:]...)
	cmd.Dir = "/"
	if _, err := os.Stat("/mnt/glacic"); err == nil {
		cmd.Dir = "/mnt/glacic"
	}
	if req.Dir != "" {
		cmd.Dir = req.Dir
	}
	cmd.Env = os.Environ()
	// Ensure standard PATH
	foundPath := false
	for i, env := range cmd.Env {
		if strings.HasPrefix(env, "PATH=") {
			cmd.Env[i] = "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
			foundPath = true
			break
		}
	}
	if !foundPath {
		cmd.Env = append(cmd.Env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}

	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	fmt.Fprintf(os.Stderr, "[Agent] Exec: %v in %s (timeout: %ds)\n", cmd.Args, cmd.Dir, req.Timeout)

	// Create process group so we can kill all children
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Setup timeout killer if timeout is specified
	var timeoutCh chan struct{}
	var timedOut bool
	if req.Timeout > 0 {
		timeoutCh = make(chan struct{})
		go func() {
			timer := time.NewTimer(time.Duration(req.Timeout) * time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
				timedOut = true
				fmt.Fprintf(os.Stderr, "[Agent] Job %s TIMEOUT after %ds, killing process group\n", msg.ID, req.Timeout)
				// Kill entire process group (negative PID)
				if cmd.Process != nil {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
			case <-timeoutCh:
				// Command completed before timeout
			}
		}()
	}

	var streamWg sync.WaitGroup
	var ptyFile *os.File

	// Shared sender for output
	sendOutput := func(t protocol.MessageType, data []byte) {
		send(protocol.Message{Type: t, Ref: msg.ID, Data: data})
	}

	isTty := req.Tty

	if isTty {
		var err error
		ptyFile, err = pty.Start(cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[Agent] PTY Start failed: %v\n", err)
			send(protocol.Message{Type: protocol.MsgError, Ref: msg.ID, Error: fmt.Sprintf("pty start: %v", err)})
			return
		}
		defer ptyFile.Close()

		proc := &ActiveProcess{Cmd: cmd, Stdin: ptyFile}
		mu.Lock()
		procs[msg.ID] = proc
		mu.Unlock()

		streamWg.Add(1)
		go func() {
			defer streamWg.Done()
			buf := make([]byte, 4096)
			for {
				n, err := ptyFile.Read(buf)
				if n > 0 {
					sendOutput(protocol.MsgStdout, buf[:n])
				}
				if err != nil {
					break
				}
			}
		}()
	} else {
		proc := &ActiveProcess{Cmd: cmd}
		mu.Lock()
		procs[msg.ID] = proc
		mu.Unlock()

		cmd.Stdout = &WriterProxy{Type: protocol.MsgStdout, Send: sendOutput}
		cmd.Stderr = &WriterProxy{Type: protocol.MsgStderr, Send: sendOutput}

		stdin, _ := cmd.StdinPipe()
		proc.Stdin = stdin

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[Agent] Start failed: %v\n", err)
			send(protocol.Message{Type: protocol.MsgError, Ref: msg.ID, Error: err.Error()})
			mu.Lock()
			delete(procs, msg.ID)
			mu.Unlock()
			return
		}
	}

	go func() {
		err := cmd.Wait()

		// Cancel timeout goroutine if it's running
		if timeoutCh != nil {
			close(timeoutCh)
		}

		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		// Override exit code for timeout (killed by SIGKILL = -1 or 137)
		if timedOut {
			exitCode = 124 // Standard timeout exit code
			fmt.Fprintf(os.Stderr, "[Agent] Job %s killed due to timeout\n", msg.ID)
		} else {
			fmt.Fprintf(os.Stderr, "[Agent] Job %s exited with %d\n", msg.ID, exitCode)
		}

		if isTty {
			streamWg.Wait()
		}

		send(protocol.Message{Type: protocol.MsgExit, Ref: msg.ID, ExitCode: exitCode})

		mu.Lock()
		delete(procs, msg.ID)
		mu.Unlock()
	}()
}

type WriterProxy struct {
	Type protocol.MessageType
	Send func(protocol.MessageType, []byte)
}

func (w *WriterProxy) Write(p []byte) (n int, err error) {
	if len(p) > 0 {
		w.Send(w.Type, p)
	}
	return len(p), nil
}

func handleStdin(msg protocol.Message, procs map[string]*ActiveProcess, mu *sync.Mutex) {
	mu.Lock()
	proc, ok := procs[msg.Ref]
	mu.Unlock()

	if ok && proc.Stdin != nil {
		if len(msg.Data) == 0 {
			// Empty data usually means EOF/Close
			proc.Stdin.Close()
		} else {
			proc.Stdin.Write(msg.Data)
		}
	}
}

func handleSignal(msg protocol.Message, procs map[string]*ActiveProcess, mu *sync.Mutex) {
	mu.Lock()
	proc, ok := procs[msg.Ref]
	mu.Unlock()
	if ok && proc.Cmd.Process != nil {
		proc.Cmd.Process.Signal(syscall.Signal(msg.Signal))
	}
}

// Helpers from original code
func openVirtioPort() (*os.File, error) {
	paths := []string{
		"/dev/virtio-ports/glacic.agent",
		"/dev/vport0p1",
	}

	for i := 0; i < 10; i++ {
		for _, p := range paths {
			if f, err := os.OpenFile(p, os.O_RDWR, 0); err == nil {
				return f, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("no serial port found")
}
