package threatintel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/services/dns"
)

// Service manages threat intelligence polling and enforcement.
type Service struct {
	config   *config.ThreatIntel
	fwMgr    *firewall.Manager // Need access to IPSetManager mainly
	ipsetMgr *firewall.IPSetManager
	dnsSvc   *dns.Service

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	running bool
}

// NewService creates a new threat intelligence service.
// Note: We need IPSetManager, but currently Manager encapsulates it.
// We might need to expose it or add helper methods on Manager.
// For now let's assume we can create a new IPSetManager instance since it's stateless wrapper around nft/runner.
// NewService creates a new threat intelligence service.
func NewService(cfg *config.ThreatIntel, dnsSvc *dns.Service, ipsetMgr *firewall.IPSetManager) *Service {
	if cfg == nil || !cfg.Enabled {
		return nil
	}

	// If no manager provided, create default (backward compatibility/convenience, though explicit is better)
	if ipsetMgr == nil {
		ipsetMgr = firewall.NewIPSetManager("firewall")
	}

	return &Service{
		config:   cfg,
		ipsetMgr: ipsetMgr,
		dnsSvc:   dnsSvc,
	}
}

// Start starts the threat intelligence polling service.
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	log.Printf("[ThreatIntel] Starting service...")
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.running = true

	// Parse interval
	interval := 1 * time.Hour
	if s.config.Interval != "" {
		if d, err := time.ParseDuration(s.config.Interval); err == nil {
			interval = d
		}
	}

	s.wg.Add(1)
	go s.runLoop(interval)
}

// Stop stops the service.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	log.Printf("[ThreatIntel] Stopping service...")
	s.cancel()
	s.wg.Wait()
	s.running = false
}

func (s *Service) runLoop(interval time.Duration) {
	defer s.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial poll
	s.Poll()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.Poll()
		}
	}
}

// Poll fetches indicators from all configured sources.
func (s *Service) Poll() {
	log.Printf("[ThreatIntel] Polling %d sources...", len(s.config.Sources))

	var allIPv4 []string
	var allIPv6 []string
	var allDomains []string

	for _, source := range s.config.Sources {
		v4, v6, domains, err := s.fetchSource(source)
		if err != nil {
			log.Printf("[ThreatIntel] Failed to fetch source %s: %v", source.Name, err)
			continue
		}
		log.Printf("[ThreatIntel] Source %s returned: %d IPv4, %d IPv6, %d Domains",
			source.Name, len(v4), len(v6), len(domains))

		allIPv4 = append(allIPv4, v4...)
		allIPv6 = append(allIPv6, v6...)
		allDomains = append(allDomains, domains...)
	}

	s.Enforce(allIPv4, allIPv6, allDomains)
}

// Enforce applies the collected indicators to the system.
func (s *Service) Enforce(ipv4, ipv6, domains []string) {
	// 1. Update IP Sets
	// For now, we just add them. Ideally we'd use the swapping mechanic for atomicity,
	// checking if Manager supports it or we do it manually.
	// Manager.ApplyConfig does FlushSet/AddElements. We can do the same.

	if len(ipv4) > 0 {
		if err := s.updateSet("threat_v4", ipv4); err != nil {
			log.Printf("[ThreatIntel] Failed to update threat_v4: %v", err)
		}
	}

	if len(ipv6) > 0 {
		if err := s.updateSet("threat_v6", ipv6); err != nil {
			log.Printf("[ThreatIntel] Failed to update threat_v6: %v", err)
		}
	}

	// 2. Update DNS
	if s.dnsSvc != nil {
		s.dnsSvc.UpdateBlockedDomains(domains)
	}
}

func (s *Service) updateSet(setName string, elements []string) error {
	// Use ReloadSet for atomic flush+add via 'nft -f'
	// This prevents a window where blocking is disabled during separate flush/add
	return s.ipsetMgr.ReloadSet(setName, elements)
}

// fetchSource fetches and parses a STIX/TAXII source.
func (s *Service) fetchSource(src config.ThreatSource) ([]string, []string, []string, error) {
	// Real implementation
	if src.Format == "taxii" || (src.Format == "" && strings.HasSuffix(src.URL, "taxii")) {
		return nil, nil, nil, fmt.Errorf("TAXII format not yet supported")
	}

	// Basic HTTP fetch for text lists or simple JSON
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(src.URL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	// Parse based on content type or format
	return s.parseList(resp.Body)
}

func (s *Service) parseList(r io.Reader) ([]string, []string, []string, error) {
	var ipv4, ipv6, domains []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Basic detection
		if ip := net.ParseIP(line); ip != nil {
			if ip.To4() != nil {
				ipv4 = append(ipv4, line)
			} else {
				ipv6 = append(ipv6, line)
			}
		} else {
			// Assume domain if not IP
			// Simple validation
			if strings.Contains(line, ".") {
				domains = append(domains, line)
			}
		}
	}
	return ipv4, ipv6, domains, scanner.Err()
}
