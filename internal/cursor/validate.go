package cursor

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MaxNameLen is the max characters for a cursor name (AC: ≤64).
const MaxNameLen = 64

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateName enforces lowercase alphanumeric + hyphens, ≤64 chars.
// Empty / leading-hyphen / uppercase / underscore / non-ASCII rejected.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("cursor name: empty")
	}
	if len(name) > MaxNameLen {
		return fmt.Errorf("cursor name %q: too long (max %d chars)", name, MaxNameLen)
	}
	if !nameRE.MatchString(name) {
		return fmt.Errorf("cursor name %q: must be lowercase alphanumeric with hyphens", name)
	}
	return nil
}

// ParseTimestamp accepts RFC3339 ("2026-04-28T14:22:11Z") or relative
// duration with sign ("-7d", "+12h"). Returns UTC.
//
// Relative units: s, m, h, d, w. Sign required ("-" / "+"). Negative =
// past, positive = future.
func ParseTimestamp(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("timestamp: empty")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	// Date-only fallback.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	if s[0] == '-' || s[0] == '+' {
		d, err := parseRelative(s)
		if err != nil {
			return time.Time{}, err
		}
		return time.Now().UTC().Add(d), nil
	}
	return time.Time{}, fmt.Errorf("timestamp %q: must be RFC3339 or signed duration (e.g. -7d)", s)
}

func parseRelative(s string) (time.Duration, error) {
	if len(s) < 3 {
		return 0, fmt.Errorf("duration %q: too short", s)
	}
	sign := s[0]
	body := s[1:]
	unit := body[len(body)-1]
	numStr := body[:len(body)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("duration %q: bad number: %w", s, err)
	}
	var mult time.Duration
	switch unit {
	case 's':
		mult = time.Second
	case 'm':
		mult = time.Minute
	case 'h':
		mult = time.Hour
	case 'd':
		mult = 24 * time.Hour
	case 'w':
		mult = 7 * 24 * time.Hour
	default:
		return 0, fmt.Errorf("duration %q: unknown unit %q (use s|m|h|d|w)", s, unit)
	}
	d := time.Duration(n) * mult
	if sign == '-' {
		d = -d
	}
	return d, nil
}
