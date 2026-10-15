package jit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/events"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// flipOutage is an OutageDetector backed by a pointer so tests can toggle
// the reported state between calls. Models a real breaker that opens and
// closes during a process lifetime.
type flipOutage struct{ active *bool }

func (f flipOutage) IsOutage() bool { return f.active != nil && *f.active }

// TestPipeline_OutageDetectorQueriedPerCall guards against a regression in
// which the OutageDetector is captured at NewPipeline time and reused
// across calls. The detector must be re-queried on every RunOnce: a flip
// from "outage active" to "healthy" between two calls on the same key
// should change recipe.served emission accordingly.
func TestPipeline_OutageDetectorQueriedPerCall(t *testing.T) {
	prop := &fakeProposer{}
	cache := jit.NewMemoryProposalCache()
	cache.Put(context.Background(), "acme.io", "post", []string{"/cached"})
	cp := jit.NewCachedProposer(prop, cache)

	exec := jit.NewExecutor(&pipelineFetcher{
		bodies: map[string]string{"https://acme.io/cached": "ok"},
	})

	outageActive := true
	det := flipOutage{active: &outageActive}
	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, det)

	// First call: outage active → recipe.served fires.
	if _, err := p.RunOnce(context.Background(), "obj-1", "https://acme.io/source", "acme.io", "post"); err != nil {
		t.Fatalf("first RunOnce err = %v", err)
	}
	got := pub.snapshot()
	if len(got) != 1 || got[0].topic != string(events.RecipeServed) {
		t.Fatalf("after first call: events = %+v, want one recipe.served", got)
	}

	// Flip detector: outage cleared.
	outageActive = false

	// Second call: same key, detector now healthy → no new event.
	if _, err := p.RunOnce(context.Background(), "obj-1", "https://acme.io/source", "acme.io", "post"); err != nil {
		t.Fatalf("second RunOnce err = %v", err)
	}
	got = pub.snapshot()
	if len(got) != 1 {
		t.Fatalf("after second call: events = %d (%+v), want still 1 (detector flipped to healthy must suppress recipe.served)", len(got), got)
	}

	// Flip back to outage.
	outageActive = true

	// Third call: detector active again → second recipe.served fires.
	if _, err := p.RunOnce(context.Background(), "obj-1", "https://acme.io/source", "acme.io", "post"); err != nil {
		t.Fatalf("third RunOnce err = %v", err)
	}
	got = pub.snapshot()
	if len(got) != 2 {
		t.Fatalf("after third call: events = %d, want 2 (detector flipped back to outage must re-emit)", len(got))
	}
	if got[1].topic != string(events.RecipeServed) {
		t.Fatalf("third call topic = %q, want recipe.served", got[1].topic)
	}
}

// TestProposeWithOutageFallback_TruthTableSweep exhaustively walks the
// (cache state, proposer outcome, outage signal) cube and asserts the
// full return tuple. Catches regressions where one quadrant's behavior
// changes without a corresponding direct test.
//
// Cube axes:
//
//	cache:    {empty, populated}
//	proposer: {ok-with-reply, ok-empty-reply, error}
//	outage:   {nil, false, true}
//
// 2 × 3 × 3 = 18 combinations. Each row asserts (subpaths, served, err).
func TestProposeWithOutageFallback_TruthTableSweep(t *testing.T) {
	for _, c := range []sweepCache{cacheEmpty, cachePopulated} {
		for _, pOut := range []sweepProposer{proposerReply, proposerEmpty, proposerError} {
			for _, o := range []sweepOutage{outageNil, outageFalse, outageTrue} {
				t.Run(sweepName(c, pOut, o), func(t *testing.T) {
					prop := sweepNewProposer(pOut)
					cache := jit.NewMemoryProposalCache()
					if c == cachePopulated {
						cache.Put(context.Background(), "acme.io", "post", sweepCachedReply)
					}
					cp := jit.NewCachedProposer(prop, cache)

					got, served, err := cp.ProposeWithOutageFallback(context.Background(), "acme.io", "post", sweepNewDetector(o))

					if c == cachePopulated {
						sweepCheckHit(t, prop, o, got, served, err)
						return
					}
					sweepCheckMiss(t, cache, pOut, got, served, err)
				})
			}
		}
	}
}

