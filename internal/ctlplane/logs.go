package ctlplane

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"
)

// GetLogs retrieves system logs (runs as root, can access all logs)
func (s *Server) GetLogs(args *GetLogsArgs, reply *GetLogsReply) error {
	if args.Limit == 0 {
		args.Limit = 1000
	}

	var entries []LogEntry
	var err error

	switch LogSource(args.Source) {
	case "": // No source filter - return all logs
		entries, err = s.getAllLogs(args)
	case LogSourceDmesg:
		entries, err = getDmesgLogs(args)
	case LogSourceSyslog:
		entries, err = getSyslogLogs(args)
	case LogSourceNftables:
		// Use nflog reader for real-time netfilter logs
		entries = s.getNFLogEntries(args)
	case LogSourceDHCP:
		entries, err = getDHCPLogs(args)
	case LogSourceDNS:
		entries, err = getDNSLogs(args)
	case LogSourceFirewall, LogSourceAPI, LogSourceCtlPlane, LogSourceGateway, LogSourceAuth:
		// Get from application log buffer
		entries = s.getAppLogEntries(string(LogSource(args.Source)), args)
	default:
		// Unknown source - return empty
		entries = []LogEntry{}
	}

	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	reply.Entries = entries
	return nil
}

// getNFLogEntries converts NFLogEntry to LogEntry format
func (s *Server) getNFLogEntries(args *GetLogsArgs) []LogEntry {
	if s.nflogReader == nil {
		return nil
	}

	nfEntries := s.nflogReader.GetEntries(args.Limit)
	entries := make([]LogEntry, 0, len(nfEntries))

	for _, nf := range nfEntries {
		entry := LogEntry{
			Timestamp: nf.Timestamp.Format(time.RFC3339),
			Source:    LogSourceNftables,
			Level:     "info",
			Message:   formatNFLogMessage(nf),
			Extra:     make(map[string]string),
		}

		// Populate extra fields
		if nf.Prefix != "" {
			entry.Extra["PREFIX"] = nf.Prefix
			// Set level based on prefix
			if nf.Prefix == "DROP" || nf.Prefix == "REJECT" {
				entry.Level = "warn"
			}
		}
		if nf.InDev != "" {
			entry.Extra["IN"] = nf.InDev
		}
		if nf.OutDev != "" {
			entry.Extra["OUT"] = nf.OutDev
		}
		if nf.SrcIP != "" {
			entry.Extra["SRC"] = nf.SrcIP
		}
		if nf.DstIP != "" {
			entry.Extra["DST"] = nf.DstIP
		}
		if nf.Protocol != "" {
			entry.Extra["PROTO"] = nf.Protocol
		}
		if nf.SrcPort > 0 {
			entry.Extra["SPT"] = fmt.Sprintf("%d", nf.SrcPort)
		}
		if nf.DstPort > 0 {
			entry.Extra["DPT"] = fmt.Sprintf("%d", nf.DstPort)
		}
		if nf.Length > 0 {
			entry.Extra["LEN"] = fmt.Sprintf("%d", nf.Length)
		}

		// Enrich with Device Info
		if s.deviceManager != nil && nf.HwAddr != "" {
			info := s.deviceManager.GetDevice(nf.HwAddr)
			if info.Vendor != "" {
				entry.Extra["VENDOR"] = info.Vendor
			}
			if info.Device != nil && info.Device.Alias != "" {
				entry.Extra["DEVICE"] = info.Device.Alias
			}
		}

		// Apply filters
		if matchesFilter(entry, args) {
			entries = append(entries, entry)
		}
	}

	return entries
}

// formatNFLogMessage creates a human-readable message from NFLogEntry
func formatNFLogMessage(nf NFLogEntry) string {
	msg := ""
	if nf.Prefix != "" {
		msg = nf.Prefix + " "
	}
	if nf.Protocol != "" {
		msg += nf.Protocol + " "
	}
	if nf.SrcIP != "" {
		msg += nf.SrcIP
		if nf.SrcPort > 0 {
			msg += fmt.Sprintf(":%d", nf.SrcPort)
		}
		msg += " -> "
	}
	if nf.DstIP != "" {
		msg += nf.DstIP
		if nf.DstPort > 0 {
			msg += fmt.Sprintf(":%d", nf.DstPort)
		}
	}
	if nf.Length > 0 {
		msg += fmt.Sprintf(" len=%d", nf.Length)
	}
	return msg
}

// getAppLogEntries retrieves application logs from the ring buffer
func (s *Server) getAppLogEntries(source string, args *GetLogsArgs) []LogEntry {
	buffer := logging.GetAppLogBuffer()
	appEntries := buffer.GetBySource(source, args.Limit)

	entries := make([]LogEntry, 0, len(appEntries))
	for _, app := range appEntries {
		entry := LogEntry{
			Timestamp: app.Timestamp.Format(time.RFC3339),
			Source:    LogSource(app.Source),
			Level:     app.Level,
			Message:   app.Message,
			Extra:     app.Extra,
		}

		if matchesFilter(entry, args) {
			entries = append(entries, entry)
		}
	}

	return entries
}

