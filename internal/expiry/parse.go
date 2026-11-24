// Package expiry parses capture/watch expiry timestamps and TTL durations,
// and resolves optional natural-language expiry/redaction scopes.
package expiry

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/remind"
)

// ParseTTL parses a positive duration string supporting standard Go units
// (ns, us, ms, s, m, h) plus day ("d" = 24h) and week ("w" = 7*24h) suffixes,
// e.g. "30m", "24h", "7d", "2w", "1d12h".
func ParseTTL(raw string) (time.Duration, error) {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return 0, fmt.Errorf("empty ttl")
	}
	var total time.Duration
	for s != "" {
		i := 0
		for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
			i++
		}
		if i == 0 {
			return 0, fmt.Errorf("invalid ttl %q", raw)
		}
		numStr := s[:i]
		s = s[i:]
		j := 0
		for j < len(s) && (s[j] < '0' || s[j] > '9') {
			j++
		}
		unit := s[:j]
		s = s[j:]
		switch unit {
		case "w":
			v, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid ttl %q: %w", raw, err)
			}
			total += time.Duration(v * float64(7*24*time.Hour))
		case "d":
			v, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid ttl %q: %w", raw, err)
			}
			total += time.Duration(v * float64(24*time.Hour))
		default:
			d, err := time.ParseDuration(numStr + unit)
			if err != nil {
				return 0, fmt.Errorf("invalid ttl %q: %w", raw, err)
			}
			total += d
		}
	}
	if total <= 0 {
		return 0, fmt.Errorf("ttl %q must be positive", raw)
	}
	return total, nil
}

// ParseExpiry parses an expiry expression relative to now and returns UTC.
// Supported forms:
//   - RFC3339 timestamps (e.g. "2026-12-31T23:59:59Z")
//   - Relative TTL durations (e.g. "24h", "7d", "2w", "in 7d")
//   - Natural date/time expressions via remind.ParseTime ("tomorrow 9am", "2026-12-31")
func ParseExpiry(expr string, now time.Time) (time.Time, error) {
	s := strings.TrimSpace(expr)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty expiry expression")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "in ") {
		if d, err := ParseTTL(strings.TrimPrefix(lower, "in ")); err == nil {
			return now.UTC().Add(d).Truncate(time.Second), nil
		}
	}
	if d, err := ParseTTL(s); err == nil {
		return now.UTC().Add(d).Truncate(time.Second), nil
	}
	t, err := remind.ParseTime(s, now)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse expiry %q: %w", expr, err)
	}
	return t.UTC(), nil
}
