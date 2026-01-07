package dns

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"

	"context"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/services"
	"grimm.is/glacic/internal/upgrade"

	"github.com/miekg/dns"
)

type Service struct {
	servers        []*dns.Server
	config         *config.DNSServer
	upstreams        []upstream // Unified list of upstreams (UDP, DoT, DoH)
	dynamicUpstreams []upstream // From DHCP/etc
	records          map[string]config.DNSRecord // FQDN -> Record
	blockedDomains   map[string]bool             // Blocked domains
	cache            map[string]cachedResponse
	mu               sync.RWMutex
	running          bool
	stopCleanup      chan struct{}
	upgradeMgr       *upgrade.Manager
	fw               ValidatingFirewall
	
	// Egress Filter State
	egressFilterEnabled bool
	egressFilterTTL     int
}

// ValidatingFirewall defines the interface for firewall authorization
type ValidatingFirewall interface {
	AuthorizeIP(ip net.IP, ttl time.Duration) error
} // ValidatingFirewall

type upstream struct {
	Addr       string
	Protocol   string // "udp", "tcp", "tcp-tls", "https"
	ServerName string // For TLS/HTTPS verification
	URL        string // For DoH (full URL)
}

type cachedResponse struct {
	msg       *dns.Msg
	expiresAt time.Time
}

// NewService creates a new DNS service.
func NewService(cfg *config.Config, logger *logging.Logger) *Service {
	return &Service{
		config:         &config.DNSServer{},
		records:        make(map[string]config.DNSRecord),
		blockedDomains: make(map[string]bool),
		cache:          make(map[string]cachedResponse),
		stopCleanup:    make(chan struct{}),
	}
}

// SetFirewall sets the firewall manager for validation
func (s *Service) SetFirewall(fw ValidatingFirewall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fw = fw
}

// SetUpgradeManager sets the upgrade manager for socket handoff.
func (s *Service) SetUpgradeManager(mgr *upgrade.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upgradeMgr = mgr
}

// Name returns the service name.
func (s *Service) Name() string {
	return "DNS"
}

// Start starts the service.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	if len(s.servers) == 0 {
		// Nothing to start
		return nil
	}

	log.Printf("[DNS] Starting %d servers...", len(s.servers))
	for i, srv := range s.servers {
		go func(srv *dns.Server, index int) {
			// user ActivateAndServe if generic listener/packetconn is set
			if srv.Listener != nil || srv.PacketConn != nil {
				if err := srv.ActivateAndServe(); err != nil {
					log.Printf("[DNS] Server %d error: %v", index, err)
				}
			} else {
				if err := srv.ListenAndServe(); err != nil {
					log.Printf("[DNS] Server %d error: %v", index, err)
				}
			}
		}(srv, i+1)
	}

	// Start cleanup manually since we manage it
	go s.startCacheCleanup()

	s.running = true
	return nil
}

// Stop stops the service.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	log.Printf("[DNS] Stopping %d servers...", len(s.servers))
	for i, srv := range s.servers {
		if err := srv.Shutdown(); err != nil {
			log.Printf("[DNS] Failed to stop server %d: %v", i+1, err)
		}
	}

	// Stop cleanup
	select {
	case s.stopCleanup <- struct{}{}:
	default:
	}

	s.running = false
	return nil
}

