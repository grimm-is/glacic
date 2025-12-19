// Package dns implements a DNS server with forwarding, caching, and filtering.
package dns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"grimm.is/glacic/internal/config"
)

// Server is a DNS server with forwarding, caching, and filtering capabilities.
type Server struct {
	config     *config.DNSServer
	logger     *slog.Logger
	udpServer  *dns.Server
	tcpServer  *dns.Server
	cache      *Cache
	blocklist  *BlocklistManager
	hostMap    map[string][]net.IP // hostname -> IPs
	zoneMap    map[string]*Zone    // zone name -> zone data
	forwarders []string
	condFwd    map[string][]string // domain -> forwarders

	// DHCP integration
	dhcpHosts   map[string]net.IP // hostname -> IP from DHCP
	dhcpHostsMu sync.RWMutex

	// Rate limiting
	rateLimiter *RateLimiter

	// Stats
	stats Stats

	running atomic.Bool
	ctx     context.Context
	cancel  context.CancelFunc
}

// Stats holds DNS server statistics.
type Stats struct {
	QueriesTotal     atomic.Uint64
	QueriesBlocked   atomic.Uint64
	QueriesCached    atomic.Uint64
	QueriesForwarded atomic.Uint64
	CacheHits        atomic.Uint64
	CacheMisses      atomic.Uint64
}

// Zone represents a DNS zone with its records.
type Zone struct {
	Name    string
	Records map[string][]dns.RR // name -> records
}

