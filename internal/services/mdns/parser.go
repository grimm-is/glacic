package mdns

import (
	"encoding/hex"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// ParsedMDNS contains extracted device information from an mDNS packet
type ParsedMDNS struct {
	SrcMAC     string
	SrcIP      string
	Interface  string
	Hostname   string
	Services   []string
	TXTRecords map[string]string
}

// ParseMDNSPacket extracts device information from raw mDNS packet data
// srcMAC should be obtained from the L2 header if available
func ParseMDNSPacket(data []byte, srcIP net.IP, iface string) (*ParsedMDNS, error) {
	var parser dnsmessage.Parser
	hdr, err := parser.Start(data)
	if err != nil {
		return nil, err
	}

	result := &ParsedMDNS{
		SrcIP:      srcIP.String(),
		Interface:  iface,
		Services:   []string{},
		TXTRecords: make(map[string]string),
	}

	// Skip questions
	if err := parser.SkipAllQuestions(); err != nil {
		return nil, err
	}

	// Parse answers - this is where service announcements are
	for {
		rr, err := parser.Answer()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			break
		}
		extractRecord(rr, result)
	}

	// Parse authority section
	for {
		rr, err := parser.Authority()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			break
		}
		extractRecord(rr, result)
	}

	// Parse additional section
	for {
		rr, err := parser.Additional()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			break
		}
		extractRecord(rr, result)
	}

	// Only include response flag check if header indicates it's a response
	if !hdr.Response {
		// This is a query, not an announcement - less useful for profiling
		// but we can still extract service types being queried
	}

	return result, nil
}

func extractRecord(rr dnsmessage.Resource, result *ParsedMDNS) {
	name := rr.Header.Name.String()

	switch body := rr.Body.(type) {
	case *dnsmessage.PTRResource:
		// Service discovery: _services._dns-sd._udp.local -> service types
		// Or service instance: _googlecast._tcp.local -> device instance
		ptr := body.PTR.String()
		if strings.Contains(name, "_tcp") || strings.Contains(name, "_udp") {
			// This is a service type
			svc := extractServiceType(name)
			if svc != "" && !containsStr(result.Services, svc) {
				result.Services = append(result.Services, svc)
			}
		}
		// PTR might also point to a hostname
		if strings.HasSuffix(ptr, ".local.") && !strings.Contains(ptr, "_") {
			result.Hostname = strings.TrimSuffix(ptr, ".local.")
		}

	case *dnsmessage.AResource:
		// A record: hostname.local -> IP
		if strings.HasSuffix(name, ".local.") && !strings.Contains(name, "_") {
			result.Hostname = strings.TrimSuffix(name, ".local.")
		}

	case *dnsmessage.AAAAResource:
		// AAAA record: hostname.local -> IPv6
		if strings.HasSuffix(name, ".local.") && !strings.Contains(name, "_") {
			result.Hostname = strings.TrimSuffix(name, ".local.")
		}

	case *dnsmessage.SRVResource:
		// SRV record: _service._proto.local -> target:port
		svc := extractServiceType(name)
		if svc != "" && !containsStr(result.Services, svc) {
			result.Services = append(result.Services, svc)
		}
		// Target is often the hostname
		target := body.Target.String()
		if strings.HasSuffix(target, ".local.") && !strings.Contains(target, "_") {
			result.Hostname = strings.TrimSuffix(target, ".local.")
		}

	case *dnsmessage.TXTResource:
		// TXT records contain key=value metadata
		for _, txt := range body.TXT {
			// Try to parse as key=value
			if idx := strings.Index(txt, "="); idx > 0 {
				key := txt[:idx]
				value := txt[idx+1:]
				result.TXTRecords[key] = value
			} else {
				// Just a value, use hex of first few bytes as key
				key := "txt_" + hex.EncodeToString([]byte(txt[:min(4, len(txt))]))
				result.TXTRecords[key] = txt
			}
		}

		// Also extract service from name
		svc := extractServiceType(name)
		if svc != "" && !containsStr(result.Services, svc) {
			result.Services = append(result.Services, svc)
		}
	}
}

// extractServiceType extracts _service._proto from a DNS name
func extractServiceType(name string) string {
	// Examples:
	// "_googlecast._tcp.local." -> "_googlecast._tcp"
	// "My Chromecast._googlecast._tcp.local." -> "_googlecast._tcp"
	parts := strings.Split(name, ".")
	for i, part := range parts {
		if strings.HasPrefix(part, "_") && i+1 < len(parts) {
			next := parts[i+1]
			if next == "_tcp" || next == "_udp" {
				return part + "." + next
			}
		}
	}
	return ""
}

func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MDNSEventCollector can receive mDNS events for device profiling
type MDNSEventCollector interface {
	SendMDNS(timestamp time.Time, srcMAC, srcIP, iface, hostname string, services []string, txtRecords map[string]string)
}
