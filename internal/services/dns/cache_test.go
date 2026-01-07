package dns

import (
	"grimm.is/glacic/internal/config"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestService_Cache(t *testing.T) {
	cfg := &config.DNSServer{Enabled: true}
	s, _ := newTestService(cfg)

	// Manually inject a cached response
	qname := "cached.example.com."
	qtype := dns.TypeA
	msg := new(dns.Msg)
	msg.SetQuestion(qname, qtype)
	msg.Answer = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   []byte{1, 2, 3, 4},
		},
	}

	cacheKey := "cached.example.com.:1"
	s.cache[cacheKey] = cachedResponse{
		msg:       msg,
		expiresAt: time.Now().Add(1 * time.Hour),
	}

	// Verify cache hit logic (simulating ServeDNS internal check)
	s.mu.RLock()
	cached, found := s.cache[cacheKey]
	s.mu.RUnlock()

	if !found {
		t.Error("Expected cache hit")
	}
	if time.Now().After(cached.expiresAt) {
		t.Error("Cache entry should not be expired")
	}
}

func TestService_Cache_Expiry(t *testing.T) {
	cfg := &config.DNSServer{Enabled: true}
	s, _ := newTestService(cfg)

	// Case 1: Active Entry
	validName := "valid.example.com."
	validMsg := new(dns.Msg)
	validMsg.SetQuestion(validName, dns.TypeA)
	validMsg.Answer = []dns.RR{
		&dns.A{Hdr: dns.RR_Header{Name: validName, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600}, A: []byte{1, 1, 1, 1}},
	}
	s.cache[validName+":1"] = cachedResponse{msg: validMsg, expiresAt: time.Now().Add(time.Hour)}

	// Case 2: Expired Entry
	expiredName := "expired.example.com."
	expiredMsg := new(dns.Msg)
	expiredMsg.SetQuestion(expiredName, dns.TypeA)
	expiredMsg.Answer = []dns.RR{
		&dns.A{Hdr: dns.RR_Header{Name: expiredName, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600}, A: []byte{2, 2, 2, 2}},
	}
	s.cacheResponse(expiredMsg, expiredMsg) // Use helper
	s.cache[expiredName+":1"] = cachedResponse{msg: expiredMsg, expiresAt: time.Now().Add(-time.Hour)}

	// Verify Valid
	req := new(dns.Msg)
	req.SetQuestion(validName, dns.TypeA)
	w := &MockResponseWriter{}
	s.ServeDNS(w, req)

	if w.msg == nil || len(w.msg.Answer) == 0 {
		t.Error("Expected valid cache hit, got nothing")
	}

	// Verify Expired (should fall through to NXDOMAIN as no other source exists)
	req = new(dns.Msg)
	req.SetQuestion(expiredName, dns.TypeA)
	w = &MockResponseWriter{}
	s.ServeDNS(w, req)

	if w.msg == nil {
		t.Error("Expected response (NXDOMAIN), got nil")
	} else if w.msg.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN for expired cache hit, got Rcode %d", w.msg.Rcode)
	}
}

func TestService_Cache_Eviction(t *testing.T) {
	cfg := &config.DNSServer{Enabled: true, CacheSize: 10000}
	s, _ := newTestService(cfg)

	// Pre-fill cache to 10000
	s.mu.Lock()
	for i := 0; i < 10000; i++ {
		key := string(rune(i)) // distinct keys
		s.cache[key] = cachedResponse{expiresAt: time.Now().Add(time.Hour)}
	}
	s.mu.Unlock()

	// Try to add one more
	req := new(dns.Msg)
	req.SetQuestion("new.example.com.", dns.TypeA)
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Answer = []dns.RR{
		&dns.A{Hdr: dns.RR_Header{Name: "new.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: []byte{1, 2, 3, 4}},
	}

	s.cacheResponse(req, resp)

	s.mu.RLock()
	size := len(s.cache)
	s.mu.RUnlock()

	if size > 10000 {
		t.Errorf("Cache size exceeded limit: %d", size)
	}

	// Verify "new.example.com.:1" exists
	// This ensures we evicted something else, rather than dropping the new one
	cacheKey := "new.example.com.:1"
	s.mu.RLock()
	_, found := s.cache[cacheKey]
	s.mu.RUnlock()

	if !found {
		t.Error("New entry was dropped instead of evicting an old one")
	}
}
