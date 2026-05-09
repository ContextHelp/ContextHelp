package jit_test

import (
	"context"
	"testing"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/events"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// fixedPageType is a PageTypeClassifier that returns a constant label.
type fixedPageType struct{ label string }

func (f fixedPageType) Classify(_ context.Context, _ string) (string, error) {
	return f.label, nil
}

// TestE2E_NoTierAURLDispatchesToJIT is the T-0251 integration gate: a
// CapturedEvent on a domain with NO platform/shape strategy registered is
// dispatched to JIT, which proposes sub-paths via its proposer, fetches
// them via its executor, and emits candidates with the correct shape.
//
// Substrate post-processing (scoring, cap-gate, identity, materialize) is
// already covered by internal/lateral/integration_test.go (T-0227). The
// boundary here is "JIT produces correct candidates from a
// no-Tier-A URL"; downstream pipelines reuse the proven substrate path.
func TestE2E_NoTierAURLDispatchesToJIT(t *testing.T) {
	// 1. Wire the JIT strategy with real collaborators.
	prop := &fakeProposer{reply: []string{"/about", "/team", "/blog"}}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)

	exec := jit.NewExecutor(&pipelineFetcher{
		bodies: map[string]string{
			"https://random-no-tier-a.example/about": "About us page",
			"https://random-no-tier-a.example/team":  "Team page",
			"https://random-no-tier-a.example/blog":  "",
		},
	})

	pub := &recordingPub{}
	classifier := fixedPageType{label: "Blog"}

	strategy := jit.New(cp, exec, pub, jit.NoOutage(), classifier)

	// 2. Build a registry with NO platform/shape strategies. Only JIT.
	reg := lateral.NewRegistry()
	reg.Register(strategy)

	// 3. Captured event on a no-Tier-A URL.
	ev := lateral.CapturedEvent{
		ObjectID:        "obj-123",
		Namespace:       "@x.note",
		SourceURL:       "https://random-no-tier-a.example/post/featured-1",
		CapturePipeline: "test",
	}

	// 4. Dispatch must return exactly the JIT strategy.
	chosen := reg.Dispatch(context.Background(), ev)
	if len(chosen) != 1 {
		t.Fatalf("Dispatch returned %d strategies, want 1 (JIT fallback)", len(chosen))
	}
	if chosen[0].ID() != jit.StrategyID {
		t.Fatalf("Dispatch returned %q, want %q", chosen[0].ID(), jit.StrategyID)
	}

	// 5. Probe runs the pipeline.
	candidates, err := chosen[0].Probe(context.Background(), ev, lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("Probe err = %v", err)
	}

	// 6. Three sub-paths, all same-host, all fetched successfully → 3 candidates.
	if len(candidates) != 3 {
		t.Fatalf("candidates = %d, want 3", len(candidates))
	}

	// 7. Per-candidate shape: correct URL, type, strategy, preview.
	wantURLs := map[string]bool{
		"https://random-no-tier-a.example/about": false,
		"https://random-no-tier-a.example/team":  false,
		"https://random-no-tier-a.example/blog":  false,
	}
	for i, c := range candidates {
		if _, ok := wantURLs[c.URL]; !ok {
			t.Errorf("candidates[%d].URL = %q, not in expected set", i, c.URL)
		}
		wantURLs[c.URL] = true

		if c.Strategy != jit.StrategyID {
			t.Errorf("candidates[%d].Strategy = %q, want %q", i, c.Strategy, jit.StrategyID)
		}
		if c.CandidateType != jit.CandidateType {
			t.Errorf("candidates[%d].CandidateType = %q, want %q", i, c.CandidateType, jit.CandidateType)
		}
		if pt := c.Preview["page_type"]; pt != "Blog" {
			t.Errorf("candidates[%d].Preview[page_type] = %v, want Blog (from classifier)", i, pt)
		}
		if c.Preview["body_size"] == nil {
			t.Errorf("candidates[%d].Preview missing body_size", i)
		}
	}
	for url, seen := range wantURLs {
		if !seen {
			t.Errorf("expected URL %q never appeared in candidates", url)
		}
	}

	// 8. Happy path: no failure events, no recipe.served.
	if got := pub.snapshot(); len(got) != 0 {
		t.Fatalf("happy path emitted %d events, want 0: %+v", len(got), got)
	}

	// 9. Cache populated for next call.
	if v, ok := cache.Get(context.Background(), "random-no-tier-a.example", "Blog"); !ok {
		t.Error("cache not populated after fresh proposer call")
	} else if len(v) != 3 {
		t.Errorf("cache holds %d entries, want 3", len(v))
	}
}