// New creates a new DNS server.
func New(cfg *config.DNSServer, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Server{
		config:     cfg,
		logger:     logger.With("component", "dns"),
		hostMap:    make(map[string][]net.IP),
		zoneMap:    make(map[string]*Zone),
		condFwd:    make(map[string][]string),
		dhcpHosts:  make(map[string]net.IP),
		forwarders: cfg.Forwarders,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Initialize cache if enabled
	if cfg.CacheEnabled {
		cacheSize := cfg.CacheSize
		if cacheSize <= 0 {
			cacheSize = 10000 // Default 10k entries
		}
		s.cache = NewCache(cacheSize, time.Duration(cfg.CacheMinTTL)*time.Second, time.Duration(cfg.CacheMaxTTL)*time.Second)
	}

	// Initialize blocklist manager
	s.blocklist = NewBlocklistManager(logger)
	for _, bl := range cfg.Blocklists {
		if bl.Enabled {
			if err := s.blocklist.AddBlocklist(bl); err != nil {
				logger.Warn("failed to add blocklist", "name", bl.Name, "error", err)
			}
		}
	}

	// Load allowlist
	for _, domain := range cfg.Allowlist {
		s.blocklist.AddAllowlist(domain)
	}

	// Initialize rate limiter if configured
	if cfg.RateLimitPerSec > 0 {
		s.rateLimiter = NewRateLimiter(cfg.RateLimitPerSec)
	}

	// Load static hosts
	for _, host := range cfg.Hosts {
		ip := net.ParseIP(host.IP)
		if ip == nil {
			logger.Warn("invalid IP in host entry", "ip", host.IP)
			continue
		}
		for _, hostname := range host.Hostnames {
			hostname = strings.ToLower(hostname)
			s.hostMap[hostname] = append(s.hostMap[hostname], ip)
			// Also add with local domain suffix
			if cfg.LocalDomain != "" {
				fqdn := hostname + "." + cfg.LocalDomain
				s.hostMap[fqdn] = append(s.hostMap[fqdn], ip)
			}
		}
	}

	// Load zones
	for _, zone := range cfg.Zones {
		z := &Zone{
			Name:    zone.Name,
			Records: make(map[string][]dns.RR),
		}
		for _, rec := range zone.Records {
			rr, err := createRR(zone.Name, rec)
			if err != nil {
				logger.Warn("failed to create DNS record", "zone", zone.Name, "record", rec.Name, "error", err)
				continue
			}
			key := strings.ToLower(rec.Name + "." + zone.Name)
			z.Records[key] = append(z.Records[key], rr)
		}
		s.zoneMap[zone.Name] = z
	}

	// Load conditional forwarders
	for _, cf := range cfg.ConditionalForwarders {
		s.condFwd[strings.ToLower(cf.Domain)] = cf.Servers
	}

	return s, nil
}

// Start starts the DNS server.
func (s *Server) Start() error {
	if s.running.Load() {
		return fmt.Errorf("server already running")
	}

	port := s.config.ListenPort
	if port == 0 {
		port = 53
	}

	listenAddrs := s.config.ListenOn
	if len(listenAddrs) == 0 {
		listenAddrs = []string{"0.0.0.0"}
	}

	handler := dns.HandlerFunc(s.handleQuery)

	// Start UDP and TCP servers for each listen address
	for _, addr := range listenAddrs {
		listenAddr := fmt.Sprintf("%s:%d", addr, port)

		// UDP server
		s.udpServer = &dns.Server{
			Addr:    listenAddr,
			Net:     "udp",
			Handler: handler,
		}

		// TCP server
		s.tcpServer = &dns.Server{
			Addr:    listenAddr,
			Net:     "tcp",
			Handler: handler,
		}

		go func(addr string) {
			s.logger.Info("starting DNS server (UDP)", "addr", addr)
			if err := s.udpServer.ListenAndServe(); err != nil {
				s.logger.Error("UDP server error", "error", err)
			}
		}(listenAddr)

		go func(addr string) {
			s.logger.Info("starting DNS server (TCP)", "addr", addr)
			if err := s.tcpServer.ListenAndServe(); err != nil {
				s.logger.Error("TCP server error", "error", err)
			}
		}(listenAddr)
	}

	// Start blocklist refresh goroutine
	go s.blocklist.StartRefresh(s.ctx)

	s.running.Store(true)
	s.logger.Info("DNS server started")
	return nil
}

// Stop stops the DNS server.
func (s *Server) Stop() error {
	if !s.running.Load() {
		return nil
	}

	s.cancel()

	if s.udpServer != nil {
		s.udpServer.Shutdown()
	}
	if s.tcpServer != nil {
		s.tcpServer.Shutdown()
	}

	s.running.Store(false)
	s.logger.Info("DNS server stopped")
	return nil
}

// RegisterDHCPHost registers a hostname from DHCP.
func (s *Server) RegisterDHCPHost(hostname string, ip net.IP) {
	if !s.config.DHCPIntegration {
		return
	}

	hostname = strings.ToLower(hostname)
	s.dhcpHostsMu.Lock()
	s.dhcpHosts[hostname] = ip
	// Also register with local domain
	if s.config.LocalDomain != "" {
		fqdn := hostname + "." + s.config.LocalDomain
		s.dhcpHosts[fqdn] = ip
	}
	s.dhcpHostsMu.Unlock()

	s.logger.Debug("registered DHCP host", "hostname", hostname, "ip", ip)
}

// UnregisterDHCPHost removes a hostname registered from DHCP.
func (s *Server) UnregisterDHCPHost(hostname string) {
	hostname = strings.ToLower(hostname)
	s.dhcpHostsMu.Lock()
	delete(s.dhcpHosts, hostname)
	if s.config.LocalDomain != "" {
		delete(s.dhcpHosts, hostname+"."+s.config.LocalDomain)
	}
	s.dhcpHostsMu.Unlock()
}

// handleQuery handles incoming DNS queries.
func (s *Server) handleQuery(w dns.ResponseWriter, r *dns.Msg) {
	s.stats.QueriesTotal.Add(1)

	// Rate limiting
	if s.rateLimiter != nil {
		clientIP, _, _ := net.SplitHostPort(w.RemoteAddr().String())
		if !s.rateLimiter.Allow(clientIP) {
			s.logger.Debug("rate limited", "client", clientIP)
			dns.HandleFailed(w, r)
			return
		}
	}

	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = false
	m.RecursionAvailable = true

	if len(r.Question) == 0 {
		w.WriteMsg(m)
		return
	}

	q := r.Question[0]
	qname := strings.ToLower(q.Name)
	qnameNoTrail := strings.TrimSuffix(qname, ".")

	if s.config.QueryLogging {
		s.logger.Info("DNS query", "name", qname, "type", dns.TypeToString[q.Qtype], "client", w.RemoteAddr())
	}

	// Check blocklist (skip for allowlisted domains)
	if s.blocklist.IsBlocked(qnameNoTrail) {
		s.stats.QueriesBlocked.Add(1)
		s.logger.Debug("blocked query", "name", qname)
		s.respondBlocked(m, q)
		w.WriteMsg(m)
		return
	}

	// Check cache
	if s.cache != nil {
		if cached := s.cache.Get(qname, q.Qtype); cached != nil {
			s.stats.CacheHits.Add(1)
			s.stats.QueriesCached.Add(1)
			m.Answer = cached
			w.WriteMsg(m)
			return
		}
		s.stats.CacheMisses.Add(1)
	}

	// Check static hosts
	if ips := s.lookupHost(qnameNoTrail); len(ips) > 0 {
		for _, ip := range ips {
			if q.Qtype == dns.TypeA && ip.To4() != nil {
				rr := &dns.A{
					Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
					A:   ip.To4(),
				}
				m.Answer = append(m.Answer, rr)
			} else if q.Qtype == dns.TypeAAAA && ip.To4() == nil {
				rr := &dns.AAAA{
					Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
					AAAA: ip,
				}
				m.Answer = append(m.Answer, rr)
			}
		}
		if len(m.Answer) > 0 {
			w.WriteMsg(m)
			return
		}
	}

	// Check DHCP hosts
	if ip := s.lookupDHCPHost(qnameNoTrail); ip != nil {
		if q.Qtype == dns.TypeA && ip.To4() != nil {
			rr := &dns.A{
				Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   ip.To4(),
			}
			m.Answer = append(m.Answer, rr)
			w.WriteMsg(m)
			return
		}
	}

	// Check local zones
	if rrs := s.lookupZone(qnameNoTrail, q.Qtype); len(rrs) > 0 {
		m.Answer = rrs
		m.Authoritative = true
		w.WriteMsg(m)
		return
	}

	// Forward to upstream
	s.stats.QueriesForwarded.Add(1)
	resp, err := s.forward(r, qnameNoTrail)
	if err != nil {
		s.logger.Debug("forward failed", "error", err)
		m.Rcode = dns.RcodeServerFailure
		w.WriteMsg(m)
		return
	}

	// DNS rebinding protection
	if s.config.RebindProtection && !isLocalQuery(qnameNoTrail, s.config.LocalDomain) {
		resp.Answer = filterPrivateIPs(resp.Answer)
	}

	// Cache the response
	if s.cache != nil && len(resp.Answer) > 0 {
		s.cache.Set(qname, q.Qtype, resp.Answer)
	}

	w.WriteMsg(resp)
}

// lookupHost looks up a hostname in static hosts.
func (s *Server) lookupHost(name string) []net.IP {
	name = strings.ToLower(name)
	if ips, ok := s.hostMap[name]; ok {
		return ips
	}
	// Try with local domain
	if s.config.LocalDomain != "" && !strings.Contains(name, ".") {
		fqdn := name + "." + s.config.LocalDomain
		if ips, ok := s.hostMap[fqdn]; ok {
			return ips
		}
	}
	return nil
}

// lookupDHCPHost looks up a hostname in DHCP registrations.
func (s *Server) lookupDHCPHost(name string) net.IP {
	s.dhcpHostsMu.RLock()
	defer s.dhcpHostsMu.RUnlock()

	name = strings.ToLower(name)
	if ip, ok := s.dhcpHosts[name]; ok {
		return ip
	}
	// Try with local domain
	if s.config.LocalDomain != "" && !strings.Contains(name, ".") {
		fqdn := name + "." + s.config.LocalDomain
		if ip, ok := s.dhcpHosts[fqdn]; ok {
			return ip
		}
	}
	return nil
}

// lookupZone looks up a name in local zones.
func (s *Server) lookupZone(name string, qtype uint16) []dns.RR {
	name = strings.ToLower(name)
	for zoneName, zone := range s.zoneMap {
		if strings.HasSuffix(name, zoneName) || name == strings.TrimSuffix(zoneName, ".") {
			if rrs, ok := zone.Records[name]; ok {
				var result []dns.RR
				for _, rr := range rrs {
					if rr.Header().Rrtype == qtype {
						result = append(result, rr)
					}
				}
				return result
			}
		}
	}
	return nil
}

// forward forwards a query to upstream DNS servers.
func (s *Server) forward(r *dns.Msg, qname string) (*dns.Msg, error) {
	// Check conditional forwarders
	forwarders := s.forwarders
	for domain, servers := range s.condFwd {
		if strings.HasSuffix(qname, domain) || qname == domain {
			forwarders = servers
			break
		}
	}

	if len(forwarders) == 0 {
		return nil, fmt.Errorf("no forwarders configured")
	}

	timeout := time.Duration(s.config.UpstreamTimeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	c := &dns.Client{Timeout: timeout}

	var lastErr error
	for _, server := range forwarders {
		if !strings.Contains(server, ":") {
			server = server + ":53"
		}
		resp, _, err := c.Exchange(r, server)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}

	return nil, fmt.Errorf("all forwarders failed: %w", lastErr)
}

// respondBlocked creates a blocked response.
func (s *Server) respondBlocked(m *dns.Msg, q dns.Question) {
	ttl := uint32(s.config.BlockedTTL)
	if ttl == 0 {
		ttl = 300
	}

	blockedIP := net.ParseIP(s.config.BlockedAddress)
	if blockedIP == nil {
		blockedIP = net.IPv4zero
	}

	if q.Qtype == dns.TypeA {
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
			A:   blockedIP.To4(),
		})
	} else if q.Qtype == dns.TypeAAAA {
		m.Answer = append(m.Answer, &dns.AAAA{
			Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl},
			AAAA: net.IPv6zero,
		})
	}
}