// Reload reconfigures the service with minimal downtime.
func (s *Service) Reload(cfg *config.Config) (bool, error) {
	s.mu.RLock()
	wasRunning := s.running
	oldConfig := s.config
	s.mu.RUnlock()

	// Detect config mode
	activeMode := "legacy"
	if cfg.DNS != nil && (len(cfg.DNS.Serve) > 0 || len(cfg.DNS.Forwarders) > 0 || cfg.DNS.Mode != "") {
		activeMode = "new"
	}
	
	// Update Egress Filter State
	s.mu.Lock()
	if cfg.DNS != nil {
		s.egressFilterEnabled = cfg.DNS.EgressFilter
		s.egressFilterTTL = cfg.DNS.EgressFilterTTL
	} else {
		s.egressFilterEnabled = false
		s.egressFilterTTL = 0
	}
	s.mu.Unlock()

	// -------------------------------------------------------------
	// NEW CONFIG MODE (dns {})
	// -------------------------------------------------------------
	if activeMode == "new" {
		state := s.buildServerState(cfg)

		if wasRunning {
			log.Printf("[DNS] Restarting DNS server (New Config Mode)")
			s.Stop(context.Background())
		}

		newServers := s.buildServers(state.listenAddrs)

		s.mu.Lock()
		// Map back to legacy config structure for internal compatibility
		s.config = &config.DNSServer{
			ConditionalForwarders: cfg.DNS.ConditionalForwarders,
		}
		// Convert new mode forwarders (strings) to upstreams (udp)
		var newUpstreams []upstream
		for _, fwd := range state.forwarders {
			newUpstreams = append(newUpstreams, upstream{Addr: fwd, Protocol: "udp"})
		}
		s.upstreams = newUpstreams
		s.records = state.records
		s.blockedDomains = state.blockedDomains
		s.servers = newServers
		s.mu.Unlock()

		return true, s.Start(context.Background())
	}

	// -------------------------------------------------------------
	// LEGACY CONFIG MODE (dns_server {})
	// -------------------------------------------------------------
	dnsCfg := cfg.DNSServer
	if dnsCfg == nil || !dnsCfg.Enabled {
		if wasRunning {
			return true, s.Stop(context.Background())
		}
		return true, nil
	}

	// Check for external mode
	// ... (rest of legacy logic)

	// Return to legacy flow for diff minimality, or duplicate?
	// I'll rewrite the legacy part below or reuse existing via return.
	// Since I replaced the WHOLE function, I must provide legacy implementation here.

	// Copy of original legacy implementation:
	if dnsCfg.Mode == "external" {
		log.Println("[DNS] External DNS server configured. Skipping built-in server startup.")
		if wasRunning {
			return true, s.Stop(context.Background())
		}
		return true, nil
	}

	// 1. Pre-load everything into local variables
	newRecords := make(map[string]config.DNSRecord)
	newBlocked := make(map[string]bool)
	var newServers []*dns.Server

	// Build upstreams (Legacy UDP + DoT + DoH)
	newUpstreams := s.buildStaticUpstreams(dnsCfg)

	// Build records map
	for _, zone := range dnsCfg.Zones {
		zoneName := dns.Fqdn(zone.Name)
		for _, rec := range zone.Records {
			fqdn := zoneName
			if rec.Name != "@" {
				fqdn = dns.Fqdn(rec.Name + "." + zoneName)
			}
			newRecords[strings.ToLower(fqdn)] = rec
		}
	}

	// Build static hosts map
	for _, host := range dnsCfg.Hosts {
		for _, hostname := range host.Hostnames {
			fqdn := dns.Fqdn(hostname)
			recType := "A"
			if ip := net.ParseIP(host.IP); ip != nil && ip.To4() == nil {
				recType = "AAAA"
			}
			rec := config.DNSRecord{
				Name:  hostname,
				Type:  recType,
				Value: host.IP,
				TTL:   3600,
			}
			newRecords[strings.ToLower(fqdn)] = rec
		}
	}

	// Load blocklists
	var err error
	newBlocked, err = loadBlocklistsFromConfig(dnsCfg.Blocklists)
	if err != nil {
		log.Printf("[DNS] Warning: errors occurred loading blocklists: %v", err)
	}

	// Load /etc/hosts
	if f, err := os.Open("/etc/hosts"); err == nil {
		scanner := bufio.NewScanner(f)
		count := 0
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if idx := strings.Index(line, "#"); idx != -1 {
				line = strings.TrimSpace(line[:idx])
			}
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			ipStr := parts[0]
			ip := net.ParseIP(ipStr)
			if ip == nil {
				continue
			}
			recType := "A"
			if ip.To4() == nil {
				recType = "AAAA"
			}
			for _, hostname := range parts[1:] {
				fqdn := dns.Fqdn(hostname)
				lowerName := strings.ToLower(fqdn)
				if _, exists := newRecords[lowerName]; !exists {
					newRecords[lowerName] = config.DNSRecord{
						Name:  hostname,
						Type:  recType,
						Value: ipStr,
						TTL:   3600,
					}
					count++
				}
			}
		}
		f.Close()
		log.Printf("[DNS] Loaded %d records from /etc/hosts", count)
	}

	// 2. Check if restart is needed
	listenersChanged := true
	if wasRunning && oldConfig != nil {
		oldL := oldConfig.ListenOn
		newL := dnsCfg.ListenOn
		if len(oldL) == 0 {
			oldL = []string{"0.0.0.0"}
		}
		if len(newL) == 0 {
			newL = []string{"0.0.0.0"}
		}

		if len(oldL) == len(newL) {
			match := true
			for i, v := range oldL {
				if v != newL[i] {
					match = false
					break
				}
			}
			if match {
				listenersChanged = false
			}
		}
	}

	// 3. Apply changes (Legacy)
	if listenersChanged {
		if wasRunning {
			log.Printf("[DNS] Restarting DNS server (listener config changed)")
			s.Stop(context.Background())
		}

		listeners := dnsCfg.ListenOn
		if len(listeners) == 0 {
			listeners = []string{"0.0.0.0"}
		}
		newServers = s.buildServers(listeners)

		s.mu.Lock()
		s.config = dnsCfg
		s.upstreams = newUpstreams
		s.records = newRecords
		s.blockedDomains = newBlocked
		s.servers = newServers
		s.mu.Unlock()

		return true, s.Start(context.Background())
	} else {
		// Hot Swap
		s.mu.Lock()
		s.config = dnsCfg
		s.upstreams = newUpstreams
		s.records = newRecords
		s.blockedDomains = newBlocked
		s.mu.Unlock()
		log.Printf("[DNS] Hot-reloaded configuration (no restart)")
		return true, nil
	}
}

