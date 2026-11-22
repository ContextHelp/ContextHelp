package storage

import (
	"testing"
	"time"
)

var stewardTestT0 = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

// The effective expiry is min(expires_at, not_before + max_duration)
// (ADR-083 §2): a cap below the window shortens it, a cap above it
// changes nothing, no cap leaves the window as approved.
func TestStewardGrantEffectiveExpiry(t *testing.T) {
	g := &StewardGrant{NotBefore: stewardTestT0, ExpiresAt: stewardTestT0.Add(10 * 24 * time.Hour)}
	for name, c := range map[string]struct {
		max  time.Duration
		want time.Time
	}{
		"cap shorter than the window": {7 * 24 * time.Hour, stewardTestT0.Add(7 * 24 * time.Hour)},
		"cap equal to the window":     {10 * 24 * time.Hour, g.ExpiresAt},
		"cap longer than the window":  {14 * 24 * time.Hour, g.ExpiresAt},
		"no cap":                      {0, g.ExpiresAt},
		"negative cap is no cap":      {-time.Hour, g.ExpiresAt},
	} {
		if got := g.EffectiveExpiry(c.max); !got.Equal(c.want) {
			t.Errorf("%s: effective expiry %s, want %s", name, got, c.want)
		}
	}
}

// StateAt computes active and expired from the clock and the cap on
// every call, at the boundaries: not_before is in, the effective expiry
// is out. Revoked wins over the window, and a status the store never
// writes is never in force.
func TestStewardGrantStateAt(t *testing.T) {
	g := &StewardGrant{Status: StewardGrantApproved, NotBefore: stewardTestT0, ExpiresAt: stewardTestT0.Add(48 * time.Hour)}
	day := 24 * time.Hour
	for name, c := range map[string]struct {
		now  time.Time
		max  time.Duration
		want StewardGrantState
	}{
		"before not_before":                {stewardTestT0.Add(-time.Millisecond), 0, StewardStatePending},
		"at not_before":                    {stewardTestT0, 0, StewardStateActive},
		"inside":                           {stewardTestT0.Add(day), 0, StewardStateActive},
		"just before expires_at":           {g.ExpiresAt.Add(-time.Millisecond), 0, StewardStateActive},
		"at expires_at":                    {g.ExpiresAt, 0, StewardStateExpired},
		"after expires_at":                 {g.ExpiresAt.Add(time.Hour), 0, StewardStateExpired},
		"cap shortens a live grant":        {stewardTestT0.Add(day + time.Hour), day, StewardStateExpired},
		"cap boundary is out":              {stewardTestT0.Add(day), day, StewardStateExpired},
		"just inside the cap":              {stewardTestT0.Add(day - time.Millisecond), day, StewardStateActive},
		"a looser cap changes nothing":     {stewardTestT0.Add(day), 30 * day, StewardStateActive},
		"a cap does not move not_before":   {stewardTestT0.Add(-time.Millisecond), day, StewardStatePending},
		"expired and past the cap, either": {g.ExpiresAt.Add(time.Hour), day, StewardStateExpired},
	} {
		if got := g.StateAt(c.now, c.max); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
	revoked := *g
	revoked.Status = StewardGrantRevoked
	if got := revoked.StateAt(stewardTestT0.Add(time.Hour), 0); got != StewardStateRevoked {
		t.Errorf("revoked inside the window: %s", got)
	}
	if got := revoked.StateAt(g.ExpiresAt.Add(time.Hour), 0); got != StewardStateRevoked {
		t.Errorf("revoked past the window: %s, want revoked (the stored fact wins)", got)
	}
	for _, status := range []StewardGrantStatus{"", "active", "expired", "bogus"} {
		odd := *g
		odd.Status = status
		if got := odd.StateAt(stewardTestT0.Add(time.Hour), 0); got == StewardStateActive {
			t.Errorf("status %q reads as active", status)
		}
	}
}
