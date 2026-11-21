package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestResolveDate(t *testing.T) {
	loc := time.FixedZone("T", 2*3600)
	now := time.Date(2026, 11, 20, 15, 30, 0, 0, loc)
	at := func(y int, m time.Month, d, h, mi, s int) time.Time { return time.Date(y, m, d, h, mi, s, 0, loc) }

	cases := []struct {
		name  string
		in    string
		upper bool
		want  time.Time
	}{
		{"3d", "3d", false, at(2026, 11, 17, 15, 30, 0)},
		{"42h", "42h", false, at(2026, 11, 18, 21, 30, 0)},
		{"1w", "1w", false, at(2026, 11, 13, 15, 30, 0)},
		{"30m is minutes", "30m", false, at(2026, 11, 20, 15, 0, 0)},
		{"30min is minutes", "30min", false, at(2026, 11, 20, 15, 0, 0)},
		{"1M is a month", "1M", false, at(2026, 10, 20, 15, 30, 0)},
		{"2M is months", "2M", false, at(2026, 9, 20, 15, 30, 0)},
		{"1mo is a month", "1mo", false, at(2026, 10, 20, 15, 30, 0)},
		{"2mo is months", "2mo", false, at(2026, 9, 20, 15, 30, 0)},
		{"1y", "1y", false, at(2025, 11, 20, 15, 30, 0)},
		{"relative upper is literal", "3d", true, at(2026, 11, 17, 15, 30, 0)},
		{"today since", "today", false, at(2026, 11, 20, 0, 0, 0)},
		{"today until", "today", true, at(2026, 11, 21, 0, 0, 0)},
		{"yesterday since", "yesterday", false, at(2026, 11, 19, 0, 0, 0)},
		{"yesterday until", "yesterday", true, at(2026, 11, 20, 0, 0, 0)},
		{"date since", "2026-01-05", false, at(2026, 1, 5, 0, 0, 0)},
		{"date until is next day start", "2026-01-05", true, at(2026, 1, 6, 0, 0, 0)},
		{"local datetime", "2026-01-05T10:00:00", false, at(2026, 1, 5, 10, 0, 0)},
		{"local datetime upper literal", "2026-01-05T10:00:00", true, at(2026, 1, 5, 10, 0, 0)},
		{"rfc3339 since", "2026-01-05T10:00:00Z", false, time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)},
		{"rfc3339 until literal", "2026-01-05T10:00:00+02:00", true, time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC)},
		{"padded", "  3d ", false, at(2026, 11, 17, 15, 30, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveDate("since", tc.in, tc.upper, now)
			if err != nil {
				t.Fatalf("resolveDate(%q): %v", tc.in, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("resolveDate(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}

	for _, bad := range []string{"", "soon", "0d", "-3d", "3", "d", "3x", "2026-13-01", "2026-1-5", "next monday", "3 days ago", "7s", "3MO", "3mi", "mo", "min"} {
		t.Run("bad "+bad, func(t *testing.T) {
			_, err := resolveDate("since", bad, false, now)
			if err == nil {
				t.Fatalf("resolveDate(%q): want error", bad)
			}
			if !strings.Contains(err.Error(), "--since") || !strings.Contains(err.Error(), "yesterday") {
				t.Errorf("error %q must name the flag and the accepted forms", err)
			}
		})
	}
}

func TestResolveDay(t *testing.T) {
	loc := time.FixedZone("T", 2*3600)
	now := time.Date(2026, 11, 20, 0, 30, 0, 0, loc) // 22:30Z on the 19th
	for in, want := range map[string]string{
		"2026-04-01":           "2026-04-01",
		"3d":                   "2026-11-17",
		"today":                "2026-11-20",
		"yesterday":            "2026-11-19",
		"2026-04-01T23:30:00Z": "2026-04-01",
	} {
		got, err := resolveDay("mentioned-since", in, now)
		if err != nil || got != want {
			t.Errorf("resolveDay(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// withClock fixes the clock date flags resolve against, in UTC so day
// boundaries do not follow the machine's zone.
func withClock(t *testing.T, now time.Time) {
	t.Helper()
	prev := dateNow
	dateNow = now.UTC
	t.Cleanup(func() { dateNow = prev })
}
