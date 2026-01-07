package lldp

import (
	"context"
	"log"
	"sync"
	"time"

	"grimm.is/glacic/internal/network/lldp"
)

// Neighbor info augmented with timestamp
type Neighbor struct {
	Interface string
	Info      *lldp.DiscoveryInfo
	LastSeen  time.Time
}

// Service manages LLDP listeners and neighbor state
type Service struct {
	mu        sync.RWMutex
	neighbors map[string]*Neighbor // Interface Name -> Neighbor

	listeners map[string]*lldp.Listener
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewService creates a new LLDP service
func NewService() *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		neighbors: make(map[string]*Neighbor),
		listeners: make(map[string]*lldp.Listener),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start (placeholder, really needs list of interfaces or external trigger)
func (s *Service) Start() {
	// In a real implementation, we would subscribe to netlink updates
	// or be told which interfaces to listen on.
	// For now, we provide API to AddInterface/RemoveInterface
	log.Println("[LLDP] Service started")
}

// Stop stops the service
func (s *Service) Stop() {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.listeners {
		l.Close()
	}
	log.Println("[LLDP] Service stopped")
}

// AddInterface starts listening on an interface
func (s *Service) AddInterface(ifaceName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.listeners[ifaceName]; exists {
		return nil // Already listening
	}

	l, err := lldp.NewListener(ifaceName)
	if err != nil {
		return err
	}

	s.listeners[ifaceName] = l

	// Start reader loop
	go s.readLoop(l, ifaceName)

	log.Printf("[LLDP] Listening on %s", ifaceName)
	return nil
}

// RemoveInterface stops listening on an interface
func (s *Service) RemoveInterface(ifaceName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if l, exists := s.listeners[ifaceName]; exists {
		l.Close()
		delete(s.listeners, ifaceName)
		delete(s.neighbors, ifaceName) // Remove neighbor state too? Or keep as stale?
	}
}

func (s *Service) readLoop(l *lldp.Listener, ifaceName string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[LLDP] CRITICAL: readLoop panic for %s: %v", ifaceName, r)
		}
	}()
	for {
		info, err := l.ReadPacket(s.ctx)
		if err != nil {
			// If context cancelled, exit
			if s.ctx.Err() != nil {
				return
			}
			// Temporary error or interface down?
			// Ideally backoff
			time.Sleep(1 * time.Second)
			continue
		}

		s.updateNeighbor(ifaceName, info)
	}
}

func (s *Service) updateNeighbor(ifaceName string, info *lldp.DiscoveryInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.neighbors[ifaceName] = &Neighbor{
		Interface: ifaceName,
		Info:      info,
		LastSeen:  time.Now(),
	}
}

// GetNeighbors returns a copy of current neighbor state
func (s *Service) GetNeighbors() []Neighbor {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]Neighbor, 0, len(s.neighbors))
	for _, n := range s.neighbors {
		// Prune old? (TTL)
		// For display, might show "Last seen Xs ago"
		res = append(res, *n)
	}
	return res
}
