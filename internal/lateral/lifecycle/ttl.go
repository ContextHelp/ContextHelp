package lifecycle

import "time"

// SanityFutureBound caps how far in the future expires_at may sit before
// IsSanityViolation treats it as a clock-skew bug rather than a real value.
const SanityFutureBound = 30 * 24 * time.Hour

// ComputeExpiresAt returns now + lifespan. Pure helper — adopters inject
// `now` rather than reading the wall clock here, mirroring the materializer
// (T10) WithNowFunc pattern and kit's job.WithNowFunc convention. Composition
// layers (reaper, materializer) read the clock once and thread `now` into
// all TTL helpers for one cycle.
func ComputeExpiresAt(now time.Time, lifespan time.Duration) time.Time {
	return now.Add(lifespan)
}

// IsColdStale reports whether the cold-cycle threshold has elapsed since
// coldSince. A zero coldSince means "candidate never went cold" and returns
// false unconditionally.
func IsColdStale(now, coldSince time.Time, threshold time.Duration) bool {
	if coldSince.IsZero() {
		return false
	}
	return now.Sub(coldSince) > threshold
}

// IsSanityViolation guards the reaper against clock-skew bugs. It returns
// true when expiresAt is more than SanityFutureBound ahead of now (clock
// skew protection) OR before discoveredAt (impossible-state protection).
//
// Call this at reap time only. A freshly materialized candidate has
// expires_at = discovered_at + parent_lifespan (default 180d), which
// will trip the future-bound check at creation; by the time the reaper
// scans the same record, expires_at is normally within ~30d of the
// reaper's `now` or already past. Calling this on fresh records produces
// false positives.
//
// A zero discoveredAt skips the before-discovered check (callers that
// don't have discoveredAt available pass time.Time{}).
func IsSanityViolation(now, expiresAt, discoveredAt time.Time) bool {
	if expiresAt.Sub(now) > SanityFutureBound {
		return true
	}
	if !discoveredAt.IsZero() && expiresAt.Before(discoveredAt) {
		return true
	}
	return false
}
