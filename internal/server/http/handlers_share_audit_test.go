package http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"2026-10-01":           time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		"2026-10-14T08:30:00Z": time.Date(2026, 10, 14, 8, 30, 0, 0, time.UTC),
		"24h":                  now.Add(-24 * time.Hour),
		"7d":                   now.Add(-7 * 24 * time.Hour),
		"90m":                  now.Add(-90 * time.Minute),
	}
	for in, want := range cases {
		got, ok := parseSince(in, now)
		assert.True(t, ok, in)
		assert.True(t, want.Equal(got), "%s: got %s want %s", in, got, want)
	}
	for _, in := range []string{"soon", "-24h", "0h", "d", "2026-13-01"} {
		_, ok := parseSince(in, now)
		assert.False(t, ok, in)
	}
}
