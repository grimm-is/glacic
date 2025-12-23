package stats

import (
	"testing"
	"time"
)

func TestRingBuffer_Add(t *testing.T) {
	buf := NewRingBuffer(5)

	// Add values
	for i := 0; i < 5; i++ {
		buf.Add(float64(i))
	}

	snapshot := buf.Snapshot()
	if len(snapshot) != 5 {
		t.Errorf("Expected 5 items, got %d", len(snapshot))
	}

	// Should be [0, 1, 2, 3, 4]
	for i, v := range snapshot {
		if v != float64(i) {
			t.Errorf("Expected %f at index %d, got %f", float64(i), i, v)
		}
	}
}

func TestRingBuffer_Wrap(t *testing.T) {
	buf := NewRingBuffer(3)

	// Add 5 values to a buffer of size 3
	for i := 0; i < 5; i++ {
		buf.Add(float64(i))
	}

	snapshot := buf.Snapshot()
	if len(snapshot) != 3 {
		t.Errorf("Expected 3 items, got %d", len(snapshot))
	}

	// Should be [2, 3, 4] (oldest to newest after wrap)
	expected := []float64{2, 3, 4}
	for i, v := range snapshot {
		if v != expected[i] {
			t.Errorf("Expected %f at index %d, got %f", expected[i], i, v)
		}
	}
}

func TestRingBuffer_Len(t *testing.T) {
	buf := NewRingBuffer(5)

	if buf.Len() != 0 {
		t.Errorf("Expected length 0, got %d", buf.Len())
	}

	buf.Add(1.0)
	buf.Add(2.0)
	if buf.Len() != 2 {
		t.Errorf("Expected length 2, got %d", buf.Len())
	}

	// Fill and overflow
	for i := 0; i < 10; i++ {
		buf.Add(float64(i))
	}
	if buf.Len() != 5 {
		t.Errorf("Expected length 5 (capacity), got %d", buf.Len())
	}
}

// MockFetcher implements CounterFetcher for testing.
type MockFetcher struct {
	Counters map[string]uint64
	Err      error
}

func (m *MockFetcher) FetchCounters() (map[string]uint64, error) {
	return m.Counters, m.Err
}

func TestCollector_Tick(t *testing.T) {
	mock := &MockFetcher{
		Counters: map[string]uint64{
			"rule1": 1000,
			"rule2": 2000,
		},
	}

	c := NewCollector(100*time.Millisecond, WithFetcher(mock), WithCapacity(10))

	// First tick - initializes but no rate yet
	c.tick()

	// Second tick - should have rate
	mock.Counters = map[string]uint64{
		"rule1": 1200, // +200 bytes
		"rule2": 2500, // +500 bytes
	}
	c.tick()

	// Check sparkline for rule1
	sparkline := c.GetSparkline("rule1")
	if len(sparkline) != 1 {
		t.Errorf("Expected 1 data point, got %d", len(sparkline))
	}

	// Rate should be 200 / 0.1s = 2000 bytes/sec
	expectedRate := 200.0 / 0.1
	if sparkline[0] != expectedRate {
		t.Errorf("Expected rate %f, got %f", expectedRate, sparkline[0])
	}
}

func TestCollector_GetAllStats(t *testing.T) {
	mock := &MockFetcher{
		Counters: map[string]uint64{
			"rule1": 100,
			"rule2": 200,
		},
	}

	c := NewCollector(100*time.Millisecond, WithFetcher(mock), WithCapacity(5))
	c.tick()

	// Update counters
	mock.Counters["rule1"] = 150
	mock.Counters["rule2"] = 300
	c.tick()

	stats := c.GetAllStats()
	if len(stats) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(stats))
	}
}

func TestCollector_Reset(t *testing.T) {
	mock := &MockFetcher{
		Counters: map[string]uint64{"rule1": 100},
	}

	c := NewCollector(100*time.Millisecond, WithFetcher(mock))
	c.tick()

	c.Reset()

	stats := c.GetAllStats()
	if len(stats) != 0 {
		t.Errorf("Expected 0 rules after reset, got %d", len(stats))
	}
}

func TestCollector_CounterReset(t *testing.T) {
	mock := &MockFetcher{
		Counters: map[string]uint64{"rule1": 1000},
	}

	c := NewCollector(100*time.Millisecond, WithFetcher(mock), WithCapacity(5))
	c.tick()

	// Simulate counter reset (nftables reload)
	mock.Counters["rule1"] = 50 // Less than before
	c.tick()

	sparkline := c.GetSparkline("rule1")
	if len(sparkline) != 1 {
		t.Errorf("Expected 1 data point, got %d", len(sparkline))
	}

	// Should assume delta is 50 (treat as reset from 0)
	expectedRate := 50.0 / 0.1
	if sparkline[0] != expectedRate {
		t.Errorf("Expected rate %f after reset, got %f", expectedRate, sparkline[0])
	}
}