func uniqueStrings(input []string) []string {
	u := make([]string, 0, len(input))
	m := make(map[string]bool)
	for _, val := range input {
		if !m[val] {
			m[val] = true
			u = append(u, val)
		}
	}
	return u
}

func (s *Service) buildServers(listeners []string) []*dns.Server {
	var newServers []*dns.Server
	for _, addr := range listeners {
		// UDP
		udpName := fmt.Sprintf("dns-udp-%s", addr)
		var pc net.PacketConn

		if s.upgradeMgr != nil {
			if existing, ok := s.upgradeMgr.GetPacketConn(udpName); ok {
				pc = existing
				log.Printf("[DNS] Inherited UDP socket %s", udpName)
			}
		}

		if pc == nil {
			var err error
			pc, err = net.ListenPacket("udp", net.JoinHostPort(addr, "53"))
			if err != nil {
				log.Printf("[DNS] Failed to bind UDP %s: %v", addr, err)
			} else if s.upgradeMgr != nil {
				s.upgradeMgr.RegisterPacketConn(udpName, pc)
			}
		}

		if pc != nil {
			newServers = append(newServers, &dns.Server{PacketConn: pc, Addr: pc.LocalAddr().String(), Net: "udp", Handler: s})
		}

		// TCP
		tcpName := fmt.Sprintf("dns-tcp-%s", addr)
		var list net.Listener

		if s.upgradeMgr != nil {
			if existing, ok := s.upgradeMgr.GetListener(tcpName); ok {
				list = existing
				log.Printf("[DNS] Inherited TCP listener %s", tcpName)
			}
		}

		if list == nil {
			var err error
			list, err = net.Listen("tcp", net.JoinHostPort(addr, "53"))
			if err != nil {
				log.Printf("[DNS] Failed to bind TCP %s: %v", addr, err)
			} else if s.upgradeMgr != nil {
				s.upgradeMgr.RegisterListener(tcpName, list)
			}
		}

		if list != nil {
			newServers = append(newServers, &dns.Server{Listener: list, Addr: list.Addr().String(), Net: "tcp", Handler: s})
		}
	}
	return newServers
}

// IsRunning returns true if the service is running.
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Status returns the current status of the service.
func (s *Service) Status() services.ServiceStatus {
	return services.ServiceStatus{
		Name:    s.Name(),
		Running: s.IsRunning(),
	}
}

