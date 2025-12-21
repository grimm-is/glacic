package mcast

import (
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"golang.org/x/net/ipv4"
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
	conn, err := net.ListenPacket("udp4", fmt.Sprintf("0.0.0.0:%d", group.Port))
	if err != nil {
		return fmt.Errorf("listen failed: %v", err)
	}
	defer conn.Close()

	pc := ipv4.NewPacketConn(conn)
	if err := pc.JoinGroup(iface, group); err != nil {
		return fmt.Errorf("JoinGroup failed: %v", err)
	}

	if err := pc.SetControlMessage(ipv4.FlagInterface, true); err != nil {
		return fmt.Errorf("SetControlMessage failed: %v", err)
	}

	log.Printf("Listening on %s %s...", iface.Name, group.String())

	buf := make([]byte, 1024)
	
	// Timeout
	conn.SetReadDeadline(time.Now().Add(timeout))

	for {
		n, cm, src, err := pc.ReadFrom(buf)
		if err != nil {
			return fmt.Errorf("read error: %v", err)
		}
		
		if cm != nil && cm.IfIndex != iface.Index {
			continue 
		}

		fmt.Printf("RECEIVED: %s FROM: %s\n", string(buf[:n]), src.String())
		// exit after one packet for simple test
		return nil
	}
}

func send(iface *net.Interface, group *net.UDPAddr, msg string) error {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return fmt.Errorf("listen failed: %v", err)
	}
	defer conn.Close()

	pc := ipv4.NewPacketConn(conn)
	pc.SetMulticastInterface(iface)
	pc.SetMulticastTTL(255)

	if _, err := pc.WriteTo([]byte(msg), nil, group); err != nil {
		return fmt.Errorf("write failed: %v", err)
	}
	log.Printf("Sent message to %s via %s", group.String(), iface.Name)
	return nil
}
