package mcast

import (
	"flag"
	"fmt"
	"log"
	"net"
	"time"
)

func Run(args []string) error {
	fs := flag.NewFlagSet("mcast", flag.ExitOnError)
	mode := fs.String("mode", "listen", "listen or send")
	addr := fs.String("addr", "224.0.0.251:5353", "Multicast address:port")
	ifaceName := fs.String("iface", "", "Interface to use")
	msg := fs.String("msg", "Glacic mDNS Test", "Message to send")
	timeout := fs.Duration("timeout", 10*time.Second, "Timeout for listen")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *ifaceName == "" {
		return fmt.Errorf("interface must be specified")
	}

	iface, err := net.InterfaceByName(*ifaceName)
	if err != nil {
		return fmt.Errorf("interface %s not found: %v", *ifaceName, err)
	}

	udpAddr, err := net.ResolveUDPAddr("udp", *addr)
	if err != nil {
		return fmt.Errorf("invalid address: %v", err)
	}

	if *mode == "listen" {
		return listen(iface, udpAddr, *timeout)
	} else if *mode == "send" {
		return send(iface, udpAddr, *msg)
	} else {
		return fmt.Errorf("unknown mode: %s", *mode)
	}
}

func listen(iface *net.Interface, group *net.UDPAddr, timeout time.Duration) error {
	conn, err := net.ListenMulticastUDP("udp4", iface, group)
	if err != nil {
		return fmt.Errorf("listen multicast failed: %v", err)
	}
	defer conn.Close()

	log.Printf("Listening on %s %s...", iface.Name, group.String())

	buf := make([]byte, 1024)

	// Timeout
	conn.SetReadDeadline(time.Now().Add(timeout))

	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			return fmt.Errorf("read error: %v", err)
		}

		// Filter for self-sent packets? No, we want to receive whatever comes.
		// Since ListenMulticastUDP binds to the group, we rely on kernel filtering.

		fmt.Printf("RECEIVED: %s FROM: %s\n", string(buf[:n]), src.String())
		// exit after one packet for simple test
		return nil
	}
}

func send(iface *net.Interface, group *net.UDPAddr, msg string) error {
	// Standard net package send (relies on routing)
	conn, err := net.DialUDP("udp4", nil, group)
	if err != nil {
		return fmt.Errorf("dial failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write failed: %v", err)
	}
	log.Printf("Sent message to %s", group.String())
	return nil
}
