// Package scanner provides internal network service discovery.
// It scans LAN hosts for common services that users may want to expose to the internet.
package scanner

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/logging"
)

// Common ports for services users might want to expose
var CommonPorts = []Port{
	{Number: 22, Name: "SSH", Description: "Secure Shell"},
	{Number: 80, Name: "HTTP", Description: "Web Server"},
	{Number: 443, Name: "HTTPS", Description: "Secure Web Server"},
	{Number: 21, Name: "FTP", Description: "File Transfer"},
	{Number: 25, Name: "SMTP", Description: "Mail Server"},
	{Number: 53, Name: "DNS", Description: "DNS Server"},
	{Number: 110, Name: "POP3", Description: "Mail Retrieval"},
	{Number: 143, Name: "IMAP", Description: "Mail Access"},
	{Number: 445, Name: "SMB", Description: "Windows File Sharing"},
	{Number: 993, Name: "IMAPS", Description: "Secure IMAP"},
	{Number: 995, Name: "POP3S", Description: "Secure POP3"},
	{Number: 3306, Name: "MySQL", Description: "MySQL Database"},
	{Number: 5432, Name: "PostgreSQL", Description: "PostgreSQL Database"},
	{Number: 6379, Name: "Redis", Description: "Redis Cache"},
	{Number: 27017, Name: "MongoDB", Description: "MongoDB Database"},
	{Number: 8080, Name: "HTTP-Alt", Description: "Alt Web Server"},
	{Number: 8443, Name: "HTTPS-Alt", Description: "Alt Secure Web"},
	{Number: 3389, Name: "RDP", Description: "Remote Desktop"},
	{Number: 5900, Name: "VNC", Description: "VNC Remote Access"},
	{Number: 32400, Name: "Plex", Description: "Plex Media Server"},
	{Number: 8096, Name: "Jellyfin", Description: "Jellyfin Media Server"},
	{Number: 9000, Name: "Portainer", Description: "Docker Management"},
	{Number: 51820, Name: "WireGuard", Description: "WireGuard VPN"},
	{Number: 1194, Name: "OpenVPN", Description: "OpenVPN"},
	{Number: 25565, Name: "Minecraft", Description: "Minecraft Server"},
	{Number: 7777, Name: "Game-Server", Description: "Game Server"},
}

// Port represents a port to scan
type Port struct {
	Number      int    `json:"number"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// HostResult represents scan results for a single host
type HostResult struct {
	IP           string        `json:"ip"`
	Hostname     string        `json:"hostname,omitempty"`
	MAC          string        `json:"mac,omitempty"`
	OpenPorts    []PortResult  `json:"open_ports"`
	ScanDuration time.Duration `json:"scan_duration_ms"`
	ScannedAt    time.Time     `json:"scanned_at"`
}

// PortResult represents a discovered open port
type PortResult struct {
	Port        int    `json:"port"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Banner      string `json:"banner,omitempty"` // Service banner if available
}

// ScanResult represents a full network scan result
type ScanResult struct {
	Network    string        `json:"network"`     // CIDR scanned
	Hosts      []HostResult  `json:"hosts"`       // Hosts with open ports
	TotalHosts int           `json:"total_hosts"` // Total hosts scanned
	Duration   time.Duration `json:"duration_ms"`
	StartedAt  time.Time     `json:"started_at"`
	Error      string        `json:"error,omitempty"`
}

// Scanner performs network service discovery
type Scanner struct {
	logger      *logging.Logger
	timeout     time.Duration
	concurrency int
	mu          sync.Mutex
	scanning    bool
	lastResult  *ScanResult
}

// Config holds scanner configuration
type Config struct {
	Timeout     time.Duration // Per-port timeout
	Concurrency int           // Max concurrent connections
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Timeout:     2 * time.Second,
		Concurrency: 100, // Max 100 concurrent port checks
	}
}

// New creates a new network scanner
func New(logger *logging.Logger, cfg Config) *Scanner {
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 100
	}
	return &Scanner{
		logger:      logger,
		timeout:     cfg.Timeout,
		concurrency: cfg.Concurrency,
	}
}

// IsScanning returns true if a scan is in progress
func (s *Scanner) IsScanning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanning
}

// LastResult returns the most recent scan result
func (s *Scanner) LastResult() *ScanResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastResult
}

