//go:build linux

package ctlplane

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"

	"grimm.is/glacic/internal/clock"

	"github.com/florianl/go-nfqueue/v2"
)

// NFQueueReader reads packets inline via nfqueue and returns verdicts.
// Unlike NFLogReader (async), this holds packets until a verdict is returned,
// enabling "first packet" acceptance in learning mode.
type NFQueueReader struct {
	queue       *nfqueue.Nfqueue
	group       uint16
	maxQueueLen uint32
	running     bool
	cancel      context.CancelFunc
	ctx         context.Context
	mu          sync.RWMutex

	// VerdictFunc is called for each packet to determine accept/drop.
	// The function should return true for ACCEPT, false for DROP.
	// This is called from the packet processing goroutine, so it must be fast.
	VerdictFunc func(entry NFLogEntry) bool

	// Stats
	packetsProcessed uint64
	packetsAccepted  uint64
	packetsDropped   uint64
	verdictErrors    uint64
	statsMu          sync.RWMutex
}

// NewNFQueueReader creates a new inline packet queue reader.
// queueNum should match the nftables "queue num X" rule.
func NewNFQueueReader(queueNum uint16) *NFQueueReader {
	return &NFQueueReader{
		group:       queueNum,
		maxQueueLen: 1024, // Reasonable default
	}
}

// SetVerdictFunc sets the callback that determines packet fate.
// This must be set before calling Start().
func (r *NFQueueReader) SetVerdictFunc(fn func(entry NFLogEntry) bool) {
	r.VerdictFunc = fn
}

// Start begins listening for packets on the queue.
func (r *NFQueueReader) Start() error {
	if r.VerdictFunc == nil {
		return fmt.Errorf("VerdictFunc must be set before starting NFQueueReader")
	}

	config := nfqueue.Config{
		NfQueue:      r.group,
		MaxPacketLen: 256, // Only need headers for flow identification
		MaxQueueLen:  r.maxQueueLen,
		Copymode:     nfqueue.NfQnlCopyPacket,
	}

	nf, err := nfqueue.Open(&config)
	if err != nil {
		return fmt.Errorf("failed to open nfqueue: %w", err)
	}
	r.queue = nf

	ctx, cancel := context.WithCancel(context.Background())
	r.ctx = ctx
	r.cancel = cancel
	r.running = true

	// Register callback for queued packets
	err = nf.RegisterWithErrorFunc(ctx,
		func(attrs nfqueue.Attribute) int {
			r.handlePacket(attrs)
			return 0
		},
		func(err error) int {
			if r.running {
				log.Printf("[NFQUEUE] Error: %v", err)
			}
			return 0
		},
	)
	if err != nil {
		nf.Close()
		return fmt.Errorf("failed to register nfqueue callback: %w", err)
	}

	log.Printf("[NFQUEUE] Listening on queue %d (inline mode)", r.group)
	return nil
}

// handlePacket processes a queued packet and returns a verdict.
func (r *NFQueueReader) handlePacket(attrs nfqueue.Attribute) {
	r.statsMu.Lock()
	r.packetsProcessed++
	r.statsMu.Unlock()

	// Parse packet to extract flow info
	entry := r.parseAttributes(attrs)

	// Get verdict from callback
	accept := r.VerdictFunc(entry)

	// Set verdict
	packetID := *attrs.PacketID
	var err error
	if accept {
		err = r.queue.SetVerdict(packetID, nfqueue.NfAccept)
		r.statsMu.Lock()
		r.packetsAccepted++
		r.statsMu.Unlock()
	} else {
		err = r.queue.SetVerdict(packetID, nfqueue.NfDrop)
		r.statsMu.Lock()
		r.packetsDropped++
		r.statsMu.Unlock()
	}

	if err != nil {
		r.statsMu.Lock()
		r.verdictErrors++
		r.statsMu.Unlock()
		log.Printf("[NFQUEUE] Failed to set verdict for packet %d: %v", packetID, err)
	}
}

// parseAttributes converts nfqueue attributes to NFLogEntry format.
// This mirrors NFLogReader.parseAttributes for consistency.
func (r *NFQueueReader) parseAttributes(attrs nfqueue.Attribute) NFLogEntry {
	entry := NFLogEntry{
		Timestamp: clock.Now(),
		Extra:     make(map[string]string),
	}

	// Hardware info (source MAC)
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
	}

	return entry
}

// parsePacket extracts IP and transport layer info from packet bytes.
func (r *NFQueueReader) parsePacket(payload []byte, entry *NFLogEntry) {
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

// parseIPv4 parses an IPv4 packet.
func (r *NFQueueReader) parseIPv4(payload []byte, entry *NFLogEntry) {
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

// parseIPv6 parses an IPv6 packet.
func (r *NFQueueReader) parseIPv6(payload []byte, entry *NFLogEntry) {
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

// Stop stops listening for packets.
func (r *NFQueueReader) Stop() {
	r.running = false
	if r.cancel != nil {
		r.cancel()
	}
	if r.queue != nil {
		r.queue.Close()
	}
}

// IsRunning returns whether the reader is currently active.
func (r *NFQueueReader) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// NFQueueStats holds statistics for the queue reader.
type NFQueueStats struct {
	PacketsProcessed uint64 `json:"packets_processed"`
	PacketsAccepted  uint64 `json:"packets_accepted"`
	PacketsDropped   uint64 `json:"packets_dropped"`
	VerdictErrors    uint64 `json:"verdict_errors"`
}

// GetStats returns current statistics.
func (r *NFQueueReader) GetStats() NFQueueStats {
	r.statsMu.RLock()
	defer r.statsMu.RUnlock()

	return NFQueueStats{
		PacketsProcessed: r.packetsProcessed,
		PacketsAccepted:  r.packetsAccepted,
		PacketsDropped:   r.packetsDropped,
		VerdictErrors:    r.verdictErrors,
	}
}

// DefaultLearningVerdictFunc creates a verdict function that uses the learning engine.
// Returns true (ACCEPT) if learning mode is enabled or flow is already allowed.
func DefaultLearningVerdictFunc(isLearningMode func() bool, processPacket func(pkt NFLogEntry) (bool, error)) func(NFLogEntry) bool {
	return func(entry NFLogEntry) bool {
		// Call the learning engine's ProcessPacket
		verdict, err := processPacket(entry)
		if err != nil {
			log.Printf("[NFQUEUE] Error processing packet: %v", err)
			// Fail-open: accept on error to avoid blocking legitimate traffic
			return true
		}
		return verdict
	}
}
