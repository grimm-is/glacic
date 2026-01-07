package nc

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// Run executes netcat
// Syntax: nc [-u] [-l] [-p port] [host] [port]
// Minimal support for tests
func Run(args []string) error {
	var (
		udp     bool
		listen  bool
		port    string
		host    string
		timeout = 5 * time.Second
	)

	// Simple strict parsing for built-in flags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-u":
			udp = true
		case "-l":
			listen = true
		case "-p":
			if i+1 < len(args) {
				port = args[i+1]
				i++
			}
		case "-w":
			if i+1 < len(args) {
				// parse int seconds
				// simplified
				i++
			}
		default:
			if strings.HasPrefix(arg, "-") {
				continue // ignore unknown flags
			}
			if host == "" {
				host = arg
			} else if port == "" {
				port = arg
			}
		}
	}

	if listen {
		protocol := "tcp"
		if udp {
			protocol = "udp"
		}
		addr := ":" + port
		if host != "" { // if host provided in listen mode? usually -l -p port. host is optional bind addr.
			if port == "" {
				// assume host is port? nc -l 8080
				addr = ":" + host
			} else {
				addr = host + ":" + port
			}
		}

		l, err := net.Listen(protocol, addr)
		if err != nil {
			return err
		}
		defer l.Close()
		fmt.Fprintf(os.Stderr, "Listening on %s...\n", addr)

		for {
			conn, err := l.Accept()
			if err != nil {
				return err
			}
			go handle(conn)
		}
	} else {
		// Dial mode
		if host == "" || port == "" {
			return fmt.Errorf("usage: nc [-u] host port")
		}
		protocol := "tcp"
		if udp {
			protocol = "udp"
		}

		conn, err := net.DialTimeout(protocol, net.JoinHostPort(host, port), timeout)
		if err != nil {
			return err
		}
		defer conn.Close()

		go io.Copy(os.Stdout, conn)
		io.Copy(conn, os.Stdin)
	}

	return nil
}

func handle(conn net.Conn) {
	defer conn.Close()
	io.Copy(os.Stdout, conn)
}
