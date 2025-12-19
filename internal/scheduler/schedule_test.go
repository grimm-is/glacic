package scheduler

import (
	"testing"
	"time"
)

func TestIntervalSchedule(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	s := Every(1 * time.Hour)
	next := s.Next(now)
	if !next.Equal(now.Add(1 * time.Hour)) {
		t.Errorf("Expected %v, got %v", now.Add(1*time.Hour), next)
	}
}

func TestDailySchedule(t *testing.T) {
	now := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	// Case 1: Time is later today
	s1 := Daily(14, 30) // 14:30
	next1 := s1.Next(now)
	expected1 := time.Date(2025, 1, 1, 14, 30, 0, 0, time.UTC)
	if !next1.Equal(expected1) {
		t.Errorf("Case 1: Expected %v, got %v", expected1, next1)
	}

	// Case 2: Time has passed today, should be tomorrow
	s2 := Daily(8, 0) // 08:00
	next2 := s2.Next(now)
	expected2 := time.Date(2025, 1, 2, 8, 0, 0, 0, time.UTC)
	if !next2.Equal(expected2) {
		t.Errorf("Case 2: Expected %v, got %v", expected2, next2)
	}
}

func TestWeeklySchedule(t *testing.T) {
	// 2025-01-01 is a Wednesday
	now := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	// Case 1: Same day, later time
	s1 := Weekly([]time.Weekday{time.Wednesday}, 14, 0)
	next1 := s1.Next(now)
	expected1 := time.Date(2025, 1, 1, 14, 0, 0, 0, time.UTC)
	if !next1.Equal(expected1) {
		t.Errorf("Case 1: Expected %v, got %v", expected1, next1)
	}

	// Case 2: Same day, earlier time -> Next week (or next occurrence)
	// If only Wednesday is in list, next is next Wednesday
	s2 := Weekly([]time.Weekday{time.Wednesday}, 8, 0)
	next2 := s2.Next(now)
	expected2 := time.Date(2025, 1, 8, 8, 0, 0, 0, time.UTC)
	if !next2.Equal(expected2) {
		t.Errorf("Case 2: Expected %v, got %v", expected2, next2)
	}

	// Case 3: Different day (Friday)
	s3 := Weekly([]time.Weekday{time.Friday}, 10, 0)
	next3 := s3.Next(now)
	expected3 := time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC)
	if !next3.Equal(expected3) {
		t.Errorf("Case 3: Expected %v, got %v", expected3, next3)
	}

	// Case 4: Multiple days (Mon, Wed, Fri), currently Wed 10:00. Next is Fri 10:00?
	// Wait, if Wed 12:00 is in list?
	// Let's stick to days.
	s4 := Weekly([]time.Weekday{time.Monday, time.Friday}, 10, 0)
	next4 := s4.Next(now)
	expected4 := time.Date(2025, 1, 3, 10, 0, 0, 0, time.UTC) // Friday
	if !next4.Equal(expected4) {
		t.Errorf("Case 4: Expected %v, got %v", expected4, next4)
	}
}

func TestCronSchedule_Parsing(t *testing.T) {
	tests := []struct {
		expr    string
		wantErr bool
	}{
		{"* * * * *", false},
		{"*/5 * * * *", false},
		{"0 0 1 1 *", false},
		{"* * * *", true},        // too short
		{"* * * * * *", true},    // too long
		{"60 * * * *", true},     // invalid minute
		{"* 24 * * *", true},     // invalid hour
		{"a * * * *", true},      // invalid char
		{"1-5 * * * *", false},   // range
		{"1,2,3 * * * *", false}, // list
	}

	for _, tt := range tests {
		_, err := Cron(tt.expr)
		if (err != nil) != tt.wantErr {
			t.Errorf("Cron(%q) error = %v, wantErr %v", tt.expr, err, tt.wantErr)
		}
	}
}

func TestCronSchedule_Next(t *testing.T) {
	// 2025-01-01 10:00:00 (Wed)
	now := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		expr string
		want time.Time
	}{
		// Every minute (next minute)
		{"* * * * *", now.Add(1 * time.Minute)},
		// At minute 30
		{"30 * * * *", time.Date(2025, 1, 1, 10, 30, 0, 0, time.UTC)},
		// Next hour
		{"0 * * * *", time.Date(2025, 1, 1, 11, 0, 0, 0, time.UTC)},
		// At 2PM
		{"0 14 * * *", time.Date(2025, 1, 1, 14, 0, 0, 0, time.UTC)},
		// Tomorrow 8AM
		{"0 8 * * *", time.Date(2025, 1, 2, 8, 0, 0, 0, time.UTC)},
		// Specific date (Feb 1st)
		{"0 0 1 2 *", time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)},
		// Specific weekday (Friday)
		{"0 12 * * 5", time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		s, err := Cron(tt.expr)
		if err != nil {
			t.Errorf("Cron(%q) failed: %v", tt.expr, err)
			continue
		}
		got := s.Next(now)
		if !got.Equal(tt.want) {
			t.Errorf("Cron(%q).Next() = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestTimeRangeSchedule(t *testing.T) {
	base := Every(1 * time.Hour)
	// 9:00 to 17:00
	s := DuringHours(base, 9, 0, 17, 0)

	// Case 1: Within range (10:00 -> 11:00)
	now1 := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	next1 := s.Next(now1)
	expected1 := time.Date(2025, 1, 1, 11, 0, 0, 0, time.UTC)
	if !next1.Equal(expected1) {
		t.Errorf("Case 1: Expected %v, got %v", expected1, next1)
	}

	// Case 2: Before range (08:00 -> 09:00)
	// Base next is 09:00, which is start of range.
	now2 := time.Date(2025, 1, 1, 8, 0, 0, 0, time.UTC)
	next2 := s.Next(now2)
	expected2 := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)
	if !next2.Equal(expected2) {
		t.Errorf("Case 2: Expected %v, got %v", expected2, next2)
	}

	// Case 3: End of range (16:30 -> 17:30, which is out. Next allowed is tomorrow 9:00?)
	// Base next 17:30. Skipped.
	// Base next 18:30. Skipped.
	// ...
	// Base next 09:30 (if aligned?). Interval is 1h constant from 16:30.
	// 16:30 -> 17:30(x) -> 18:30(x) ... -> 08:30(x) -> 09:30(ok)
	now3 := time.Date(2025, 1, 1, 16, 30, 0, 0, time.UTC)
	next3 := s.Next(now3)
	expected3 := time.Date(2025, 1, 2, 9, 30, 0, 0, time.UTC)
	if !next3.Equal(expected3) {
		t.Errorf("Case 3: Expected %v, got %v", expected3, next3)
	}
}
