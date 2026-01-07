package dhcp

import (
	"net"
	"sync"
	"testing"
	"time"

	"grimm.is/glacic/internal/clock"
	"grimm.is/glacic/internal/config"
)

// TestLeaseListener tracks lease events including expirations
type TestLeaseListener struct {
	mu          sync.Mutex
	leases      []testLeaseEvent
	expirations []testLeaseEvent
}

type testLeaseEvent struct {
	MAC      string
	IP       net.IP
	Hostname string
}

func (m *TestLeaseListener) OnLease(mac string, ip net.IP, hostname string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leases = append(m.leases, testLeaseEvent{mac, ip, hostname})
}

func (m *TestLeaseListener) OnLeaseExpired(mac string, ip net.IP, hostname string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expirations = append(m.expirations, testLeaseEvent{mac, ip, hostname})
}

func (m *TestLeaseListener) LeaseCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.leases)
}

func (m *TestLeaseListener) ExpirationCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.expirations)
}

// TestLeaseExpiration_CallbackFiresOnExpiry tests that expiration callback is triggered
// RED PHASE: ExpireLeases and related methods don't exist yet - these tests SHOULD FAIL
func TestLeaseExpiration_CallbackFiresOnExpiry(t *testing.T) {
	mockClock := clock.NewMockClock(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	dnsUpdater := &MockDNSUpdater{}
	listener := &TestLeaseListener{}

	// Create a short-lived lease
	startIP := net.ParseIP("192.168.1.100").To4()
	endIP := net.ParseIP("192.168.1.105").To4()

	store := &LeaseStore{
		Leases:       make(map[string]net.IP),
		TakenIPs:     make(map[string]string),
		Reservations: make(map[string]config.DHCPReservation),
		ReservedIPs:  make(map[string]string),
		RangeStart:   startIP,
		RangeEnd:     endIP,
		clock:        mockClock,     // Inject mock clock
		leaseTime:    1 * time.Hour, // Short lease for testing
	}

	// Allocate a lease
	mac := "00:11:22:33:44:55"
	ip, err := store.Allocate(mac)
	if err != nil {
		t.Fatalf("Failed to allocate: %v", err)
	}

	// Simulate DNS registration
	hostname := "testhost.local"
	dnsUpdater.AddRecord(hostname, ip)
	store.SetHostname(mac, hostname) // Track hostname for expiration callback

	if dnsUpdater.records[hostname] == nil {
		t.Fatal("DNS record should exist before expiration")
	}

	// Advance time past lease expiration
	mockClock.Advance(2 * time.Hour)

	// Trigger expiration check
	// This requires the ExpireLeases method to be implemented
	expiredCount := store.ExpireLeases(dnsUpdater, listener)

	if expiredCount == 0 {
		t.Error("Expected at least 1 lease to expire")
	}

	// Verify DNS record was removed
	if dnsUpdater.records[hostname] != nil {
		t.Error("DNS record should be removed after expiration")
	}

	// Verify callback was fired
	if listener.ExpirationCount() == 0 {
		t.Error("Expiration callback should have been fired")
	}
}

// TestLeaseRenewal_ExtendsExpiration tests that renewing a lease extends its lifetime
// RED PHASE: RenewLease doesn't exist yet
func TestLeaseRenewal_ExtendsExpiration(t *testing.T) {
	mockClock := clock.NewMockClock(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	startIP := net.ParseIP("192.168.1.100").To4()
	endIP := net.ParseIP("192.168.1.105").To4()

	store := &LeaseStore{
		Leases:       make(map[string]net.IP),
		TakenIPs:     make(map[string]string),
		Reservations: make(map[string]config.DHCPReservation),
		ReservedIPs:  make(map[string]string),
		RangeStart:   startIP,
		RangeEnd:     endIP,
		clock:        mockClock,
		leaseTime:    24 * time.Hour,
	}

	mac := "00:11:22:33:44:55"

	// First allocation
	ip1, err := store.Allocate(mac)
	if err != nil {
		t.Fatalf("First allocation failed: %v", err)
	}

	// Advance time but stay within lease period
	mockClock.Advance(12 * time.Hour)

	// Renew lease (should extend from current time)
	err = store.RenewLease(mac)
	if err != nil {
		t.Fatalf("Renewal failed: %v", err)
	}

	// Advance time past original expiration but within renewed period
	mockClock.Advance(18 * time.Hour) // 30 hours total, but renewed at 12h

	// Expire should NOT affect this lease
	store.ExpireLeases(nil, nil)

	// Check lease is still valid (was renewed)
	ip2, err := store.Allocate(mac)
	if err != nil {
		t.Fatalf("Post-renewal allocation failed: %v", err)
	}

	if !ip1.Equal(ip2) {
		t.Errorf("Expected same IP after renewal, got %v vs %v", ip1, ip2)
	}
}

// TestExpiredLease_IPIsReclaimed tests that expired lease IPs become available again
func TestExpiredLease_IPIsReclaimed(t *testing.T) {
	mockClock := clock.NewMockClock(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	startIP := net.ParseIP("192.168.1.100").To4()
	endIP := net.ParseIP("192.168.1.100").To4() // Single IP pool

	store := &LeaseStore{
		Leases:       make(map[string]net.IP),
		TakenIPs:     make(map[string]string),
		Reservations: make(map[string]config.DHCPReservation),
		ReservedIPs:  make(map[string]string),
		RangeStart:   startIP,
		RangeEnd:     endIP,
		clock:        mockClock,
		leaseTime:    1 * time.Hour,
	}

	mac1 := "00:11:22:33:44:01"
	mac2 := "00:11:22:33:44:02"

	// Allocate the only available IP
	ip1, err := store.Allocate(mac1)
	if err != nil {
		t.Fatalf("First allocation failed: %v", err)
	}

	// Another client should fail (pool exhausted)
	_, err = store.Allocate(mac2)
	if err == nil {
		t.Fatal("Second allocation should fail with exhausted pool")
	}

	// Expire the first lease
	mockClock.Advance(2 * time.Hour)
	store.ExpireLeases(nil, nil)

	// Now second client should succeed with reclaimed IP
	ip2, err := store.Allocate(mac2)
	if err != nil {
		t.Fatalf("Post-expiration allocation failed: %v", err)
	}

	if !ip1.Equal(ip2) {
		t.Errorf("Expected reclaimed IP %v, got %v", ip1, ip2)
	}
}
