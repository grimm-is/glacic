package mdns

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func Run(args []string) error {
	fs := flag.NewFlagSet("mdns-publish", flag.ContinueOnError)
	ifaceName := fs.String("iface", "eth0", "Interface to publish on")
	hostname := fs.String("host", "test-device.local", "Hostname to announce")
	service := fs.String("service", "_googlecast._tcp", "Service type")
	port := fs.Int("port", 8009, "Service port")
	txt := fs.String("txt", "fn=Test Device", "TXT record (comma separated)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Printf("Publishing mDNS on %s: %s (%s)\n", *ifaceName, *hostname, *service)

	// Ensure hostname ends with dot
	host := *hostname
	if !strings.HasSuffix(host, ".") {
		host += "."
	}

	// Construct Packet
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		Response:      true,
		Authoritative: true,
	})

	// Answers
	b.StartAnswers()

	svcName := fmt.Sprintf("%s.local.", *service)
	instanceName := fmt.Sprintf("TestDevice.%s.local.", *service)

	// PTR
	b.PTRResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName(svcName),
		Type:  dnsmessage.TypePTR,
		Class: dnsmessage.ClassINET,
		TTL:   120,
	}, dnsmessage.PTRResource{
		PTR: dnsmessage.MustNewName(instanceName),
	})

	// SRV
	b.SRVResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName(instanceName),
		Type:  dnsmessage.TypeSRV,
		Class: dnsmessage.ClassINET,
		TTL:   120,
	}, dnsmessage.SRVResource{
		Priority: 0,
		Weight:   0,
		Port:     uint16(*port),
		Target:   dnsmessage.MustNewName(host),
	})

	// A Record (Fake IP)
	b.AResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName(host),
		Type:  dnsmessage.TypeA,
		Class: dnsmessage.ClassINET,
		TTL:   120,
	}, dnsmessage.AResource{
		A: [4]byte{192, 168, 1, 100},
	})

	// TXT
	txtParts := strings.Split(*txt, ",")
	b.TXTResource(dnsmessage.ResourceHeader{
		Name:  dnsmessage.MustNewName(instanceName),
		Type:  dnsmessage.TypeTXT,
		Class: dnsmessage.ClassINET,
		TTL:   120,
	}, dnsmessage.TXTResource{
		TXT: txtParts,
	})

	packet, err := b.Finish()
	if err != nil {
		return fmt.Errorf("failed to build packet: %v", err)
	}

	// Send
	addr, err := net.ResolveUDPAddr("udp4", "224.0.0.251:5353")
	if err != nil {
		return err
	}

	c, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return err
	}
	defer c.Close()

	if p, ok := c.(interface {
		SetMulticastInterface(iface *net.Interface) error
	}); ok {
		iface, err := net.InterfaceByName(*ifaceName)
		if err == nil {
			p.SetMulticastInterface(iface)
		}
	}

	// Send repeatedly
	for i := 0; i < 5; i++ {
		_, err = c.WriteTo(packet, addr)
		if err != nil {
			fmt.Printf("Error sending: %v\n", err)
		} else {
			fmt.Println("Sent mDNS announcement")
		}
		time.Sleep(1 * time.Second)
	}

	return nil
}