func (s *Service) loadHostsFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[DNS] Failed to open hosts file %s: %v", path, err)
		}
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Remove inline comments
		if idx := strings.Index(line, "#"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		ipStr := parts[0]
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}

		recType := "A"
		if ip.To4() == nil {
			recType = "AAAA"
		}

		for _, hostname := range parts[1:] {
			fqdn := dns.Fqdn(hostname)
			lowerName := strings.ToLower(fqdn)

			// Don't overwrite existing records (config takes precedence)
			if _, exists := s.records[lowerName]; !exists {
				s.records[lowerName] = config.DNSRecord{
					Name:  hostname,
					Type:  recType,
					Value: ipStr,
					TTL:   3600,
				}
				count++
			}
		}
	}
	log.Printf("[DNS] Loaded %d records from %s", count, path)
}

func loadBlocklistsFromConfig(blocklists []config.DNSBlocklist) (map[string]bool, error) {
	blocked := make(map[string]bool)
	cachePath := "/var/lib/glacic/blocklist_cache"

	for _, bl := range blocklists {
		if !bl.Enabled {
			continue
		}

		var domains []string
		var err error
		var source string

		if bl.URL != "" {
			// URL-based blocklist - download with cache fallback
			domains, err = DownloadBlocklistWithCache(bl.URL, cachePath)
			if err != nil {
				log.Printf("[DNS] Failed to load blocklist %s from URL: %v", bl.Name, err)
				continue
			}
			source = bl.URL
		} else if bl.File != "" {
			// File-based blocklist
			f, err := os.Open(bl.File)
			if err != nil {
				log.Printf("[DNS] Failed to open blocklist file %s: %v", bl.File, err)
				continue
			}

			// Use the existing parseBlocklist helper from blocklist.go if possible?
			// parseBlocklist takes io.Reader. Yes.
			domains, err = parseBlocklist(f)
			f.Close()

			if err != nil {
				log.Printf("[DNS] Failed to parse blocklist file %s: %v", bl.File, err)
				continue
			}
			source = bl.File
		} else {
			log.Printf("[DNS] Blocklist %s has no URL or file specified", bl.Name)
			continue
		}

		// Add domains to blocked set
		count := 0
		for _, d := range domains {
			domain := strings.ToLower(dns.Fqdn(d))
			blocked[domain] = true
			count++
		}
		log.Printf("[DNS] Loaded %d domains from blocklist %s (%s)", count, bl.Name, source)
	}

	return blocked, nil
}

// AddRecord adds or updates a DNS record dynamically
func (s *Service) AddRecord(name string, ip net.IP) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fqdn := dns.Fqdn(name)

	rec := config.DNSRecord{
		Name:  name,
		Type:  "A",
		Value: ip.String(),
		TTL:   300,
	}
	if ip.To4() == nil {
		rec.Type = "AAAA"
	}

	s.records[strings.ToLower(fqdn)] = rec
	log.Printf("[DNS] Added dynamic record: %s -> %s", fqdn, ip)
}

// RemoveRecord removes a DNS record dynamically
func (s *Service) RemoveRecord(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fqdn := dns.Fqdn(name)
	delete(s.records, strings.ToLower(fqdn))
	log.Printf("[DNS] Removed dynamic record: %s", fqdn)
}

// UpdateBlockedDomains updates the set of blocked domains dynamically
func (s *Service) UpdateBlockedDomains(domains []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, d := range domains {
		domain := strings.ToLower(dns.Fqdn(d))
		if !s.blockedDomains[domain] {
			s.blockedDomains[domain] = true
			count++
		}
	}
	if count > 0 {
		log.Printf("[DNS] Added %d domains to blocklist from threat intel", count)
	}
}

// UpdateForwarders updates the upstream DNS forwarders dynamically (e.g. from DHCP)
// UpdateForwarders updates the upstream DNS forwarders dynamically (e.g. from DHCP)
func (s *Service) UpdateForwarders(forwarders []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Dedup and validate
	var valid []upstream
	seen := make(map[string]bool)

	for _, fwd := range forwarders {
		if seen[fwd] {
			continue
		}
		// Basic IP check
		if net.ParseIP(fwd) != nil {
			valid = append(valid, upstream{
				Addr:     fwd,
				Protocol: "udp",
			})
			seen[fwd] = true
		}
	}

	s.dynamicUpstreams = valid
	log.Printf("[DNS] Updated dynamic forwarders: %v", s.dynamicUpstreams)
}

