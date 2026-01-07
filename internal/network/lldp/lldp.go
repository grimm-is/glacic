package lldp

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/mdlayher/packet"
)

// EtherTypeLLDP is the EtherType for LLDP frames (0x88CC)
const EtherTypeLLDP = 0x88CC

// DiscoveryInfo contains info discovered from an LLDP neighbor
type DiscoveryInfo struct {
	ChassisID      string
	PortID         string
	SystemName     string
	SystemDesc     string
	TTL            time.Duration
	LastSeen       time.Time
	LocalInterface string
}

// Listener handles LLDP packet capture
type Listener struct {
	iface *net.Interface
	conn  *packet.Conn
	stop  chan struct{}
}

// NewListener creates a listener for a specific interface
func NewListener(ifaceName string) (*Listener, error) {
	ifi, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, err
	}

	conn, err := packet.Listen(ifi, packet.Raw, EtherTypeLLDP, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to open socket: %w", err)
	}

	return &Listener{
		iface: ifi,
		conn:  conn,
		stop:  make(chan struct{}),
	}, nil
}

// ReadPacket blocks until a packet is received or context is cancelled
func (l *Listener) ReadPacket(ctx context.Context) (*DiscoveryInfo, error) {
	// helper buffer
	buf := make([]byte, 1500)

	// We need a way to read with context cancel.
	// packet.Conn behaves like net.PacketConn.

	// Fast path loop
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			l.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, _, err := l.conn.ReadFrom(buf)
			if err != nil {
				// check timeout
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					continue
				}
				return nil, err
			}

			// Parse
			info, err := ParseLLDP(buf[:n])
			if err != nil {
				// Malformed packet, ignore and continue
				continue
			}
			info.LocalInterface = l.iface.Name
			return info, nil
		}
	}
}

// Close closes the listener
func (l *Listener) Close() error {
	return l.conn.Close()
}

// TLV Types
const (
	TLVEnd             = 0
	TLVChassisID       = 1
	TLVPortID          = 2
	TLVTTL             = 3
	TLVPortDescription = 4
	TLVSystemName      = 5
	TLVSystemDesc      = 6
	TLVSystemCap       = 7
	TLVMgmtAddr        = 8
)

// ParseLLDP parses raw LLDP frame bytes
func ParseLLDP(frame []byte) (*DiscoveryInfo, error) {
	// Skip Ethernet Header (14 bytes) if using ETH_P_ALL equivalent,
	// BUT github.com/mdlayher/packet with packet.Raw usually gives the payload?
	// Spec says: "When listening on a Protocol Identifier ... the data returned starts at the MAC header..."
	// Wait, packet.Raw usually includes L2 header.
	// EtherType is offset 12.

	if len(frame) < 14 {
		return nil, fmt.Errorf("frame too short")
	}

	// Double check EtherType
	ethType := binary.BigEndian.Uint16(frame[12:14])
	if ethType != EtherTypeLLDP {
		return nil, fmt.Errorf("not an LLDP frame")
	}

	payload := frame[14:]
	info := &DiscoveryInfo{
		LastSeen: time.Now(),
	}

	offset := 0
	for offset < len(payload) {
		// TLV Header is 2 bytes (7 bits Type, 9 bits Length)
		if offset+2 > len(payload) {
			break
		}

		tlvHeader := binary.BigEndian.Uint16(payload[offset:])
		tlvType := tlvHeader >> 9
		tlvLen := int(tlvHeader & 0x01FF)
		offset += 2

		if offset+tlvLen > len(payload) {
			break
		}

		val := payload[offset : offset+tlvLen]
		offset += tlvLen

		switch tlvType {
		case TLVEnd:
			return info, nil

		case TLVChassisID:
			if len(val) > 1 {
				// Subtype is first byte
				// subtype := val[0]
				// Simply hexify for now to match generic behavior,
				// or use string if subtype indicates?
				// Common: MAC address (subtype 4)
				info.ChassisID = fmt.Sprintf("%x", val[1:])
				if val[0] == 4 && len(val) == 7 { // MAC
					info.ChassisID = net.HardwareAddr(val[1:]).String()
				}
			}

		case TLVPortID:
			if len(val) > 1 {
				// Subtype 7 (Local) or 5 (Interface Name) often printable
				// Subtype 3 (MAC)
				if val[0] == 5 || val[0] == 7 {
					info.PortID = string(val[1:])
				} else if val[0] == 3 && len(val) == 7 {
					info.PortID = net.HardwareAddr(val[1:]).String()
				} else {
					info.PortID = fmt.Sprintf("%x", val[1:])
				}
			}

		case TLVTTL:
			if len(val) == 2 {
				secs := binary.BigEndian.Uint16(val)
				info.TTL = time.Duration(secs) * time.Second
			}

		case TLVSystemName:
			info.SystemName = string(val)

		case TLVSystemDesc:
			info.SystemDesc = string(val)
		}
	}

	return info, nil
}
