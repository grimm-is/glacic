package lldp

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestParseLLDP(t *testing.T) {
	// Construct a frame
	// Eth Header (14 bytes)
	// Dest: 01:80:c2:00:00:0e (LLDP Mcast)
	// Src:  00:11:22:33:44:55
	// EtherType: 0x88CC

	frame := make([]byte, 0, 100)

	// Ethernet Header
	dest, _ := net.ParseMAC("01:80:c2:00:00:0e")
	src, _ := net.ParseMAC("00:11:22:33:44:55")
	frame = append(frame, dest...)
	frame = append(frame, src...)
	frame = append(frame, 0x88, 0xCC) // EtherType 0x88CC

	// TLV 1: Chassis ID (Type 1, Len 7) -> Subtype 4 (MAC), Val: src
	// Header: (Type << 9) | Len
	// Type 1 = 0000001
	// 0000001 000000111 = 02 07
	frame = append(frame, 0x02, 0x07)
	frame = append(frame, 0x04) // Subtype 4 (MAC Address)
	frame = append(frame, src...)

	// TLV 2: Port ID (Type 2, Len 5) -> Subtype 5 (Interface Name), Val: "eth0"
	// Type 2 = 0000010
	// 0000010 000000101 = 04 05
	frame = append(frame, 0x04, 0x05)
	frame = append(frame, 0x05) // Subtype 5
	frame = append(frame, []byte("eth0")...)

	// TLV 3: TTL (Type 3, Len 2) -> 120 seconds
	// Type 3 = 0000011
	// 0000011 000000010 = 06 02
	frame = append(frame, 0x06, 0x02)
	frame = append(frame, 0x00, 0x78) // 120

	// TLV 5: System Name (Type 5, Len 6) -> "Router"
	// Type 5 = 0000101
	// 0000101 000000110 = 0A 06
	frame = append(frame, 0x0A, 0x06)
	frame = append(frame, []byte("Router")...)

	// TLV 0: End
	frame = append(frame, 0x00, 0x00)

	info, err := ParseLLDP(frame)
	if err != nil {
		t.Fatalf("ParseLLDP failed: %v", err)
	}

	if info.ChassisID != src.String() {
		t.Errorf("ChassisID mismatch: want %s, got %s", src.String(), info.ChassisID)
	}

	if info.PortID != "eth0" {
		t.Errorf("PortID mismatch: want eth0, got %s", info.PortID)
	}

	if info.TTL != 120*time.Second {
		t.Errorf("TTL mismatch: want 120s, got %v", info.TTL)
	}

	if info.SystemName != "Router" {
		t.Errorf("SystemName mismatch: want Router, got %s", info.SystemName)
	}
}

func TestParseLLDP_Invalid(t *testing.T) {
	// Short frame
	if _, err := ParseLLDP([]byte{0x00, 0x01}); err == nil {
		t.Error("Msg too short should error")
	}

	// Wrong EtherType
	frame := make([]byte, 14)
	binary.BigEndian.PutUint16(frame[12:], 0x0800) // IPv4
	if _, err := ParseLLDP(frame); err == nil {
		t.Error("Wrong EtherType should error")
	}
}
