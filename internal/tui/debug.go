package tui

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	debugFile *os.File
	debugMu   sync.Mutex
)

// EnableDebugLogging opens the debug log file
func EnableDebugLogging(path string) error {
	debugMu.Lock()
	defer debugMu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	debugFile = f
	return nil
}

// DebugLog writes a formatted message to the debug log if enabled
func DebugLog(format string, args ...interface{}) {
	debugMu.Lock()
	defer debugMu.Unlock()

	if debugFile == nil {
		return
	}

	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format(time.RFC3339)
	fmt.Fprintf(debugFile, "[%s] %s\n", timestamp, msg)
}

// CloseDebugLog closes the log file
func CloseDebugLog() {
	debugMu.Lock()
	defer debugMu.Unlock()

	if debugFile != nil {
		debugFile.Close()
		debugFile = nil
	}
}
