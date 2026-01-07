package dhcp

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/services"

	"grimm.is/glacic/internal/upgrade"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"

	"grimm.is/glacic/internal/state"
)

type dhcpInstance struct {
	conn    net.PacketConn
	handler func(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4)
}

// DNSUpdater is an interface for updating DNS records
type DNSUpdater interface {
	AddRecord(name string, ip net.IP)
	RemoveRecord(name string)
}

// LeaseListener is an interface for listening to DHCP lease events
type LeaseListener interface {
	OnLease(mac string, ip net.IP, hostname string)
}

// ExpirationListener extends LeaseListener with expiration callbacks
type ExpirationListener interface {
	LeaseListener
	OnLeaseExpired(mac string, ip net.IP, hostname string)
}

// PacketListener is a callback for observing DHCP packets
type PacketListener func(pkt *dhcpv4.DHCPv4, iface string, src net.Addr)

// Service manages DHCP servers for multiple scopes.
type Service struct {
	mu             sync.RWMutex
	servers        []*dhcpInstance
	leaseStores    []*LeaseStore // Track stores for expiration reaper
	dnsUpdater     DNSUpdater
	leaseListener  LeaseListener
	packetListener PacketListener // Passive sniffing listener
	store          state.Store
	running        bool
	stopReaper     chan struct{} // Signal to stop expiration reaper
	upgradeMgr     *upgrade.Manager
}

// SetUpgradeManager sets the upgrade manager for socket handoff.
func (s *Service) SetUpgradeManager(mgr *upgrade.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upgradeMgr = mgr
}

// SetLeaseListener sets the lease listener
func (s *Service) SetLeaseListener(l LeaseListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leaseListener = l
}

// SetPacketListener sets the packet listener for passive sniffing
func (s *Service) SetPacketListener(l PacketListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packetListener = l
}

// NewService creates a new DHCP service.
func NewService(dnsUpdater DNSUpdater, store state.Store) *Service {
	return &Service{
		dnsUpdater: dnsUpdater,
		store:      store,
	}
}

// Name returns the service name.
func (s *Service) Name() string {
	return "DHCP"
}

// Start starts the service.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	for _, srv := range s.servers {
		go func(inst *dhcpInstance) {
			log.Printf("[DHCP] Starting server instance...")
			s.serveDHCP(inst.conn, inst.handler)
		}(srv)
	}
	s.running = true
	return nil
}

// Stop stops the service.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		// Even if not running, close existing servers to be safe?
		// But usually servers are created on Reload.
		return nil
	}

	for _, srv := range s.servers {
		if err := srv.conn.Close(); err != nil {
			log.Printf("[DHCP] Failed to stop server: %v", err)
		}
	}
	s.running = false
	return nil
}

// Reload reconfigures the service.
func (s *Service) Reload(cfg *config.Config) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop existing servers and reaper
	if s.running {
		// Stop the expiration reaper
		if s.stopReaper != nil {
			close(s.stopReaper)
			s.stopReaper = nil
		}

		for _, srv := range s.servers {
			srv.conn.Close()
		}
		s.running = false
	}
	s.servers = nil     // Clear old servers
	s.leaseStores = nil // Clear old lease stores

	if cfg.DHCP == nil || !cfg.DHCP.Enabled {
		return true, nil
	}

	// Check mode
	if cfg.DHCP.Mode == "external" || cfg.DHCP.Mode == "import" {
		log.Printf("[DHCP] Configured in '%s' mode. Skipping built-in server startup.", cfg.DHCP.Mode)
		return true, nil
	}

	// Parse scopes (only if built-in)
	for _, scope := range cfg.DHCP.Scopes {
		srv, ls, err := s.createServer(scope)
		if err != nil {
			return true, fmt.Errorf("failed to create DHCP server for scope %s: %w", scope.Name, err)
		}
		s.servers = append(s.servers, srv)
		s.leaseStores = append(s.leaseStores, ls)
	}

	// Restart servers
	for _, srv := range s.servers {
		go func(inst *dhcpInstance) {
			log.Printf("[DHCP] Starting server instance...")
			s.serveDHCP(inst.conn, inst.handler)
		}(srv)
	}

	// Start expiration reaper
	s.stopReaper = make(chan struct{})
	go s.runExpirationReaper(s.stopReaper)

	s.running = true

	return true, nil
}

