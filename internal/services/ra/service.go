package ra

import (
	"context"
	"log"
	"net"
	"net/netip"
	"sync"
	"time"

	"grimm.is/glacic/internal/config"

	"github.com/mdlayher/ndp"
)

// Service provides IPv6 Router Advertisements.
type Service struct {
	config []config.Interface

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
	mu      sync.Mutex
}

func NewService(cfg *config.Config) *Service {
	// Filter for interfaces with RA enabled
	var raIfaces []config.Interface
	for _, iface := range cfg.Interfaces {
		if iface.RA && len(iface.IPv6) > 0 {
			raIfaces = append(raIfaces, iface)
		}
	}

	return &Service{
		config: raIfaces,
	}
}

func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.running = true
	s.ctx, s.cancel = context.WithCancel(context.Background())

	for _, iface := range s.config {
		s.wg.Add(1)
		go s.runRA(iface)
	}
	log.Printf("[RA] Service started on %d interfaces", len(s.config))
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.cancel()
	s.wg.Wait()
	s.running = false
	log.Printf("[RA] Service stopped")
}

func (s *Service) runRA(cfg config.Interface) {
	defer s.wg.Done()

	// Open NDP connection
	ifi, err := net.InterfaceByName(cfg.Name)
	if err != nil {
		log.Printf("[RA] Failed to get interface %s: %v", cfg.Name, err)
		return
	}

	conn, _, err := ndp.Listen(ifi, ndp.LinkLocal)
	if err != nil {
		log.Printf("[RA] Failed to listen on %s: %v", cfg.Name, err)
		return
	}
	defer conn.Close()

	// Parse prefixes from config
	var prefixOpts []ndp.Option
	for _, cidr := range cfg.IPv6 {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			log.Printf("[RA] Invalid IPv6 prefix %q on %s: %v", cidr, cfg.Name, err)
			continue
		}
		// Skip link-local prefixes
		if prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		prefixOpts = append(prefixOpts, &ndp.PrefixInformation{
			PrefixLength:                   uint8(prefix.Bits()),
			OnLink:                         true,
			AutonomousAddressConfiguration: true,                // Enable SLAAC
			ValidLifetime:                  86400 * time.Second, // 1 day
			PreferredLifetime:              14400 * time.Second, // 4 hours
			Prefix:                         prefix.Addr(),
		})
	}

	if len(prefixOpts) == 0 {
		log.Printf("[RA] No valid prefixes for %s, RA will not advertise SLAAC", cfg.Name)
	}

	// Send RA every 30s + respond to RS
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Channel for RS events
	rsChan := make(chan netip.Addr, 10)

	// Start RS listener goroutine
	go s.listenForRS(conn, cfg.Name, rsChan)

	// Send initial RA immediately
	s.sendRA(conn, cfg.Name, prefixOpts)

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// Periodic unsolicited RA
			s.sendRA(conn, cfg.Name, prefixOpts)
		case srcAddr := <-rsChan:
			// Respond to Router Solicitation with unicast RA
			s.sendRATo(conn, cfg.Name, prefixOpts, srcAddr)
		}
	}
}

// listenForRS listens for Router Solicitation messages and notifies the main loop.
func (s *Service) listenForRS(conn *ndp.Conn, ifaceName string, rsChan chan<- netip.Addr) {
	for {
		// Set read deadline to allow periodic context checks
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		msg, _, src, err := conn.ReadFrom()
		if err != nil {
			// Check if we should stop
			select {
			case <-s.ctx.Done():
				return
			default:
				// Timeout or other error, continue
				continue
			}
		}

		// Only process Router Solicitation messages
		if _, ok := msg.(*ndp.RouterSolicitation); ok {
			log.Printf("[RA] Received RS from %s on %s", src, ifaceName)
			select {
			case rsChan <- src:
			default:
				// Channel full, drop (will get periodic RA anyway)
			}
		}
	}
}

func (s *Service) sendRA(conn *ndp.Conn, ifaceName string, prefixOpts []ndp.Option) {
	ra := &ndp.RouterAdvertisement{
		CurrentHopLimit:           64,
		RouterSelectionPreference: ndp.Medium,
		RouterLifetime:            1800 * time.Second, // 30 mins
		Options:                   prefixOpts,
	}

	dst, _ := netip.ParseAddr("ff02::1")
	if err := conn.WriteTo(ra, nil, dst); err != nil {
		log.Printf("[RA] Failed to send RA on %s: %v", ifaceName, err)
	}
}

// sendRATo sends a Router Advertisement to a specific address (unicast response to RS).
func (s *Service) sendRATo(conn *ndp.Conn, ifaceName string, prefixOpts []ndp.Option, dst netip.Addr) {
	ra := &ndp.RouterAdvertisement{
		CurrentHopLimit:           64,
		RouterSelectionPreference: ndp.Medium,
		RouterLifetime:            1800 * time.Second, // 30 mins
		Options:                   prefixOpts,
	}

	if err := conn.WriteTo(ra, nil, dst); err != nil {
		log.Printf("[RA] Failed to send RA to %s on %s: %v", dst, ifaceName, err)
	}
}