// ScanNetwork scans a CIDR network for common services
func (s *Scanner) ScanNetwork(ctx context.Context, cidr string) (*ScanResult, error) {
	s.mu.Lock()
	if s.scanning {
		s.mu.Unlock()
		return nil, fmt.Errorf("scan already in progress")
	}
	s.scanning = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.scanning = false
		s.mu.Unlock()
	}()

	start := clock.Now()
	result := &ScanResult{
		Network:   cidr,
		Hosts:     []HostResult{},
		StartedAt: start,
	}

	// Parse CIDR
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		result.Error = fmt.Sprintf("invalid CIDR: %v", err)
		return result, err
	}

	// Generate host IPs
	var hosts []net.IP
	for ip := ipnet.IP.Mask(ipnet.Mask); ipnet.Contains(ip); incIP(ip) {
		// Skip network and broadcast for /24 and smaller
		ones, bits := ipnet.Mask.Size()
		if bits-ones <= 8 {
			if ip[len(ip)-1] == 0 || ip[len(ip)-1] == 255 {
				continue
			}
		}
		hostIP := make(net.IP, len(ip))
		copy(hostIP, ip)
		hosts = append(hosts, hostIP)
	}
	result.TotalHosts = len(hosts)

	s.logger.Info("Starting network scan",
		"network", cidr,
		"hosts", len(hosts),
		"ports", len(CommonPorts),
	)

	// Scan hosts concurrently
	var wg sync.WaitGroup
	var resultsMu sync.Mutex
	sem := make(chan struct{}, s.concurrency)

	for _, ip := range hosts {
		select {
		case <-ctx.Done():
			result.Error = "scan cancelled"
			result.Duration = time.Since(start)
			return result, ctx.Err()
		default:
		}

		wg.Add(1)
		go func(hostIP net.IP) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			hostResult := s.scanHost(ctx, hostIP.String())
			if len(hostResult.OpenPorts) > 0 {
				resultsMu.Lock()
				result.Hosts = append(result.Hosts, hostResult)
				resultsMu.Unlock()
			}
		}(ip)
	}

	wg.Wait()

	// Sort results by IP
	sort.Slice(result.Hosts, func(i, j int) bool {
		return result.Hosts[i].IP < result.Hosts[j].IP
	})

	result.Duration = time.Since(start)
	s.logger.Info("Network scan complete",
		"network", cidr,
		"hosts_with_services", len(result.Hosts),
		"duration", result.Duration,
	)

	// Cache result
	s.mu.Lock()
	s.lastResult = result
	s.mu.Unlock()

	return result, nil
}

// scanHost scans a single host for common ports
func (s *Scanner) scanHost(ctx context.Context, ip string) HostResult {
	start := clock.Now()
	result := HostResult{
		IP:        ip,
		OpenPorts: []PortResult{},
		ScannedAt: start,
	}

	// Try reverse DNS
	names, err := net.LookupAddr(ip)
	if err == nil && len(names) > 0 {
		result.Hostname = names[0]
	}

	// Scan each common port
	var wg sync.WaitGroup
	var portsMu sync.Mutex

	for _, port := range CommonPorts {
		select {
		case <-ctx.Done():
			result.ScanDuration = time.Since(start)
			return result
		default:
		}

		wg.Add(1)
		go func(p Port) {
			defer wg.Done()

			if s.isPortOpen(ctx, ip, p.Number) {
				portsMu.Lock()
				result.OpenPorts = append(result.OpenPorts, PortResult{
					Port:        p.Number,
					Name:        p.Name,
					Description: p.Description,
				})
				portsMu.Unlock()
			}
		}(port)
	}

	wg.Wait()

	// Sort ports by number
	sort.Slice(result.OpenPorts, func(i, j int) bool {
		return result.OpenPorts[i].Port < result.OpenPorts[j].Port
	})

	result.ScanDuration = time.Since(start)
	return result
}

// isPortOpen checks if a TCP port is open
func (s *Scanner) isPortOpen(ctx context.Context, ip string, port int) bool {
	addr := fmt.Sprintf("%s:%d", ip, port)

	// Create dialer with timeout
	dialer := net.Dialer{Timeout: s.timeout}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ScanHost scans a single host (public API)
func (s *Scanner) ScanHost(ctx context.Context, ip string) (*HostResult, error) {
	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ip)
	}

	result := s.scanHost(ctx, ip)
	return &result, nil
}

// incIP increments an IP address
func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// GetCommonPorts returns the list of ports that are scanned
func GetCommonPorts() []Port {
	return CommonPorts
}