// SyncFirewall re-authorizes all currently cached IPs in the firewall.
// This ensures that after a firewall reload, valid connections are not dropped.
func (s *Service) SyncFirewall() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fw == nil {
		return
	}

	count := 0
	for _, item := range s.cache {
		// Only sync valid items
		if time.Now().After(item.expiresAt) {
			continue
		}

		// Extract answers
		for _, ans := range item.msg.Answer {
			if a, ok := ans.(*dns.A); ok {
				ttl := time.Until(item.expiresAt)
				if ttl > 0 {
					s.fw.AuthorizeIP(a.A, ttl)
					count++
				}
			} else if aaaa, ok := ans.(*dns.AAAA); ok {
				ttl := time.Until(item.expiresAt)
				if ttl > 0 {
					s.fw.AuthorizeIP(aaaa.AAAA, ttl)
					count++
				}
			}
		}
	}
	// Log only if non-zero to avoid spam on empty cache
	if count > 0 {
		log.Printf("[DNS] Synced DNS cache to firewall: %d records", count)
	}
}

// RemoveBlockedDomains removes domains from the blocklist
func (s *Service) RemoveBlockedDomains(domains []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range domains {
		domain := strings.ToLower(dns.Fqdn(d))
		delete(s.blockedDomains, domain)
	}
}

// Old Start/Stop removed
// Old Start/Stop removed

// Old Start/Stop methods replaced by interface methods

func (s *Service) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Compress = false

	if len(r.Question) == 0 {
		w.WriteMsg(msg)
		return
	}

	q := r.Question[0]
	name := strings.ToLower(q.Name)

	// Check Blocklists
	s.mu.RLock()
	blocked := s.blockedDomains[name]
	// Also check without trailing dot
	if !blocked && strings.HasSuffix(name, ".") {
		blocked = s.blockedDomains[name[:len(name)-1]]
	}
	s.mu.RUnlock()

	if blocked {
		log.Printf("[DNS] Blocked query for %s", name)
		msg.Rcode = dns.RcodeNameError // NXDOMAIN
		w.WriteMsg(msg)
		return
	}

	// Check Cache
	cacheKey := fmt.Sprintf("%s:%d", name, q.Qtype)
	s.mu.RLock()
	cached, found := s.cache[cacheKey]
	s.mu.RUnlock()

	if found && clock.Now().Before(cached.expiresAt) {
		resp := cached.msg.Copy()
		resp.SetReply(r)
		w.WriteMsg(resp)
		return
	}

	// Local Lookup
	s.mu.RLock()
	rec, ok := s.records[name]
	s.mu.RUnlock()

	if ok {
		// Check type matches (or handled CNAME?)
		// For now strictly match type requested vs type configured
		// If q.Qtype is A and we have A, good.
		// If q.Qtype is AAAA and we have A, empty response (NODATA) or next logic?
		// Simplified: only return if type matches.

		rr := s.createRR(q, rec)
		if rr != nil {
			msg.Answer = append(msg.Answer, rr)
			w.WriteMsg(msg)
			return
		}
	}

	// Conditional Forwarding
	for _, cf := range s.config.ConditionalForwarders {
		domain := dns.Fqdn(cf.Domain)
		if strings.HasSuffix(name, strings.ToLower(domain)) {
			// Convert string servers to upstreams (assume UDP for now)
			var cfUpstreams []upstream
			for _, srv := range cf.Servers {
				cfUpstreams = append(cfUpstreams, upstream{Addr: srv, Protocol: "udp"})
			}
			s.forwardTo(w, r, cfUpstreams)
			return
		}
	}

	// Forwarding
	// Merge static and dynamic upstreams
	var allUpstreams []upstream
	s.mu.RLock()
	if len(s.upstreams) > 0 {
		allUpstreams = append(allUpstreams, s.upstreams...)
	}
	if len(s.dynamicUpstreams) > 0 {
		allUpstreams = append(allUpstreams, s.dynamicUpstreams...)
	}
	s.mu.RUnlock()

	if len(allUpstreams) > 0 {
		s.forwardTo(w, r, allUpstreams)
		return
	}

	// NXDOMAIN
	msg.Rcode = dns.RcodeNameError
	w.WriteMsg(msg)
}

