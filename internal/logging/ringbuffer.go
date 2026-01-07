package logging

import (
	"fmt"
	"grimm.is/glacic/internal/clock"
	"log/slog"
	"sync"
	"time"
)

// AppLogEntry represents an application log entry
type AppLogEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	Level     string            `json:"level"`  // "debug", "info", "warn", "error"
	Source    string            `json:"source"` // "api", "ctlplane", "gateway", etc.
	Message   string            `json:"message"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// RingBuffer is a thread-safe circular buffer for log entries
type RingBuffer struct {
	entries []AppLogEntry
	size    int
	head    int
	count   int
	mu      sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with the given capacity
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		entries: make([]AppLogEntry, size),
		size:    size,
	}
}

// Add adds an entry to the ring buffer
func (rb *RingBuffer) Add(entry AppLogEntry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.entries[rb.head] = entry
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// GetAll returns all entries in chronological order
func (rb *RingBuffer) GetAll() []AppLogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]AppLogEntry, rb.count)
	if rb.count == 0 {
		return result
	}

	start := 0
	if rb.count == rb.size {
		start = rb.head
	}

	for i := 0; i < rb.count; i++ {
		idx := (start + i) % rb.size
		result[i] = rb.entries[idx]
	}

	return result
}

// GetLast returns the last n entries in chronological order
func (rb *RingBuffer) GetLast(n int) []AppLogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.count {
		n = rb.count
	}
	if n == 0 {
		return []AppLogEntry{}
	}

	result := make([]AppLogEntry, n)
	start := (rb.head - n + rb.size) % rb.size

	for i := 0; i < n; i++ {
		idx := (start + i) % rb.size
		result[i] = rb.entries[idx]
	}

	return result
}

// GetBySource returns entries filtered by source
func (rb *RingBuffer) GetBySource(source string, limit int) []AppLogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	var result []AppLogEntry

	start := 0
	if rb.count == rb.size {
		start = rb.head
	}

	for i := 0; i < rb.count; i++ {
		idx := (start + i) % rb.size
		if rb.entries[idx].Source == source {
			result = append(result, rb.entries[idx])
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}

	return result
}

// Count returns the number of entries in the buffer
func (rb *RingBuffer) Count() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Clear removes all entries from the buffer
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.head = 0
	rb.count = 0
}

// Global application log buffer
var (
	appLogBuffer *RingBuffer
	bufferOnce   sync.Once
)

// GetAppLogBuffer returns the global application log buffer
func GetAppLogBuffer() *RingBuffer {
	bufferOnce.Do(func() {
		appLogBuffer = NewRingBuffer(5000)
	})
	return appLogBuffer
}

// Log adds a log entry to the global buffer
func Log(source string, level string, format string, args ...interface{}) {
	entry := AppLogEntry{
		Timestamp: clock.Now(),
		Level:     level,
		Source:    source,
		Message:   fmt.Sprintf(format, args...),
	}
	GetAppLogBuffer().Add(entry)
}

// LogWithExtra adds a log entry with extra fields
func LogWithExtra(source string, level string, extra map[string]string, format string, args ...interface{}) {
	entry := AppLogEntry{
		Timestamp: clock.Now(),
		Level:     level,
		Source:    source,
		Message:   fmt.Sprintf(format, args...),
		Extra:     extra,
	}
	GetAppLogBuffer().Add(entry)
}

// LevelFromSlog converts slog.Level to string
func LevelFromSlog(level slog.Level) string {
	switch {
	case level <= slog.LevelDebug:
		return "debug"
	case level <= slog.LevelInfo:
		return "info"
	case level <= slog.LevelWarn:
		return "warn"
	default:
		return "error"
	}
}

// Convenience functions for different sources
func APILog(level string, format string, args ...interface{}) {
	Log("api", level, format, args...)
}

func CtlLog(level string, format string, args ...interface{}) {
	Log("ctlplane", level, format, args...)
}

func GatewayLog(level string, format string, args ...interface{}) {
	Log("gateway", level, format, args...)
}

func AuthLog(level string, format string, args ...interface{}) {
	Log("auth", level, format, args...)
}

func FirewallLog(level string, format string, args ...interface{}) {
	Log("firewall", level, format, args...)
}
