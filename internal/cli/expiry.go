package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseExpiry reads an --expires value. Empty never expires; a duration
// (720h, or whole days as 7d) counts from now; a date (2026-12-31)
// expires at the end of that day, UTC; an RFC 3339 time is taken as
// given. A duration must be positive.
func ParseExpiry(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if d, ok := parseDays(s); ok {
		return now.Add(d), nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d <= 0 {
			return time.Time{}, fmt.Errorf("--expires %q: the duration must be positive", s)
		}
		return now.Add(d), nil
	}
	if day, err := time.Parse(time.DateOnly, s); err == nil {
		return day.AddDate(0, 0, 1), nil
	}
	if at, err := time.Parse(time.RFC3339, s); err == nil {
		return at.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("--expires %q: give a duration (720h, 7d), a date (2026-12-31) or an RFC 3339 time", s)
}

// parseDays reads a whole number of days with a d suffix (7d).
func parseDays(s string) (time.Duration, bool) {
	n, ok := strings.CutSuffix(s, "d")
	if !ok {
		return 0, false
	}
	days, err := strconv.Atoi(n)
	if err != nil || days <= 0 {
		return 0, false
	}
	return time.Duration(days) * 24 * time.Hour, true
}
