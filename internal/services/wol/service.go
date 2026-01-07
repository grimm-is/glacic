package wol

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

// MagicPacket represents a Wake-on-LAN magic packet.
// Format: 6 bytes of 0xFF followed by target MAC address repeated 16 times.
type MagicPacket [102]byte

// New creates a new magic packet for the given MAC address.
func New(mac string) (*MagicPacket, error) {
	// Parse MAC address
	macBytes, err := ParseMAC(mac)
	if err != nil {
		return nil, err
	}

	var packet MagicPacket

	// First 6 bytes are 0xFF
	for i := 0; i < 6; i++ {
		packet[i] = 0xFF
	}

	// Repeat MAC address 16 times
	for i := 0; i < 16; i++ {
		copy(packet[6+i*6:], macBytes)
	}

	return &packet, nil
}

// Send sends the magic packet to the broadcast address.
func (p *MagicPacket) Send(broadcastAddr string) error {
	if broadcastAddr == "" {
		broadcastAddr = "255.255.255.255:9"
	}

	// Ensure port is included
	if !strings.Contains(broadcastAddr, ":") {
		broadcastAddr += ":9"
	}

	conn, err := net.Dial("udp", broadcastAddr)
	if err != nil {
		return fmt.Errorf("failed to dial broadcast: %w", err)
	}
	defer conn.Close()

	// Enable broadcast
	if udpConn, ok := conn.(*net.UDPConn); ok {
		udpConn.SetWriteBuffer(len(p))
	}

	_, err = conn.Write(p[:])
	if err != nil {
		return fmt.Errorf("failed to send magic packet: %w", err)
	}

	return nil
}

// SendToInterface sends the magic packet to the broadcast address of a specific interface.
func SendToInterface(mac, ifaceName string) error {
	packet, err := New(mac)
	if err != nil {
		return err
	}

	// Get interface broadcast address
	broadcastAddr, err := getInterfaceBroadcast(ifaceName)
	if err != nil {
		// Fallback to global broadcast
		broadcastAddr = "255.255.255.255"
	}

	return packet.Send(broadcastAddr + ":9")
}

// Wake sends a WoL packet to the specified MAC address.
func Wake(mac string) error {
	packet, err := New(mac)
	if err != nil {
		return err
	}
	return packet.Send("255.255.255.255:9")
}

// ParseMAC parses a MAC address string into bytes.
func ParseMAC(mac string) ([]byte, error) {
	// Remove common separators
	mac = strings.ReplaceAll(mac, ":", "")
	mac = strings.ReplaceAll(mac, "-", "")
	mac = strings.ReplaceAll(mac, ".", "")
	mac = strings.ToLower(mac)

	if len(mac) != 12 {
		return nil, fmt.Errorf("invalid MAC address length: %d", len(mac))
	}

	bytes, err := hex.DecodeString(mac)
	if err != nil {
		return nil, fmt.Errorf("invalid MAC address format: %w", err)
	}

	return bytes, nil
}

// getInterfaceBroadcast returns the broadcast address for an interface.
func getInterfaceBroadcast(ifaceName string) (string, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return "", err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
			// Calculate broadcast address
			ip := ipNet.IP.To4()
			mask := ipNet.Mask
			broadcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				broadcast[i] = ip[i] | ^mask[i]
			}
			return broadcast.String(), nil
		}
	}

	return "", fmt.Errorf("no IPv4 address on interface %s", ifaceName)
}
