// Package logging provides system log access and management.
package logging

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"grimm.is/glacic/internal/clock"
)

// LogSource represents a log source type
type LogSource string

const (
	LogSourceDmesg    LogSource = "dmesg"
	LogSourceSyslog   LogSource = "syslog"
	LogSourceFirewall LogSource = "firewall"
	LogSourceNftables LogSource = "nftables"
	LogSourceDHCP     LogSource = "dhcp"
	LogSourceDNS      LogSource = "dns"
	LogSourceAPI      LogSource = "api"
	LogSourceCtlPlane LogSource = "ctlplane"
	LogSourceGateway  LogSource = "gateway"
	LogSourceAuth     LogSource = "auth"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	Source    LogSource         `json:"source"`
	Level     string            `json:"level"` // debug, info, warn, error
	Message   string            `json:"message"`
	Facility  string            `json:"facility,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// LogFilter specifies filtering options for log queries
type LogFilter struct {
	Source LogSource `json:"source,omitempty"`
	Level  string    `json:"level,omitempty"`
	Search string    `json:"search,omitempty"`
	Since  time.Time `json:"since,omitempty"`
	Until  time.Time `json:"until,omitempty"`
	Limit  int       `json:"limit,omitempty"`
}

// LogReader provides access to system logs
type LogReader struct {
	logDir string
}

// NewLogReader creates a new log reader
func NewLogReader() *LogReader {
	return &LogReader{
		logDir: "/var/log",
	}
}

// GetLogs retrieves logs based on the filter
func (r *LogReader) GetLogs(filter LogFilter) ([]LogEntry, error) {
	if filter.Limit == 0 {
		filter.Limit = 500
	}

	switch filter.Source {
	case LogSourceDmesg:
		return r.getDmesgLogs(filter)
	case LogSourceSyslog:
		return r.getSyslogLogs(filter)
	case LogSourceNftables:
		return r.getNftablesLogs(filter)
	case LogSourceDHCP:
		return r.getDHCPLogs(filter)
	case LogSourceDNS:
		return r.getDNSLogs(filter)
	case LogSourceFirewall, LogSourceAPI, LogSourceCtlPlane, LogSourceGateway, LogSourceAuth:
		return r.getServiceLogs(filter)
	default:
		return r.getAllLogs(filter)
	}
}

// getDmesgLogs reads kernel ring buffer
func (r *LogReader) getDmesgLogs(filter LogFilter) ([]LogEntry, error) {
	cmd := exec.Command("dmesg", "-T", "--level=emerg,alert,crit,err,warn,notice,info,debug")
	output, err := cmd.Output()
	if err != nil {
		// Try without -T flag (older systems)
		cmd = exec.Command("dmesg")
		output, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("failed to read dmesg: %w", err)
		}
	}

	var entries []LogEntry
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	// Pattern for dmesg with timestamp: [Thu Dec  5 12:34:56 2024] message
	timestampRe := regexp.MustCompile(`^\[([^\]]+)\]\s*(.*)$`)
	// Pattern for dmesg with numeric timestamp: [12345.678901] message
	numericRe := regexp.MustCompile(`^\[\s*(\d+\.\d+)\]\s*(.*)$`)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		entry := LogEntry{
			Source:    LogSourceDmesg,
			Level:     "info",
			Timestamp: clock.Now(),
		}

		if matches := timestampRe.FindStringSubmatch(line); len(matches) == 3 {
			if t, err := time.Parse("Mon Jan  2 15:04:05 2006", matches[1]); err == nil {
				entry.Timestamp = t
			}
			entry.Message = matches[2]
		} else if matches := numericRe.FindStringSubmatch(line); len(matches) == 3 {
			entry.Message = matches[2]
		} else {
			entry.Message = line
		}

		// Detect log level from message content
		entry.Level = detectLevel(entry.Message)

		if matchesFilter(entry, filter) {
			entries = append(entries, entry)
		}
	}

	return limitEntries(entries, filter.Limit), nil
}

// getSyslogLogs reads from syslog/messages
func (r *LogReader) getSyslogLogs(filter LogFilter) ([]LogEntry, error) {
	// Try common syslog locations
	logFiles := []string{
		"/var/log/messages",
		"/var/log/syslog",
	}

	var entries []LogEntry
	for _, logFile := range logFiles {
		if fileEntries, err := r.parseLogFile(logFile, LogSourceSyslog, filter); err == nil {
			entries = append(entries, fileEntries...)
		}
	}

	return limitEntries(entries, filter.Limit), nil
}

// getNftablesLogs reads nftables/netfilter logs
func (r *LogReader) getNftablesLogs(filter LogFilter) ([]LogEntry, error) {
	var entries []LogEntry

	// Read from kernel log for netfilter messages
	dmesgEntries, err := r.getDmesgLogs(LogFilter{Limit: 10000})
	if err == nil {
		for _, entry := range dmesgEntries {
			// Filter for nftables/netfilter messages
			if strings.Contains(entry.Message, "nft_") ||
				strings.Contains(entry.Message, "nf_") ||
				strings.Contains(entry.Message, "netfilter") ||
				strings.Contains(entry.Message, "IN=") ||
				strings.Contains(entry.Message, "OUT=") {
				entry.Source = LogSourceNftables
				entry.Extra = parseNetfilterLog(entry.Message)
				if matchesFilter(entry, filter) {
					entries = append(entries, entry)
				}
			}
		}
	}

	// Also check /var/log/ulog if it exists (ulogd)
	if fileEntries, err := r.parseLogFile("/var/log/ulog/syslogemu.log", LogSourceNftables, filter); err == nil {
		entries = append(entries, fileEntries...)
	}

	return limitEntries(entries, filter.Limit), nil
}

// getDHCPLogs reads DHCP server logs
func (r *LogReader) getDHCPLogs(filter LogFilter) ([]LogEntry, error) {
	var entries []LogEntry

	// Check syslog for dhcp messages
	syslogEntries, err := r.getSyslogLogs(LogFilter{Limit: 10000})
	if err == nil {
		for _, entry := range syslogEntries {
			msg := strings.ToLower(entry.Message)
			if strings.Contains(msg, "dhcp") ||
				strings.Contains(msg, "dnsmasq-dhcp") ||
				strings.Contains(msg, "lease") {
				entry.Source = LogSourceDHCP
				if matchesFilter(entry, filter) {
					entries = append(entries, entry)
				}
			}
		}
	}

	return limitEntries(entries, filter.Limit), nil
}

// getDNSLogs reads DNS query logs
func (r *LogReader) getDNSLogs(filter LogFilter) ([]LogEntry, error) {
	var entries []LogEntry

	// Check syslog for DNS messages
	syslogEntries, err := r.getSyslogLogs(LogFilter{Limit: 10000})
	if err == nil {
		for _, entry := range syslogEntries {
			msg := strings.ToLower(entry.Message)
			if strings.Contains(msg, "dns") ||
				strings.Contains(msg, "dnsmasq") ||
				strings.Contains(msg, "query") ||
				strings.Contains(msg, "named") {
				entry.Source = LogSourceDNS
				if matchesFilter(entry, filter) {
					entries = append(entries, entry)
				}
			}
		}
	}

	// Check dnsmasq log if it exists
	if fileEntries, err := r.parseLogFile("/var/log/dnsmasq.log", LogSourceDNS, filter); err == nil {
		entries = append(entries, fileEntries...)
	}

	return limitEntries(entries, filter.Limit), nil
}

// getServiceLogs reads firewall service logs
func (r *LogReader) getServiceLogs(filter LogFilter) ([]LogEntry, error) {
	var entries []LogEntry

	// Read from our application log file
	logFile := "/var/log/glacic/glacic.log"
	if fileEntries, err := r.parseLogFile(logFile, filter.Source, filter); err == nil {
		entries = append(entries, fileEntries...)
	}

	// Also check syslog for our service messages
	syslogEntries, err := r.getSyslogLogs(LogFilter{Limit: 10000})
	if err == nil {
		for _, entry := range syslogEntries {
			if strings.Contains(entry.Message, "firewall") ||
				strings.Contains(entry.Message, "ctlplane") ||
				strings.Contains(entry.Message, "api-server") {
				entry.Source = filter.Source
				if matchesFilter(entry, filter) {
					entries = append(entries, entry)
				}
			}
		}
	}

	return limitEntries(entries, filter.Limit), nil
}

// getAllLogs combines logs from all sources
func (r *LogReader) getAllLogs(filter LogFilter) ([]LogEntry, error) {
	var allEntries []LogEntry

	sources := []LogSource{
		LogSourceDmesg,
		LogSourceSyslog,
		LogSourceNftables,
		LogSourceDHCP,
		LogSourceDNS,
	}

	for _, source := range sources {
		sourceFilter := filter
		sourceFilter.Source = source
		sourceFilter.Limit = filter.Limit / len(sources)
		if entries, err := r.GetLogs(sourceFilter); err == nil {
			allEntries = append(allEntries, entries...)
		}
	}

	// Sort by timestamp (newest first)
	sortByTimestamp(allEntries)

	return limitEntries(allEntries, filter.Limit), nil
}

// parseLogFile parses a standard log file
func (r *LogReader) parseLogFile(path string, source LogSource, filter LogFilter) ([]LogEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(file)

	// Common syslog format: Dec  5 12:34:56 hostname process[pid]: message
	syslogRe := regexp.MustCompile(`^(\w+\s+\d+\s+\d+:\d+:\d+)\s+(\S+)\s+([^:]+):\s*(.*)$`)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		entry := LogEntry{
			Source:    source,
			Level:     "info",
			Timestamp: clock.Now(),
		}

		if matches := syslogRe.FindStringSubmatch(line); len(matches) == 5 {
			// Parse timestamp (add current year)
			timeStr := matches[1] + " " + strconv.Itoa(clock.Now().Year())
			if t, err := time.Parse("Jan  2 15:04:05 2006", timeStr); err == nil {
				entry.Timestamp = t
			}
			entry.Facility = matches[3]
			entry.Message = matches[4]
		} else {
			entry.Message = line
		}

		entry.Level = detectLevel(entry.Message)

		if matchesFilter(entry, filter) {
			entries = append(entries, entry)
		}
	}

	return entries, scanner.Err()
}

// parseNetfilterLog extracts fields from netfilter log messages
func parseNetfilterLog(msg string) map[string]string {
	extra := make(map[string]string)

	// Parse key=value pairs
	re := regexp.MustCompile(`(\w+)=(\S+)`)
	matches := re.FindAllStringSubmatch(msg, -1)
	for _, match := range matches {
		if len(match) == 3 {
			extra[match[1]] = match[2]
		}
	}

	return extra
}

// detectLevel detects log level from message content
func detectLevel(msg string) string {
	lower := strings.ToLower(msg)

	if strings.Contains(lower, "error") || strings.Contains(lower, "fail") ||
		strings.Contains(lower, "crit") || strings.Contains(lower, "emerg") {
		return "error"
	}
	if strings.Contains(lower, "warn") {
		return "warn"
	}
	if strings.Contains(lower, "debug") {
		return "debug"
	}
	return "info"
}

// matchesFilter checks if an entry matches the filter criteria
func matchesFilter(entry LogEntry, filter LogFilter) bool {
	// Level filter
	if filter.Level != "" && entry.Level != filter.Level {
		return false
	}

	// Search filter
	if filter.Search != "" {
		if !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(filter.Search)) {
			return false
		}
	}

	// Time filters
	if !filter.Since.IsZero() && entry.Timestamp.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && entry.Timestamp.After(filter.Until) {
		return false
	}

	return true
}

// limitEntries returns the last N entries
func limitEntries(entries []LogEntry, limit int) []LogEntry {
	if len(entries) <= limit {
		return entries
	}
	return entries[len(entries)-limit:]
}

// sortByTimestamp sorts entries by timestamp (newest first)
func sortByTimestamp(entries []LogEntry) {
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].Timestamp.Before(entries[j].Timestamp) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}

// GetAvailableSources returns list of available log sources
func (r *LogReader) GetAvailableSources() []LogSource {
	return []LogSource{
		LogSourceDmesg,
		LogSourceSyslog,
		LogSourceNftables,
		LogSourceDHCP,
		LogSourceDNS,
		LogSourceFirewall,
		LogSourceAPI,
		LogSourceCtlPlane,
		LogSourceGateway,
		LogSourceAuth,
	}
}

// LogSourceInfo provides metadata about a log source
type LogSourceInfo struct {
	ID          LogSource `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
}

// GetSourceInfo returns metadata for all log sources
func GetSourceInfo() []LogSourceInfo {
	return []LogSourceInfo{
		{LogSourceDmesg, "Kernel", "Linux kernel ring buffer (dmesg)", "cpu"},
		{LogSourceSyslog, "System", "System messages and syslog", "server"},
		{LogSourceNftables, "Firewall", "nftables packet filter logs", "shield"},
		{LogSourceDHCP, "DHCP", "DHCP server lease activity", "network"},
		{LogSourceDNS, "DNS", "DNS query and resolution logs", "globe"},
		{LogSourceFirewall, "Service", "Firewall service application logs", "activity"},
		{LogSourceAPI, "API", "Web API server logs", "terminal"},
		{LogSourceCtlPlane, "Control", "Control plane RPC logs", "settings"},
		{LogSourceGateway, "Gateway", "Gateway monitoring and health", "router"},
		{LogSourceAuth, "Auth", "Authentication and session logs", "lock"},
	}
}