func (s *Service) createRR(q dns.Question, rec config.DNSRecord) dns.RR {
	ttl := uint32(rec.TTL)
	if ttl == 0 {
		ttl = 3600
	}

	header := dns.RR_Header{
		Name:   q.Name,
		Rrtype: q.Qtype,
		Class:  dns.ClassINET,
		Ttl:    ttl,
	}

	if dns.TypeToString[q.Qtype] == rec.Type {
		switch q.Qtype {
		case dns.TypeA:
			if ip := net.ParseIP(rec.Value); ip != nil && ip.To4() != nil {
				return &dns.A{Hdr: header, A: ip.To4()}
			}
		case dns.TypeAAAA:
			if ip := net.ParseIP(rec.Value); ip != nil && ip.To16() != nil {
				return &dns.AAAA{Hdr: header, AAAA: ip.To16()}
			}
		case dns.TypeCNAME:
			return &dns.CNAME{Hdr: header, Target: dns.Fqdn(rec.Value)}
		case dns.TypeTXT:
			return &dns.TXT{Hdr: header, Txt: []string{rec.Value}}
		case dns.TypePTR:
			return &dns.PTR{Hdr: header, Ptr: dns.Fqdn(rec.Value)}
		case dns.TypeMX:
			var pref uint16
			var target string
			if n, _ := fmt.Sscanf(rec.Value, "%d %s", &pref, &target); n == 2 {
				return &dns.MX{Hdr: header, Preference: pref, Mx: dns.Fqdn(target)}
			}
		case dns.TypeSRV:
			var priority, weight, port uint16
			var target string
			if n, _ := fmt.Sscanf(rec.Value, "%d %d %d %s", &priority, &weight, &port, &target); n == 4 {
				return &dns.SRV{Hdr: header, Priority: priority, Weight: weight, Port: port, Target: dns.Fqdn(target)}
			}
		}
	}
	return nil
}

func (s *Service) forwardTo(w dns.ResponseWriter, r *dns.Msg, upstreams []upstream) {
	if r == nil || len(r.Question) == 0 {
		return
	}

	c := new(dns.Client)
	c.Timeout = 2 * time.Second

	for _, up := range upstreams {
		var resp *dns.Msg
		var err error

		// Enable DNSSEC if configured
		if s.config.DNSSEC {
			setDO(r)
		}

		switch up.Protocol {
		case "tcp-tls": // DNS-over-TLS
			c.Net = "tcp-tls"
			c.TLSConfig = &tls.Config{
				ServerName: up.ServerName,
				MinVersion: tls.VersionTLS12,
			}
			addr := up.Addr
			if !strings.Contains(addr, ":") {
				addr = addr + ":853"
			}
			resp, _, err = c.Exchange(r, addr)

		case "https": // DNS-over-HTTPS
			c.Net = "https"
			// For DoH, Address is the URL
			resp, _, err = c.Exchange(r, up.URL)

		case "tcp":
			c.Net = "tcp"
			addr := up.Addr
			if !strings.Contains(addr, ":") {
				addr = addr + ":53"
			}
			resp, _, err = c.Exchange(r, addr)

		default: // udp
			c.Net = "udp"
			addr := up.Addr
			if !strings.Contains(addr, ":") {
				addr = addr + ":53"
			}
			resp, _, err = c.Exchange(r, addr)
		}

		if err == nil && resp != nil {
			// DNSSEC Validation Check
			if s.config.DNSSEC && !validateResponse(resp) {
				// Only warn for now, don't drop (Soft Fail)
				// Real validation would require local trust anchors or hard fail mode
				log.Printf("[DNSSEC] Warning: Response from %s not authenticated (AD bit unset)", up.Addr)
			}

			s.cacheResponse(r, resp)
			w.WriteMsg(resp)
			return
		}
	}

	// Failed to forward
	log.Printf("[DNS] All forwarders failed")
	dns.HandleFailed(w, r)
}

