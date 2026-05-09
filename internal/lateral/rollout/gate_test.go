package rollout

import (
	"sync"
	"testing"
)

func TestStrategyGate_NilAllowsEverything(t *testing.T) {
	t.Parallel()
	var g *StrategyGate
	if !g.Allowed("any") {
		t.Error("nil gate must allow")
	}
}

func TestStrategyGate_ZeroValueAllowsEverything(t *testing.T) {
	t.Parallel()
	g := NewStrategyGate()
	if !g.Allowed("github") {
		t.Error("empty gate must allow")
	}
}

func TestStrategyGate_SetTogglesIDIndependently(t *testing.T) {
	t.Parallel()
	g := NewStrategyGate()
	g.Set("github", false)
	if g.Allowed("github") {
		t.Error("github should be denied")
	}
	if !g.Allowed("jit") {
		t.Error("jit should still be allowed (untouched)")
	}
	g.Set("github", true)
	if !g.Allowed("github") {
		t.Error("github should be re-allowed")
	}
}

func TestStrategyGate_SetAllReplacesMap(t *testing.T) {
	t.Parallel()
	g := NewStrategyGate()
	g.Set("a", false)
	g.SetAll(map[string]bool{"b": false})
	if !g.Allowed("a") {
		t.Error("a should be allowed after SetAll dropped it")
	}
	if g.Allowed("b") {
		t.Error("b should be denied")
	}
}

func TestStrategyGate_ConcurrentReadsAndWrites(t *testing.T) {
	t.Parallel()
	g := NewStrategyGate()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if i%2 == 0 {
					g.Set("strategy", j%2 == 0)
				} else {
					_ = g.Allowed("strategy")
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestStrategyGate_SnapshotIndependentCopy(t *testing.T) {
	t.Parallel()
	g := NewStrategyGate()
	g.Set("a", true)
	snap := g.Snapshot()
	snap["a"] = false
	if !g.Allowed("a") {
		t.Error("mutation of snapshot leaked into gate")
	}
}
