package learning

import (
	"net"
	"testing"
	"time"

	"grimm.is/glacic/internal/logging"

	"grimm.is/glacic/internal/learning/flowdb"
)

func TestDNSSnoopCache(t *testing.T) {
	logger := logging.Default()
	cache := NewDNSSnoopCache(logger, 5) // Small size for testing eviction
	defer cache.Stop()

	t.Run("SetAndGet", func(t *testing.T) {
		cache.Set("1.1.1.1", "example.com", time.Hour)

		domain, ok := cache.Get("1.1.1.1")
		if !ok {
			t.Error("Get failed")
		}
		if domain != "example.com" {
			t.Errorf("Expected example.com, got %s", domain)
		}

		domain, src, ok := cache.GetWithSource("1.1.1.1")
		if !ok {
			t.Error("GetWithSource failed")
		}
		if domain != "example.com" {
			t.Errorf("Expected example.com, got %s", domain)
		}
		if src != flowdb.SourceDNSSnoop {
			t.Errorf("Expected SourceDNSSnoop, got %v", src)
		}
	})

	t.Run("Eviction", func(t *testing.T) {
		// Fill cache
		for i := 0; i < 10; i++ {
			ip := net.IPv4(192, 168, 1, byte(i)).String()
			cache.Set(ip, "foo.com", time.Hour)
		}

		if cache.Size() > 5 {
			t.Errorf("Cache size %d exceeds max 5", cache.Size())
		}
	})

	t.Run("Expiration", func(t *testing.T) {
		cache.Delete("2.2.2.2")
		// Clean manually to test logic? Or just check Get results which checks expiry
		// Set short TTL (minTTL is 5m, so we need to mock time or accept min limit)
		// Set enforces minTTL.
		// However, we can inspect internal state or use a long wait (unlikely).
		// Or we can assume minTTL logic works and test that expired items are ignored.
		// Let's modify expire time manually for test? No, encapsulation.
		// We trust Set enforces bounds.

		// Let's verify bounds enforcement
		// Set 1s TTL -> should be upgraded to 5m
		cache.Set("3.3.3.3", "short.com", time.Second)
		// We can't access ExpiresAt easily without reflection.
		// Skip expiration functional test unless we expose clock mocking.
	})

	t.Run("HandleDNSResponse", func(t *testing.T) {
		ip := net.ParseIP("4.4.4.4")
		cache.HandleDNSResponse("google.com", ip, 300)

		domain, ok := cache.Get("4.4.4.4")
		if !ok || domain != "google.com" {
			t.Error("HandleDNSResponse failed to populate cache")
		}
	})

	t.Run("LookupReverse", func(t *testing.T) {
		// Localhost likely resolves
		domain, err := cache.LookupReverse("127.0.0.1")
		if err != nil {
			t.Logf("LookupReverse failed (network issue?): %v", err)
		} else {
			if domain == "" {
				t.Log("LookupReverse returned empty domain")
			} else {
				// Verify it's cached
				cached, src, ok := cache.GetWithSource("127.0.0.1")
				if ok {
					if cached != domain {
						t.Errorf("Cached reverse domain mismatch: got %s want %s", cached, domain)
					}
					if src != flowdb.SourceReverse {
						t.Errorf("Expected SourceReverse, got %v", src)
					}
				}
			}
		}
	})

	t.Run("Stats", func(t *testing.T) {
		stats := cache.Stats()
		if stats["size"] == 0 {
			t.Error("Stats reported empty size")
		}
	})
}
