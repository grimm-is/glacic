package dhcp

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/logging"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/mdlayher/packet"
)

// SnifferEvent represents a DHCP fingerprint observation
type SnifferEvent struct {
	Timestamp   time.Time
	ClientMAC   string
	Interface   string
	Hostname    string           // Option 12
	Fingerprint string           // Option 55 (Parameter Request List)
	VendorClass string           // Option 60 (Vendor Class Identifier)
	ClientID    string           // Option 61 (Client Identifier)
	Options     map[uint8]string // All DHCP options (hex-encoded values)
}

// SnifferConfig holds the configuration for the DHCP sniffer
type SnifferConfig struct {
	Enabled    bool
	Interfaces []string // LAN interfaces to listen on
	// EventCallback is called when a DHCP packet is observed
	EventCallback func(event SnifferEvent)
}

// Sniffer passively captures DHCP broadcasts for device fingerprinting.
// Unlike the DHCP server, it only observes and doesn't respond.
type Sniffer struct {
	config SnifferConfig
	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
	logger *logging.Logger
}

// NewSniffer creates a new DHCP sniffer
func NewSniffer(cfg SnifferConfig) *Sniffer {
	return &Sniffer{
		config: cfg,
		logger: logging.WithComponent("dhcp-sniffer"),
	}
}

// SetEventCallback sets or updates the event callback
func (s *Sniffer) SetEventCallback(cb func(event SnifferEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.EventCallback = cb
}

// Start begins listening for DHCP broadcasts
func (s *Sniffer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.config.Enabled || len(s.config.Interfaces) == 0 {
		return nil // Nothing to do
	}

	ctx, s.cancel = context.WithCancel(ctx)

	// Start a listener on each interface using AF_PACKET (Linux) to avoid binding conflicts
	for _, ifaceName := range s.config.Interfaces {
		iface, err := net.InterfaceByName(ifaceName)
		if err != nil {
			s.logger.Warn("interface not found", "iface", ifaceName, "err", err)
			continue
		}

		// Listen on ETH_P_IP (IPv4 only)
		// We filter UDP/67 in userspace to avoid complex BPF construction here
		conn, err := packet.Listen(iface, packet.Raw, 0x0800, nil)
		if err != nil {
			s.logger.Warn("failed to listen on raw socket", "iface", ifaceName, "err", err)
			continue
		}

		s.wg.Add(1)
		go s.run(ctx, conn, ifaceName)
		s.logger.Info("started DHCP sniffer", "iface", ifaceName)
	}

	return nil
}

// Stop stops the DHCP sniffer
func (s *Sniffer) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.mu.Unlock()

	s.wg.Wait()
	s.logger.Info("stopped")
}

// run listens for DHCP packets on a raw connection
func (s *Sniffer) run(ctx context.Context, conn *packet.Conn, ifaceName string) {
	defer s.wg.Done()
	defer conn.Close()

	buf := make([]byte, 1500) // Standard MTU

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Set read deadline to periodically check for cancellation
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))

			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				// Timeouts are expected
				if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
					continue
				}
				if strings.Contains(err.Error(), "closed network connection") {
					return
				}
				continue
			}

			// Parse DHCP from Ethernet/IP/UDP frame
			pkt, err := parseDHCPFromFrame(buf[:n])
			if err != nil {
				// Not a DHCP packet or malformed
				continue
			}

			// Only process DISCOVER and REQUEST messages (client->server)
			if pkt.OpCode != dhcpv4.OpcodeBootRequest {
				continue
			}

			// Extract fingerprint and emit event
			event := s.extractEvent(pkt, ifaceName, addr)
			if s.config.EventCallback != nil {
				s.logger.Info("broadcast detected",
					"mac", event.ClientMAC,
					"hostname", event.Hostname,
					"fingerprint", event.Fingerprint,
					"vendor", event.VendorClass)
				s.config.EventCallback(event)
			}
		}
	}
}

