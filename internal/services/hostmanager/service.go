package hostmanager

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
)

// Service manages dynamic hostname resolution and IPSet updates.
type Service struct {
	cfg    *config.Config
	logger *logging.Logger
	stop   chan struct{}
	wg     sync.WaitGroup
}

// New creates a new HostManager service.
func New(cfg *config.Config, logger *logging.Logger) *Service {
	return &Service{
		cfg:    cfg,
		logger: logger,
		stop:   make(chan struct{}),
	}
}

// Start begins the background resolution tasks.
func (s *Service) Start() error {
	s.logger.Info("Starting HostManager service")

	found := false
	for _, ipset := range s.cfg.IPSets {
		if ipset.Type == "dns" && len(ipset.Domains) > 0 {
			found = true
			s.wg.Add(1)
			go s.manageSet(ipset)
		}
	}

	if !found {
		s.logger.Info("No DNS-based IPSets configured")
	}

	return nil
}

// Stop stops the service.
func (s *Service) Stop() error {
	s.logger.Info("Stopping HostManager service")
	close(s.stop)
	s.wg.Wait()
	return nil
}

func (s *Service) manageSet(ipset config.IPSet) {
	defer s.wg.Done()

	interval := 5 * time.Minute
	if ipset.RefreshInterval != "" {
		if d, err := time.ParseDuration(ipset.RefreshInterval); err == nil {
			interval = d
		}
	}

	s.logger.Info("Managing DNS set", "name", ipset.Name, "interval", interval)

	// Initial update
	s.updateSet(ipset)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.updateSet(ipset)
		}
	}
}

func (s *Service) updateSet(ipset config.IPSet) {
	var allIPs []string

	// Resolve all domains
	for _, domain := range ipset.Domains {
		// Resolve IPv4
		ips, err := net.LookupIP(domain)
		if err != nil {
			s.logger.Error("Failed to resolve domain", "domain", domain, "error", err)
			continue
		}
		for _, ip := range ips {
			// Only include IPv4 for now as default type is ipv4_addr.
			// TODO: Support ipv6_addr type sets.
			if ip4 := ip.To4(); ip4 != nil {
				allIPs = append(allIPs, ip4.String())
			}
		}
	}

	if len(allIPs) == 0 {
		return
	}

	// Update nftables set
	// Strategy: Flush and refill (Atomic updates in set are hard without diffing).
	// "nft flush set inet filter <name>"
	// "nft add element inet filter <name> { ... }"
	
	// Construct element string
	elements := strings.Join(allIPs, ", ")
	
	// We need to execute nft commands.
	// Using exec for simplicity rather than native library to avoid dependency complexity for this task.
	// Note: In production, use google/nftables.
	
	// 1. Flush
	cmdFlush := exec.Command("nft", "flush", "set", "inet", "filter", ipset.Name)
	if err := cmdFlush.Run(); err != nil {
		s.logger.Error("Failed to flush set", "set", ipset.Name, "error", err)
		return 
	}
	
	// 2. Add elements
	// Using --file via stdin typically safer for large sets, but command line OK for small.
	cmdAdd := exec.Command("nft", "add", "element", "inet", "filter", ipset.Name, fmt.Sprintf("{ %s }", elements))
	if output, err := cmdAdd.CombinedOutput(); err != nil {
		s.logger.Error("Failed to update set elements", "set", ipset.Name, "error", err, "output", string(output))
	} else {
		s.logger.Info("Updated DNS set", "set", ipset.Name, "count", len(allIPs))
	}
}
