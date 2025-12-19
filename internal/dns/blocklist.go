package dns

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/config"
)

// BlocklistManager manages DNS blocklists.
type BlocklistManager struct {
	blocked    map[string]struct{} // blocked domains
	allowlist  map[string]struct{} // domains that bypass blocking
	blocklists []config.DNSBlocklist
	mu         sync.RWMutex
	logger     *slog.Logger
}

// NewBlocklistManager creates a new blocklist manager.
func NewBlocklistManager(logger *slog.Logger) *BlocklistManager {
	return &BlocklistManager{
		blocked:   make(map[string]struct{}),
		allowlist: make(map[string]struct{}),
		logger:    logger,
	}
}

// AddBlocklist adds a blocklist configuration.
func (m *BlocklistManager) AddBlocklist(bl config.DNSBlocklist) error {
	m.mu.Lock()
	m.blocklists = append(m.blocklists, bl)
	m.mu.Unlock()

	// Load immediately
	return m.loadBlocklist(bl)
}

// AddAllowlist adds a domain to the allowlist.
func (m *BlocklistManager) AddAllowlist(domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowlist[strings.ToLower(domain)] = struct{}{}
}

// IsBlocked checks if a domain is blocked.
func (m *BlocklistManager) IsBlocked(domain string) bool {
	domain = strings.ToLower(domain)

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check allowlist first (exact match and parent domains)
	if m.isAllowlisted(domain) {
		return false
	}

	// Check blocklist (exact match and parent domains)
	return m.isBlockedDomain(domain)
}

// isAllowlisted checks if domain or any parent is allowlisted.
func (m *BlocklistManager) isAllowlisted(domain string) bool {
	// Check exact match
	if _, ok := m.allowlist[domain]; ok {
		return true
	}

	// Check parent domains
	parts := strings.Split(domain, ".")
	for i := 1; i < len(parts); i++ {
		parent := strings.Join(parts[i:], ".")
		if _, ok := m.allowlist[parent]; ok {
			return true
		}
	}

	return false
}

// isBlockedDomain checks if domain or any parent is blocked.
func (m *BlocklistManager) isBlockedDomain(domain string) bool {
	// Check exact match
	if _, ok := m.blocked[domain]; ok {
		return true
	}

	// Check parent domains
	parts := strings.Split(domain, ".")
	for i := 1; i < len(parts); i++ {
		parent := strings.Join(parts[i:], ".")
		if _, ok := m.blocked[parent]; ok {
			return true
		}
	}

	return false
}

// loadBlocklist loads a single blocklist.
func (m *BlocklistManager) loadBlocklist(bl config.DNSBlocklist) error {
	var reader io.ReadCloser
	var err error

	if bl.URL != "" {
		reader, err = m.fetchURL(bl.URL)
	} else if bl.File != "" {
		reader, err = os.Open(bl.File)
	} else {
		return nil
	}

	if err != nil {
		return err
	}
	defer reader.Close()

	domains, err := m.parseBlocklist(reader, bl.Format)
	if err != nil {
		return err
	}

	m.mu.Lock()
	for _, domain := range domains {
		m.blocked[domain] = struct{}{}
	}
	m.mu.Unlock()

	m.logger.Info("loaded blocklist", "name", bl.Name, "domains", len(domains))
	return nil
}

// fetchURL fetches a blocklist from a URL.
func (m *BlocklistManager) fetchURL(url string) (io.ReadCloser, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &httpError{StatusCode: resp.StatusCode}
	}
	return resp.Body, nil
}

type httpError struct {
	StatusCode int
}

func (e *httpError) Error() string {
	return "HTTP error: " + http.StatusText(e.StatusCode)
}

// parseBlocklist parses a blocklist in various formats.
func (m *BlocklistManager) parseBlocklist(r io.Reader, format string) ([]string, error) {
	var domains []string
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		var domain string

		switch format {
		case "hosts":
			// Format: 0.0.0.0 domain.com or 127.0.0.1 domain.com
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				domain = parts[1]
			}

		case "adblock":
			// Format: ||domain.com^ or ||domain.com
			if strings.HasPrefix(line, "||") {
				domain = strings.TrimPrefix(line, "||")
				domain = strings.TrimSuffix(domain, "^")
				domain = strings.TrimSuffix(domain, "$")
				// Skip rules with special characters
				if strings.ContainsAny(domain, "*/@") {
					continue
				}
			}

		case "domains", "":
			// Plain domain list, one per line
			domain = line
		}

		if domain != "" {
			domain = strings.ToLower(domain)
			// Basic validation
			if !strings.Contains(domain, " ") && strings.Contains(domain, ".") {
				domains = append(domains, domain)
			}
		}
	}

	return domains, scanner.Err()
}

// StartRefresh starts the background refresh goroutine.
func (m *BlocklistManager) StartRefresh(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refreshAll()
		}
	}
}

// refreshAll refreshes all blocklists that need updating.
func (m *BlocklistManager) refreshAll() {
	m.mu.RLock()
	blocklists := make([]config.DNSBlocklist, len(m.blocklists))
	copy(blocklists, m.blocklists)
	m.mu.RUnlock()

	for _, bl := range blocklists {
		if bl.RefreshHours > 0 {
			if err := m.loadBlocklist(bl); err != nil {
				m.logger.Warn("failed to refresh blocklist", "name", bl.Name, "error", err)
			}
		}
	}
}

// Stats returns blocklist statistics.
func (m *BlocklistManager) Stats() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]int{
		"blocked_domains":   len(m.blocked),
		"allowlist_entries": len(m.allowlist),
		"blocklist_count":   len(m.blocklists),
	}
}

// Flush clears all blocked domains.
func (m *BlocklistManager) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocked = make(map[string]struct{})
}
