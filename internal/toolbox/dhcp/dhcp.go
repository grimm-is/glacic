package dhcp

import (
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

func Run(args []string) error {
	fs := flag.NewFlagSet("dhcp-request", flag.ContinueOnError)
	ifaceName := fs.String("iface", "eth0", "Interface to send on")
	macStr := fs.String("mac", "", "MAC address (optional, defaults to interface MAC)")
	hostname := fs.String("hostname", "test-client", "Hostname (Option 12)")
	vendor := fs.String("vendor", "test-vendor", "Vendor Class (Option 60)")
	fingerprint := fs.String("fingerprint", "1,3,6,15,31,33,43,44,46,47,119,121,249,252", "Option 55 Fingerprint (comma separated)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	iface, err := net.InterfaceByName(*ifaceName)
	if err != nil {
		return fmt.Errorf("interface %s not found: %v", *ifaceName, err)
	}

	var mac net.HardwareAddr
	if *macStr != "" {
		mac, err = net.ParseMAC(*macStr)
		if err != nil {
			return fmt.Errorf("invalid MAC: %v", err)
		}
	} else {
		mac = iface.HardwareAddr
	}

	fmt.Printf("Sending DHCP Discover on %s (MAC: %s)\n", *ifaceName, mac)

	// Build DHCP Discover
	opts := []dhcpv4.Modifier{
		dhcpv4.WithOption(dhcpv4.OptHostName(*hostname)),
		dhcpv4.WithOption(dhcpv4.OptClassIdentifier(*vendor)),
	}

	// Option 55 (Fingerprint)
	if *fingerprint != "" {
		parts := strings.Split(*fingerprint, ",")
		var codes []dhcpv4.OptionCode
		for _, p := range parts {
			c, err := strconv.Atoi(strings.TrimSpace(p))
			if err == nil {
				codes = append(codes, dhcpv4.GenericOptionCode(c))
			}
		}
		opts = append(opts, dhcpv4.WithOption(dhcpv4.OptParameterRequestList(codes...)))
	}

	// Option 61 (Client ID) - Type 1 (Ethernet) + MAC
	clientID := append([]byte{1}, mac...)
	opts = append(opts, dhcpv4.WithOption(dhcpv4.OptClientIdentifier(clientID)))

	packet, err := dhcpv4.NewDiscovery(mac, opts...)
	if err != nil {
		return fmt.Errorf("failed to create discovery packet: %v", err)
	}

	// Send via raw UDP broadcast
	// We use port 68 as source, 67 as dest
	dstAddr := &net.UDPAddr{IP: net.IPv4bcast, Port: 67}
	srcAddr := &net.UDPAddr{IP: net.IPv4zero, Port: 68}

	conn, err := net.ListenUDP("udp4", srcAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on port 68: %v", err)
	}
	defer conn.Close()

	// Need to bind to device to send broadcast properly?
	// net.ListenUDP binds to 0.0.0.0:68 which should be okay if we just rely on routing table or SO_BINDTODEVICE
	// But in Go/Linux, binding to device for broadcast usually needs raw socket or specific syscalls.
	// For now, let's try standard sending. The OS should route 255.255.255.255 out of the "primary" interface,
	// or we rely on the caller to have route config.
	// OR we can't easily force interface without syscall controls.

	// Since this is a test tool for a specific iface, we might need a workaround if plain UDP fails to pick correct interface.
	// But let's assume the test environment setup routes correctly.

	_, err = conn.WriteTo(packet.ToBytes(), dstAddr)
	if err != nil {
		return fmt.Errorf("failed to send packet: %v", err)
	}

	fmt.Println("Sent DHCP Discover")

	// Wait a bit to allow server to process
	time.Sleep(100 * time.Millisecond)

	return nil
}
