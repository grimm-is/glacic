package logging

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// SyslogConfig holds syslog remote server configuration.
type SyslogConfig struct {
	Enabled  bool   // Enable remote syslog
	Host     string // Remote syslog server hostname or IP
	Port     int    // Remote syslog server port (default: 514)
	Protocol string // udp or tcp (default: udp)
	Tag      string // Syslog tag/app name (default: glacic)
	Facility int    // Syslog facility (default: 1 = user)
}

// DefaultSyslogConfig returns sensible defaults.
func DefaultSyslogConfig() SyslogConfig {
	return SyslogConfig{
		Enabled:  false,
		Port:     514,
		Protocol: "udp",
		Tag:      "glacic",
		Facility: 1, // LOG_USER
	}
}

// SyslogWriter implements io.Writer and sends logs to a remote syslog server.
type SyslogWriter struct {
	mu       sync.Mutex
	conn     net.Conn
	config   SyslogConfig
	facility int
}

// NewSyslogWriter creates a new syslog writer.
func NewSyslogWriter(cfg SyslogConfig) (*SyslogWriter, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("syslog host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 514
	}
	if cfg.Protocol == "" {
		cfg.Protocol = "udp"
	}
	if cfg.Tag == "" {
		cfg.Tag = "glacic"
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	conn, err := net.DialTimeout(cfg.Protocol, addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to syslog server %s: %w", addr, err)
	}

	return &SyslogWriter{
		conn:     conn,
		config:   cfg,
		facility: cfg.Facility,
	}, nil
}

// Write implements io.Writer for syslog.
// Formats message in RFC 3164 format: <priority>timestamp hostname tag: message
func (w *SyslogWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return 0, fmt.Errorf("syslog connection closed")
	}

	// Priority = facility * 8 + severity
	// We use INFO level (6) as default severity
	priority := w.facility*8 + 6

	// RFC 3164 format
	timestamp := time.Now().Format(time.Stamp)
	hostname := "glacic"
	msg := fmt.Sprintf("<%d>%s %s %s: %s", priority, timestamp, hostname, w.config.Tag, string(p))

	_, err = w.conn.Write([]byte(msg))
	if err != nil {
		// Try to reconnect
		w.reconnect()
		return 0, err
	}

	return len(p), nil
}

// reconnect attempts to re-establish the syslog connection.
func (w *SyslogWriter) reconnect() {
	if w.conn != nil {
		w.conn.Close()
	}

	addr := fmt.Sprintf("%s:%d", w.config.Host, w.config.Port)
	conn, err := net.DialTimeout(w.config.Protocol, addr, 5*time.Second)
	if err != nil {
		log.Printf("[syslog] Failed to reconnect: %v", err)
		return
	}
	w.conn = conn
}

// Close closes the syslog connection.
func (w *SyslogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn != nil {
		err := w.conn.Close()
		w.conn = nil
		return err
	}
	return nil
}

// MultiWriter combines multiple io.Writers (e.g., stderr + syslog).
func MultiWriter(writers ...io.Writer) io.Writer {
	return io.MultiWriter(writers...)
}
