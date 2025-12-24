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
			AutonomousAddressConfiguration: true, // Enable SLAAC
			ValidLifetime:                  86400 * time.Second, // 1 day
			PreferredLifetime:              14400 * time.Second, // 4 hours
			Prefix:                         prefix.Addr(),
		})
	}

	if len(prefixOpts) == 0 {
		log.Printf("[RA] No valid prefixes for %s, RA will not advertise SLAAC", cfg.Name)
	}

	ticker := time.NewTicker(30 * time.Second) // Send RA every 30s
	defer ticker.Stop()

	// Send initial RA immediately
	s.sendRA(conn, cfg.Name, prefixOpts)

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.sendRA(conn, cfg.Name, prefixOpts)
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

