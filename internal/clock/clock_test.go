package clock

import (
	"testing"
	"time"
)

func TestNow_ReturnsCurrentTime(t *testing.T) {
	before := time.Now()
	result := Now()
	after := time.Now()

	if result.Before(before) || result.After(after) {
		t.Errorf("Now() returned %v, expected between %v and %v", result, before, after)
	}
}

func TestMockClock_Now(t *testing.T) {
	mockTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mock := NewMockClock(mockTime)

	result := mock.Now()

	if !result.Equal(mockTime) {
		t.Errorf("MockClock.Now() returned %v, expected exactly %v", result, mockTime)
	}
}

func TestMockClock_Advance(t *testing.T) {
	mockTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mock := NewMockClock(mockTime)

	first := mock.Now()
	mock.Advance(time.Hour)
	second := mock.Now()

	expected := mockTime.Add(time.Hour)
	if !second.Equal(expected) {
		t.Errorf("After Advance, Now() = %v, expected %v", second, expected)
	}
	if !first.Equal(mockTime) {
		t.Errorf("Before Advance, Now() = %v, expected %v", first, mockTime)
	}
}

func TestMockClock_Set(t *testing.T) {
	mock := NewMockClock(time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC))

	newTime := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	mock.Set(newTime)

	result := mock.Now()
	if !result.Equal(newTime) {
		t.Errorf("After Set, Now() = %v, expected %v", result, newTime)
	}
}

func TestMockClock_Since(t *testing.T) {
	mockTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mock := NewMockClock(mockTime)

	past := mockTime.Add(-time.Hour)
	result := mock.Since(past)

	expected := time.Hour
	if result != expected {
		t.Errorf("Since() = %v, expected %v", result, expected)
	}
}

func TestMockClock_Until(t *testing.T) {
	mockTime := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mock := NewMockClock(mockTime)

	future := mockTime.Add(time.Hour)
	result := mock.Until(future)

	expected := time.Hour
	if result != expected {
		t.Errorf("Until() = %v, expected %v", result, expected)
	}
}

func TestSince(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	result := Since(past)

	// Should be approximately 1 hour
	if result < time.Hour-time.Second || result > time.Hour+time.Second {
		t.Errorf("Since() = %v, expected approximately 1 hour", result)
	}
}

func TestUntil(t *testing.T) {
	future := time.Now().Add(time.Hour)
	result := Until(future)

	// Should be approximately 1 hour
	if result < time.Hour-time.Second || result > time.Hour+time.Second {
		t.Errorf("Until() = %v, expected approximately 1 hour", result)
	}
}

func TestIsReasonableTime(t *testing.T) {
	tests := []struct {
		name     string
		t        time.Time
		expected bool
	}{
		{"Epoch", time.Unix(0, 0), false},
		{"Year 2000", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{"Year 2020", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{"Year 2023", time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{"Year 2025", time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC), true},
		{"Year 2099", time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsReasonableTime(tc.t)
			if result != tc.expected {
				t.Errorf("IsReasonableTime(%v) = %v, expected %v", tc.t, result, tc.expected)
			}
		})
	}
}

func TestClockInterface(t *testing.T) {
	// Verify both implementations satisfy the Clock interface
	var _ Clock = &RealClock{}
	var _ Clock = &MockClock{}
}

func TestRealClock_Now(t *testing.T) {
	c := &RealClock{}

	before := time.Now()
	result := c.Now()
	after := time.Now()

	if result.Before(before) || result.After(after) {
		t.Errorf("RealClock.Now() = %v, expected between %v and %v", result, before, after)
	}
}

func TestRealClock_Since(t *testing.T) {
	c := &RealClock{}

	past := time.Now().Add(-time.Hour)
	result := c.Since(past)

	if result < time.Hour-time.Second || result > time.Hour+time.Second {
		t.Errorf("RealClock.Since() = %v, expected approximately 1 hour", result)
	}
}

func TestRealClock_Until(t *testing.T) {
	c := &RealClock{}

	future := time.Now().Add(time.Hour)
	result := c.Until(future)

	if result < time.Hour-time.Second || result > time.Hour+time.Second {
		t.Errorf("RealClock.Until() = %v, expected approximately 1 hour", result)
	}
}