// TestE2E_TierAStrategyShadowsJIT proves the negative side of the gate:
// when ANY platform/shape strategy claims, JIT is silenced — even though
// JIT.Applies always matches. Substrate's registry_test.go already covers
// this with a stub; this test verifies it holds for the real *jit.Strategy.
func TestE2E_TierAStrategyShadowsJIT(t *testing.T) {
	prop := &fakeProposer{reply: []string{"/should-not-fetch"}}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)
	exec := jit.NewExecutor(&pipelineFetcher{})
	jitStrat := jit.New(cp, exec, &recordingPub{}, jit.NoOutage(), fixedPageType{label: "Blog"})

	// A platform-family stub that claims everything at specificity 1.
	platform := &platformAlwaysMatches{}

	reg := lateral.NewRegistry()
	reg.Register(platform)
	reg.Register(jitStrat)

	chosen := reg.Dispatch(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://random-no-tier-a.example/post/1",
	})
	if len(chosen) != 1 {
		t.Fatalf("Dispatch returned %d, want 1", len(chosen))
	}
	if chosen[0].ID() == jit.StrategyID {
		t.Fatal("Dispatch returned JIT, but a Tier-A strategy claimed; JIT must be silenced")
	}
	// Proposer must not have been called — JIT never ran.
	if n := prop.calls.Load(); n != 0 {
		t.Errorf("proposer called %d times, want 0 (Tier-A claimed)", n)
	}
}

// TestE2E_FetchFailureEmitsSubpathFailedEndToEnd ties the failure-event
// path through a real Probe call: a per-path fetch error must surface as
// ctxt.lateral.subpath.failed while surviving candidates are still
// returned to the caller.
func TestE2E_FetchFailureEmitsSubpathFailedEndToEnd(t *testing.T) {
	prop := &fakeProposer{reply: []string{"/good", "/broken"}}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)
	exec := jit.NewExecutor(&pipelineFetcher{
		bodies: map[string]string{"https://acme.io/good": "ok"},
		errs:   map[string]error{"https://acme.io/broken": context.DeadlineExceeded},
	})
	pub := &recordingPub{}
	strategy := jit.New(cp, exec, pub, jit.NoOutage(), fixedPageType{label: "Blog"})

	reg := lateral.NewRegistry()
	reg.Register(strategy)

	candidates, err := reg.Dispatch(context.Background(), lateral.CapturedEvent{
		ObjectID:  "obj-1",
		SourceURL: "https://acme.io/post/1",
	})[0].Probe(context.Background(), lateral.CapturedEvent{
		ObjectID:  "obj-1",
		SourceURL: "https://acme.io/post/1",
	}, lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("Probe err = %v (per-path fetch err must NOT propagate)", err)
	}
	if len(candidates) != 1 || candidates[0].URL != "https://acme.io/good" {
		t.Fatalf("candidates = %+v, want one /good", candidates)
	}

	got := pub.snapshot()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1 (subpath.failed for /broken)", len(got))
	}
	if got[0].topic != string(events.SubpathFailed) {
		t.Errorf("topic = %q, want %q", got[0].topic, events.SubpathFailed)
	}
}

// platformAlwaysMatches is a minimal FamilyPlatform stub for shadow tests.
// Defined here (not in jit_test.go) because it's only used by the
// integration suite.
type platformAlwaysMatches struct{}

func (p *platformAlwaysMatches) ID() string                     { return "test.platform" }
func (p *platformAlwaysMatches) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (p *platformAlwaysMatches) Applies(_ context.Context, _ lateral.CapturedEvent) lateral.AppliesResult {
	return lateral.AppliesResult{Matches: true, Specificity: 1}
}
func (p *platformAlwaysMatches) Probe(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}
func (p *platformAlwaysMatches) Preconditions() []string { return nil }
