package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// ConsoleHandler is a slog.Handler that writes logs in a human-readable format:
// YYYY/MM/DD HH:MM:SS [COMPONENT] Message key=value
type ConsoleHandler struct {
	opts  slog.HandlerOptions
	out   io.Writer
	mu    sync.Mutex
	attrs []slog.Attr
}

// processPrefix is the global prefix used for log output (e.g., "GLACIC", "GLACIC-OLD", "GLACIC-NEW")
var (
	processPrefix   = "GLACIC"
	processPrefixMu sync.RWMutex
)

// SetPrefix sets the global log prefix (e.g., "GLACIC-OLD" during upgrade)
func SetPrefix(prefix string) {
	processPrefixMu.Lock()
	defer processPrefixMu.Unlock()
	processPrefix = prefix
}

// GetPrefix returns the current global log prefix
func GetPrefix() string {
	processPrefixMu.RLock()
	defer processPrefixMu.RUnlock()
	return processPrefix
}

// NewConsoleHandler creates a new ConsoleHandler.
func NewConsoleHandler(out io.Writer, opts *slog.HandlerOptions) *ConsoleHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &ConsoleHandler{
		out:  out,
		opts: *opts,
	}
}

// Enabled reports whether the handler is enabled for this level.
func (h *ConsoleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

// Handle handles the Record.
func (h *ConsoleHandler) Handle(ctx context.Context, r slog.Record) error {
	buf := make([]byte, 0, 1024)

	// timestamp: YYYY/MM/DD HH:MM:SS
	// Using standard Go log format layout
	// timestamp: ISO8601
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}
	buf = append(buf, t.Format(time.RFC3339)...)
	buf = append(buf, ' ')

	// Process Name and PID: glacic[12345]:
	// We use the configured prefix (usually "glacic") or os.Args[0] basename
	// User requested lower case processname[pid]:
	procName := strings.ToLower(GetPrefix())
	if procName == "" {
		procName = "glacic"
	}
	// Sanitize prefix if it was legacy "GLACIC-OLD" -> "glacic"?
	// User said "eliminate the need". If I change the format, I should probably stop setting the prefix to OLD/NEW in the caller code.
	// But if the caller sets it, I should probably respect it or clean it?
	// For now, let's just use the prefix as is (lowercased) + PID.

	pid := os.Getpid()
	buf = append(buf, fmt.Sprintf("%s[%d]: ", procName, pid)...)

	// Level [info]
	buf = append(buf, '[')
	buf = append(buf, strings.ToLower(r.Level.String())...)
	buf = append(buf, "] "...)

	// Component TAG:
	// We look for a "component" attribute in the record or pre-bound attributes.
	component := ""

	// Check pre-bound attributes
	for _, a := range h.attrs {
		if a.Key == "component" {
			component = strings.ToLower(a.Value.String())
		}
	}
	// Check record attributes (overrides pre-bound)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			component = strings.ToLower(a.Value.String())
			return false
		}
		return true
	})

	if component != "" {
		buf = append(buf, component...)
		buf = append(buf, ':')
		buf = append(buf, ' ')
	}

	// Message
	buf = append(buf, r.Message...)

	// Attributes
	// We append pre-bound attributes first, then record attributes.
	// We skip "component" as it's already promoted to header.
	if len(h.attrs) > 0 {
		for _, a := range h.attrs {
			if a.Key == "component" {
				continue
			}
			buf = append(buf, ' ')
			h.appendAttr(&buf, a)
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			return true
		}
		buf = append(buf, ' ')
		h.appendAttr(&buf, a)
		return true
	})

	buf = append(buf, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.out.Write(buf)

	// Also write to RingBuffer for UI/API visibility
	extra := make(map[string]string)
	// Add attributes to extra
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "component" { // component used as Source
			extra[a.Key] = a.Value.String()
		}
		return true
	})
	// Add pre-bound attributes too
	for _, a := range h.attrs {
		if a.Key != "component" {
			extra[a.Key] = a.Value.String()
		}
	}

	// Determine source from component
	source := "system"
	if component != "" {
		source = strings.ToLower(component)
	}

	entry := AppLogEntry{
		Timestamp: t,
		Level:     LevelFromSlog(r.Level),
		Source:    source,
		Message:   r.Message,
		Extra:     extra,
	}
	GetAppLogBuffer().Add(entry)

	return err
}

func (h *ConsoleHandler) appendAttr(buf *[]byte, a slog.Attr) {
	// Simple key=value formatting
	// Quote values if they contain spaces
	*buf = append(*buf, a.Key...)
	*buf = append(*buf, '=')
	val := a.Value.String()
	if strings.ContainsAny(val, " \t\n") {
		*buf = append(*buf, '"')
		*buf = append(*buf, val...) // minimal escaping for now
		*buf = append(*buf, '"')
	} else {
		*buf = append(*buf, val...)
	}
}

// WithAttrs returns a new handler with the given attributes.
func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ConsoleHandler{
		opts:  h.opts,
		out:   h.out,
		attrs: append(h.attrs, attrs...),
	}
}

// WithGroup returns a new handler with the given group.
func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	// Grouping not strictly implemented for flat console output in this simple version
	return h // no-op for now
}
