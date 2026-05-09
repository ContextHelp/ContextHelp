package jit_test

import (
	"context"
	"errors"
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/events"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// stubOutage is a fixed-state OutageDetector for tests.
type stubOutage struct{ outage bool }

func (s stubOutage) IsOutage() bool { return s.outage }

func TestNoOutage_AlwaysFalse(t *testing.T) {
	d := jit.NoOutage()
	if d.IsOutage() {
		t.Fatal("NoOutage().IsOutage() = true, want false")
	}
}

// Truth table for ProposeWithOutageFallback:
//
//	cache hit + outage   → (cached, true,  nil)
//	cache hit + healthy  → (cached, false, nil)
//	miss + proposer ok   → (fresh,  false, nil) — cache populated
//	miss + proposer err  → (nil,    false, err) — no fallback
func TestProposeWithOutageFallback_CacheHitDuringOutage(t *testing.T) {
	p := &fakeProposer{}
	c := jit.NewMemoryProposalCache()
	c.Put(context.Background(), "acme.io", "post", []string{"/cached"})
	cp := jit.NewCachedProposer(p, c)

	got, served, err := cp.ProposeWithOutageFallback(context.Background(), "acme.io", "post", stubOutage{outage: true})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !served {
		t.Fatal("served = false, want true (cache hit during outage)")
	}
	if !equalStringSlice(got, []string{"/cached"}) {
		t.Fatalf("got = %v, want [/cached]", got)
	}
	if n := p.calls.Load(); n != 0 {
		t.Fatalf("proposer called %d times, want 0 (cache hit)", n)
	}
}

func TestProposeWithOutageFallback_CacheHitHealthy(t *testing.T) {
	p := &fakeProposer{}
	c := jit.NewMemoryProposalCache()
	c.Put(context.Background(), "acme.io", "post", []string{"/cached"})
	cp := jit.NewCachedProposer(p, c)

	got, served, err := cp.ProposeWithOutageFallback(context.Background(), "acme.io", "post", stubOutage{outage: false})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if served {
		t.Fatal("served = true on healthy cache hit; want false")
	}
	if !equalStringSlice(got, []string{"/cached"}) {
		t.Fatalf("got = %v, want [/cached]", got)
	}
}

func TestProposeWithOutageFallback_NilDetectorTreatedAsHealthy(t *testing.T) {
	p := &fakeProposer{}
	c := jit.NewMemoryProposalCache()
	c.Put(context.Background(), "acme.io", "post", []string{"/cached"})
	cp := jit.NewCachedProposer(p, c)

	_, served, err := cp.ProposeWithOutageFallback(context.Background(), "acme.io", "post", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if served {
		t.Fatal("served = true with nil detector; want false (treat as healthy)")
	}
}

func TestProposeWithOutageFallback_CacheMissProposerOK(t *testing.T) {
	p := &fakeProposer{reply: []string{"/fresh"}}
	c := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(p, c)

	got, served, err := cp.ProposeWithOutageFallback(context.Background(), "acme.io", "post", stubOutage{outage: true})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if served {
		t.Fatal("served = true on cache miss; want false (fresh fetch isn't a recipe-served event)")
	}
	if !equalStringSlice(got, []string{"/fresh"}) {
		t.Fatalf("got = %v, want [/fresh]", got)
	}
	if v, ok := c.Get(context.Background(), "acme.io", "post"); !ok || !equalStringSlice(v, []string{"/fresh"}) {
		t.Fatalf("cache Get = (%v, %v), want ([/fresh], true)", v, ok)
	}
}

func TestProposeWithOutageFallback_CacheMissProposerErr(t *testing.T) {
	wantErr := errors.New("llm down and cache empty")
	p := &fakeProposer{err: wantErr}
	c := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(p, c)

	got, served, err := cp.ProposeWithOutageFallback(context.Background(), "acme.io", "post", stubOutage{outage: true})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if served {
		t.Fatal("served = true on cache miss + proposer err; want false")
	}
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

func TestEmitRecipeServed_NilPubSafe(t *testing.T) {
	if err := jit.EmitRecipeServed(context.Background(), nil, "acme.io", "post"); err != nil {
		t.Fatalf("nil pub returned err = %v", err)
	}
}

func TestEmitRecipeServed_TopicAndCircumstance(t *testing.T) {
	pub := &recordingPub{}
	if err := jit.EmitRecipeServed(context.Background(), pub, "acme.io", "post"); err != nil {
		t.Fatalf("Publish err = %v", err)
	}
	got := pub.snapshot()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	ev := got[0]
	if ev.topic != string(events.RecipeServed) {
		t.Errorf("topic = %q, want %q", ev.topic, events.RecipeServed)
	}
	payload, ok := ev.payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", ev.payload)
	}
	if payload["source_domain"] != "acme.io" {
		t.Errorf("source_domain = %v, want acme.io", payload["source_domain"])
	}
	if payload["page_type"] != "post" {
		t.Errorf("page_type = %v, want post", payload["page_type"])
	}
	if payload["severity"] != "info" {
		t.Errorf("severity = %v, want info", payload["severity"])
	}
	q, ok := payload["qualifiers"].(bus.Qualifiers)
	if !ok {
		t.Fatalf("qualifiers type = %T", payload["qualifiers"])
	}
	if q.Circumstance != "llm_outage" {
		t.Errorf("Circumstance = %q, want llm_outage", q.Circumstance)
	}
}

func TestPipeline_RecipeServedOnOutageHit(t *testing.T) {
	prop := &fakeProposer{}
	cache := jit.NewMemoryProposalCache()
	cache.Put(context.Background(), "acme.io", "post", []string{"/cached"})
	cp := jit.NewCachedProposer(prop, cache)

	exec := jit.NewExecutor(&pipelineFetcher{
		bodies: map[string]string{"https://acme.io/cached": "ok"},
	})

	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, stubOutage{outage: true})

	cands, err := p.RunOnce(context.Background(), "obj-1", "https://acme.io/source", "acme.io", "post")
	if err != nil {
		t.Fatalf("RunOnce err = %v", err)
	}
	if len(cands) != 1 || cands[0].URL != "https://acme.io/cached" {
		t.Fatalf("candidates = %+v, want one /cached", cands)
	}
	if n := prop.calls.Load(); n != 0 {
		t.Errorf("proposer called %d times, want 0 (cache hit)", n)
	}

	got := pub.snapshot()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1 (recipe.served)", len(got))
	}
	if got[0].topic != string(events.RecipeServed) {
		t.Fatalf("topic = %q, want %q", got[0].topic, events.RecipeServed)
	}
}

