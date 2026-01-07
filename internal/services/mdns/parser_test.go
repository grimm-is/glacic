package mdns

import (
	"net"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestParseMDNSPacket(t *testing.T) {
	// Construct a sample mDNS response packet
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		Response:      true,
		Authoritative: true,
	})

	// Question
	b.StartQuestions()
	b.Question(dnsmessage.Question{
		Name:  dnsmessage.MustNewName("_googlecast._tcp.local."),
		Type:  dnsmessage.TypePTR,
		Class: dnsmessage.ClassINET,
	})

	// Answers
	b.StartAnswers()

	// PTR Record (Service Instance)
	// _googlecast._tcp.local. -> Chromecast-123._googlecast._tcp.local.
	b.PTRResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName("_googlecast._tcp.local."),
		Type:  dnsmessage.TypePTR,
		Class: dnsmessage.ClassINET,
		TTL:   120,
	}, dnsmessage.PTRResource{
		PTR: dnsmessage.MustNewName("Chromecast-123._googlecast._tcp.local."),
	})

	// SRV Record (Host and Port)
	// Chromecast-123._googlecast._tcp.local. -> 8009 target.local.
	b.SRVResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName("Chromecast-123._googlecast._tcp.local."),
		Type:  dnsmessage.TypeSRV,
		Class: dnsmessage.ClassINET,
		TTL:   120,
	}, dnsmessage.SRVResource{
		Priority: 0,
		Weight:   0,
		Port:     8009,
		Target:   dnsmessage.MustNewName("Chromecast-Device.local."),
	})

	// TXT Record (Metadata)
	// Chromecast-123._googlecast._tcp.local. -> "fn=Living Room TV", "md=Chromecast"
	b.TXTResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName("Chromecast-123._googlecast._tcp.local."),
		Type:  dnsmessage.TypeTXT,
		Class: dnsmessage.ClassINET,
		TTL:   120,
	}, dnsmessage.TXTResource{
		TXT: []string{"fn=Living Room TV", "md=Chromecast"},
	})

	// A Record (IP)
	// Chromecast-Device.local. -> 192.168.1.50
	b.AResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName("Chromecast-Device.local."),
		Type:  dnsmessage.TypeA,
		Class: dnsmessage.ClassINET,
		TTL:   120,
	}, dnsmessage.AResource{
		A: [4]byte{192, 168, 1, 50},
	})

	packet, err := b.Finish()
	if err != nil {
		t.Fatalf("Failed to build packet: %v", err)
	}

	srcIP := net.ParseIP("192.168.1.50")
	parsed, err := ParseMDNSPacket(packet, srcIP, "eth0")
	if err != nil {
		t.Fatalf("ParseMDNSPacket failed: %v", err)
	}

	// Verify Hostname
	expectedHostname := "Chromecast-Device"
	if parsed.Hostname != expectedHostname {
		t.Errorf("Expected hostname %q, got %q", expectedHostname, parsed.Hostname)
	}

	// Verify Services
	expectedService := "_googlecast._tcp"
	foundService := false
	for _, s := range parsed.Services {
		if s == expectedService {
			foundService = true
			break
		}
	}
	if !foundService {
		t.Errorf("Expected service %q not found in %v", expectedService, parsed.Services)
	}

	// Verify TXT Records
	if val, ok := parsed.TXTRecords["fn"]; !ok || val != "Living Room TV" {
		t.Errorf("Expected TXT fn='Living Room TV', got %q", val)
	}
	if val, ok := parsed.TXTRecords["md"]; !ok || val != "Chromecast" {
		t.Errorf("Expected TXT md='Chromecast', got %q", val)
	}

	// Verify Metadata
	if parsed.Interface != "eth0" {
		t.Errorf("Expected interface 'eth0', got %q", parsed.Interface)
	}
	if parsed.SrcIP != srcIP.String() {
		t.Errorf("Expected SrcIP %q, got %q", srcIP.String(), parsed.SrcIP)
	}
}

func TestParseMDNSPacket_Invalid(t *testing.T) {
	// Empty packet
	_, err := ParseMDNSPacket([]byte{}, net.IP{}, "eth0")
	if err == nil {
		t.Error("Expected error for empty packet")
	}

	// Garbage packet
	_, err = ParseMDNSPacket([]byte{0x00, 0x01, 0x02}, net.IP{}, "eth0")
	if err == nil {
		t.Error("Expected error for garbage packet")
	}
}
