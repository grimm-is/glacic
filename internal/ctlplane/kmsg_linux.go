//go:build linux

package ctlplane

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"grimm.is/glacic/internal/clock"
)

// readKmsg reads kernel messages from /dev/kmsg (Linux kernel ring buffer).
// This provides the same information as `dmesg` without spawning a process.
// Format: priority,seq,timestamp,-;message
// Example: 6,1234,12345678901,-;Linux version 6.x.x ...
func readKmsg(limit int) ([]LogEntry, error) {
	// Open /dev/kmsg in non-blocking read mode
	// Note: /dev/kmsg provides real-time streaming; we read what's available
	f, err := os.Open("/dev/kmsg")
	if err != nil {
		// Permission denied or not available - fall back to /var/log/dmesg if exists
		return readDmesgFile(limit)
	}
	defer f.Close()

	entries := make([]LogEntry, 0, limit)
	scanner := bufio.NewScanner(f)

	// Set a reasonable buffer size for kernel messages
	buf := make([]byte, 8192)
	scanner.Buffer(buf, 64*1024)

	// Pattern: priority,sequence,timestamp,flags;message
	kmsgRe := regexp.MustCompile(`^(\d+),(\d+),(\d+),[^;]*;(.*)$`)

	// Boot time for calculating absolute timestamps
	bootTime := getBootTime()

	count := 0
	for scanner.Scan() && count < limit*2 {
		line := scanner.Text()
		if line == "" {
			continue
		}

		entry := LogEntry{
			Source: LogSourceDmesg,
			Level:  "info",
		}

		if matches := kmsgRe.FindStringSubmatch(line); len(matches) == 5 {
			// Parse priority (3 bits of facility + 3 bits of level)
			if prio, err := strconv.Atoi(matches[1]); err == nil {
				entry.Level = kmsgPriorityToLevel(prio & 7)
			}

			// Parse timestamp (microseconds since boot)
			if usec, err := strconv.ParseInt(matches[3], 10, 64); err == nil {
				ts := bootTime.Add(time.Duration(usec) * time.Microsecond)
				entry.Timestamp = ts.Format(time.RFC3339)
			} else {
				entry.Timestamp = clock.Now().Format(time.RFC3339)
			}

			entry.Message = matches[4]
		} else {
			// Continuation line or unparseable
			entry.Message = line
			entry.Timestamp = clock.Now().Format(time.RFC3339)
		}

		entries = append(entries, entry)
		count++
	}

	// Return last N entries (most recent)
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	return entries, nil
}

// readDmesgFile reads from /var/log/dmesg as fallback
func readDmesgFile(limit int) ([]LogEntry, error) {
	return readLastLines("/var/log/dmesg", limit, LogSourceDmesg)
}

// kmsgPriorityToLevel converts kernel log priority to our level string
func kmsgPriorityToLevel(priority int) string {
	switch priority {
	case 0, 1, 2: // EMERG, ALERT, CRIT
		return "error"
	case 3: // ERR
		return "error"
	case 4: // WARNING
		return "warn"
	case 5, 6: // NOTICE, INFO
		return "info"
	case 7: // DEBUG
		return "debug"
	default:
		return "info"
	}
}

// getBootTime returns the system boot time
func getBootTime() time.Time {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Now()
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "btime ") {
			if btime, err := strconv.ParseInt(strings.TrimPrefix(line, "btime "), 10, 64); err == nil {
				return time.Unix(btime, 0)
			}
		}
	}

	return time.Now()
}

// readLastLines reads the last N lines from a file efficiently without using `tail` command.
// This is a pure Go replacement for exec.Command("tail", "-n", N, path).
func readLastLines(path string, n int, source LogSource) ([]LogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	// Get file size
	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	// For small files, just read the whole thing
	size := stat.Size()
	if size == 0 {
		return nil, nil
	}

	// Read from end of file, looking for newlines
	// Start with a reasonable chunk size
	chunkSize := int64(8192)
	if chunkSize > size {
		chunkSize = size
	}

	var lines []string
	offset := size

	for len(lines) < n+1 && offset > 0 {
		// Calculate read position
		readSize := chunkSize
		if offset < chunkSize {
			readSize = offset
		}
		offset -= readSize

		// Read chunk
		buf := make([]byte, readSize)
		_, err := f.ReadAt(buf, offset)
		if err != nil {
			break
		}

		// Prepend to lines
		chunk := string(buf)
		chunkLines := strings.Split(chunk, "\n")

		// If we have existing lines, merge the first with last of chunk
		if len(lines) > 0 && len(chunkLines) > 0 {
			chunkLines[len(chunkLines)-1] += lines[0]
			lines = lines[1:]
		}

		lines = append(chunkLines, lines...)
	}

	// Take last N non-empty lines
	var entries []LogEntry
	count := 0
	for i := len(lines) - 1; i >= 0 && count < n; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		entries = append([]LogEntry{{
			Source:    source,
			Level:     detectLevel(line),
			Message:   line,
			Timestamp: clock.Now().Format(time.RFC3339),
		}}, entries...)
		count++
	}

	return entries, nil
}
