package state

import (
	"testing"
	"time"
)

// TestDHCPBucket tests DHCP lease bucket operations
func TestDHCPBucket(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	bucket, err := NewDHCPBucket(store)
	if err != nil {
		t.Fatalf("failed to create DHCP bucket: %v", err)
	}

	// Create a lease
	lease := &DHCPLease{
		MAC:        "aa:bb:cc:dd:ee:ff",
		IP:         "192.168.1.100",
		Hostname:   "test-host",
		Interface:  "eth0",
		LeaseStart: time.Now(),
		LeaseEnd:   time.Now().Add(24 * time.Hour),
	}

	// Set lease
	if err := bucket.Set(lease); err != nil {
		t.Fatalf("failed to set lease: %v", err)
	}

	// Get lease
	retrieved, err := bucket.Get("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("failed to get lease: %v", err)
	}
	if retrieved.IP != "192.168.1.100" {
		t.Errorf("wrong IP: %s", retrieved.IP)
	}
	if retrieved.Hostname != "test-host" {
		t.Errorf("wrong hostname: %s", retrieved.Hostname)
	}

	// Get by IP
	byIP, err := bucket.GetByIP("192.168.1.100")
	if err != nil {
		t.Fatalf("failed to get by IP: %v", err)
	}
	if byIP.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("wrong MAC: %s", byIP.MAC)
	}

	// List leases
	leases, err := bucket.List()
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(leases) != 1 {
		t.Errorf("expected 1 lease, got %d", len(leases))
	}

	// Delete lease
	if err := bucket.Delete("aa:bb:cc:dd:ee:ff"); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// Verify deleted
	_, err = bucket.Get("aa:bb:cc:dd:ee:ff")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestDHCPBucket_NormalizeMAC tests MAC address normalization
func TestDHCPBucket_NormalizeMAC(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	bucket, _ := NewDHCPBucket(store)

	lease := &DHCPLease{
		MAC:        "AA:BB:CC:DD:EE:FF", // Uppercase
		IP:         "192.168.1.101",
		LeaseStart: time.Now(),
		LeaseEnd:   time.Now().Add(time.Hour),
	}
	bucket.Set(lease)

	// Should find with lowercase
	retrieved, err := bucket.Get("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("should find with lowercase: %v", err)
	}
	if retrieved.IP != "192.168.1.101" {
		t.Error("wrong IP")
	}
}

// TestDNSBucket tests DNS cache bucket operations
func TestDNSBucket(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	bucket, err := NewDNSBucket(store)
	if err != nil {
		t.Fatalf("failed to create DNS bucket: %v", err)
	}

	// Create cache entry
	entry := &DNSCacheEntry{
		Name:      "example.com",
		Type:      1, // A record
		Class:     1, // IN
		TTL:       300,
		Data:      []byte{93, 184, 216, 34}, // 93.184.216.34
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	// Set entry
	if err := bucket.Set(entry); err != nil {
		t.Fatalf("failed to set DNS entry: %v", err)
	}

	// Get entry
	retrieved, err := bucket.Get("example.com", 1)
	if err != nil {
		t.Fatalf("failed to get DNS entry: %v", err)
	}
	if retrieved.Name != "example.com" {
		t.Errorf("wrong name: %s", retrieved.Name)
	}
	if retrieved.TTL != 300 {
		t.Errorf("wrong TTL: %d", retrieved.TTL)
	}

	// Delete entry
	if err := bucket.Delete("example.com", 1); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// Verify deleted
	_, err = bucket.Get("example.com", 1)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestSessionBucket tests session bucket operations
func TestSessionBucket(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	bucket, err := NewSessionBucket(store)
	if err != nil {
		t.Fatalf("failed to create session bucket: %v", err)
	}

	// Create session
	session := &Session{
		ID:        "sess-123",
		Username:  "admin",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
		IP:        "192.168.1.50",
		UserAgent: "Mozilla/5.0",
	}

	// Set session
	if err := bucket.Set(session); err != nil {
		t.Fatalf("failed to set session: %v", err)
	}

	// Get session
	retrieved, err := bucket.Get("sess-123")
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if retrieved.Username != "admin" {
		t.Errorf("wrong username: %s", retrieved.Username)
	}

	// Create another session for same user
	session2 := &Session{
		ID:        "sess-456",
		Username:  "admin",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
		IP:        "192.168.1.51",
	}
	bucket.Set(session2)

	// List by user
	sessions, err := bucket.ListByUser("admin")
	if err != nil {
		t.Fatalf("failed to list by user: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}

	// Delete session
	if err := bucket.Delete("sess-123"); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// Delete by user
	if err := bucket.DeleteByUser("admin"); err != nil {
		t.Fatalf("failed to delete by user: %v", err)
	}

	// Verify all deleted
	sessions, _ = bucket.ListByUser("admin")
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after delete, got %d", len(sessions))
	}
}

// TestBucketNotFound tests get on nonexistent entries
func TestBucketNotFound(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	dhcpBucket, _ := NewDHCPBucket(store)
	dnsBucket, _ := NewDNSBucket(store)
	sessionBucket, _ := NewSessionBucket(store)

	// DHCP
	_, err := dhcpBucket.Get("nonexistent")
	if err != ErrNotFound {
		t.Errorf("DHCP: expected ErrNotFound, got %v", err)
	}

	// DNS
	_, err = dnsBucket.Get("nonexistent", 1)
	if err != ErrNotFound {
		t.Errorf("DNS: expected ErrNotFound, got %v", err)
	}

	// Session
	_, err = sessionBucket.Get("nonexistent")
	if err != ErrNotFound {
		t.Errorf("Session: expected ErrNotFound, got %v", err)
	}

	// DHCP GetByIP
	_, err = dhcpBucket.GetByIP("10.0.0.1")
	if err != ErrNotFound {
		t.Errorf("DHCP GetByIP: expected ErrNotFound, got %v", err)
	}
}
