package logging

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// OriginalStdout and OriginalStderr hold the original file descriptors
// before capturing. This allows passing the real files (not pipes) to child processes
// like upgrades, preventing SIGPIPE when the parent exits.
var (
	OriginalStdout *os.File = os.Stdout
	OriginalStderr *os.File = os.Stderr
)

// CaptureStdio hijacks os.Stdout and os.Stderr to capture raw output
// and feed it into the application log buffer.
// It accepts an optional logPath to re-open the log file directly,
// bypassing any inherited pipes (vital for upgrade stability).
func CaptureStdio(logPath string) {
	// Try to open the log file directly if provided
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create log directory: %v\n", err)
		} else {
			f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to open log file: %v\n", err)
			} else {
				// Success - redirect original outputs to this file
				OriginalStdout = f
				OriginalStderr = f
			}
		}
	}

	captureStream := func(isStderr bool) {
		r, w, err := os.Pipe()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create pipe for log capture: %v\n", err)
			return
		}

		var original *os.File
		if isStderr {
			original = OriginalStderr
			// Save the original stderr as the default output for the logger
			// to avoid infinite recursion or duplication (since logger writes to buffer internally)
			defaultOutput = original
			os.Stderr = w
		} else {
			original = OriginalStdout
			os.Stdout = w
		}

		go func() {
			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				line := scanner.Text()

				// Write to original destination (preserve file log)
				// We ignore errors here as we can't do much about them
				fmt.Fprintln(original, line)

				// Add to Ring Buffer
				// We classify stderr as error, stdout as info
				level := "info"
				if isStderr {
					level = "error"
				}

				entry := AppLogEntry{
					Timestamp: time.Now(),
					Level:     level,
					Source:    "system",
					Message:   line,
				}
				GetAppLogBuffer().Add(entry)
			}
		}()
	}

	// Capture Stderr first to set defaultOutput correctly
	captureStream(true)
	captureStream(false)
}

// logBridge adapts the standard log.Logger to write to our structured Logger
type logBridge struct{}

func (b *logBridge) Write(p []byte) (n int, err error) {
	// Strip trailing newline to avoid empty lines in structured log
	msg := string(bytes.TrimSpace(p))
	// Forward to structured logger
	// We use Info level as default for standard logs.
	// We rely on the message content (e.g. "[CTL]") to provide context.
	Info(msg)
	return len(p), nil
}

// RedirectStdLog configures the standard 'log' package to write to our structured logger.
// This ensures consistency in format and capture.
func RedirectStdLog() {
	log.SetFlags(0) // Disable timestamp/date in standard logger (managed by slog)
	log.SetOutput(&logBridge{})
}
