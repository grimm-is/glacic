package mdns

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/upgrade"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
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
	// EventCallback is called when an mDNS packet is parsed (optional)
	EventCallback func(parsed *ParsedMDNS)
}

// Reflector forwards mDNS packets between interfaces.
type Reflector struct {
	config Config
	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Interfaces we are listening on (mapped by index/name)
	ifaces map[string]*net.Interface

	// Event callback for device profiling
	eventCallback func(parsed *ParsedMDNS)

	logger       *logging.Logger
	shutdown     chan struct{}
	upgradeMgr   *upgrade.Manager
	firstAttempt bool
}

// NewReflector creates a new mDNS reflector.
func NewReflector(cfg Config, logger *logging.Logger) *Reflector {
	return &Reflector{
		config:        cfg,
		ifaces:        make(map[string]*net.Interface),
		eventCallback: cfg.EventCallback,
		logger:        logger,
		shutdown:      make(chan struct{}),
		firstAttempt:  true,
	}
}

// SetUpgradeManager sets the upgrade manager.
func (r *Reflector) SetUpgradeManager(mgr *upgrade.Manager) {
	r.upgradeMgr = mgr
}

// SetEventCallback sets or updates the event callback for mDNS device profiling.
// This can be called after Start() to add a callback when the collector is ready.
func (r *Reflector) SetEventCallback(cb func(parsed *ParsedMDNS)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventCallback = cb
}

// Start starts the mDNS reflector service.
func (r *Reflector) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.config.Enabled || len(r.config.Interfaces) == 0 {
		return nil // Nothing to do
	}

	ctx, r.cancel = context.WithCancel(ctx)

	r.logger.Info("Starting reflector", "interfaces", r.config.Interfaces)

	// Resolve interfaces
	r.logger.Info("mDNS: Resolving interfaces", "configured", r.config.Interfaces)
	for _, name := range r.config.Interfaces {
		iface, err := net.InterfaceByName(name)
		if err != nil {
			r.logger.Error("Interface not found", "interface", name, "error", err)
			continue
		}
		if iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagUp == 0 {
			r.logger.Warn("Interface is not multicast enabled or up", "interface", name)
			continue
		}
		r.ifaces[name] = iface
	}

	if len(r.ifaces) < 2 && r.eventCallback == nil {
		r.logger.Info("Not enough valid interfaces to reflect between and no event callback configured", "valid_ifaces", len(r.ifaces))
		return nil
	}

	// Launch background retry loop to handle socket binding (e.g. if port is held during upgrade)
	r.wg.Add(1)
	go r.retryStartLoop(ctx)

	return nil
}

func (r *Reflector) retryStartLoop(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Initial attempt
	if err := r.attemptStart(ctx); err == nil {
		return
	} else {
		r.logger.Error("Failed to bind (will retry)", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.attemptStart(ctx); err == nil {
				r.logger.Info("Successfully bound and started")
				return
			} else {
				r.logger.Error("Retry bind failed", "error", err)
			}
		}
	}
}

// attemptStart tries to bind sockets and start workers. Returns error if binding fails.
func (r *Reflector) attemptStart(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var conn4 net.PacketConn
	var conn6 net.PacketConn
	var err error

	// Helper to create or inherit a PacketConn
	getOrCreatePacketConn := func(network, address, connName string) (net.PacketConn, error) {
		var conn net.PacketConn
		// 1. Check inherited socket (only on first attempt)
		if r.firstAttempt && r.upgradeMgr != nil {
			if existing, ok := r.upgradeMgr.GetPacketConn(connName); ok {
				conn = existing
				r.logger.Info("Inherited mDNS socket", "name", connName)
			}
		}

		// 2. Bind new if needed
		if conn == nil {
			var lc net.ListenConfig
			lc.Control = func(network, address string, c syscall.RawConn) error {
				var opErr error
				err := c.Control(func(fd uintptr) {
					opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
					if opErr != nil {
						return
					}
					opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
				})
				if err != nil {
					return err
				}
				return opErr
			}

			c, err := lc.ListenPacket(ctx, network, address)
			if err != nil {
				return nil, fmt.Errorf("failed to bind %s %s: %w", network, address, err)
			}
			conn = c

			// Register only if success
			if r.upgradeMgr != nil {
				r.upgradeMgr.RegisterPacketConn(connName, conn)
			}
		}
		return conn, nil
	}

	// Open IPv4 socket
	r.logger.Info("Attempting to bind udp4 :5353")
	conn4, err = getOrCreatePacketConn("udp4", ":5353", "mdns-udp4-5353")
	if err != nil {
		r.logger.Error("Failed to bind udp4", "error", err)
		return err
	}
	if conn4 == nil { // Should not happen if error is returned
		return fmt.Errorf("failed to get or create udp4 connection")
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
	// Open IPv6 socket (Soft fail)
	r.logger.Info("Attempting to bind udp6 [::]:5353")
	conn6, err = getOrCreatePacketConn("udp6", "[::]:5353", "mdns-udp6-5353")
	if err != nil {
		r.logger.Warn("Failed to listen on udp6 :5353 (continuing with IPv4 only)", "error", err)
		// Do not return error, continue with conn6=nil
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

			n, cm, src, err := pc.ReadFrom(buf)
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

			// Parse mDNS packet for device profiling (if callback configured)
			if r.eventCallback != nil {
				srcIP := net.IP{}
				if src != nil {
					if udpAddr, ok := src.(*net.UDPAddr); ok {
						srcIP = udpAddr.IP
					}
				}
				r.parseAndEmit(buf[:n], srcIP, srcIfaceName)
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

// parseAndEmit parses an mDNS packet and emits the result to the event callback
func (r *Reflector) parseAndEmit(data []byte, srcIP net.IP, iface string) {
	if r.eventCallback == nil {
		return
	}

	parsed, err := ParseMDNSPacket(data, srcIP, iface)
	if err != nil {
		// Parse errors are common for malformed or non-DNS packets, ignore silently
		return
	}

	// Only emit if we got useful data
	if parsed.Hostname != "" || len(parsed.Services) > 0 || len(parsed.TXTRecords) > 0 {
		log.Printf("[MDNS] Detected: src=%s host=%s services=%v", srcIP, parsed.Hostname, parsed.Services)
		r.eventCallback(parsed)
	}
}
