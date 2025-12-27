package learning

import (
	"encoding/hex"
	"testing"
)

func TestParseSNI(t *testing.T) {
	// Constructing a minimal Transport Layer Security Client Hello packet
	// 16 (Handshake)
	// 03 01 (Version TLS 1.0) - Record Layer
	// 00 59 (Length 89)
	// 01 (Client Hello)
	// 00 00 55 (Length 85)
	// 03 03 (Version TLS 1.2)
	// 00...00 (32 bytes random)
	// 00 (Session ID Len)
	// 00 02 (Cipher Suites Len)
	// C0 2B (Cipher Suite)
	// 01 (Comp Methods Len)
	// 00 (Comp Method null)
	// 00 2E (Ext Len 46)
	// 00 00 (Ext Type SNI)
	// 00 0D (Ext Len 13)
	// 00 0B (List Len 11)
	// 00 (Type HostName)
	// 00 08 (Name Len 8)
	// 74 65 73 74 2e 63 6f 6d ("test.com")

	// Total Header: 5 bytes
	// Handshake Header: 4 bytes
	// Version+Random: 34 bytes
	// SessionID: 1 byte
	// CipherSuites: 2 + 2 = 4 bytes
	// CompMethods: 1 + 1 = 2 bytes
	// Ext Len: 2 bytes
	// Extension: 2+2 (Header) + 2 (ListLen) + 1 (Type) + 2 (NameLen) + 8 (Name) = 17 bytes
	// Wait, ExtLen in construction:
	// Header(4) + ListLen(2) + Type(1) + NameLen(2) + Name(8) = 17 bytes.
	// 0x0011 = 17

	validSNIPacketHex := "1603010045010000410303" + // Record + HS Header + Version
		"0000000000000000000000000000000000000000000000000000000000000000" + // Random
		"00" + // SessionID Len
		"0002C02B" + // Ciphers
		"0100" + // Compression
		"0011" + // Ext Len
		// SNI Extension Structure:
		// Type (2) + Len (2) + ServerNameListLen (2) + NameType (1) + NameLen (2) + Name
		// My calculation above:
		// Type (2: 00 00)
		// Len (2: 00 0D = 13) -> ListLen(2)+Type(1)+NameLen(2)+Name(8) = 13. Correct.
		// ListLen (2: 00 0B = 11) -> Type(1)+NameLen(2)+Name(8) = 11. Correct.
		// Type (1: 00)
		// NameLen (2: 00 08)
		// Name (8: test.com)

		// So hex payload:
		// 0000 000D 000B 00 0008 746573742e636f6d
		"0000000D000B000008746573742e636f6d"

	valid, _ := hex.DecodeString(validSNIPacketHex)

	tests := []struct {
		name    string
		payload []byte
		want    string
		wantErr bool
	}{
		{"Valid SNI", valid, "test.com", false},
		{"Short Packet", []byte{0x16}, "", false},
		{"Not Handshake", []byte{0x17, 0x03, 0x01, 0x00, 0x10}, "", false},
		{"Not ClientHello", []byte{0x16, 0x03, 0x01, 0x00, 0x10, 0x02, 0x00}, "", false}, // 0x02 = ServerHello
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSNI(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSNI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseSNI() = %q, want %q", got, tt.want)
			}
		})
	}
}
