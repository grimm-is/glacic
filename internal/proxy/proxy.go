package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/logging"
)

// Server handles TCP to Unix socket proxying with optional TLS termination
type Server struct {
	listenAddr string
	targetSock string
	listener   net.Listener
	logger     *logging.Logger
	wg         sync.WaitGroup
	tlsConfig  *tls.Config
}

// NewServer creates a new proxy server
func NewServer(listenAddr, targetSock string) *Server {
	return &Server{
		listenAddr: listenAddr,
		targetSock: targetSock,
		logger:     logging.WithComponent("proxy"),
	}
}

// WithTLS configures the proxy to terminate TLS using the provided certificate
func (s *Server) WithTLS(certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to load TLS certificate: %w", err)
	}
	s.tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	return nil
}

// SetTargetSock updates the target socket path (useful after chroot updates path)
func (s *Server) SetTargetSock(targetSock string) {
	s.targetSock = targetSock
}

// Start starts the proxy server in the background
// If inheritedListener is provided, it uses that instead of binding a new port
func (s *Server) Start(ctx context.Context, inheritedListener net.Listener) error {
	if inheritedListener != nil {
		s.listener = inheritedListener
	} else {
		// Create base TCP listener
		tcpListener, err := net.Listen("tcp", s.listenAddr)
		if err != nil {
			return fmt.Errorf("failed to bind proxy: %w", err)
		}

		// Wrap with TLS if configured
		if s.tlsConfig != nil {
			s.listener = tls.NewListener(tcpListener, s.tlsConfig)
			s.logger.Info(fmt.Sprintf("Proxy started with TLS: %s -> unix:%s", s.listenAddr, s.targetSock))
		} else {
			s.listener = tcpListener
			s.logger.Info(fmt.Sprintf("Proxy started: %s -> unix:%s", s.listenAddr, s.targetSock))
		}
	}

	s.wg.Add(1)
	go s.run(ctx)
	return nil
}

// run accepts connections and starts handlers
func (s *Server) run(ctx context.Context) {
	defer s.wg.Done()

	// Watch for context cancellation to close listener
	go func() {
		<-ctx.Done()
		if s.listener != nil {
			s.listener.Close()
		}
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if closed due to context
			select {
			case <-ctx.Done():
				return
			default:
			}
			// Only log non-transient errors
			if !strings.Contains(err.Error(), "use of closed network connection") {
				s.logger.Error("Accept error: " + err.Error())
			}
			continue
		}

		s.wg.Add(1)
		go s.handle(conn)
	}
}

// handle proxies a single connection
func (s *Server) handle(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	// Connect to Unix socket
	upstream, err := net.DialTimeout("unix", s.targetSock, 1*time.Second)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to dial upstream %s: %v", s.targetSock, err))
		return
	}
	defer upstream.Close()

	// Bidirectional copy
	errCh := make(chan error, 2)

	go func() {
		_, err := io.Copy(upstream, conn)
		errCh <- err
	}()

	go func() {
		_, err := io.Copy(conn, upstream)
		errCh <- err
	}()

	// Wait for first error (disconnect)
	<-errCh
}

// Wait waits for all connections to close
func (s *Server) Wait() {
	s.wg.Wait()
}