// Axes and fixtures of the truth-table sweep.
type (
	sweepCache    int
	sweepProposer int
	sweepOutage   int
)

const (
	cacheEmpty sweepCache = iota
	cachePopulated
)

const (
	proposerReply sweepProposer = iota
	proposerEmpty
	proposerError
)

const (
	outageNil sweepOutage = iota
	outageFalse
	outageTrue
)

var (
	sweepCachedReply = []string{"/cached"}
	sweepFreshReply  = []string{"/fresh"}
	errSweepProposer = errors.New("llm down")
)

func sweepName(c sweepCache, p sweepProposer, o sweepOutage) string {
	cs := []string{"empty", "populated"}[c]
	ps := []string{"reply", "emptyReply", "error"}[p]
	os := []string{"nil", "false", "true"}[o]
	return cs + "_" + ps + "_" + os
}

func sweepNewProposer(p sweepProposer) *fakeProposer {
	switch p {
	case proposerEmpty:
		return &fakeProposer{reply: nil}
	case proposerError:
		return &fakeProposer{err: errSweepProposer}
	default:
		return &fakeProposer{reply: sweepFreshReply}
	}
}

func sweepNewDetector(o sweepOutage) jit.OutageDetector {
	switch o {
	case outageFalse:
		return stubOutage{outage: false}
	case outageTrue:
		return stubOutage{outage: true}
	default:
		return nil
	}
}

// sweepCheckHit: a populated cache always answers, never calls the
// proposer, and reports served only during an outage.
func sweepCheckHit(t *testing.T, prop *fakeProposer, o sweepOutage, got []string, served bool, err error) {
	t.Helper()
	if !equalStringSlice(got, sweepCachedReply) {
		t.Errorf("got = %v, want %v (cache hit)", got, sweepCachedReply)
	}
	if err != nil {
		t.Errorf("err = %v, want nil (cache hit)", err)
	}
	wantServed := o == outageTrue
	if served != wantServed {
		t.Errorf("served = %v, want %v (outage=%v)", served, wantServed, o)
	}
	if n := prop.calls.Load(); n != 0 {
		t.Errorf("proposer called %d times, want 0 (cache hit must short-circuit)", n)
	}
}

// sweepCheckMiss: on a cache miss the proposer is called and the outage
// is irrelevant to the return tuple (no event-firing path here).
func sweepCheckMiss(t *testing.T, cache *jit.MemoryProposalCache, pOut sweepProposer, got []string, served bool, err error) {
	t.Helper()
	if served {
		t.Errorf("served = true on cache miss; want false")
	}
	switch pOut {
	case proposerReply:
		if !equalStringSlice(got, sweepFreshReply) {
			t.Errorf("got = %v, want %v (fresh)", got, sweepFreshReply)
		}
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
		// Side-effect: cache populated.
		if v, ok := cache.Get(context.Background(), "acme.io", "post"); !ok || !equalStringSlice(v, sweepFreshReply) {
			t.Errorf("cache after fresh fetch: (%v, %v), want (%v, true)", v, ok, sweepFreshReply)
		}
	case proposerEmpty:
		if got != nil {
			t.Errorf("got = %v, want nil (empty proposal)", got)
		}
		if err != nil {
			t.Errorf("err = %v, want nil", err)
		}
		// Side-effect: cache must NOT be populated with empty.
		if _, ok := cache.Get(context.Background(), "acme.io", "post"); ok {
			t.Errorf("cache populated with empty verdict; must not stick")
		}
	case proposerError:
		if got != nil {
			t.Errorf("got = %v, want nil (proposer err)", got)
		}
		if !errors.Is(err, errSweepProposer) {
			t.Errorf("err = %v, want %v", err, errSweepProposer)
		}
	}
}
