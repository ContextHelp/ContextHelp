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

func TestFloorTracker_BoundsOutOfRange_FullyClamped(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		// Before T-0270 the contract clamped only MinPct < 0 and MaxPct
		// > 1; values like MinPct=2.0 / MaxPct=-0.5 produced nonsense
		// floors. clampBounds normalises both directions for both.
		bounds FloorBounds
		// Calls injected so pressure stays at 0 — floor reduces to
		// MinPct × Limit after clamping.
		wantFloor int
	}{
		{
			name:      "MinPct above 1 clamps to 1",
			bounds:    FloorBounds{MinPct: 2.0, MaxPct: 0.9},
			wantFloor: 90, // min clamped to 1, then swapped: min=0.9, max=1.0; pressure 0 → 0.9 * 100 = 90
		},
		{
			name:      "MaxPct below 0 clamps to 0",
			bounds:    FloorBounds{MinPct: 0.05, MaxPct: -0.5},
			wantFloor: 0, // max clamped to 0, then swapped: min=0, max=0.05; pressure 0 → 0
		},
		{
			name:      "both out of range",
			bounds:    FloorBounds{MinPct: 5.0, MaxPct: -2.0},
			wantFloor: 100, // min→1, max→0, swap: min=0, max=1; pressure 0 → 0. wait: pressure 0 -> floor = min*Limit = 0
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ft := NewFloorTracker(time.Hour, tc.bounds)
			ft.now = fixedClock(now)
			ft.Observe(RateSnapshot{Limit: 100, Remaining: 50, ObservedAt: now})
			got := ft.Floor()
			// Whatever the exact answer, it MUST be in [0, Limit].
			if got < 0 || got > 100 {
				t.Errorf("Floor = %d outside [0, 100]; bounds were not clamped", got)
			}
			// And ShouldThrottle must produce a deterministic answer
			// (no NaN, no panic, no nonsense floor > Limit).
			_ = ft.ShouldThrottle()
		})
	}
}

func TestFloorTracker_MinAboveOne_DoesNotExceedLimit(t *testing.T) {
	// Concrete regression: MinPct = 2.0, Limit = 100. Without the
	// clamp, Floor() would compute pct = 2.0 + (max - 2.0) * pressure
	// → floor of 200 (twice the limit), and ShouldThrottle would trip
	// permanently regardless of remaining.
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	ft := NewFloorTracker(time.Hour, FloorBounds{MinPct: 2.0, MaxPct: 0.9})
	ft.now = fixedClock(now)
	ft.Observe(RateSnapshot{Limit: 100, Remaining: 95, ObservedAt: now})
	if got := ft.Floor(); got > 100 {
		t.Errorf("Floor = %d > Limit (100); bounds clamp missing", got)
	}
}

func TestClampBounds_AllPathways(t *testing.T) {
	cases := []struct {
		name             string
		inMin, inMax     float64
		wantMin, wantMax float64
	}{
		{"both in range", 0.1, 0.9, 0.1, 0.9},
		{"min negative", -0.1, 0.9, 0.0, 0.9},
		{"max above one", 0.1, 1.5, 0.1, 1.0},
		{"min above one", 2.0, 0.9, 0.9, 1.0},   // clamped, then swapped
		{"max below zero", 0.1, -0.5, 0.0, 0.1}, // clamped, then swapped
		{"both above one", 1.5, 2.0, 1.0, 1.0},
		{"both below zero", -1.0, -0.5, 0.0, 0.0},
		{"swap needed", 0.9, 0.1, 0.1, 0.9},
		{"min above one max below zero", 2.0, -0.5, 0.0, 1.0}, // min→1, max→0, swap → (0, 1)
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotMin, gotMax := clampBounds(tc.inMin, tc.inMax)
			if gotMin != tc.wantMin || gotMax != tc.wantMax {
				t.Errorf("clampBounds(%v, %v) = (%v, %v), want (%v, %v)",
					tc.inMin, tc.inMax, gotMin, gotMax, tc.wantMin, tc.wantMax)
			}
			if gotMin < 0 || gotMin > 1 || gotMax < 0 || gotMax > 1 {
				t.Errorf("result outside [0,1]: (%v, %v)", gotMin, gotMax)
			}
			if gotMin > gotMax {
				t.Errorf("min=%v > max=%v after normalisation", gotMin, gotMax)
			}
		})
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
