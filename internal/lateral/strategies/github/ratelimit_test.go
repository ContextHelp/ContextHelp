package github

import (
	"testing"
	"time"
)

// fixedClock returns a closure that always returns t. Used to make
// FloorTracker deterministic in tests.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestFloorTracker_NoSnapshot_ReturnsZero(t *testing.T) {
	ft := NewFloorTracker(time.Hour, DefaultFloorBounds())
	if got := ft.Floor(); got != 0 {
		t.Errorf("Floor without snapshot = %d, want 0", got)
	}
	if ft.ShouldThrottle() {
		t.Error("ShouldThrottle without snapshot must be false")
	}
}

func TestFloorTracker_LowPressure_FloorAtMin(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	ft := NewFloorTracker(4*time.Hour, FloorBounds{MinPct: 0.05, MaxPct: 0.90})
	ft.now = fixedClock(now)
	ft.Observe(RateSnapshot{Limit: 5000, Remaining: 4900, ObservedAt: now})
	// No calls recorded → pressure = 0 → floor = MinPct × Limit = 250.
	if got, want := ft.Floor(), 250; got != want {
		t.Errorf("Floor at zero pressure = %d, want %d", got, want)
	}
}

func TestFloorTracker_HighPressure_FloorAtMax(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	ft := NewFloorTracker(4*time.Hour, FloorBounds{MinPct: 0.05, MaxPct: 0.90})
	ft.now = fixedClock(now)
	ft.Observe(RateSnapshot{Limit: 100, Remaining: 1, ObservedAt: now})
	// Record more calls than Limit → pressure clamps to 1 → floor = MaxPct.
	for i := 0; i < 200; i++ {
		ft.Record(now.Add(-time.Duration(i) * time.Minute))
	}
	if got, want := ft.Floor(), 90; got != want {
		t.Errorf("Floor at saturated pressure = %d, want %d", got, want)
	}
}

func TestFloorTracker_MidPressure_LinearInterpolated(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	ft := NewFloorTracker(4*time.Hour, FloorBounds{MinPct: 0.0, MaxPct: 1.0})
	ft.now = fixedClock(now)
	ft.Observe(RateSnapshot{Limit: 100, Remaining: 50, ObservedAt: now})
	// Record exactly 50 calls in window → pressure = 0.5.
	for i := 0; i < 50; i++ {
		ft.Record(now.Add(-time.Duration(i) * time.Minute))
	}
	// pct = 0 + (1-0)*0.5 = 0.5; floor = 0.5 * 100 = 50.
	if got, want := ft.Floor(), 50; got != want {
		t.Errorf("Floor at mid pressure = %d, want %d", got, want)
	}
}

func TestFloorTracker_EvictsOutOfWindowCalls(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	ft := NewFloorTracker(time.Hour, DefaultFloorBounds())
	ft.now = fixedClock(now)
	// Record 100 calls 2h ago — all out of the 1h window.
	for i := 0; i < 100; i++ {
		ft.Record(now.Add(-2 * time.Hour))
	}
	if got := ft.CallsInWindow(); got != 0 {
		t.Errorf("CallsInWindow after eviction = %d, want 0", got)
	}
}

func TestFloorTracker_ShouldThrottle_RemainingBelowFloor(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	ft := NewFloorTracker(time.Hour, FloorBounds{MinPct: 0.5, MaxPct: 0.5})
	ft.now = fixedClock(now)
	ft.Observe(RateSnapshot{Limit: 100, Remaining: 40, ObservedAt: now})
	// Floor is 50%; Remaining 40 < 50 → throttle.
	if !ft.ShouldThrottle() {
		t.Error("ShouldThrottle = false; want true (Remaining 40 < Floor 50)")
	}
	ft.Observe(RateSnapshot{Limit: 100, Remaining: 60, ObservedAt: now})
	if ft.ShouldThrottle() {
		t.Error("ShouldThrottle = true; want false (Remaining 60 > Floor 50)")
	}
}

func TestFloorTracker_BoundsClampedTo01(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	// Pathological: MinPct > MaxPct → swapped; values outside [0,1] clamped.
	ft := NewFloorTracker(time.Hour, FloorBounds{MinPct: 0.9, MaxPct: 0.1})
	ft.now = fixedClock(now)
	ft.Observe(RateSnapshot{Limit: 100, Remaining: 50, ObservedAt: now})
	// After swap MinPct=0.1, MaxPct=0.9. No calls → pressure 0 → floor 10.
	if got, want := ft.Floor(), 10; got != want {
		t.Errorf("Floor with swapped bounds = %d, want %d", got, want)
	}
}

func TestRateSnapshot_IsZero(t *testing.T) {
	if !(RateSnapshot{}).IsZero() {
		t.Error("zero RateSnapshot.IsZero must be true")
	}
	if (RateSnapshot{Limit: 1}).IsZero() {
		t.Error("non-zero RateSnapshot.IsZero must be false")
	}
}