// GetCache returns the current DNS cache entries for upgrade state preservation
func (s *Service) GetCache() []upgrade.DNSCacheEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	var entries []upgrade.DNSCacheEntry
	for _, item := range s.cache {
		msgBytes, err := item.msg.Pack()
		if err != nil {
			log.Printf("Failed to pack DNS message for cache export: %v", err)
			continue
		}

		if len(item.msg.Question) > 0 {
			entries = append(entries, upgrade.DNSCacheEntry{
				Name:    item.msg.Question[0].Name,
				Type:    item.msg.Question[0].Qtype,
				Data:    msgBytes,
				Expires: item.expiresAt,
			})
		}
	}
	return entries
}

func (s *Service) startCacheCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanupCache()
		case <-s.stopCleanup:
			return
		}
	}
}

func (s *Service) cleanupCache() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := clock.Now()
	cleaned := 0
	for key, entry := range s.cache {
		if now.After(entry.expiresAt) {
			delete(s.cache, key)
			cleaned++
		}
	}
	if cleaned > 0 {
		log.Printf("[DNS] Cleaned %d expired cache entries", cleaned)
	}
}

func (s *Service) cacheResponse(req, resp *dns.Msg) {
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) == 0 {
		return
	}

	minTTL := uint32(3600) // Default max
	for _, rr := range resp.Answer {
		if rr.Header().Ttl < minTTL {
			minTTL = rr.Header().Ttl
		}
	}

	// Don't cache very short TTLs
	if minTTL > 5 {
		cacheKey := fmt.Sprintf("%s:%d", strings.ToLower(req.Question[0].Name), req.Question[0].Qtype)
		s.mu.Lock()
		defer s.mu.Unlock()

		// DOS Protection: Limit cache size with random eviction
		if len(s.cache) >= 10000 {
			// Evict a random entry (Go map iteration is random)
			for k := range s.cache {
				delete(s.cache, k)
				break
			}
		}

		s.cache[cacheKey] = cachedResponse{
			msg:       resp,
			expiresAt: clock.Now().Add(time.Duration(minTTL) * time.Second),
		}
	}

	
	// Snoop response for firewall authorization
	s.snoopResponse(resp)
}

func (s *Service) snoopResponse(resp *dns.Msg) {
	s.mu.RLock()
	enabled := s.egressFilterEnabled
	customTTL := s.egressFilterTTL
	s.mu.RUnlock()

	if !enabled || s.fw == nil {
		return
	}

	for _, rr := range resp.Answer {
		var ip net.IP
		ttl := time.Duration(rr.Header().Ttl) * time.Second
		if customTTL > 0 {
			ttl = time.Duration(customTTL) * time.Second
		}

		switch v := rr.(type) {
		case *dns.A:
			ip = v.A
		case *dns.AAAA:
			ip = v.AAAA
		}

		if ip != nil {
			// Async authorization
			go func(ip net.IP, ttl time.Duration) {
				if err := s.fw.AuthorizeIP(ip, ttl); err != nil {
					// Low-level debugging only
					// s.logger.Debug("Failed to authorize IP", "ip", ip, "error", err)
				}
			}(ip, ttl)
		}
	}
}

func (s *Service) buildStaticUpstreams(cfg *config.DNSServer) []upstream {
	var upstreams []upstream

	// DoT
	for _, dot := range cfg.UpstreamDoT {
		if dot.Enabled {
			upstreams = append(upstreams, upstream{
				Addr:       dot.Server,
				Protocol:   "tcp-tls",
				ServerName: dot.ServerName,
			})
		}
	}

	// DoH
	for _, doh := range cfg.UpstreamDoH {
		if doh.Enabled {
			upstreams = append(upstreams, upstream{
				URL:      doh.URL,
				Protocol: "https",
			})
		}
	}

	// UDP Forwarders (Legacy)
	for _, fwd := range cfg.Forwarders {
		upstreams = append(upstreams, upstream{
			Addr:     fwd,
			Protocol: "udp",
		})
	}

	return upstreams
}
