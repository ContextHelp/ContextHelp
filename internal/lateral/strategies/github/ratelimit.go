package github

import (
	"sync"
	"time"
)

// RateSnapshot captures the github API rate-limit headers exposed on
// each response. Implementations MAY return zero values when no signal
// is available; callers treat zero as "skip floor adjustment".
//
// Limit:        the bucket ceiling reported by the upstream
// Remaining:    requests still available before reset
// ResetAt:      when the bucket refills (UTC)
// ObservedAt:   when this snapshot was recorded
//
// Defining the type here (alongside the dynamic-floor helper) replaces
// the empty placeholder T-0254 added in api.go for compilation. T-0254
// reserves the field name; T-0260 fleshes it out.
type RateSnapshot struct {
	Limit      int
	Remaining  int
	ResetAt    time.Time
	ObservedAt time.Time
}

// IsZero reports whether s carries no signal. Used by the floor helper
// to short-circuit when the upstream hasn't reported headers yet.
func (s RateSnapshot) IsZero() bool {
	return s.Limit == 0 && s.Remaining == 0 && s.ResetAt.IsZero() && s.ObservedAt.IsZero()
}

// FloorBounds clamps the dynamic-floor adjustment. Defaults derive from
// the spec's [5%, 90%] of upstream limit:
//
//   - MinPct: floor never drops below 5% of Limit (so we always reserve
//     a tiny budget for explicit user actions even under heavy lateral
//     load).
//   - MaxPct: floor never rises above 90% of Limit (we never starve
//     ourselves of a few-call buffer, even when no lateral activity is
//     happening).
//
// Both percentages are fractions in [0,1].
type FloorBounds struct {
	MinPct float64
	MaxPct float64
}

// DefaultFloorBounds returns the spec defaults: [5%, 90%].
func DefaultFloorBounds() FloorBounds {
	return FloorBounds{MinPct: 0.05, MaxPct: 0.90}
}

// FloorTracker maintains a trailing-window count of API calls and
// computes the dynamic budget floor from that count + the latest
// RateSnapshot.
//
// Concurrency: safe for one writer (Record + LatestSnapshot) and many
// readers (Floor) thanks to the embedded mutex. github strategies use
// one tracker per APIClient.
type FloorTracker struct {
	mu       sync.Mutex
	window   time.Duration
	bounds   FloorBounds
	calls    []time.Time
	snapshot RateSnapshot
	now      func() time.Time // injectable for tests
}

// NewFloorTracker constructs a tracker observing window-long rolling
// activity. Pass DefaultFloorBounds() to use spec defaults; pass a
// custom FloorBounds to override.
func NewFloorTracker(window time.Duration, bounds FloorBounds) *FloorTracker {
	if window <= 0 {
		window = 4 * time.Hour
	}
	if bounds.MinPct <= 0 && bounds.MaxPct <= 0 {
		bounds = DefaultFloorBounds()
	}
	return &FloorTracker{
		window: window,
		bounds: bounds,
		now:    time.Now,
	}
}

// Record marks an API call at t. Use the zero time to record at "now".
func (f *FloorTracker) Record(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.IsZero() {
		t = f.now()
	}
	f.calls = append(f.calls, t)
	f.evictLocked(f.now())
}

// Observe stores the latest rate-limit snapshot. The floor calculation
// reads from this on every call.
func (f *FloorTracker) Observe(s RateSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.ObservedAt.IsZero() {
		s.ObservedAt = f.now()
	}
	f.snapshot = s
}

// LatestSnapshot returns the most recent observed snapshot.
func (f *FloorTracker) LatestSnapshot() RateSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot
}

// CallsInWindow returns the count of recorded calls within the trailing
// window from now.
func (f *FloorTracker) CallsInWindow() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evictLocked(f.now())
	return len(f.calls)
}

// Floor returns the absolute remaining-call floor we must keep above
// to reserve budget for non-lateral traffic. The lateral pipeline must
// stop scheduling new fetches when snapshot.Remaining < Floor().
//
// Algorithm:
//
//  1. If we have no snapshot yet, return 0 — without an upstream signal
//     there's nothing to clamp against.
//  2. Compute base floor as MinPct × Limit.
//  3. Compute pressure as min(1, callsInWindow / Limit). High pressure
//     means we're hammering the API; low pressure means we have room.
//  4. Linear interpolate floor from MinPct to MaxPct based on pressure:
//     low pressure → floor = MinPct × Limit
//     high pressure → floor = MaxPct × Limit
//  5. Clamp final floor to [MinPct × Limit, MaxPct × Limit].
//
// This collapses to "always reserve at least 5% of the bucket; when we
// see lots of activity, escalate the reservation up to 90%". In
// practice this means lateral throttles itself on heavy days, and
// barely throttles at all on light ones — with an absolute lower
// bound that protects user-initiated capture under any load.
func (f *FloorTracker) Floor() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshot.IsZero() || f.snapshot.Limit <= 0 {
		return 0
	}
	limit := f.snapshot.Limit
	min := f.bounds.MinPct
	max := f.bounds.MaxPct
	if min < 0 {
		min = 0
	}
	if max > 1 {
		max = 1
	}
	if min > max {
		min, max = max, min
	}
	f.evictLocked(f.now())
	calls := len(f.calls)
	pressure := float64(calls) / float64(limit)
	if pressure > 1 {
		pressure = 1
	}
	pct := min + (max-min)*pressure
	floor := int(float64(limit) * pct)
	// Clamp.
	lower := int(float64(limit) * min)
	upper := int(float64(limit) * max)
	if floor < lower {
		floor = lower
	}
	if floor > upper {
		floor = upper
	}
	return floor
}

// ShouldThrottle reports whether the strategy should pause new lateral
// fetches given the current snapshot. True when remaining < Floor().
// Returns false when there is no snapshot signal (callers proceed by
// default; they can't usefully throttle without a budget).
func (f *FloorTracker) ShouldThrottle() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshot.IsZero() || f.snapshot.Limit <= 0 {
		return false
	}
	floor := f.floorLocked(f.now())
	return f.snapshot.Remaining < floor
}

// floorLocked is the lock-held variant of Floor used by ShouldThrottle.
func (f *FloorTracker) floorLocked(now time.Time) int {
	limit := f.snapshot.Limit
	if limit <= 0 {
		return 0
	}
	min := f.bounds.MinPct
	max := f.bounds.MaxPct
	if min < 0 {
		min = 0
	}
	if max > 1 {
		max = 1
	}
	if min > max {
		min, max = max, min
	}
	// Walk back through f.calls to count those still in window without
	// mutating; the public path mutates via evictLocked, but the locked
	// helper avoids re-evicting if the caller already did.
	cutoff := now.Add(-f.window)
	calls := 0
	for _, c := range f.calls {
		if !c.Before(cutoff) {
			calls++
		}
	}
	pressure := float64(calls) / float64(limit)
	if pressure > 1 {
		pressure = 1
	}
	pct := min + (max-min)*pressure
	return int(float64(limit) * pct)
}

// evictLocked drops calls older than now-window. Caller holds f.mu.
func (f *FloorTracker) evictLocked(now time.Time) {
	cutoff := now.Add(-f.window)
	i := 0
	for ; i < len(f.calls); i++ {
		if !f.calls[i].Before(cutoff) {
			break
		}
	}
	if i > 0 {
		f.calls = f.calls[i:]
	}
}
