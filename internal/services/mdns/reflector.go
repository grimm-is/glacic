package mdns

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
"grimm.is/glacic/internal/clock"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// MDNSPort is the multicast DNS port
	MDNSPort = 5353
	// MaxPacketSize is generally 1500 for MTU, but could be larger.
	MaxPacketSize = 4096 // Safe upper bound
)

// MDNS Groups
var (
	mdnsIPv4Addr = net.ParseIP("224.0.0.251")
	mdnsIPv6Addr = net.ParseIP("ff02::fb")
)

// Config holds the configuration for the mDNS reflector.
type Config struct {
	Enabled    bool
	Interfaces []string
}

// Reflector forwards mDNS packets between interfaces.
type Reflector struct {
	config Config
	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Interfaces we are listening on (mapped by index/name)
	ifaces map[string]*net.Interface
}

// NewReflector creates a new mDNS reflector.
func NewReflector(cfg Config) *Reflector {
	return &Reflector{
		config: cfg,
		ifaces: make(map[string]*net.Interface),
	}
}

// Start starts the mDNS reflector service.
func (r *Reflector) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.config.Enabled || len(r.config.Interfaces) < 2 {
		return nil // Nothing to do
	}

	ctx, r.cancel = context.WithCancel(ctx)

	log.Printf("[mDNS] Starting reflector on: %v", r.config.Interfaces)

	// Resolve interfaces
	for _, name := range r.config.Interfaces {
		iface, err := net.InterfaceByName(name)
		if err != nil {
			log.Printf("[mDNS] Interface %s not found: %v", name, err)
			continue
		}
		if iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagUp == 0 {
			log.Printf("[mDNS] Interface %s is not multicast enabled or up", name)
			continue
		}
		r.ifaces[name] = iface
	}

	if len(r.ifaces) < 2 {
		log.Printf("[mDNS] Not enough valid interfaces to reflect between")
		return nil
	}

	// Start listeners logic
	// We need two main listeners: one for IPv4 and one for IPv6
	// We bind to 0.0.0.0:5353 and [::]:5353

	// Open IPv4 socket
	conn4, err := net.ListenPacket("udp4", ":5353")
	if err != nil {
		return fmt.Errorf("failed to listen on udp4 :5353: %w", err)
	}
	pc4 := ipv4.NewPacketConn(conn4)

	// Join groups on all interfaces
	for _, iface := range r.ifaces {
		if err := pc4.JoinGroup(iface, &net.UDPAddr{IP: mdnsIPv4Addr}); err != nil {
			log.Printf("[mDNS] Failed to join IPv4 mDNS group on %s: %v", iface.Name, err)
		} else {
			// Enable ControlMessage to get recv interface
			if err := pc4.SetControlMessage(ipv4.FlagInterface, true); err != nil {
				log.Printf("[mDNS] Failed to set control msg on %s: %v", iface.Name, err)
			}
		}
	}

	// Open IPv6 socket
	conn6, err := net.ListenPacket("udp6", ":5353")
	if err != nil {
		log.Printf("[mDNS] Failed to listen on udp6 :5353: %v (skipping IPv6)", err)
		// Proceed with IPv4 only
		conn6 = nil
	}

	var pc6 *ipv6.PacketConn
	if conn6 != nil {
		pc6 = ipv6.NewPacketConn(conn6)
		for _, iface := range r.ifaces {
			if err := pc6.JoinGroup(iface, &net.UDPAddr{IP: mdnsIPv6Addr}); err != nil {
				log.Printf("[mDNS] Failed to join IPv6 mDNS group on %s: %v", iface.Name, err)
			} else {
				if err := pc6.SetControlMessage(ipv6.FlagInterface, true); err != nil {
					log.Printf("[mDNS] Failed to set control msg on %s: %v", iface.Name, err)
				}
			}
		}
	}

	// Start worker loops
	r.wg.Add(1)
	go r.runIPv4(ctx, pc4)

	if pc6 != nil {
		r.wg.Add(1)
		go r.runIPv6(ctx, pc6)
	}

	return nil
}

