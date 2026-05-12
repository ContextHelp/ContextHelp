package rollout

import (
	"fmt"
	"testing"
)

func TestSampler_NilAllowsEverything(t *testing.T) {
	t.Parallel()
	var s *Sampler
	if !s.Allow("any", "id") {
		t.Error("nil sampler must allow")
	}
}

func TestSampler_UnconfiguredStrategyAllows(t *testing.T) {
	t.Parallel()
	s := NewSampler(map[string]int{"github": 50})
	// Strategy not in the percent map = full traffic.
	for i := 0; i < 100; i++ {
		if !s.Allow("jit", fmt.Sprintf("id-%d", i)) {
			t.Errorf("jit denied at i=%d; want always-allowed", i)
		}
	}
}

func TestSampler_ZeroPercentBlocksAll(t *testing.T) {
	t.Parallel()
	s := NewSampler(map[string]int{"x": 0})
	for i := 0; i < 100; i++ {
		if s.Allow("x", fmt.Sprintf("id-%d", i)) {
			t.Errorf("x allowed at zero pct, i=%d", i)
		}
	}
}

func TestSampler_HundredPercentAllowsAll(t *testing.T) {
	t.Parallel()
	s := NewSampler(map[string]int{"x": 100})
	for i := 0; i < 100; i++ {
		if !s.Allow("x", fmt.Sprintf("id-%d", i)) {
			t.Errorf("x denied at 100%% pct, i=%d", i)
		}
	}
}

// TestSampler_StatisticalShape proves the hash distribution is
// roughly uniform: at percent=10, ~10% of 1000 stable IDs pass.
// Tolerance is loose because FNV's small-input bias can drift; the
// test catches gross malfunction (allowing 90% at 10%) not subtle
// uniformity issues.
func TestSampler_StatisticalShape(t *testing.T) {
	t.Parallel()

	const trials = 1000
	cases := []struct {
		percent int
		min     int
		max     int
	}{
		{percent: 10, min: 50, max: 200},
		{percent: 50, min: 400, max: 600},
		{percent: 90, min: 800, max: 950},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("p%d", tc.percent), func(t *testing.T) {
			t.Parallel()
			s := NewSampler(map[string]int{"x": tc.percent})
			passed := 0
			for i := 0; i < trials; i++ {
				if s.Allow("x", fmt.Sprintf("event-%d", i)) {
					passed++
				}
			}
			if passed < tc.min || passed > tc.max {
				t.Errorf("p=%d: %d/%d passed; want [%d, %d]",
					tc.percent, passed, trials, tc.min, tc.max)
			}
		})
	}
}

// TestSampler_StableForSameID proves a given (strategy, id) pair
// always returns the same Allow result. The replay harness depends
// on this: replaying the same fixture twice MUST hit the same
// strategies.
func TestSampler_StableForSameID(t *testing.T) {
	t.Parallel()
	s := NewSampler(map[string]int{"x": 50})
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("event-%d", i)
		first := s.Allow("x", id)
		for j := 0; j < 5; j++ {
			if s.Allow("x", id) != first {
				t.Errorf("Allow(x, %s) flipped on call %d", id, j)
			}
		}
	}
}

// TestSampler_DistinctStrategiesUseDistinctSeeds proves two
// strategies with the same percent don't collude: one routing event
// E doesn't imply the other does. We compute the disagreement rate
// and assert it's > 0 (any disagreement disproves collusion).
func TestSampler_DistinctStrategiesUseDistinctSeeds(t *testing.T) {
	t.Parallel()
	s := NewSampler(map[string]int{"a": 50, "b": 50})
	disagree := 0
	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("event-%d", i)
		if s.Allow("a", id) != s.Allow("b", id) {
			disagree++
		}
	}
	// At p=50 + uniform hash, disagreement should be ~50% (since each
	// is 50% pass, joint disagreement is ~50%). We just assert >5%.
	if disagree < 10 {
		t.Errorf("disagreement = %d/200; samplers are colluding", disagree)
	}
}

func TestSampler_ClampsOutOfRangePercent(t *testing.T) {
	t.Parallel()
	s := NewSampler(map[string]int{"low": -10, "high": 999})
	if !s.Allow("high", "id") {
		t.Error("high (clamped to 100) must allow")
	}
	if s.Allow("low", "id") {
		t.Error("low (clamped to 0) must deny")
	}
}

func TestSampler_SnapshotIsCopied(t *testing.T) {
	t.Parallel()
	s := NewSampler(map[string]int{"x": 50})
	snap := s.Snapshot()
	// Mutate the snapshot — the live sampler must NOT see the change.
	snap["x"] = 0
	live := s.Snapshot()
	if live["x"] != 50 {
		t.Errorf("live snap[x] = %d after snap mutation; want 50 (snapshot must be a copy)", live["x"])
	}
}
