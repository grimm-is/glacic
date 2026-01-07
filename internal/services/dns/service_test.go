package dns

import (
	"grimm.is/glacic/internal/config"
	"net"
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"

	"github.com/miekg/dns"
)

func newTestService(cfg *config.DNSServer) (*Service, error) {
	// Create dummy logger
	logger := logging.New(logging.DefaultConfig())
	
	// Default full config
	fullCfg := &config.Config{
		DNSServer: cfg,
	}

	s := NewService(fullCfg, logger)
	_, err := s.Reload(fullCfg)
	return s, err
}

func TestService_DynamicRecords(t *testing.T) {
	cfg := &config.DNSServer{Enabled: true}
	s, err := newTestService(cfg)
	if err != nil {
		t.Fatalf("newTestService failed: %v", err)
	}

	// Add record
	ip := net.ParseIP("192.168.1.100")
	s.AddRecord("test.local", ip)

	s.mu.RLock()
	rec, exists := s.records["test.local."]
	s.mu.RUnlock()

	if !exists {
		t.Error("Record was not added")
	}
	if rec.Value != "192.168.1.100" {
		t.Errorf("Expected value 192.168.1.100, got %s", rec.Value)
	}

	// Update record
	newIP := net.ParseIP("192.168.1.200")
	s.AddRecord("test.local", newIP)

	s.mu.RLock()
	rec = s.records["test.local."]
	s.mu.RUnlock()

	if rec.Value != "192.168.1.200" {
		t.Errorf("Expected value 192.168.1.200 after update, got %s", rec.Value)
	}

	// Remove record
	s.RemoveRecord("test.local")

	s.mu.RLock()
	_, exists = s.records["test.local."]
	s.mu.RUnlock()

	if exists {
		t.Error("Record was not removed")
	}
}

func TestService_BlockedDomains(t *testing.T) {
	cfg := &config.DNSServer{Enabled: true}
	s, _ := newTestService(cfg)

	// Update blocklist
	domains := []string{"ads.example.com", "malware.test"}
	s.UpdateBlockedDomains(domains)

	s.mu.RLock()
	if !s.blockedDomains["ads.example.com."] {
		t.Error("ads.example.com not blocked")
	}
	if !s.blockedDomains["malware.test."] {
		t.Error("malware.test not blocked")
	}
	s.mu.RUnlock()

	// Remove from blocklist
	toRemove := []string{"ads.example.com"}
	s.RemoveBlockedDomains(toRemove)

	s.mu.RLock()
	if s.blockedDomains["ads.example.com."] {
		t.Error("ads.example.com should be unblocked")
	}
	if !s.blockedDomains["malware.test."] {
		t.Error("malware.test should still be blocked")
	}
	s.mu.RUnlock()
}

func TestService_GetCache(t *testing.T) {
	cfg := &config.DNSServer{Enabled: true}
	s, _ := newTestService(cfg)

	// Inject cache entry
	qname := "cached.test."
	msg := new(dns.Msg)
	msg.SetQuestion(qname, dns.TypeA)
	msg.Answer = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   []byte{1, 2, 3, 4},
		},
	}

	validTime := time.Now().Add(1 * time.Hour)
	s.cache["cached.test.:1"] = cachedResponse{
		msg:       msg,
		expiresAt: validTime,
	}

	entries := s.GetCache()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 cache entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Name != "cached.test." {
		t.Errorf("Expected name cached.test., got %s", entry.Name)
	}
	if entry.Type != uint16(dns.TypeA) {
		t.Errorf("Expected type A, got %d", entry.Type)
	}
	// Rounding might affect time comparison, but let's check basic validity
	if entry.Expires.Before(time.Now()) {
		t.Error("Entry returned as expired")
	}
}

// MockResponseWriter implements dns.ResponseWriter for testing
type MockResponseWriter struct {
	msg *dns.Msg
}

func (m *MockResponseWriter) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53}
}
func (m *MockResponseWriter) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 12345}
}
func (m *MockResponseWriter) WriteMsg(msg *dns.Msg) error {
	m.msg = msg
	return nil
}
func (m *MockResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (m *MockResponseWriter) Close() error              { return nil }
func (m *MockResponseWriter) TsigStatus() error         { return nil }
func (m *MockResponseWriter) TsigTimersOnly(bool)       {}
func (m *MockResponseWriter) Hijack()                   {}

func TestServeDNS_Blocked(t *testing.T) {
	cfg := &config.DNSServer{Enabled: true}
	s, _ := newTestService(cfg)
	s.UpdateBlockedDomains([]string{"bad.com"})

	req := new(dns.Msg)
	req.SetQuestion("bad.com.", dns.TypeA)

	w := &MockResponseWriter{}
	s.ServeDNS(w, req)

	if w.msg == nil {
		t.Fatal("No response written")
	}
	if w.msg.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN (3) for blocked domain, got %d", w.msg.Rcode)
	}
}

func TestServeDNS_LocalRecord(t *testing.T) {
	cfg := &config.DNSServer{Enabled: true}
	s, _ := newTestService(cfg)

	ip := net.ParseIP("192.168.1.50")
	s.AddRecord("local.lan", ip)

	req := new(dns.Msg)
	req.SetQuestion("local.lan.", dns.TypeA)

	w := &MockResponseWriter{}
	s.ServeDNS(w, req)

	if w.msg == nil {
		t.Fatal("No response written")
	}
	if w.msg.Rcode != dns.RcodeSuccess {
		t.Errorf("Expected Success (0), got %d", w.msg.Rcode)
	}
	if len(w.msg.Answer) != 1 {
		t.Fatalf("Expected 1 answer, got %d", len(w.msg.Answer))
	}

	if a, ok := w.msg.Answer[0].(*dns.A); ok {
		if !a.A.Equal(ip) {
			t.Errorf("Expected IP %v, got %v", ip, a.A)
		}
	} else {
		t.Error("Expected A record answer")
	}
}