// Stop stops the reflector.
func (r *Reflector) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.wg.Wait()
	log.Printf("[mDNS] Stopped")
}

func (r *Reflector) runIPv4(ctx context.Context, pc *ipv4.PacketConn) {
	defer r.wg.Done()
	defer pc.Close()

	buf := make([]byte, MaxPacketSize)

	// Filter own packets to avoid loops
	// However, since we write to socket, we might receive our own multicast?
	// Usually IP_MULTICAST_LOOP defaults to 1. We should probably set it to 0 or 1.
	// We'll rely on checking source IP vs interface IPs? Or just Interface ID.

	// Enable loopback? If we disable loopback, we won't see our own packets.
	pc.SetMulticastLoopback(false)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Set read deadline to check for ctx cancel periodically
			pc.SetReadDeadline(clock.Now().Add(1 * time.Second))

			n, cm, _, err := pc.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "closed network connection") {
					return
				}
				// Timeout is fine
				continue
			}

			if cm == nil {
				continue // Need control message to know source interface
			}

			// Validate source interface is one of ours
			var srcIfaceName string
			// We iterate because we only have IfIndex
			// Optimization: maintain a map[int]string for index->name
			for name, iface := range r.ifaces {
				if iface.Index == cm.IfIndex {
					srcIfaceName = name
					break
				}
			}

			if srcIfaceName == "" {
				// Packet from unknown interface, ignore
				continue
			}

			// Forward to valid destinations
			r.forwardIPv4(pc, buf[:n], cm.IfIndex, srcIfaceName)

			if srcIfaceName == "" {
				// Packet from unknown interface, ignore
				continue
			}

			// Forward to valid destinations
			r.forwardIPv4(pc, buf[:n], cm.IfIndex, srcIfaceName)
		}
	}
}

func (r *Reflector) runIPv6(ctx context.Context, pc *ipv6.PacketConn) {
	defer r.wg.Done()
	defer pc.Close()

	buf := make([]byte, MaxPacketSize)
	pc.SetMulticastLoopback(false)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			pc.SetReadDeadline(clock.Now().Add(1 * time.Second))
			n, cm, _, err := pc.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "closed network connection") {
					return
				}
				continue
			}

			if cm == nil {
				continue
			}

			// Simple forward logic
			// NOTE: We need to set HopLimit to 255 for mDNS compliance on egress?
			// RFC 6762 says IP TTL must be 255.

			r.forwardIPv6(pc, buf[:n], cm.IfIndex)
		}
	}
}

func (r *Reflector) forwardIPv4(pc *ipv4.PacketConn, data []byte, srcIndex int, srcName string) {
	// Replicate to all other interfaces
	for _, iface := range r.ifaces {
		if iface.Index == srcIndex {
			continue // Don't reflect back to source
		}

		// Set outgoing interface
		cm := &ipv4.ControlMessage{
			IfIndex: iface.Index,
		}

		// Destination is always multicast group
		dst := &net.UDPAddr{IP: mdnsIPv4Addr, Port: MDNSPort}

		// Set TTL 255
		pc.SetMulticastTTL(255)

		// Write
		if _, err := pc.WriteTo(data, cm, dst); err != nil {
			// Log error but don't spam
			// log.Printf("[mDNS] Failed to write to %s: %v", name, err)
		}
	}
}

func (r *Reflector) forwardIPv6(pc *ipv6.PacketConn, data []byte, srcIndex int) {
	for _, iface := range r.ifaces {
		if iface.Index == srcIndex {
			continue
		}

		cm := &ipv6.ControlMessage{
			IfIndex: iface.Index,
		}

		dst := &net.UDPAddr{IP: mdnsIPv6Addr, Port: MDNSPort}

		pc.SetMulticastHopLimit(255)

		if _, err := pc.WriteTo(data, cm, dst); err != nil {
			// log.Printf("[mDNS] Failed to write to %s: %v", name, err)
		}
	}
}
