package learning

import (
	"testing"

	"grimm.is/glacic/internal/learning/flowdb"
)

func TestFlowCache_GetPut(t *testing.T) {
	c := NewFlowCache(100)

	// Initially empty
	if _, ok := c.Get("aa:bb:cc:dd:ee:ff", "TCP", 443); ok {
		t.Error("expected cache miss on empty cache")
	}

	// Put an entry
	entry := &FlowCacheEntry{
		Flow: &flowdb.Flow{
			ID:       1,
			SrcMAC:   "aa:bb:cc:dd:ee:ff",
			Protocol: "TCP",
			DstPort:  443,
		},
		Verdict: true,
	}
	c.Put("aa:bb:cc:dd:ee:ff", "TCP", 443, entry)

	// Should hit
	got, ok := c.Get("aa:bb:cc:dd:ee:ff", "TCP", 443)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Flow.ID != 1 {
		t.Errorf("got flow ID %d, want 1", got.Flow.ID)
	}
	if !got.Verdict {
		t.Error("expected verdict true")
	}

	// Stats
	hits, misses, size := c.Stats()
	if hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}
	if misses != 1 {
		t.Errorf("misses = %d, want 1", misses)
	}
	if size != 1 {
		t.Errorf("size = %d, want 1", size)
	}
}

func TestFlowCache_LRUEviction(t *testing.T) {
	c := NewFlowCache(3) // Small cache for testing

	// Fill cache
	for i := 0; i < 3; i++ {
		c.Put("aa:bb:cc:dd:ee:ff", "TCP", 100+i, &FlowCacheEntry{
			Flow: &flowdb.Flow{ID: int64(i)},
		})
	}

	if c.Size() != 3 {
		t.Errorf("size = %d, want 3", c.Size())
	}

	// Access port 100 to make it recently used
	c.Get("aa:bb:cc:dd:ee:ff", "TCP", 100)

	// Add a new entry - should evict 101 (oldest not recently accessed)
	c.Put("aa:bb:cc:dd:ee:ff", "TCP", 200, &FlowCacheEntry{
		Flow: &flowdb.Flow{ID: 99},
	})

	if c.Size() != 3 {
		t.Errorf("size = %d, want 3 after eviction", c.Size())
	}

	// Port 100 should still be there (recently accessed)
	if _, ok := c.Get("aa:bb:cc:dd:ee:ff", "TCP", 100); !ok {
		t.Error("port 100 should still be in cache (recently accessed)")
	}

	// Port 200 should be there (just added)
	if _, ok := c.Get("aa:bb:cc:dd:ee:ff", "TCP", 200); !ok {
		t.Error("port 200 should be in cache")
	}

	// Port 101 should be evicted (LRU)
	if _, ok := c.Get("aa:bb:cc:dd:ee:ff", "TCP", 101); ok {
		t.Error("port 101 should have been evicted")
	}
}

func TestFlowCache_Invalidate(t *testing.T) {
	c := NewFlowCache(100)

	c.Put("aa:bb:cc:dd:ee:ff", "TCP", 443, &FlowCacheEntry{
		Flow: &flowdb.Flow{ID: 1},
	})

	// Verify it's there
	if _, ok := c.Get("aa:bb:cc:dd:ee:ff", "TCP", 443); !ok {
		t.Fatal("expected entry to exist")
	}

	// Invalidate
	c.Invalidate("aa:bb:cc:dd:ee:ff", "TCP", 443)

	// Should be gone
	if _, ok := c.Get("aa:bb:cc:dd:ee:ff", "TCP", 443); ok {
		t.Error("expected entry to be invalidated")
	}

	if c.Size() != 0 {
		t.Errorf("size = %d, want 0", c.Size())
	}
}

func TestFlowCache_InvalidateByID(t *testing.T) {
	c := NewFlowCache(100)

	c.Put("aa:bb:cc:dd:ee:ff", "TCP", 443, &FlowCacheEntry{
		Flow: &flowdb.Flow{ID: 42},
	})
	c.Put("aa:bb:cc:dd:ee:ff", "TCP", 80, &FlowCacheEntry{
		Flow: &flowdb.Flow{ID: 99},
	})

	// Invalidate by ID
	c.InvalidateByID(42)

	// 443 should be gone
	if _, ok := c.Get("aa:bb:cc:dd:ee:ff", "TCP", 443); ok {
		t.Error("expected flow 42 to be invalidated")
	}

	// 80 should still be there
	if _, ok := c.Get("aa:bb:cc:dd:ee:ff", "TCP", 80); !ok {
		t.Error("flow 99 should still exist")
	}
}

func TestFlowCache_FlushDirty(t *testing.T) {
	c := NewFlowCache(100)

	// Add clean entry
	c.Put("aa:bb:cc:dd:ee:ff", "TCP", 443, &FlowCacheEntry{
		Flow:  &flowdb.Flow{ID: 1},
		Dirty: false,
	})

	// Add dirty entry
	c.Put("aa:bb:cc:dd:ee:ff", "TCP", 80, &FlowCacheEntry{
		Flow:  &flowdb.Flow{ID: 2},
		Dirty: true,
	})

	// Flush
	dirty := c.FlushDirty()

	if len(dirty) != 1 {
		t.Fatalf("got %d dirty flows, want 1", len(dirty))
	}
	if dirty[0].ID != 2 {
		t.Errorf("dirty flow ID = %d, want 2", dirty[0].ID)
	}

	// Flush again - should be empty now
	dirty = c.FlushDirty()
	if len(dirty) != 0 {
		t.Errorf("second flush got %d dirty flows, want 0", len(dirty))
	}
}

func TestFlowCache_Clear(t *testing.T) {
	c := NewFlowCache(100)

	c.Put("aa:bb:cc:dd:ee:ff", "TCP", 443, &FlowCacheEntry{
		Flow: &flowdb.Flow{ID: 1},
	})
	c.Put("aa:bb:cc:dd:ee:ff", "TCP", 80, &FlowCacheEntry{
		Flow: &flowdb.Flow{ID: 2},
	})

	if c.Size() != 2 {
		t.Errorf("size = %d, want 2", c.Size())
	}

	c.Clear()

	if c.Size() != 0 {
		t.Errorf("size after clear = %d, want 0", c.Size())
	}
}
