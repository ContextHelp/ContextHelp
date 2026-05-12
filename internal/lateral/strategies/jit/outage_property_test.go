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
	cachedReply := []string{"/cached"}
	freshReply := []string{"/fresh"}
	proposerErr := errors.New("llm down")

	type cacheState int
	const (
		cacheEmpty cacheState = iota
		cachePopulated
	)

	type proposerOutcome int
	const (
		proposerReply proposerOutcome = iota
		proposerEmpty
		proposerError
	)

	type outageState int
	const (
		outageNil outageState = iota
		outageFalse
		outageTrue
	)

	stateName := func(c cacheState, p proposerOutcome, o outageState) string {
		cs := []string{"empty", "populated"}[c]
		ps := []string{"reply", "emptyReply", "error"}[p]
		os := []string{"nil", "false", "true"}[o]
		return cs + "_" + ps + "_" + os
	}

	for _, c := range []cacheState{cacheEmpty, cachePopulated} {
		for _, pOut := range []proposerOutcome{proposerReply, proposerEmpty, proposerError} {
			for _, o := range []outageState{outageNil, outageFalse, outageTrue} {
				c, pOut, o := c, pOut, o
				t.Run(stateName(c, pOut, o), func(t *testing.T) {
					// Build proposer.
					var prop *fakeProposer
					switch pOut {
					case proposerReply:
						prop = &fakeProposer{reply: freshReply}
					case proposerEmpty:
						prop = &fakeProposer{reply: nil}
					case proposerError:
						prop = &fakeProposer{err: proposerErr}
					}
					// Build cache.
					cache := jit.NewMemoryProposalCache()
					if c == cachePopulated {
						cache.Put(context.Background(), "acme.io", "post", cachedReply)
					}
					cp := jit.NewCachedProposer(prop, cache)
					// Build detector.
					var det jit.OutageDetector
					switch o {
					case outageNil:
						det = nil
					case outageFalse:
						det = stubOutage{outage: false}
					case outageTrue:
						det = stubOutage{outage: true}
					}

					got, served, err := cp.ProposeWithOutageFallback(context.Background(), "acme.io", "post", det)

					// Expectations:
					switch c {
					case cachePopulated:
						// Always returns cached, never calls proposer.
						if !equalStringSlice(got, cachedReply) {
							t.Errorf("got = %v, want %v (cache hit)", got, cachedReply)
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
					case cacheEmpty:
						// Cache miss → proposer is called, outage is irrelevant
						// to the return tuple (no event-firing path here).
						if served {
							t.Errorf("served = true on cache miss; want false")
						}
						switch pOut {
						case proposerReply:
							if !equalStringSlice(got, freshReply) {
								t.Errorf("got = %v, want %v (fresh)", got, freshReply)
							}
							if err != nil {
								t.Errorf("err = %v, want nil", err)
							}
							// Side-effect: cache populated.
							if v, ok := cache.Get(context.Background(), "acme.io", "post"); !ok || !equalStringSlice(v, freshReply) {
								t.Errorf("cache after fresh fetch: (%v, %v), want (%v, true)", v, ok, freshReply)
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
							if !errors.Is(err, proposerErr) {
								t.Errorf("err = %v, want %v", err, proposerErr)
							}
						}
					}
				})
			}
		}
	}
}