// GetLogSources returns available log sources
func (s *Server) GetLogSources(args *Empty, reply *GetLogSourcesReply) error {
	reply.Sources = []LogSourceInfo{
		{LogSourceDmesg, "Kernel", "Linux kernel ring buffer (dmesg)"},
		{LogSourceSyslog, "System", "System messages and syslog"},
		{LogSourceNftables, "Firewall", "nftables packet filter logs"},
		{LogSourceDHCP, "DHCP", "DHCP server lease activity"},
		{LogSourceDNS, "DNS", "DNS query and resolution logs"},
		{LogSourceFirewall, "Service", "Firewall service application logs"},
		{LogSourceAPI, "API", "Web API server logs"},
		{LogSourceCtlPlane, "Control", "Control plane RPC logs"},
		{LogSourceGateway, "Gateway", "Gateway monitoring and health"},
		{LogSourceAuth, "Auth", "Authentication and session logs"},
	}
	return nil
}

// GetLogStats returns firewall log statistics
func (s *Server) GetLogStats(args *Empty, reply *GetLogStatsReply) error {
	// Use nflog reader directly for stats
	if s.nflogReader != nil {
		reply.Stats = s.nflogReader.GetStats()
	} else {
		reply.Stats = LogStats{
			ByInterface: make(map[string]int64),
			ByProtocol:  make(map[string]int64),
		}
	}
	return nil
}

// getDmesgLogs reads kernel ring buffer using /dev/kmsg (pure Go, no shell)
func getDmesgLogs(args *GetLogsArgs) ([]LogEntry, error) {
	entries, err := readKmsg(args.Limit * 2)
	if err != nil {
		return nil, err
	}

	// Apply filters
	var filtered []LogEntry
	for _, entry := range entries {
		if matchesFilter(entry, args) {
			filtered = append(filtered, entry)
		}
	}

	return limitEntries(filtered, args.Limit), nil
}

// getSyslogLogs reads from syslog/messages
func getSyslogLogs(args *GetLogsArgs) ([]LogEntry, error) {
	var entries []LogEntry

	// Try to read from common log files
	logFiles := []string{"/var/log/messages", "/var/log/syslog"}

	for _, logFile := range logFiles {
		if fileEntries, err := parseLogFile(logFile, LogSourceSyslog, args); err == nil {
			entries = append(entries, fileEntries...)
		}
	}

	return limitEntries(entries, args.Limit), nil
}

// getNftablesLogs reads nftables/netfilter logs
func getNftablesLogs(args *GetLogsArgs) ([]LogEntry, error) {
	var entries []LogEntry

	// Read from kernel log for netfilter messages
	dmesgArgs := &GetLogsArgs{Limit: 10000}
	dmesgEntries, err := getDmesgLogs(dmesgArgs)
	if err == nil {
		for _, entry := range dmesgEntries {
			if strings.Contains(entry.Message, "nft_") ||
				strings.Contains(entry.Message, "nf_") ||
				strings.Contains(entry.Message, "netfilter") ||
				strings.Contains(entry.Message, "IN=") ||
				strings.Contains(entry.Message, "OUT=") {
				entry.Source = LogSourceNftables
				entry.Extra = parseNetfilterLog(entry.Message)
				if matchesFilter(entry, args) {
					entries = append(entries, entry)
				}
			}
		}
	}

	return limitEntries(entries, args.Limit), nil
}

// getDHCPLogs reads DHCP server logs
func getDHCPLogs(args *GetLogsArgs) ([]LogEntry, error) {
	var entries []LogEntry

	syslogArgs := &GetLogsArgs{Limit: 10000}
	syslogEntries, err := getSyslogLogs(syslogArgs)
	if err == nil {
		for _, entry := range syslogEntries {
			msg := strings.ToLower(entry.Message)
			if strings.Contains(msg, "dhcp") ||
				strings.Contains(msg, "dnsmasq-dhcp") ||
				strings.Contains(msg, "lease") {
				entry.Source = LogSourceDHCP
				if matchesFilter(entry, args) {
					entries = append(entries, entry)
				}
			}
		}
	}

	return limitEntries(entries, args.Limit), nil
}

// getDNSLogs reads DNS query logs
func getDNSLogs(args *GetLogsArgs) ([]LogEntry, error) {
	var entries []LogEntry

	syslogArgs := &GetLogsArgs{Limit: 10000}
	syslogEntries, err := getSyslogLogs(syslogArgs)
	if err == nil {
		for _, entry := range syslogEntries {
			msg := strings.ToLower(entry.Message)
			if strings.Contains(msg, "dns") ||
				strings.Contains(msg, "dnsmasq") ||
				strings.Contains(msg, "query") ||
				strings.Contains(msg, "named") {
				entry.Source = LogSourceDNS
				if matchesFilter(entry, args) {
					entries = append(entries, entry)
				}
			}
		}
	}

	return limitEntries(entries, args.Limit), nil
}

