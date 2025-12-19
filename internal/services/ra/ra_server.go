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

	// Note: Prefix parsing from cfg.IPv6 is deferred - the current implementation
	// sends minimal RAs. Full prefix info options can be added when needed.

	ticker := time.NewTicker(10 * time.Second) // Send RA every 10s (too frequent for prod, good for test)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// Construct RA
			ra := &ndp.RouterAdvertisement{
				CurrentHopLimit:           64,
				RouterSelectionPreference: ndp.Medium,
				RouterLifetime:            1800 * time.Second, // 30 mins
				Options:                   []ndp.Option{
					// Prefix Option
					// MTU Option
				},
			}

			// Send to multicast
			// to := &net.IPAddr{IP: net.IPv6linklocalallrouters} // Unused for now
			// RAs are sent to LinkLocalAllNodes (ff02::1)
			// netip.Addr is required by mdlayher/ndp v1
			dst, _ := netip.ParseAddr("ff02::1")

			if err := conn.WriteTo(ra, nil, dst); err != nil {
				log.Printf("[RA] Failed to send RA on %s: %v", cfg.Name, err)
			}
		}
	}
}