// runExpirationReaper periodically checks for and removes expired leases
func (s *Service) runExpirationReaper(stop <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	log.Printf("[DHCP] Expiration reaper started")

	for {
		select {
		case <-ticker.C:
			s.expireLeases()
		case <-stop:
			log.Printf("[DHCP] Expiration reaper stopped")
			return
		}
	}
}

// expireLeases checks all lease stores for expired leases
func (s *Service) expireLeases() {
	s.mu.RLock()
	stores := s.leaseStores
	dnsUpdater := s.dnsUpdater
	listener := s.leaseListener
	s.mu.RUnlock()

	var expListener ExpirationListener
	if listener != nil {
		if el, ok := listener.(ExpirationListener); ok {
			expListener = el
		}
	}

	totalExpired := 0
	for _, store := range stores {
		totalExpired += store.ExpireLeases(dnsUpdater, expListener)
	}

	if totalExpired > 0 {
		log.Printf("[DHCP] Expired %d leases", totalExpired)
	}
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

// LeaseExpiry stores the expiration info for a lease
type LeaseExpiry struct {
	IP       net.IP
	Hostname string
	Expires  time.Time
}

// Lease storage (simple in-memory)
type LeaseStore struct {
	sync.Mutex
	Leases       map[string]net.IP                 // MAC -> IP
	TakenIPs     map[string]string                 // IP (string) -> MAC (for O(1) isTaken lookup)
	Reservations map[string]config.DHCPReservation // MAC -> Reservation
	ReservedIPs  map[string]string                 // IP (string) -> MAC
	RangeStart   net.IP
	RangeEnd     net.IP
	bucket       *state.DHCPBucket // Persistent storage

	// Expiration support
	clock       clock.Clock          // Injectable clock for testing
	leaseTime   time.Duration        // Default lease duration
	hostnames   map[string]string    // MAC -> hostname for DNS cleanup
	leaseExpiry map[string]time.Time // MAC -> expiration time
}

func (s *LeaseStore) Allocate(mac string) (net.IP, error) {
	s.Lock()
	defer s.Unlock()

	// 1. Check for static reservation
	if res, ok := s.Reservations[mac]; ok {
		// Parse IP from reservation
		ip := net.ParseIP(res.IP).To4()
		if ip != nil {
			return ip, nil
		}
	}

	// 2. Check existing dynamic lease
	if ip, ok := s.Leases[mac]; ok {
		return ip, nil
	}

	// 3. Allocate new dynamic IP (Naive linear scan)
	for ip := s.RangeStart; !ipMatches(ip, s.RangeEnd); ip = incIP(ip) {
		ipStr := ip.String()

		// Skip if this IP is reserved for another MAC
		if _, reserved := s.ReservedIPs[ipStr]; reserved {
			continue
		}

		// Skip if currently leased
		if !s.isTaken(ip) {
			newIP := make(net.IP, len(ip))
			copy(newIP, ip)

			// Persist first
			if err := s.persistLease(mac, newIP, "hostname-unknown"); err != nil {
				log.Printf("[DHCP] Failed to persist lease: %v", err)
				// Continue anyway or fail? Fail to ensure safety.
				return nil, fmt.Errorf("failed to persist lease: %w", err)
			}

			s.Leases[mac] = newIP
			s.TakenIPs[newIP.String()] = mac // Maintain reverse lookup
			s.setLeaseExpiry(mac)
			return newIP, nil
		}
	}

	// Check the last one (RangeEnd)
	if _, reserved := s.ReservedIPs[s.RangeEnd.String()]; !reserved && !s.isTaken(s.RangeEnd) {
		newIP := make(net.IP, len(s.RangeEnd))
		copy(newIP, s.RangeEnd)

		// Persist
		if err := s.persistLease(mac, newIP, "hostname-unknown"); err != nil {
			return nil, fmt.Errorf("failed to persist lease: %w", err)
		}

		s.Leases[mac] = newIP
		s.TakenIPs[newIP.String()] = mac // Maintain reverse lookup
		s.setLeaseExpiry(mac)
		return newIP, nil
	}

	return nil, fmt.Errorf("no IPs available")
}

func (s *LeaseStore) isTaken(ip net.IP) bool {
	// O(1) lookup using TakenIPs reverse map
	_, exists := s.TakenIPs[ip.String()]
	return exists
}

func incIP(ip net.IP) net.IP {
	ret := make(net.IP, len(ip))
	copy(ret, ip)
	for i := len(ret) - 1; i >= 0; i-- {
		ret[i]++
		if ret[i] > 0 {
			break
		}
	}
	return ret
}

func ipMatches(a, b net.IP) bool {
	return a.Equal(b)
}

func (s *LeaseStore) persistLease(mac string, ip net.IP, hostname string) error {
	if s.bucket == nil {
		return nil
	}

	lease := &state.DHCPLease{
		MAC:        mac,
		IP:         ip.String(),
		Hostname:   hostname,
		LeaseStart: clock.Now(),
		LeaseEnd:   clock.Now().Add(24 * time.Hour), // Should match scope config
	}

	// Synchronous write to ensure persistence
	if err := s.bucket.Set(lease); err != nil {
		return err
	}
	log.Printf("[DHCP] Persisted lease for %s: %s", mac, ip.String())
	return nil
}

// getNow returns the current time using injected clock or real time
func (s *LeaseStore) getNow() time.Time {
	if s.clock != nil {
		return s.clock.Now()
	}
	return clock.Now()
}

// getLeaseTime returns the configured lease time or default 24 hours
func (s *LeaseStore) getLeaseTime() time.Duration {
	if s.leaseTime > 0 {
		return s.leaseTime
	}
	return 24 * time.Hour
}

// SetHostname associates a hostname with a MAC for expiration callbacks
func (s *LeaseStore) SetHostname(mac string, hostname string) {
	s.Lock()
	defer s.Unlock()
	if s.hostnames == nil {
		s.hostnames = make(map[string]string)
	}
	s.hostnames[mac] = hostname
}

// setLeaseExpiry records when a lease expires (called during allocation)
func (s *LeaseStore) setLeaseExpiry(mac string) {
	// Called while already holding lock
	if s.leaseExpiry == nil {
		s.leaseExpiry = make(map[string]time.Time)
	}
	s.leaseExpiry[mac] = s.getNow().Add(s.getLeaseTime())
}

// RenewLease extends the lease for an existing MAC address
func (s *LeaseStore) RenewLease(mac string) error {
	s.Lock()
	defer s.Unlock()

	if _, ok := s.Leases[mac]; !ok {
		return fmt.Errorf("no active lease for MAC %s", mac)
	}

	// Extend expiration from now
	if s.leaseExpiry == nil {
		s.leaseExpiry = make(map[string]time.Time)
	}
	s.leaseExpiry[mac] = s.getNow().Add(s.getLeaseTime())

	log.Printf("[DHCP] Renewed lease for %s, new expiry: %v", mac, s.leaseExpiry[mac])
	return nil
}

// ExpireLeases checks for expired leases and removes them
// Returns the number of leases expired
func (s *LeaseStore) ExpireLeases(dnsUpdater DNSUpdater, listener ExpirationListener) int {
	s.Lock()
	defer s.Unlock()

	if s.leaseExpiry == nil {
		return 0
	}

	now := s.getNow()
	expired := 0

	for mac, expiry := range s.leaseExpiry {
		if now.After(expiry) {
			ip := s.Leases[mac]
			hostname := ""
			if s.hostnames != nil {
				hostname = s.hostnames[mac]
			}

			// Remove lease
			delete(s.Leases, mac)
			if ip != nil {
				delete(s.TakenIPs, ip.String()) // Maintain reverse lookup
			}
			delete(s.leaseExpiry, mac)
			if s.hostnames != nil {
				delete(s.hostnames, mac)
			}

			// Remove DNS record if updater provided
			if dnsUpdater != nil && hostname != "" {
				dnsUpdater.RemoveRecord(hostname)
			}

			// Notify listener
			if listener != nil {
				listener.OnLeaseExpired(mac, ip, hostname)
			}

			log.Printf("[DHCP] Expired lease for %s (%s)", mac, ip)
			expired++
		}
	}

	return expired
}

func (s *Service) createServer(scope config.DHCPScope) (*dhcpInstance, *LeaseStore, error) {
	startIP := net.ParseIP(scope.RangeStart).To4()
	endIP := net.ParseIP(scope.RangeEnd).To4()
	routerIP := net.ParseIP(scope.Router).To4()

	if startIP == nil || endIP == nil || routerIP == nil {
		return nil, nil, fmt.Errorf("invalid IP configuration")
	}

	// Setup Lease Store with Reservations
	ls := &LeaseStore{
		Leases:       make(map[string]net.IP),
		TakenIPs:     make(map[string]string), // O(1) reverse lookup
		Reservations: make(map[string]config.DHCPReservation),
		ReservedIPs:  make(map[string]string),
		RangeStart:   startIP,
		RangeEnd:     endIP,
	}

	// Initialize bucket and load existing leases
	if s.store != nil {
		bucket, err := state.NewDHCPBucket(s.store)
		if err != nil {
			log.Printf("[DHCP] Warning: Failed to create/open DHCP bucket: %v", err)
		} else {
			ls.bucket = bucket
			leases, err := bucket.List()
			if err != nil {
				log.Printf("[DHCP] Warning: Failed to list existing leases: %v", err)
			} else {
				// Load leases into memory
				for _, l := range leases {
					ip := net.ParseIP(l.IP).To4()
					if ip != nil {
						// Note: We intentionally don't check if IP is within this scope's range.
						// Leases may span pool boundaries if config changed, and we preserve them.
						ls.Leases[l.MAC] = ip
						ls.TakenIPs[ip.String()] = l.MAC // Populate reverse lookup
					}
				}
				log.Printf("[DHCP] Loaded %d leases from state store", len(leases))
			}
		}
	}

	// Populate reservations
	for _, res := range scope.Reservations {
		ip := net.ParseIP(res.IP).To4()
		if ip != nil {
			ls.Reservations[res.MAC] = res
			ls.ReservedIPs[ip.String()] = res.MAC
		}
	}

	handler := func(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {
		// Basic logging
		// log.Printf("[DHCP] Received packet: %v", m.Summary())

		// Notify passive packet listener
		s.mu.RLock()
		pl := s.packetListener
		s.mu.RUnlock()
		if pl != nil {
			// Run in goroutine to not block DHCP server logic
			go pl(m, scope.Interface, peer)
		}

		// Helper to determine destination address
		// If peer is 0.0.0.0 (DHCP Discover), we MUST reply to Broadcast.
		dest := peer
		if udpAddr, ok := peer.(*net.UDPAddr); ok {
			if udpAddr.IP.IsUnspecified() || udpAddr.IP.Equal(net.IPv4zero) {
				dest = &net.UDPAddr{
					IP:   net.IPv4bcast,
					Port: 68,
				}
				// log.Printf("[DHCP] Peer is 0.0.0.0, forcing broadcast reply to 255.255.255.255:68")
			}
		}

		switch m.MessageType() {
		case dhcpv4.MessageTypeDiscover:
			offer, err := handleDiscover(m, ls, scope, routerIP)
			if err != nil {
				log.Printf("[DHCP] Discover error: %v", err)
				return
			}
			if _, err := conn.WriteTo(offer.ToBytes(), dest); err != nil {
				log.Printf("[DHCP] WriteOffer error: %v (dest=%v)", err, dest)
			}
		case dhcpv4.MessageTypeRequest:
			ack, err := handleRequest(m, ls, scope, routerIP, s.dnsUpdater, s.leaseListener)
			if err != nil {
				log.Printf("[DHCP] Request error: %v", err)
				return
			}
			if _, err := conn.WriteTo(ack.ToBytes(), dest); err != nil {
				log.Printf("[DHCP] WriteAck error: %v (dest=%v)", err, dest)
			}
		}
	}

	// Listen on the interface address (standard DHCP port 67)
	// Bind to 0.0.0.0 to receive broadcast packets (unless inherited)

	linkName := "dhcp-v4" // Single listener for now (0.0.0.0)
	// If we supported per-interface binding properly, we would name it "dhcp-v4-<iface>"

	var conn net.PacketConn
	if s.upgradeMgr != nil {
		if existing, ok := s.upgradeMgr.GetPacketConn(linkName); ok {
			conn = existing
			log.Printf("[DHCP] Inherited socket %s", linkName)
		}
	}

	if conn == nil {
		addr := &net.UDPAddr{
			IP:   net.IPv4zero,
			Port: 67,
		}

		// Use standard library or custom binder?
		// server4.NewIPv4UDPConn allows SO_BROADCAST etc.
		// net.ListenPacket usually sets SO_BROADCAST by default?
		// Actually, standard net.ListenPacket udp4 might not set SO_BROADCAST on some OS.
		// Safe to use server4 helper if possible? No, it returns *net.UDPConn which IS a PacketConn.
		// server4.NewIPv4UDPConn(iface, addr)

		// To be safe and reuse library logic for socket options:
		udpConn, err := server4.NewIPv4UDPConn(scope.Interface, addr)
		if err != nil {
			return nil, nil, err
		}
		conn = udpConn

		if s.upgradeMgr != nil {
			s.upgradeMgr.RegisterPacketConn(linkName, conn)
		}
	}

	// Manually construct server instance
	srv := &dhcpInstance{
		conn:    conn,
		handler: handler,
	}

	return srv, ls, nil
}

// serveDHCP runs the read loop for a DHCP server instance
func (s *Service) serveDHCP(conn net.PacketConn, handler func(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4)) {
	buf := make([]byte, 4096) // Standard UDP buffer
	for {
		// Set read deadline to allow clean shutdown if conn closed
		// Actually conn.Close() causes ReadFrom to return error immediately.

		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if s.running { // Log only if we expect to be running
				// Check for closed error
				log.Printf("[DHCP] Read error: %v", err)
			}
			return
		}

		// Make a copy of data for async handling?
		// dhcpv4.FromBytes usually parsers it.
		// If handler runs async, we need copy.
		// Current handler logic seems synchronous enough or safe?
		// "go pl(m, ...)" in handler.
		// We'll pass the slice. FromBytes parses it.

		pkt, err := dhcpv4.FromBytes(buf[:n])
		if err != nil {
			// Malformed packet
			continue
		}

		// Run handler
		// We run it synchronously to preserve packet order per interface?
		// Or async? server4 runs it synchronously in loop usually.
		handler(conn, addr, pkt)
	}
}

func handleDiscover(m *dhcpv4.DHCPv4, store *LeaseStore, scope config.DHCPScope, routerIP net.IP) (*dhcpv4.DHCPv4, error) {
	// Allocate IP
	mac := m.ClientHWAddr.String()
	ip, err := store.Allocate(mac)
	if err != nil {
		return nil, err
	}

	// Prepare options
	opts := []dhcpv4.Modifier{
		dhcpv4.WithMessageType(dhcpv4.MessageTypeOffer),
		dhcpv4.WithYourIP(ip),
		dhcpv4.WithServerIP(routerIP),
		dhcpv4.WithRouter(routerIP),
		dhcpv4.WithNetmask(net.IPv4Mask(255, 255, 255, 0)), // Assuming /24 for now
		dhcpv4.WithDNS(parseIPs(scope.DNS)...),
		dhcpv4.WithLeaseTime(uint32(24 * time.Hour.Seconds())),
	}

	// Add scope custom options
	for k, v := range scope.Options {
		opt, err := parseOption(k, v)
		if err != nil {
			log.Printf("[DHCP] Warning: failed to parse option %s=%s: %v", k, v, err)
			continue
		}
		opts = append(opts, dhcpv4.WithOption(opt))
	}

	// Add per-host custom options
	store.Lock()
	res, hasRes := store.Reservations[mac]
	store.Unlock()

	if hasRes {
		for k, v := range res.Options {
			opt, err := parseOption(k, v)
			if err != nil {
				log.Printf("[DHCP] Warning: failed to parse host option %s=%s: %v", k, v, err)
				continue
			}
			opts = append(opts, dhcpv4.WithOption(opt))
		}
	}

	return dhcpv4.NewReplyFromRequest(m, opts...)
}

func handleRequest(m *dhcpv4.DHCPv4, store *LeaseStore, scope config.DHCPScope, routerIP net.IP, dnsUpdater DNSUpdater, listener LeaseListener) (*dhcpv4.DHCPv4, error) {
	mac := m.ClientHWAddr.String()

	// Verify the requested IP matches what we would allocate (or have allocated)
	requestedIP := m.RequestedIPAddress()
	if requestedIP == nil {
		requestedIP = m.ClientIPAddr
	}

	allocatedIP, err := store.Allocate(mac)
	if err != nil {
		return nil, err
	}

	if !allocatedIP.Equal(requestedIP) && !requestedIP.IsUnspecified() {
		// Send DHCP NAK to tell client their IP is invalid
		// This causes immediate DISCOVER rather than waiting for timeout
		log.Printf("[DHCP] NAK: client %s requested %v but should get %v", mac, requestedIP, allocatedIP)
		nakOpts := []dhcpv4.Modifier{
			dhcpv4.WithMessageType(dhcpv4.MessageTypeNak),
			dhcpv4.WithServerIP(routerIP),
		}
		return dhcpv4.NewReplyFromRequest(m, nakOpts...)
	}

	// Retrieve reservation for hostname/options
	store.Lock()
	res, hasRes := store.Reservations[mac]
	store.Unlock()

	// Handle DNS Integration
	hostname := m.HostName()
	if hostname == "" && hasRes && res.Hostname != "" {
		hostname = res.Hostname
	}

	if hostname != "" && dnsUpdater != nil {
		// If domain is set, append it
		if scope.Domain != "" {
			hostname = hostname + "." + scope.Domain
		}
		dnsUpdater.AddRecord(hostname, allocatedIP)
	}

	// Prepare options
	opts := []dhcpv4.Modifier{
		dhcpv4.WithMessageType(dhcpv4.MessageTypeAck),
		dhcpv4.WithYourIP(allocatedIP),
		dhcpv4.WithServerIP(routerIP),
		dhcpv4.WithRouter(routerIP),
		dhcpv4.WithNetmask(net.IPv4Mask(255, 255, 255, 0)),
		dhcpv4.WithDNS(parseIPs(scope.DNS)...),
		dhcpv4.WithLeaseTime(uint32(24 * time.Hour.Seconds())),
	}

	// Add scope custom options
	for k, v := range scope.Options {
		opt, err := parseOption(k, v)
		if err != nil {
			log.Printf("[DHCP] Warning: failed to parse option %s=%s: %v", k, v, err)
			continue
		}
		opts = append(opts, dhcpv4.WithOption(opt))
	}

	// Add per-host custom options
	if hasRes {
		for k, v := range res.Options {
			opt, err := parseOption(k, v)
			if err != nil {
				log.Printf("[DHCP] Warning: failed to parse host option %s=%s: %v", k, v, err)
				continue
			}
			opts = append(opts, dhcpv4.WithOption(opt))
		}
	}

	// Trigger listener
	if listener != nil {
		go listener.OnLease(mac, allocatedIP, hostname)
	}

	return dhcpv4.NewReplyFromRequest(m, opts...)
}

// Lease represents a DHCP lease for external consumption
type Lease struct {
	MAC        string
	IP         net.IP
	Hostname   string
	Expiration time.Time
}

// GetLeases returns all active leases across all scopes
func (s *Service) GetLeases() []Lease {
	s.mu.RLock()
	stores := s.leaseStores
	s.mu.RUnlock()

	var leases []Lease

	for _, store := range stores {
		store.Lock()
		for mac, ip := range store.Leases {
			l := Lease{
				MAC: mac,
				IP:  ip,
			}
			if store.leaseExpiry != nil {
				if expiry, ok := store.leaseExpiry[mac]; ok {
					l.Expiration = expiry
				}
			}
			if store.hostnames != nil {
				if hostname, ok := store.hostnames[mac]; ok {
					l.Hostname = hostname
				}
			}
			leases = append(leases, l)
		}
		store.Unlock()
	}
	return leases
}
