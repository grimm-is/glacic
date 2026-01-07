package ctlplane

import (
	"context"
	"encoding/binary"
	"fmt"
	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/learning"
	"log"
	"net"
	"sync"
	"time"

	"github.com/florianl/go-nflog/v2"
)

// NFLogGroup is the nflog group we listen on (must match nftables rules)
const NFLogGroup = 100

// NFLogEntry represents a parsed netfilter log entry
type NFLogEntry struct {
	Timestamp  time.Time         `json:"timestamp"`
	Prefix     string            `json:"prefix"`
	InDev      string            `json:"in_dev,omitempty"`
	InDevName  string            `json:"in_dev_name,omitempty"`
	OutDev     string            `json:"out_dev,omitempty"`
	OutDevName string            `json:"out_dev_name,omitempty"`
	SrcIP      string            `json:"src_ip,omitempty"`
	DstIP      string            `json:"dst_ip,omitempty"`
	SrcPort    uint16            `json:"src_port,omitempty"`
	DstPort    uint16            `json:"dst_port,omitempty"`
	Protocol   string            `json:"protocol,omitempty"`
	Length     uint32            `json:"length,omitempty"`
	Mark       uint32            `json:"mark,omitempty"`
	HwAddr     string            `json:"hw_addr,omitempty"`
	SrcMAC     string            `json:"src_mac,omitempty"`
	PayloadLen int               `json:"payload_len,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
}

// LogReader is the interface for reading firewall logs
type LogReader interface {
	Start() error
	Stop()
	GetEntries(limit int) []NFLogEntry
	GetStats() LogStats
	Clear()
	Count() int
	Subscribe() <-chan NFLogEntry
}

// NFLogReader reads netfilter logs via nflog
type NFLogReader struct {
	nf      *nflog.Nflog
	entries []NFLogEntry
	mu      sync.RWMutex
	maxSize int
	running bool
	cancel  context.CancelFunc
	subs    []chan NFLogEntry
	subsMu  sync.RWMutex
	group   uint16
}

// NewNFLogReader creates a new nflog reader
func NewNFLogReader(maxSize int, group uint16) *NFLogReader {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &NFLogReader{
		entries: make([]NFLogEntry, 0, maxSize),
		maxSize: maxSize,
		group:   group,
	}
}

// Start begins listening for nflog messages
func (r *NFLogReader) Start() error {
	config := nflog.Config{
		Group:       r.group,
		Copymode:    nflog.CopyPacket,
		ReadTimeout: 10 * time.Millisecond,
	}

	nf, err := nflog.Open(&config)
	if err != nil {
		return fmt.Errorf("failed to open nflog: %w", err)
	}
	r.nf = nf

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true

	// Register callback for log messages
	err = nf.RegisterWithErrorFunc(ctx,
		func(attrs nflog.Attribute) int {
			entry := r.parseAttributes(attrs)
			r.addEntry(entry)
			r.broadcast(entry)
			return 0
		},
		func(err error) int {
			if r.running {
				log.Printf("[NFLOG] Error: %v", err)
			}
			return 0
		},
	)
	if err != nil {
		nf.Close()
		return fmt.Errorf("failed to register nflog callback: %w", err)
	}

	log.Printf("[NFLOG] Listening on group %d", NFLogGroup)
	return nil
}

// Stop stops listening for nflog messages
func (r *NFLogReader) Stop() {
	r.running = false
	if r.cancel != nil {
		r.cancel()
	}
	if r.nf != nil {
		r.nf.Close()
	}
}

// parseAttributes converts nflog attributes to our entry format
func (r *NFLogReader) parseAttributes(attrs nflog.Attribute) NFLogEntry {
	entry := NFLogEntry{
		Timestamp: clock.Now(),
		Extra:     make(map[string]string),
	}

	// Log prefix (set in nftables rule)
	if attrs.Prefix != nil {
		entry.Prefix = *attrs.Prefix
	}

	// Hardware info
	if attrs.HwAddr != nil {
		hw := *attrs.HwAddr
		entry.HwAddr = net.HardwareAddr(hw).String()
		// Extract Source MAC (bytes 6-11) if length is sufficient (14 bytes for Ethernet)
		if len(hw) >= 14 {
			entry.SrcMAC = net.HardwareAddr(hw[6:12]).String()
		}
	}

	// Interface names
	if attrs.InDev != nil {
		entry.InDev = fmt.Sprintf("ifindex:%d", *attrs.InDev)
		if iface, err := net.InterfaceByIndex(int(*attrs.InDev)); err == nil {
			entry.InDevName = iface.Name
		}
	}
	if attrs.OutDev != nil {
		entry.OutDev = fmt.Sprintf("ifindex:%d", *attrs.OutDev)
		if iface, err := net.InterfaceByIndex(int(*attrs.OutDev)); err == nil {
			entry.OutDevName = iface.Name
		}
	}

	// Mark
	if attrs.Mark != nil {
		entry.Mark = *attrs.Mark
	}

	// Parse the packet payload for IP/TCP/UDP info
	if attrs.Payload != nil && len(*attrs.Payload) > 0 {
		entry.PayloadLen = len(*attrs.Payload)
		r.parsePacket(*attrs.Payload, &entry)

		// Check for SNI inspection prefix
		if entry.Prefix == "TLS_SNI: " {
			// Extract TCP payload to find SNI
			// Note: parsePacket populates SrcIP/DstIP etc but doesn't return payload offset.
			// We need to re-parse or extract payload.
			// Simple approach: skip IP/TCP headers manually here or in ParseSNI (ParseSNI expects TCP payload)
			// Let's implement a helper or assume standard offset.
			// Actually parsePacket logic is complex with options.
			// We will try a helper in this file.
			tcpPayload := r.extractTCPPayload(*attrs.Payload)
			if len(tcpPayload) > 0 {
				sni, err := learning.ParseSNI(tcpPayload)
				if err == nil && sni != "" {
					entry.Extra["sni"] = sni
				}
			}
		}
	}

	return entry
}

// extractTCPPayload finds the TCP payload offset
func (r *NFLogReader) extractTCPPayload(packet []byte) []byte {
	if len(packet) < 20 {
		return nil
	}
	version := packet[0] >> 4
	var ipHL int
	var protocol byte
	var ipPayload []byte

	if version == 4 {
		ipHL = int(packet[0]&0x0f) * 4
		if len(packet) < ipHL {
			return nil
		}
		protocol = packet[9]
		ipPayload = packet[ipHL:]
	} else if version == 6 {
		if len(packet) < 40 {
			return nil
		}
		protocol = packet[6]
		ipPayload = packet[40:]
	} else {
		return nil
	}

	if protocol != 6 { // Not TCP
		return nil
	}

	if len(ipPayload) < 20 {
		return nil
	}
	tcpHL := int((ipPayload[12] >> 4) * 4)
	if len(ipPayload) < tcpHL {
		return nil
	}
	return ipPayload[tcpHL:]
}

// parsePacket extracts IP and transport layer info from packet bytes
func (r *NFLogReader) parsePacket(payload []byte, entry *NFLogEntry) {
	if len(payload) < 20 {
		return
	}

	// Check IP version
	version := payload[0] >> 4

	if version == 4 {
		r.parseIPv4(payload, entry)
	} else if version == 6 && len(payload) >= 40 {
		r.parseIPv6(payload, entry)
	}
}

// parseIPv4 parses an IPv4 packet
func (r *NFLogReader) parseIPv4(payload []byte, entry *NFLogEntry) {
	if len(payload) < 20 {
		return
	}

	ihl := int(payload[0]&0x0f) * 4
	if ihl < 20 || len(payload) < ihl {
		return
	}

	entry.Length = uint32(binary.BigEndian.Uint16(payload[2:4]))
	protocol := payload[9]
	entry.SrcIP = net.IP(payload[12:16]).String()
	entry.DstIP = net.IP(payload[16:20]).String()

	switch protocol {
	case 1:
		entry.Protocol = "ICMP"
	case 6:
		entry.Protocol = "TCP"
		if len(payload) >= ihl+4 {
			entry.SrcPort = binary.BigEndian.Uint16(payload[ihl : ihl+2])
			entry.DstPort = binary.BigEndian.Uint16(payload[ihl+2 : ihl+4])
		}
	case 17:
		entry.Protocol = "UDP"
		if len(payload) >= ihl+4 {
			entry.SrcPort = binary.BigEndian.Uint16(payload[ihl : ihl+2])
			entry.DstPort = binary.BigEndian.Uint16(payload[ihl+2 : ihl+4])
		}
	default:
		entry.Protocol = fmt.Sprintf("IP/%d", protocol)
	}
}

// parseIPv6 parses an IPv6 packet
func (r *NFLogReader) parseIPv6(payload []byte, entry *NFLogEntry) {
	if len(payload) < 40 {
		return
	}

	entry.Length = uint32(binary.BigEndian.Uint16(payload[4:6])) + 40
	nextHeader := payload[6]
	entry.SrcIP = net.IP(payload[8:24]).String()
	entry.DstIP = net.IP(payload[24:40]).String()

	switch nextHeader {
	case 6:
		entry.Protocol = "TCP"
		if len(payload) >= 44 {
			entry.SrcPort = binary.BigEndian.Uint16(payload[40:42])
			entry.DstPort = binary.BigEndian.Uint16(payload[42:44])
		}
	case 17:
		entry.Protocol = "UDP"
		if len(payload) >= 44 {
			entry.SrcPort = binary.BigEndian.Uint16(payload[40:42])
			entry.DstPort = binary.BigEndian.Uint16(payload[42:44])
		}
	case 58:
		entry.Protocol = "ICMPv6"
	default:
		entry.Protocol = fmt.Sprintf("IPv6/%d", nextHeader)
	}
}

// addEntry adds an entry to the ring buffer
func (r *NFLogReader) addEntry(entry NFLogEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.entries) >= r.maxSize {
		// Remove oldest entries (keep last 90%)
		cutoff := r.maxSize / 10
		r.entries = r.entries[cutoff:]
	}
	r.entries = append(r.entries, entry)
}

// GetEntries returns recent log entries
func (r *NFLogReader) GetEntries(limit int) []NFLogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if limit <= 0 || limit > len(r.entries) {
		limit = len(r.entries)
	}

	// Return newest entries (from end of slice)
	start := len(r.entries) - limit
	if start < 0 {
		start = 0
	}

	result := make([]NFLogEntry, limit)
	copy(result, r.entries[start:])
	return result
}

// GetStats returns statistics about logged packets
func (r *NFLogReader) GetStats() LogStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := LogStats{
		ByInterface: make(map[string]int64),
		ByProtocol:  make(map[string]int64),
	}

	srcCounts := make(map[string]int64)
	dstCounts := make(map[string]int64)

	for _, entry := range r.entries {
		stats.TotalPackets++

		if entry.InDev != "" {
			stats.ByInterface[entry.InDev]++
		}
		if entry.OutDev != "" {
			stats.ByInterface[entry.OutDev]++
		}
		if entry.Protocol != "" {
			stats.ByProtocol[entry.Protocol]++
		}
		if entry.SrcIP != "" {
			srcCounts[entry.SrcIP]++
		}
		if entry.DstIP != "" {
			dstCounts[entry.DstIP]++
		}

		// Detect drops by prefix
		if entry.Prefix != "" && (entry.Prefix == "DROP" || entry.Prefix == "REJECT" ||
			len(entry.Prefix) > 0 && entry.Prefix[0] == 'D') {
			stats.DroppedPackets++
		} else {
			stats.AcceptedPackets++
		}
	}

	stats.TopSources = getTopN(srcCounts, 10)
	stats.TopDestinations = getTopN(dstCounts, 10)

	return stats
}

// Clear clears all stored entries
func (r *NFLogReader) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = r.entries[:0]
}

// Count returns the number of stored entries
func (r *NFLogReader) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// Subscribe returns a channel that receives new log entries
func (r *NFLogReader) Subscribe() <-chan NFLogEntry {
	r.subsMu.Lock()
	defer r.subsMu.Unlock()

	ch := make(chan NFLogEntry, 100)
	r.subs = append(r.subs, ch)
	return ch
}

// broadcast sends an entry to all subscribers
func (r *NFLogReader) broadcast(entry NFLogEntry) {
	r.subsMu.RLock()
	defer r.subsMu.RUnlock()

	for _, sub := range r.subs {
		select {
		case sub <- entry:
		default:
			// Drop if channel full to prevent blocking
		}
	}
}
