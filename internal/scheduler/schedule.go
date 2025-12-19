package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// IntervalSchedule runs a task at a fixed interval.
type IntervalSchedule struct {
	Interval time.Duration
}

// Every creates an interval schedule.
func Every(d time.Duration) *IntervalSchedule {
	return &IntervalSchedule{Interval: d}
}

// Next returns the next run time.
func (s *IntervalSchedule) Next(after time.Time) time.Time {
	return after.Add(s.Interval)
}

// DailySchedule runs a task at a specific time each day.
type DailySchedule struct {
	Hour   int
	Minute int
}

// Daily creates a daily schedule at the specified time.
func Daily(hour, minute int) *DailySchedule {
	return &DailySchedule{Hour: hour, Minute: minute}
}

// Next returns the next run time.
func (s *DailySchedule) Next(after time.Time) time.Time {
	next := time.Date(after.Year(), after.Month(), after.Day(), s.Hour, s.Minute, 0, 0, after.Location())
	if !next.After(after) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// WeeklySchedule runs a task on specific days of the week.
type WeeklySchedule struct {
	Days   []time.Weekday // Days to run (0=Sunday, 6=Saturday)
	Hour   int
	Minute int
}

// Weekly creates a weekly schedule.
func Weekly(days []time.Weekday, hour, minute int) *WeeklySchedule {
	return &WeeklySchedule{Days: days, Hour: hour, Minute: minute}
}

// Next returns the next run time.
func (s *WeeklySchedule) Next(after time.Time) time.Time {
	if len(s.Days) == 0 {
		return time.Time{} // Never runs
	}

	// Start from today at the scheduled time
	next := time.Date(after.Year(), after.Month(), after.Day(), s.Hour, s.Minute, 0, 0, after.Location())

	// If today's time has passed, start from tomorrow
	if !next.After(after) {
		next = next.AddDate(0, 0, 1)
	}

	// Find the next matching day
	for i := 0; i < 8; i++ {
		for _, day := range s.Days {
			if next.Weekday() == day {
				return next
			}
		}
		next = next.AddDate(0, 0, 1)
	}

	return next
}

// CronSchedule implements cron-like scheduling.
// Supports: minute hour day-of-month month day-of-week
// Supports: * (any), */n (every n), n-m (range), n,m,o (list)
type CronSchedule struct {
	Minutes     []int // 0-59
	Hours       []int // 0-23
	DaysOfMonth []int // 1-31
	Months      []int // 1-12
	DaysOfWeek  []int // 0-6 (0=Sunday)
}

// Cron parses a cron expression and creates a schedule.
// Format: "minute hour day-of-month month day-of-week"
// Examples:
//   - "0 * * * *" - Every hour
//   - "*/15 * * * *" - Every 15 minutes
//   - "0 2 * * *" - Daily at 2:00 AM
//   - "0 0 * * 0" - Weekly on Sunday at midnight
//   - "0 0 1 * *" - Monthly on the 1st at midnight
func Cron(expr string) (*CronSchedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("invalid cron expression: expected 5 fields, got %d", len(parts))
	}

	minutes, err := parseCronField(parts[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("invalid minute field: %w", err)
	}

	hours, err := parseCronField(parts[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("invalid hour field: %w", err)
	}

	daysOfMonth, err := parseCronField(parts[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("invalid day-of-month field: %w", err)
	}

	months, err := parseCronField(parts[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("invalid month field: %w", err)
	}

	daysOfWeek, err := parseCronField(parts[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("invalid day-of-week field: %w", err)
	}

	return &CronSchedule{
		Minutes:     minutes,
		Hours:       hours,
		DaysOfMonth: daysOfMonth,
		Months:      months,
		DaysOfWeek:  daysOfWeek,
	}, nil
}

// MustCron parses a cron expression and panics on error.
func MustCron(expr string) *CronSchedule {
	s, err := Cron(expr)
	if err != nil {
		panic(err)
	}
	return s
}

// Next returns the next run time.
func (s *CronSchedule) Next(after time.Time) time.Time {
	// Start from the next minute
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Search for up to 4 years
	maxTime := after.AddDate(4, 0, 0)

	for t.Before(maxTime) {
		// Check month
		if !contains(s.Months, int(t.Month())) {
			// Move to next month
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}

		// Check day of month and day of week
		// In cron, if both are specified (not *), either can match
		domMatch := contains(s.DaysOfMonth, t.Day())
		dowMatch := contains(s.DaysOfWeek, int(t.Weekday()))

		// If both fields are restricted, either can match
		// If only one is restricted, that one must match
		dayMatch := false
		if len(s.DaysOfMonth) == 31 && len(s.DaysOfWeek) == 7 {
			dayMatch = true // Both are "*"
		} else if len(s.DaysOfMonth) == 31 {
			dayMatch = dowMatch // Only DOW is restricted
		} else if len(s.DaysOfWeek) == 7 {
			dayMatch = domMatch // Only DOM is restricted
		} else {
			dayMatch = domMatch || dowMatch // Both restricted, either matches
		}

		if !dayMatch {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}

		// Check hour
		if !contains(s.Hours, t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}

		// Check minute
		if !contains(s.Minutes, t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}

		return t
	}

	return time.Time{} // No match found
}

// parseCronField parses a single cron field.
func parseCronField(field string, min, max int) ([]int, error) {
	var values []int

	// Handle comma-separated values
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)

		// Handle step values (*/n or n-m/s)
		step := 1
		if idx := strings.Index(part, "/"); idx != -1 {
			var err error
			step, err = strconv.Atoi(part[idx+1:])
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("invalid step: %s", part)
			}
			part = part[:idx]
		}

		// Handle wildcard
		if part == "*" {
			for i := min; i <= max; i += step {
				values = append(values, i)
			}
			continue
		}

		// Handle range (n-m)
		if idx := strings.Index(part, "-"); idx != -1 {
			start, err := strconv.Atoi(part[:idx])
			if err != nil {
				return nil, fmt.Errorf("invalid range start: %s", part)
			}
			end, err := strconv.Atoi(part[idx+1:])
			if err != nil {
				return nil, fmt.Errorf("invalid range end: %s", part)
			}
			if start < min || end > max || start > end {
				return nil, fmt.Errorf("invalid range: %s", part)
			}
			for i := start; i <= end; i += step {
				values = append(values, i)
			}
			continue
		}

		// Handle single value
		val, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid value: %s", part)
		}
		if val < min || val > max {
			return nil, fmt.Errorf("value out of range: %d", val)
		}
		values = append(values, val)
	}

	return values, nil
}

// contains checks if a slice contains a value.
func contains(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

// TimeRangeSchedule enables a task only during specific time ranges.
// Useful for scheduled firewall rules.
type TimeRangeSchedule struct {
	Base      Schedule // The underlying schedule
	StartHour int
	StartMin  int
	EndHour   int
	EndMin    int
	Days      []time.Weekday // Empty means all days
}

// DuringHours wraps a schedule to only run during specific hours.
func DuringHours(base Schedule, startHour, startMin, endHour, endMin int) *TimeRangeSchedule {
	return &TimeRangeSchedule{
		Base:      base,
		StartHour: startHour,
		StartMin:  startMin,
		EndHour:   endHour,
		EndMin:    endMin,
	}
}

// Next returns the next run time within the allowed time range.
func (s *TimeRangeSchedule) Next(after time.Time) time.Time {
	next := s.Base.Next(after)
	if next.IsZero() {
		return next
	}

	// Check if next is within the time range
	for i := 0; i < 366; i++ { // Search up to a year
		if s.isInRange(next) {
			return next
		}
		next = s.Base.Next(next)
		if next.IsZero() {
			return next
		}
	}

	return time.Time{}
}

// isInRange checks if a time is within the allowed range.
func (s *TimeRangeSchedule) isInRange(t time.Time) bool {
	// Check day of week
	if len(s.Days) > 0 {
		found := false
		for _, d := range s.Days {
			if t.Weekday() == d {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check time of day
	startMins := s.StartHour*60 + s.StartMin
	endMins := s.EndHour*60 + s.EndMin
	currentMins := t.Hour()*60 + t.Minute()

	if startMins <= endMins {
		// Normal range (e.g., 9:00 - 17:00)
		return currentMins >= startMins && currentMins <= endMins
	}
	// Overnight range (e.g., 22:00 - 6:00)
	return currentMins >= startMins || currentMins <= endMins
}
