package wol

import (
	"testing"
)

func TestParseMAC_Colon(t *testing.T) {
	mac, err := ParseMAC("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("Failed to parse MAC: %v", err)
	}
	if len(mac) != 6 {
		t.Fatalf("Expected 6 bytes, got %d", len(mac))
	}
	expected := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	for i, b := range mac {
		if b != expected[i] {
			t.Errorf("Byte %d: expected %02x, got %02x", i, expected[i], b)
		}
	}
}

func TestParseMAC_Dash(t *testing.T) {
	mac, err := ParseMAC("AA-BB-CC-DD-EE-FF")
	if err != nil {
		t.Fatalf("Failed to parse MAC: %v", err)
	}
	if len(mac) != 6 {
		t.Fatalf("Expected 6 bytes, got %d", len(mac))
	}
}

func TestParseMAC_NoSeparator(t *testing.T) {
	mac, err := ParseMAC("aabbccddeeff")
	if err != nil {
		t.Fatalf("Failed to parse MAC: %v", err)
	}
	if len(mac) != 6 {
		t.Fatalf("Expected 6 bytes, got %d", len(mac))
	}
}

func TestParseMAC_Dots(t *testing.T) {
	mac, err := ParseMAC("aabb.ccdd.eeff")
	if err != nil {
		t.Fatalf("Failed to parse MAC: %v", err)
	}
	if len(mac) != 6 {
		t.Fatalf("Expected 6 bytes, got %d", len(mac))
	}
}

func TestParseMAC_InvalidLength(t *testing.T) {
	_, err := ParseMAC("aabbcc")
	if err == nil {
		t.Error("Expected error for short MAC")
	}
}

func TestParseMAC_InvalidChars(t *testing.T) {
	_, err := ParseMAC("xx:xx:xx:xx:xx:xx")
	if err == nil {
		t.Error("Expected error for invalid hex")
	}
}

func TestNewMagicPacket(t *testing.T) {
	packet, err := New("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("Failed to create magic packet: %v", err)
	}

	// Check first 6 bytes are 0xFF
	for i := 0; i < 6; i++ {
		if packet[i] != 0xFF {
			t.Errorf("Byte %d: expected 0xFF, got %02x", i, packet[i])
		}
	}

	// Check MAC is repeated 16 times
	expectedMAC := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	for i := 0; i < 16; i++ {
		offset := 6 + i*6
		for j := 0; j < 6; j++ {
			if packet[offset+j] != expectedMAC[j] {
				t.Errorf("MAC repeat %d, byte %d: expected %02x, got %02x",
					i, j, expectedMAC[j], packet[offset+j])
			}
		}
	}
}

func TestNewMagicPacket_InvalidMAC(t *testing.T) {
	_, err := New("invalid")
	if err == nil {
		t.Error("Expected error for invalid MAC")
	}
}

func TestMagicPacket_Size(t *testing.T) {
	packet, _ := New("aa:bb:cc:dd:ee:ff")
	if len(packet) != 102 {
		t.Errorf("Expected packet size 102, got %d", len(packet))
	}
}
