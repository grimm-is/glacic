package orca

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Multiplexer allows multiple clients to share a single virtio-serial connection
// by serializing requests and routing responses.
type Multiplexer struct {
	serialConn net.Conn      // The single connection to the agent
	listenPath string        // Unix socket path for clients
	listener   net.Listener  // Accept client connections
	mu         sync.Mutex    // Serialize access to serialConn
	closed     bool
	wg         sync.WaitGroup
}

// NewMultiplexer creates a multiplexer that bridges a virtio-serial connection
// to a Unix socket that accepts multiple clients.
func NewMultiplexer(serialPath, listenPath string) (*Multiplexer, error) {
	// Connect to the serial socket (owned by QEMU)
	serialConn, err := net.Dial("unix", serialPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to serial: %w", err)
	}

	// Read and discard the HELLO handshake
	reader := bufio.NewReader(serialConn)
	reader.ReadString('\n')

	return newMultiplexerWithConn(serialConn, listenPath)
}

// NewMultiplexerWithConn creates a multiplexer using an existing connection
// (useful when the HELLO handshake was already consumed during probing)
func NewMultiplexerWithConn(serialConn net.Conn, listenPath string) (*Multiplexer, error) {
	return newMultiplexerWithConn(serialConn, listenPath)
}

func newMultiplexerWithConn(serialConn net.Conn, listenPath string) (*Multiplexer, error) {
	// Create listening socket for clients
	_ = os.Remove(listenPath) // Clean up any stale socket
	listener, err := net.Listen("unix", listenPath)
	if err != nil {
		serialConn.Close()
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	m := &Multiplexer{
		serialConn: serialConn,
		listenPath: listenPath,
		listener:   listener,
	}

	return m, nil
}

// Start begins accepting client connections
func (m *Multiplexer) Start() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.acceptLoop()
	}()
}

// Close shuts down the multiplexer
func (m *Multiplexer) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	m.listener.Close()
	m.serialConn.Close()
	_ = os.Remove(m.listenPath)
	m.wg.Wait()
}

func (m *Multiplexer) acceptLoop() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			m.mu.Lock()
			closed := m.closed
			m.mu.Unlock()
			if closed {
				return
			}
			fmt.Printf("Multiplexer accept error: %v\n", err)
			continue
		}

		m.wg.Add(1)
		go func(c net.Conn) {
			defer m.wg.Done()
			defer c.Close()
			m.handleClient(c)
		}(conn)
	}
}

func (m *Multiplexer) handleClient(client net.Conn) {
	// Send fake HELLO to client (they expect it)
	fmt.Fprintln(client, "HELLO AGENT_v1")

	reader := bufio.NewReader(client)

	for {
		client.SetReadDeadline(time.Now().Add(5 * time.Minute))
		line, err := reader.ReadString('\n')
		if err != nil {
			return // Client disconnected
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Forward request to agent and get response
		response, err := m.forwardRequest(line)
		if err != nil {
			fmt.Fprintf(client, "ERR: %v\n", err)
			continue
		}

		// Send response back to client
		_, err = client.Write([]byte(response))
		if err != nil {
			return // Client disconnected
		}
	}
}

// forwardRequest sends a command to the agent and returns the complete response
func (m *Multiplexer) forwardRequest(request string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Send request
	m.serialConn.SetDeadline(time.Now().Add(30 * time.Second))
	_, err := fmt.Fprintf(m.serialConn, "%s\n", request)
	if err != nil {
		return "", fmt.Errorf("write error: %w", err)
	}

	reader := bufio.NewReader(m.serialConn)
	cmd := strings.ToUpper(strings.Fields(request)[0])

	switch cmd {
	case "PING":
		// Single line response
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return line, nil

	case "STATUS":
		// Single line response
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return line, nil

	case "EXEC":
		// Multi-line response between BEGIN/END markers
		return m.readUntilMarker(reader, "--- END OUTPUT")

	case "TEST":
		// Multi-line response between TAP_START/TAP_END
		return m.readUntilMarker(reader, "TAP_END")

	case "SHELL":
		// Shell is interactive, not supported through multiplexer
		return "ERR: SHELL not supported through multiplexer\n", nil

	case "EXIT", "QUIT":
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return line, nil

	default:
		// Unknown command, read single line response
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		return line, nil
	}
}

func (m *Multiplexer) readUntilMarker(reader *bufio.Reader, marker string) (string, error) {
	var response strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return response.String(), err
		}
		response.WriteString(line)

		if strings.HasPrefix(strings.TrimSpace(line), marker) {
			break
		}
	}

	return response.String(), nil
}

// SerialPath returns the path clients should connect to
func (m *Multiplexer) ClientPath() string {
	return m.listenPath
}

// RunMultiplexer is a helper that creates and runs a multiplexer until stopped
func RunMultiplexer(serialPath, clientPath string) (*Multiplexer, error) {
	mux, err := NewMultiplexer(serialPath, clientPath)
	if err != nil {
		return nil, err
	}
	mux.Start()
	return mux, nil
}
