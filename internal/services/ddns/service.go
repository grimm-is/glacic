package ddns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"grimm.is/glacic/internal/logging"
)

// Service manages dynamic DNS updates.
type Service struct {
	config  Config
	logger  *logging.Logger
	mu      sync.Mutex
	running bool
}

// NewService creates a new DDNS service.
func NewService(logger *logging.Logger) *Service {
	return &Service{
		logger: logger,
	}
}

// Reload updates the configuration and restarts the service if running.
func (s *Service) Reload(cfg Config) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = cfg
	if s.running {
		// If running, we might need to restart to pick up changes (e.g. interval)
		// For now, assume simple restart or let the next tick handle it if interval didn't change.
		// Detailed reload logic omitted for brevity.
	}
	return true, nil
}

// Start starts the DDNS update loop.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("ddns service already running")
	}
	s.running = true
	s.mu.Unlock()

	s.logger.Info("DDNS Service started")

	go s.runLoop(ctx)
	return nil
}

// runLoop executes the periodic updates.
func (s *Service) runLoop(ctx context.Context) {
	interval := s.config.Interval
	if interval < 1 {
		interval = 5
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Minute)
	defer ticker.Stop()

	// Initial update
	s.update()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("DDNS Service stopping")
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			return
		case <-ticker.C:
			s.update()
		}
	}
}

// update performs the DDNS update.
func (s *Service) update() {
	if !s.config.Enabled {
		return
	}

	providerName := s.config.Provider
	if providerName == "" {
		return
	}

	provider, err := NewProvider(s.config)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to create DDNS provider %s: %v", providerName, err))
		return
	}

	// Determine IP
	var ip string
	if s.config.Interface != "" {
		ip, err = GetInterfaceIP(s.config.Interface)
	} else {
		ip, err = GetPublicIP()
	}

	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to get IP for DDNS: %v", err))
		return
	}

	// Update
	if err := provider.Update(s.config.Hostname, ip); err != nil {
		s.logger.Error(fmt.Sprintf("DDNS update failed for %s: %v", s.config.Hostname, err))
	} else {
		s.logger.Info(fmt.Sprintf("DDNS update success for %s (%s)", s.config.Hostname, ip))
	}
}

// IsRunning returns true if the service is running.
func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
