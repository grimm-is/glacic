package learning

import (
	"container/list"
	"fmt"
	"sync"
	"sync/atomic"

	"grimm.is/glacic/internal/learning/flowdb"
)

// FlowCacheEntry represents a cached flow with its computed verdict.
type FlowCacheEntry struct {
	Flow    *flowdb.Flow
	Verdict bool // Cached verdict for quick return
	Dirty   bool // Needs DB write (updated since last flush)
}

// cacheNode wraps an entry for LRU tracking
type cacheNode struct {
	key   string
	entry *FlowCacheEntry
	elem  *list.Element
}

// FlowCache is a concurrent-safe LRU cache for flow lookups.
// This eliminates per-packet SQLite queries in ProcessPacket.
type FlowCache struct {
	mu      sync.RWMutex
	cache   map[string]*cacheNode // fingerprint -> node
	lru     *list.List            // Front = most recent, Back = least recent
	maxSize int

	// Stats
	hits   uint64
	misses uint64
}

// NewFlowCache creates a new flow cache with the given maximum size.
func NewFlowCache(maxSize int) *FlowCache {
	if maxSize <= 0 {
		maxSize = 10000 // Default: 10k entries
	}
	return &FlowCache{
		cache:   make(map[string]*cacheNode),
		lru:     list.New(),
		maxSize: maxSize,
	}
}

// makeKey creates a cache key from flow fingerprint
func makeKey(mac, proto string, port int) string {
	return fmt.Sprintf("%s:%s:%d", mac, proto, port)
}

// Get retrieves a flow from the cache.
// Returns the entry and true if found, nil and false otherwise.
func (c *FlowCache) Get(mac, proto string, port int) (*FlowCacheEntry, bool) {
	key := makeKey(mac, proto, port)

	c.mu.Lock()
	defer c.mu.Unlock()

	node, ok := c.cache[key]
	if !ok {
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	// Move to front (most recently used)
	c.lru.MoveToFront(node.elem)
	atomic.AddUint64(&c.hits, 1)

	return node.entry, true
}

// Put adds or updates a flow in the cache.
func (c *FlowCache) Put(mac, proto string, port int, entry *FlowCacheEntry) {
	key := makeKey(mac, proto, port)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if node, ok := c.cache[key]; ok {
		// Update existing entry
		node.entry = entry
		c.lru.MoveToFront(node.elem)
		return
	}

	// Evict if at capacity
	if c.lru.Len() >= c.maxSize {
		c.evictLRU()
	}

	// Add new entry
	node := &cacheNode{
		key:   key,
		entry: entry,
	}
	node.elem = c.lru.PushFront(node)
	c.cache[key] = node
}

// evictLRU removes the least recently used entry (must hold lock)
func (c *FlowCache) evictLRU() {
	back := c.lru.Back()
	if back == nil {
		return
	}

	node := back.Value.(*cacheNode)
	c.lru.Remove(back)
	delete(c.cache, node.key)
}

// Invalidate removes a specific flow from the cache.
// Call this when a flow's state changes (AllowFlow/DenyFlow).
func (c *FlowCache) Invalidate(mac, proto string, port int) {
	key := makeKey(mac, proto, port)

	c.mu.Lock()
	defer c.mu.Unlock()

	if node, ok := c.cache[key]; ok {
		c.lru.Remove(node.elem)
		delete(c.cache, node.key)
	}
}

// InvalidateByID removes a flow by its database ID.
// This requires scanning but is only called on state changes (infrequent).
func (c *FlowCache) InvalidateByID(flowID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, node := range c.cache {
		if node.entry.Flow != nil && node.entry.Flow.ID == flowID {
			c.lru.Remove(node.elem)
			delete(c.cache, key)
			return
		}
	}
}

// FlushDirty collects all dirty entries and marks them clean.
// Returns flows that need to be written to the database.
func (c *FlowCache) FlushDirty() []*flowdb.Flow {
	c.mu.Lock()
	defer c.mu.Unlock()

	var dirty []*flowdb.Flow
	for _, node := range c.cache {
		if node.entry.Dirty && node.entry.Flow != nil {
			dirty = append(dirty, node.entry.Flow)
			node.entry.Dirty = false
		}
	}
	return dirty
}

// Stats returns cache statistics.
func (c *FlowCache) Stats() (hits, misses uint64, size int) {
	c.mu.RLock()
	size = len(c.cache)
	c.mu.RUnlock()

	hits = atomic.LoadUint64(&c.hits)
	misses = atomic.LoadUint64(&c.misses)
	return
}

// Clear removes all entries from the cache.
func (c *FlowCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*cacheNode)
	c.lru.Init()
}

// Size returns the current number of entries in the cache.
func (c *FlowCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}