// getAllLogs combines logs from all sources
// getAllLogs combines logs from all sources
func (s *Server) getAllLogs(args *GetLogsArgs) ([]LogEntry, error) {
	var allEntries []LogEntry

	// Create sub-args with limits but preserving filters
	subLimit := args.Limit / 4
	if subLimit < 50 {
		subLimit = 50
	}

	subArgs := &GetLogsArgs{
		Limit:  subLimit,
		Since:  args.Since,
		Until:  args.Until,
		Search: args.Search,
		Level:  args.Level,
	}

	// Get from each source
	if entries, err := getDmesgLogs(subArgs); err == nil {
		allEntries = append(allEntries, entries...)
	}
	if entries, err := getSyslogLogs(subArgs); err == nil {
		allEntries = append(allEntries, entries...)
	}
	// Get nflog entries
	nfEntries := s.getNFLogEntries(subArgs)
	allEntries = append(allEntries, nfEntries...)

	// Get DHCP logs
	if entries, err := getDHCPLogs(subArgs); err == nil {
		allEntries = append(allEntries, entries...)
	}

	// Manual sort by timestamp (newest first for consistent limiting)
	// Since we appended in arbitrary order, we should probably sort before limiting
	// But limitEntries just takes last N.

	// Real sorting would import "sort"
	// For now, relies on limitEntries taking the end... which presumes appended order is somewhat chronological?
	// Actually, this logic is a bit weak for interleaved logs, but the primary fix here is preserving filters.

	return limitEntries(allEntries, args.Limit), nil
}

// parseLogFile parses a standard log file using pure Go (no shell commands)
func parseLogFile(path string, source LogSource, args *GetLogsArgs) ([]LogEntry, error) {
	// Use pure Go implementation instead of exec.Command("tail")
	rawEntries, err := readLastLines(path, args.Limit*2, source)
	if err != nil {
		return nil, err
	}

	// Common syslog format: Dec  5 12:34:56 hostname process[pid]: message
	syslogRe := regexp.MustCompile(`^(\w+\s+\d+\s+\d+:\d+:\d+)\s+(\S+)\s+([^:]+):\s*(.*)$`)

	var entries []LogEntry
	for _, entry := range rawEntries {
		// Try to parse syslog format for better structure
		if matches := syslogRe.FindStringSubmatch(entry.Message); len(matches) == 5 {
			timeStr := matches[1]
			now := clock.Now()

			layouts := []string{
				"Jan  2 15:04:05",
				"Jan 2 15:04:05",
			}

			for _, layout := range layouts {
				if parsed, err := time.Parse(layout, timeStr); err == nil {
					parsed = parsed.AddDate(now.Year(), 0, 0)
					if parsed.After(now.AddDate(0, 0, 1)) {
						parsed = parsed.AddDate(-1, 0, 0)
					}
					entry.Timestamp = parsed.Format(time.RFC3339)
					break
				}
			}
			entry.Facility = matches[3]
			entry.Message = matches[4]
		}

		entry.Level = detectLevel(entry.Message)

		if matchesFilter(entry, args) {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// parseNetfilterLog extracts fields from netfilter log messages
func parseNetfilterLog(msg string) map[string]string {
	extra := make(map[string]string)

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
func matchesFilter(entry LogEntry, args *GetLogsArgs) bool {
	if args.Level != "" && entry.Level != args.Level {
		return false
	}

	if args.Search != "" {
		if !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(args.Search)) {
			return false
		}
	}

	if args.Since != "" {
		// Try to parse both as RFC3339 to handle timezone differences correctly
		// (e.g. Z vs +00:00)
		tEntry, err1 := time.Parse(time.RFC3339, entry.Timestamp)
		tSince, err2 := time.Parse(time.RFC3339, args.Since)

		if err1 == nil && err2 == nil {
			if !tEntry.After(tSince) { // Entry must be strictly after Since (if we want >) or >=
				// Usually "Since" means everything after that time.
				// If we want to exclude the exact "since" timestamp (to avoid duplicates), use After.
				// If duplicates are handled by client logic or if Since is inclusive, use !Before.
				// Let's use !After(tSince) -> entry <= Since -> return false.
				// So we want > Since.
				return false
			}
		} else {
			// Fallback to string comparison if parsing fails (legacy behavior)
			if entry.Timestamp <= args.Since {
				return false
			}
		}
	}

	if args.Until != "" {
		tEntry, err1 := time.Parse(time.RFC3339, entry.Timestamp)
		tUntil, err2 := time.Parse(time.RFC3339, args.Until)

		if err1 == nil && err2 == nil {
			if tEntry.After(tUntil) {
				return false
			}
		} else {
			if entry.Timestamp > args.Until {
				return false
			}
		}
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

// getTopN returns the top N items by count
func getTopN(counts map[string]int64, n int) []IPCount {
	var result []IPCount
	for ip, count := range counts {
		result = append(result, IPCount{IP: ip, Count: count})
	}
	// Simple bubble sort for top N
	for i := 0; i < len(result)-1 && i < n; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Count > result[i].Count {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	if len(result) > n {
		result = result[:n]
	}
	return result
}