func TestPipeline_NoRecipeServedOnHealthyHit(t *testing.T) {
	prop := &fakeProposer{}
	cache := jit.NewMemoryProposalCache()
	cache.Put(context.Background(), "acme.io", "post", []string{"/cached"})
	cp := jit.NewCachedProposer(prop, cache)

	exec := jit.NewExecutor(&pipelineFetcher{
		bodies: map[string]string{"https://acme.io/cached": "ok"},
	})

	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, jit.NoOutage())

	_, err := p.RunOnce(context.Background(), "obj-1", "https://acme.io/source", "acme.io", "post")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := pub.snapshot(); len(got) != 0 {
		t.Fatalf("healthy cache hit emitted %d events, want 0", len(got))
	}
}

func TestPipeline_NoRecipeServedOnFreshFetch(t *testing.T) {
	// Outage true but cache miss → proposer succeeds → fresh fetch.
	// recipe.served must NOT fire (the served value didn't come from cache).
	prop := &fakeProposer{reply: []string{"/fresh"}}
	cache := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(prop, cache)

	exec := jit.NewExecutor(&pipelineFetcher{
		bodies: map[string]string{"https://acme.io/fresh": "ok"},
	})

	pub := &recordingPub{}
	p := jit.NewPipeline(cp, exec, pub, stubOutage{outage: true})

	_, err := p.RunOnce(context.Background(), "obj-1", "https://acme.io/source", "acme.io", "post")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := pub.snapshot()
	if len(got) != 0 {
		t.Fatalf("fresh fetch with outage=true emitted %d events, want 0 (served value came from proposer, not cache)", len(got))
	}
}
