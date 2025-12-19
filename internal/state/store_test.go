package state

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestNewSQLiteStore tests store creation
func TestNewSQLiteStore(t *testing.T) {
	store, err := NewSQLiteStore(DefaultOptions(":memory:"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if store.CurrentVersion() != 0 {
		t.Errorf("expected version 0, got %d", store.CurrentVersion())
	}
}

// TestNewSQLiteStore_FileBackend tests store with file backend
func TestNewSQLiteStore_FileBackend(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	defer os.Remove(tmpFile)

	store, err := NewSQLiteStore(DefaultOptions(tmpFile))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	store.Close()

	// Reopen and verify
	store2, err := NewSQLiteStore(DefaultOptions(tmpFile))
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer store2.Close()
}

// TestBucketOperations tests bucket CRUD
func TestBucketOperations(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	// Create bucket
	if err := store.CreateBucket("test"); err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	// Create duplicate should fail
	if err := store.CreateBucket("test"); err != ErrBucketExists {
		t.Errorf("expected ErrBucketExists, got %v", err)
	}

	// List buckets
	buckets, err := store.ListBuckets()
	if err != nil {
		t.Fatalf("failed to list buckets: %v", err)
	}
	if len(buckets) != 1 || buckets[0] != "test" {
		t.Errorf("expected [test], got %v", buckets)
	}

	// Delete bucket
	if err := store.DeleteBucket("test"); err != nil {
		t.Fatalf("failed to delete bucket: %v", err)
	}

	// Delete nonexistent should fail
	if err := store.DeleteBucket("nonexistent"); err != ErrBucketMissing {
		t.Errorf("expected ErrBucketMissing, got %v", err)
	}

	// List should be empty
	buckets, _ = store.ListBuckets()
	if len(buckets) != 0 {
		t.Errorf("expected empty buckets, got %v", buckets)
	}
}

// TestKeyValueOperations tests Get/Set/Delete
func TestKeyValueOperations(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	store.CreateBucket("kv")

	// Set value
	if err := store.Set("kv", "key1", []byte("value1")); err != nil {
		t.Fatalf("failed to set: %v", err)
	}

	// Get value
	val, err := store.Get("kv", "key1")
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("expected value1, got %s", val)
	}

	// Get nonexistent
	_, err = store.Get("kv", "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// Update value
	if err := store.Set("kv", "key1", []byte("updated")); err != nil {
		t.Fatalf("failed to update: %v", err)
	}
	val, _ = store.Get("kv", "key1")
	if string(val) != "updated" {
		t.Errorf("expected updated, got %s", val)
	}

	// Delete value
	if err := store.Delete("kv", "key1"); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// Verify deleted
	_, err = store.Get("kv", "key1")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}

	// Delete nonexistent
	if err := store.Delete("kv", "nonexistent"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestGetWithMeta tests metadata retrieval
func TestGetWithMeta(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	store.CreateBucket("meta")
	store.Set("meta", "key1", []byte("value1"))

	entry, err := store.GetWithMeta("meta", "key1")
	if err != nil {
		t.Fatalf("failed to get with meta: %v", err)
	}

	if string(entry.Value) != "value1" {
		t.Errorf("wrong value: %s", entry.Value)
	}
	if entry.Version != 1 {
		t.Errorf("expected version 1, got %d", entry.Version)
	}
	if entry.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

// TestSetWithTTL tests TTL functionality
func TestSetWithTTL(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	store.CreateBucket("ttl")

	// Set with short TTL
	if err := store.SetWithTTL("ttl", "expires", []byte("soon"), 100*time.Millisecond); err != nil {
		t.Fatalf("failed to set with TTL: %v", err)
	}

	// Should exist immediately
	val, err := store.Get("ttl", "expires")
	if err != nil {
		t.Fatalf("should exist: %v", err)
	}
	if string(val) != "soon" {
		t.Errorf("wrong value: %s", val)
	}

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	// Should not exist after expiry
	_, err = store.Get("ttl", "expires")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after TTL, got %v", err)
	}
}

// TestListOperations tests List and ListKeys
func TestListOperations(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	store.CreateBucket("list")
	store.Set("list", "a", []byte("1"))
	store.Set("list", "b", []byte("2"))
	store.Set("list", "c", []byte("3"))

	// List all
	all, err := store.List("list")
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 entries, got %d", len(all))
	}

	// ListKeys
	keys, err := store.ListKeys("list")
	if err != nil {
		t.Fatalf("failed to list keys: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
	// Should be sorted
	if keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Errorf("keys not sorted: %v", keys)
	}
}

// TestJSONOperations tests GetJSON/SetJSON
func TestJSONOperations(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	store.CreateBucket("json")

	type TestData struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	// Set JSON
	data := TestData{Name: "test", Count: 42}
	if err := store.SetJSON("json", "obj", data); err != nil {
		t.Fatalf("failed to set JSON: %v", err)
	}

	// Get JSON
	var result TestData
	if err := store.GetJSON("json", "obj", &result); err != nil {
		t.Fatalf("failed to get JSON: %v", err)
	}

	if result.Name != "test" || result.Count != 42 {
		t.Errorf("wrong JSON data: %+v", result)
	}
}

// TestChangeTracking tests change log functionality
func TestChangeTracking(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	store.CreateBucket("changes")

	// Initial version
	if store.CurrentVersion() != 0 {
		t.Errorf("expected version 0, got %d", store.CurrentVersion())
	}

	// Make changes
	store.Set("changes", "k1", []byte("v1"))
	store.Set("changes", "k2", []byte("v2"))
	store.Set("changes", "k1", []byte("v1-updated"))
	store.Delete("changes", "k2")

	// Check version
	if store.CurrentVersion() != 4 {
		t.Errorf("expected version 4, got %d", store.CurrentVersion())
	}

	// Get changes since version 0
	changes, err := store.GetChangesSince(0)
	if err != nil {
		t.Fatalf("failed to get changes: %v", err)
	}
	if len(changes) != 4 {
		t.Errorf("expected 4 changes, got %d", len(changes))
	}

	// Verify change types
	if changes[0].Type != ChangeInsert {
		t.Errorf("expected insert, got %s", changes[0].Type)
	}
	if changes[2].Type != ChangeUpdate {
		t.Errorf("expected update, got %s", changes[2].Type)
	}
	if changes[3].Type != ChangeDelete {
		t.Errorf("expected delete, got %s", changes[3].Type)
	}

	// Get changes since version 2
	changes, _ = store.GetChangesSince(2)
	if len(changes) != 2 {
		t.Errorf("expected 2 changes, got %d", len(changes))
	}
}

// TestSubscribe tests the subscription mechanism
func TestSubscribe(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	store.CreateBucket("sub")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := store.Subscribe(ctx)

	// Make a change
	store.Set("sub", "key", []byte("value"))

	// Should receive it
	select {
	case change := <-ch:
		if change.Key != "key" || change.Type != ChangeInsert {
			t.Errorf("unexpected change: %+v", change)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("did not receive change")
	}
}

// TestSnapshot tests snapshot creation and restoration
func TestSnapshot(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	// Create data
	store.CreateBucket("snap1")
	store.CreateBucket("snap2")
	store.Set("snap1", "a", []byte("1"))
	store.Set("snap1", "b", []byte("2"))
	store.Set("snap2", "x", []byte("X"))

	// Create snapshot
	snapshot, err := store.CreateSnapshot()
	if err != nil {
		t.Fatalf("failed to create snapshot: %v", err)
	}

	if len(snapshot.Buckets) != 2 {
		t.Errorf("expected 2 buckets in snapshot, got %d", len(snapshot.Buckets))
	}
	if len(snapshot.Buckets["snap1"]) != 2 {
		t.Errorf("expected 2 entries in snap1, got %d", len(snapshot.Buckets["snap1"]))
	}

	// Modify data
	store.Set("snap1", "c", []byte("3"))
	store.Delete("snap1", "a")

	// Restore snapshot
	if err := store.RestoreSnapshot(snapshot); err != nil {
		t.Fatalf("failed to restore snapshot: %v", err)
	}

	// Verify restoration
	val, err := store.Get("snap1", "a")
	if err != nil {
		t.Errorf("should have restored 'a': %v", err)
	}
	if string(val) != "1" {
		t.Errorf("wrong value for 'a': %s", val)
	}

	_, err = store.Get("snap1", "c")
	if err != ErrNotFound {
		t.Errorf("'c' should not exist after restore")
	}
}

// TestClosedStore tests operations on closed store
func TestClosedStore(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	store.Close()

	if err := store.CreateBucket("test"); err != ErrStoreClosed {
		t.Errorf("expected ErrStoreClosed, got %v", err)
	}
	if _, err := store.Get("test", "key"); err != ErrStoreClosed {
		t.Errorf("expected ErrStoreClosed, got %v", err)
	}
	if err := store.Set("test", "key", []byte("value")); err != ErrStoreClosed {
		t.Errorf("expected ErrStoreClosed, got %v", err)
	}
}

// TestConcurrency tests concurrent access
func TestConcurrency(t *testing.T) {
	store, _ := NewSQLiteStore(DefaultOptions(":memory:"))
	defer store.Close()

	store.CreateBucket("concurrent")

	// Concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			key := fmt.Sprintf("key-%d", id)
			if err := store.Set("concurrent", key, []byte("val")); err != nil {
				t.Errorf("concurrent set failed: %v", err)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Read validation
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		if _, err := store.Get("concurrent", key); err != nil {
			t.Errorf("missing key %s: %v", key, err)
		}
	}
}

// --- SQLite Time Function Override Tests ---

// TestSQLiteDatetimeUsesClockNow tests that datetime('now') uses clock.Now()
func TestSQLiteDatetimeUsesClockNow(t *testing.T) {
	store, err := NewSQLiteStore(DefaultOptions(":memory:"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Query datetime('now')
	var result string
	err = store.db.QueryRow("SELECT datetime('now')").Scan(&result)
	if err != nil {
		t.Fatalf("failed to query datetime: %v", err)
	}

	// Parse the result
	parsed, err := time.Parse("2006-01-02 15:04:05", result)
	if err != nil {
		t.Fatalf("failed to parse datetime result %q: %v", result, err)
	}

	// Should be close to current time (within 2 seconds)
	diff := time.Since(parsed).Abs()
	if diff > 2*time.Second {
		t.Errorf("datetime('now') returned %v, expected close to now (diff: %v)", parsed, diff)
	}
}

// TestSQLiteDateUsesClockNow tests that date('now') uses clock.Now()
func TestSQLiteDateUsesClockNow(t *testing.T) {
	store, err := NewSQLiteStore(DefaultOptions(":memory:"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Query date('now')
	var result string
	err = store.db.QueryRow("SELECT date('now')").Scan(&result)
	if err != nil {
		t.Fatalf("failed to query date: %v", err)
	}

	// Should be today's date
	expected := time.Now().UTC().Format("2006-01-02")
	if result != expected {
		t.Errorf("date('now') returned %q, expected %q", result, expected)
	}
}

// TestSQLiteTimeUsesClockNow tests that time('now') uses clock.Now()
func TestSQLiteTimeUsesClockNow(t *testing.T) {
	store, err := NewSQLiteStore(DefaultOptions(":memory:"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Query time('now')
	var result string
	err = store.db.QueryRow("SELECT time('now')").Scan(&result)
	if err != nil {
		t.Fatalf("failed to query time: %v", err)
	}

	// Should be a valid time format
	_, err = time.Parse("15:04:05", result)
	if err != nil {
		t.Fatalf("time('now') returned invalid time %q: %v", result, err)
	}
}

// TestSQLiteJuliandayUsesClockNow tests that julianday('now') uses clock.Now()
func TestSQLiteJuliandayUsesClockNow(t *testing.T) {
	store, err := NewSQLiteStore(DefaultOptions(":memory:"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Query julianday('now')
	var result float64
	err = store.db.QueryRow("SELECT julianday('now')").Scan(&result)
	if err != nil {
		t.Fatalf("failed to query julianday: %v", err)
	}

	// Julian day for 2024+ should be > 2460000
	if result < 2460000 {
		t.Errorf("julianday('now') returned %v, expected > 2460000", result)
	}
}

// TestSQLiteStrftimeUsesClockNow tests that strftime('%Y', 'now') uses clock.Now()
func TestSQLiteStrftimeUsesClockNow(t *testing.T) {
	store, err := NewSQLiteStore(DefaultOptions(":memory:"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Query strftime('%Y', 'now')
	var result string
	err = store.db.QueryRow("SELECT strftime('%Y', 'now')").Scan(&result)
	if err != nil {
		t.Fatalf("failed to query strftime: %v", err)
	}

	// Should be current year
	expected := time.Now().UTC().Format("2006")
	if result != expected {
		t.Errorf("strftime returned %q, expected %q", result, expected)
	}
}

// Note: Anchor-based tests were removed because clock anchoring is now handled
// in the ctl layer via file-based persistence, not in the clock package itself.

// TestOnWriteHookCalled tests that OnWrite hook is called after state writes
func TestOnWriteHookCalled(t *testing.T) {
	store, err := NewSQLiteStore(DefaultOptions(":memory:"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Track hook calls
	var hookCalls int
	store.OnWrite = func() {
		hookCalls++
	}

	store.CreateBucket("test")
	store.Set("test", "key1", []byte("value1"))
	store.Set("test", "key2", []byte("value2"))

	// Should have been called for each Set
	if hookCalls != 2 {
		t.Errorf("expected OnWrite called 2 times, got %d", hookCalls)
	}
}
