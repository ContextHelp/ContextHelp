// Package remind provides time-expression parsing for the reminder feature.
package remind

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseTime parses a human-readable time expression relative to now.
//
// Supported forms (case-insensitive):
//   - "tomorrow 9am", "tomorrow 14:30"
//   - "in 2h", "in 30m", "in 1h30m"
//   - "2026-04-01", "2026-04-01 10:00"
//   - "today 9am", "today 14:00"
//   - "monday 9am" (next occurrence of weekday)
func ParseTime(expr string, now time.Time) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	lower := strings.ToLower(expr)

	// "in <duration>"
	if strings.HasPrefix(lower, "in ") {
		d, err := parseDuration(strings.TrimPrefix(lower, "in "))
		if err != nil {
			return time.Time{}, fmt.Errorf("parse duration %q: %w", expr, err)
		}
		return now.Add(d), nil
	}

	// "today <time>" / "tomorrow <time>"
	for _, kw := range []string{"today", "tomorrow"} {
		if strings.HasPrefix(lower, kw) {
			base := now
			if kw == "tomorrow" {
				base = base.AddDate(0, 0, 1)
			}
			suffix := strings.TrimSpace(strings.TrimPrefix(lower, kw))
			if suffix == "" {
				// No time part → start of day
				return midnight(base), nil
			}
			t, err := parseTimeOfDay(suffix, base)
			if err != nil {
				return time.Time{}, fmt.Errorf("parse %q: %w", expr, err)
			}
			return t, nil
		}
	}

	// Weekday "monday 9am"
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		name := strings.ToLower(wd.String())
		if strings.HasPrefix(lower, name) {
			base := nextWeekday(now, wd)
			suffix := strings.TrimSpace(strings.TrimPrefix(lower, name))
			if suffix == "" {
				return midnight(base), nil
			}
			t, err := parseTimeOfDay(suffix, base)
			if err != nil {
				return time.Time{}, fmt.Errorf("parse %q: %w", expr, err)
			}
			return t, nil
		}
	}

	// ISO date/datetime: "2026-04-01" / "2026-04-01 10:00" / "2026-04-01T10:00"
	for _, layout := range []string{
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, expr, now.Location()); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognised time expression %q; try: \"in 2h\", \"tomorrow 9am\", \"2026-04-01 10:00\"", expr)
}

// parseDuration extends time.ParseDuration to support mixed "1h30m"-style strings
// and plain integers (interpreted as minutes).
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	// Try plain integer → minutes
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Minute, nil
	}
	return 0, fmt.Errorf("cannot parse duration %q", s)
}

// parseTimeOfDay parses "9am", "9:30am", "14:00", "14:30" relative to a base date.
func parseTimeOfDay(s string, base time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	y, m, d := base.Date()

	// Try 12-hour forms first.
	for _, layout := range []string{"3:04pm", "3pm", "3:04AM", "3AM"} {
		if t, err := time.ParseInLocation(layout, strings.ToLower(s), base.Location()); err == nil {
			return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, base.Location()), nil
		}
	}
	// 24-hour forms.
	for _, layout := range []string{"15:04", "15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, base.Location()); err == nil {
			return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, base.Location()), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time-of-day %q", s)
}

func midnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func nextWeekday(from time.Time, target time.Weekday) time.Time {
	days := int(target - from.Weekday())
	if days <= 0 {
		days += 7
	}
	return from.AddDate(0, 0, days)
}