// parseDHCPFromFrame extracts a DHCPv4 packet from an Ethernet frame
func parseDHCPFromFrame(frame []byte) (*dhcpv4.DHCPv4, error) {
	// Minimum Ethernet(14) + IP(20) + UDP(8) = 42 bytes
	if len(frame) < 42 {
		return nil, fmt.Errorf("frame too short")
	}

	// Ethernet Header (14 bytes)
	// EtherType at offset 12
	ethType := binary.BigEndian.Uint16(frame[12:14])
	if ethType != 0x0800 { // IPv4
		return nil, fmt.Errorf("not ipv4")
	}

	// IP Header
	// IHL is lower 4 bits of first byte
	ipOffset := 14
	ihl := int(frame[ipOffset] & 0x0F)
	ipHeaderLen := ihl * 4
	if ipHeaderLen < 20 {
		return nil, fmt.Errorf("invalid ip header/ihl")
	}

	// Protocol is at offset 9 in IP header
	proto := frame[ipOffset+9]
	if proto != 17 { // UDP
		return nil, fmt.Errorf("not udp")
	}

	// UDP Header matches?
	udpOffset := ipOffset + ipHeaderLen
	if udpOffset+8 > len(frame) {
		return nil, fmt.Errorf("frame too short for udp")
	}

	// Dst Port is bytes 2-3 of UDP header
	dstPort := binary.BigEndian.Uint16(frame[udpOffset+2 : udpOffset+4])
	if dstPort != 67 {
		return nil, fmt.Errorf("not bootps")
	}

	// DHCP Payload starts after UDP header
	payloadOffset := udpOffset + 8
	if payloadOffset >= len(frame) {
		return nil, fmt.Errorf("no payload")
	}

	return dhcpv4.FromBytes(frame[payloadOffset:])
}

// ExtractEvent extracts fingerprint information from a DHCP packet
func ExtractEvent(pkt *dhcpv4.DHCPv4, ifaceName string, src net.Addr) SnifferEvent {
	event := SnifferEvent{
		Timestamp: time.Now(),
		ClientMAC: pkt.ClientHWAddr.String(),
		Interface: ifaceName,
		Options:   make(map[uint8]string),
	}

	// Option 12: Hostname
	if opt := pkt.Options.Get(dhcpv4.OptionHostName); opt != nil {
		event.Hostname = string(opt)
	}

	// Option 55: Parameter Request List (fingerprint)
	if opt := pkt.Options.Get(dhcpv4.OptionParameterRequestList); opt != nil {
		// Convert to comma-separated list of option codes
		codes := make([]string, len(opt))
		for i, code := range opt {
			codes[i] = strconv.Itoa(int(code))
		}
		event.Fingerprint = strings.Join(codes, ",")
	}

	// Option 60: Vendor Class Identifier
	if opt := pkt.Options.Get(dhcpv4.OptionClassIdentifier); opt != nil {
		event.VendorClass = string(opt)
	}

	// Option 61: Client Identifier
	if opt := pkt.Options.Get(dhcpv4.OptionClientIdentifier); opt != nil {
		event.ClientID = hex.EncodeToString(opt)
	}

	// Store important options for later analysis
	for _, optCode := range []dhcpv4.OptionCode{
		dhcpv4.OptionHostName,                     // 12
		dhcpv4.OptionParameterRequestList,         // 55
		dhcpv4.OptionClassIdentifier,              // 60
		dhcpv4.OptionClientIdentifier,             // 61
		dhcpv4.OptionVendorIdentifyingVendorClass, // 124
		dhcpv4.OptionUserClassInformation,         // 77
		dhcpv4.OptionClientMachineIdentifier,      // 97
	} {
		if opt := pkt.Options.Get(optCode); opt != nil {
			event.Options[optCode.Code()] = hex.EncodeToString(opt)
		}
	}

	return event
}

// extractEvent extracts fingerprint information from a DHCP packet (legacy wrapper or internal use)
func (s *Sniffer) extractEvent(pkt *dhcpv4.DHCPv4, ifaceName string, src net.Addr) SnifferEvent {
	return ExtractEvent(pkt, ifaceName, src)
}

// Common DHCP fingerprints for reference (can be extended to a database later)
// These are from https://fingerbank.org
var knownFingerprints = map[string]string{
	"1,3,6,15,31,33,43,44,46,47,119,121,249,252": "Windows 10/11",
	"1,121,3,6,15,119,252":                       "macOS",
	"1,3,6,12,15,28,42":                          "Linux",
	"1,3,28,6":                                   "Android",
	"1,3,6,15,119,252":                           "iOS",
}

// InferDeviceOS attempts to infer the device OS from its DHCP fingerprint
func InferDeviceOS(fingerprint string) string {
	if os, ok := knownFingerprints[fingerprint]; ok {
		return os
	}
	return ""
}
