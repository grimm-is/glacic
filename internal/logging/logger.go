package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
)

// Level represents log severity levels.
type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

var (
	defaultLogger *Logger
	once          sync.Once

	// defaultOutput is the default destination for logs.
	// We use a variable so CaptureStdio can override the "uncaptured" destination.
	defaultOutput io.Writer = os.Stderr
)

// Logger wraps slog with firewall-specific functionality.
type Logger struct {
	*slog.Logger
	level  *slog.LevelVar
	output io.Writer
}

// Config holds logger configuration.
type Config struct {
	Level      Level
	Output     io.Writer
	JSON       bool
	AddSource  bool
	TimeFormat string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Level:      LevelInfo,
		Output:     defaultOutput,
		JSON:       false,
		AddSource:  false,
		TimeFormat: time.RFC3339,
	}
}

// New creates a new Logger with the given configuration.
func New(cfg Config) *Logger {
	if cfg.Output == nil {
		cfg.Output = os.Stderr
	}

	levelVar := &slog.LevelVar{}
	levelVar.Set(cfg.Level)

	opts := &slog.HandlerOptions{
		Level:     levelVar,
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler
	if cfg.JSON {
		handler = slog.NewJSONHandler(cfg.Output, opts)
	} else {
		handler = NewConsoleHandler(cfg.Output, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
		level:  levelVar,
		output: cfg.Output,
	}
}

// Default returns the default logger, creating it if necessary.
func Default() *Logger {
	once.Do(func() {
		defaultLogger = New(DefaultConfig())
	})
	return defaultLogger
}

// SetDefault sets the default logger.
func SetDefault(l *Logger) {
	defaultLogger = l
}

// SetLevel changes the log level dynamically.
func (l *Logger) SetLevel(level Level) {
	l.level.Set(level)
}

// GetLevel returns the current log level.
func (l *Logger) GetLevel() Level {
	return l.level.Level()
}

// WithComponent returns a logger with a component field.
func (l *Logger) WithComponent(name string) *Logger {
	return &Logger{
		Logger: l.Logger.With("component", name),
		level:  l.level,
		output: l.output,
	}
}

// WithFields returns a logger with additional fields.
func (l *Logger) WithFields(fields map[string]any) *Logger {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return &Logger{
		Logger: l.Logger.With(args...),
		level:  l.level,
		output: l.output,
	}
}

// Audit logs an audit event (always logged regardless of level).
func (l *Logger) Audit(action, resource string, details map[string]any) {
	args := []any{
		"audit", true,
		"action", action,
		"resource", resource,
		"timestamp", clock.Now().UTC().Format(time.RFC3339),
	}
	for k, v := range details {
		args = append(args, k, v)
	}
	l.Info("AUDIT", args...)
}

// Metric logs a metric event for later scraping.
func (l *Logger) Metric(name string, value float64, labels map[string]string) {
	args := []any{
		"metric", true,
		"name", name,
		"value", value,
	}
	for k, v := range labels {
		args = append(args, "label_"+k, v)
	}
	l.Debug("METRIC", args...)
}

// Package-level convenience functions using default logger

// Debug logs at debug level.
func Debug(msg string, args ...any) {
	Default().Debug(msg, args...)
}

// Info logs at info level.
func Info(msg string, args ...any) {
	Default().Info(msg, args...)
}

// Warn logs at warn level.
func Warn(msg string, args ...any) {
	Default().Warn(msg, args...)
}

// Error logs at error level.
func Error(msg string, args ...any) {
	Default().Error(msg, args...)
}

// Errorf logs a formatted error message.
func Errorf(format string, args ...any) {
	Default().Error(fmt.Sprintf(format, args...))
}

// Audit logs an audit event.
func Audit(action, resource string, details map[string]any) {
	Default().Audit(action, resource, details)
}

// WithComponent returns a component-scoped logger.
func WithComponent(name string) *Logger {
	return Default().WithComponent(name)
}