// GetStats returns current server statistics.
func (s *Server) GetStats() map[string]uint64 {
	return map[string]uint64{
		"queries_total":     s.stats.QueriesTotal.Load(),
		"queries_blocked":   s.stats.QueriesBlocked.Load(),
		"queries_cached":    s.stats.QueriesCached.Load(),
		"queries_forwarded": s.stats.QueriesForwarded.Load(),
		"cache_hits":        s.stats.CacheHits.Load(),
		"cache_misses":      s.stats.CacheMisses.Load(),
	}
}

// createRR creates a dns.RR from a config record.
func createRR(zoneName string, rec config.DNSRecord) (dns.RR, error) {
	ttl := uint32(rec.TTL)
	if ttl == 0 {
		ttl = 3600
	}

	fqdn := rec.Name + "." + zoneName + "."
	if rec.Name == "@" {
		fqdn = zoneName + "."
	}

	switch strings.ToUpper(rec.Type) {
	case "A":
		ip := net.ParseIP(rec.Value)
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf("invalid A record IP: %s", rec.Value)
		}
		return &dns.A{
			Hdr: dns.RR_Header{Name: fqdn, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
			A:   ip.To4(),
		}, nil

	case "AAAA":
		ip := net.ParseIP(rec.Value)
		if ip == nil || ip.To4() != nil {
			return nil, fmt.Errorf("invalid AAAA record IP: %s", rec.Value)
		}
		return &dns.AAAA{
			Hdr:  dns.RR_Header{Name: fqdn, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: ttl},
			AAAA: ip,
		}, nil

	case "CNAME":
		target := rec.Value
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		return &dns.CNAME{
			Hdr:    dns.RR_Header{Name: fqdn, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: ttl},
			Target: target,
		}, nil

	case "MX":
		target := rec.Value
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		return &dns.MX{
			Hdr:        dns.RR_Header{Name: fqdn, Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: ttl},
			Preference: uint16(rec.Priority),
			Mx:         target,
		}, nil

	case "TXT":
		return &dns.TXT{
			Hdr: dns.RR_Header{Name: fqdn, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: ttl},
			Txt: []string{rec.Value},
		}, nil

	case "PTR":
		target := rec.Value
		if !strings.HasSuffix(target, ".") {
			target += "."
		}
		return &dns.PTR{
			Hdr: dns.RR_Header{Name: fqdn, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl},
			Ptr: target,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported record type: %s", rec.Type)
	}
}

// isLocalQuery checks if a query is for the local domain.
func isLocalQuery(name, localDomain string) bool {
	if localDomain == "" {
		return false
	}
	return strings.HasSuffix(name, "."+localDomain) || name == localDomain
}

// filterPrivateIPs removes private IP addresses from DNS responses (rebind protection).
func filterPrivateIPs(answers []dns.RR) []dns.RR {
	var filtered []dns.RR
	for _, rr := range answers {
		switch r := rr.(type) {
		case *dns.A:
			if !isPrivateIP(r.A) {
				filtered = append(filtered, rr)
			}
		case *dns.AAAA:
			if !isPrivateIP(r.AAAA) {
				filtered = append(filtered, rr)
			}
		default:
			filtered = append(filtered, rr)
		}
	}
	return filtered
}

// isPrivateIP checks if an IP is in a private range.
func isPrivateIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 127.0.0.0/8
		if ip4[0] == 127 {
			return true
		}
	}
	// IPv6 private ranges
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() {
		return true
	}
	return false
}
