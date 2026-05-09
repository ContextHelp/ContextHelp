package jit_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// fakeProposer counts Propose calls and returns the canned reply.
type fakeProposer struct {
	calls atomic.Int64
	reply []string
	err   error
}

func (f *fakeProposer) Propose(_ context.Context, _, _ string) ([]string, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.reply, nil
}

func TestCachedProposer_CacheHitSkipsProposer(t *testing.T) {
	p := &fakeProposer{reply: []string{"/about", "/team"}}
	c := jit.NewMemoryProposalCache()
	c.Put(context.Background(), "blog.acme.io", "post", []string{"/cached/1", "/cached/2"})

	cp := jit.NewCachedProposer(p, c)

	got, err := cp.Propose(context.Background(), "blog.acme.io", "post")
	if err != nil {
		t.Fatalf("Propose err = %v, want nil", err)
	}
	if want := []string{"/cached/1", "/cached/2"}; !equalStringSlice(got, want) {
		t.Fatalf("Propose = %v, want %v (cached)", got, want)
	}
	if n := p.calls.Load(); n != 0 {
		t.Fatalf("proposer calls = %d, want 0 (cache hit must short-circuit)", n)
	}
}

func TestCachedProposer_CacheMissCallsProposerAndStores(t *testing.T) {
	p := &fakeProposer{reply: []string{"/a", "/b"}}
	c := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(p, c)

	got, err := cp.Propose(context.Background(), "x.io", "doc")
	if err != nil {
		t.Fatalf("Propose err = %v", err)
	}
	if want := []string{"/a", "/b"}; !equalStringSlice(got, want) {
		t.Fatalf("first Propose = %v, want %v", got, want)
	}
	if n := p.calls.Load(); n != 1 {
		t.Fatalf("first call: proposer calls = %d, want 1", n)
	}

	// Second call must serve from cache without invoking the proposer again.
	got2, err := cp.Propose(context.Background(), "x.io", "doc")
	if err != nil {
		t.Fatalf("second Propose err = %v", err)
	}
	if !equalStringSlice(got2, got) {
		t.Fatalf("second Propose = %v, want %v (same as cached)", got2, got)
	}
	if n := p.calls.Load(); n != 1 {
		t.Fatalf("after second call: proposer calls = %d, want 1 (still)", n)
	}

	// Direct cache check — Put happened.
	if v, ok := c.Get(context.Background(), "x.io", "doc"); !ok || !equalStringSlice(v, got) {
		t.Fatalf("cache.Get = (%v, %v), want (%v, true)", v, ok, got)
	}
}

func TestCachedProposer_EmptyProposerVerdictNotCached(t *testing.T) {
	p := &fakeProposer{reply: nil}
	c := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(p, c)

	got, err := cp.Propose(context.Background(), "x.io", "doc")
	if err != nil {
		t.Fatalf("Propose err = %v", err)
	}
	if got != nil {
		t.Fatalf("Propose = %v, want nil", got)
	}
	if _, ok := c.Get(context.Background(), "x.io", "doc"); ok {
		t.Fatal("empty verdict was cached; want NOT cached so next call retries")
	}

	// Next call: proposer must be re-invoked since nothing was cached.
	_, _ = cp.Propose(context.Background(), "x.io", "doc")
	if n := p.calls.Load(); n != 2 {
		t.Fatalf("proposer calls after retry = %d, want 2 (empty verdict must not stick)", n)
	}
}

func TestCachedProposer_ProposerErrorPropagates(t *testing.T) {
	wantErr := errors.New("llm down")
	p := &fakeProposer{err: wantErr}
	c := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(p, c)

	got, err := cp.Propose(context.Background(), "x.io", "doc")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if got != nil {
		t.Fatalf("Propose = %v, want nil on error", got)
	}
	if _, ok := c.Get(context.Background(), "x.io", "doc"); ok {
		t.Fatal("error result was cached; want NOT cached")
	}
}

// promptRoundTripProposer simulates the production wiring: it takes the
// (domain, pageType), builds the prompt via BuildPrompt, hands it to a
// canned LLM transport, then parses the response via ParseProposalResponse.
// This proves the prompt builder + parser compose into a working Proposer.
type promptRoundTripProposer struct {
	transport func(prompt string) string
}

func (p *promptRoundTripProposer) Propose(_ context.Context, domain, pageType string) ([]string, error) {
	prompt := jit.BuildPrompt(jit.ProposalRequest{SourceDomain: domain, PageType: pageType})
	raw := p.transport(prompt)
	return jit.ParseProposalResponse(raw), nil
}

func TestCachedProposer_PromptRoundTrip(t *testing.T) {
	var seenPrompt string
	transport := func(prompt string) string {
		seenPrompt = prompt
		// Realistic LLM output: bullets, blank line, comment, duplicate.
		return "- /about\n* /team\n\n# extra context\n/about\n/blog\n"
	}

	p := &promptRoundTripProposer{transport: transport}
	c := jit.NewMemoryProposalCache()
	cp := jit.NewCachedProposer(p, c)

	got, err := cp.Propose(context.Background(), "blog.acme.io", "post")
	if err != nil {
		t.Fatalf("Propose err = %v", err)
	}

	// Parser cleaned bullets, dropped blank + comment + duplicate.
	want := []string{"/about", "/team", "/blog"}
	if !equalStringSlice(got, want) {
		t.Fatalf("round-trip Propose = %v, want %v", got, want)
	}

	// Prompt the transport saw must contain both inputs.
	if !strings.Contains(seenPrompt, "blog.acme.io") {
		t.Fatal("prompt missing SourceDomain")
	}
	if !strings.Contains(seenPrompt, "post") {
		t.Fatal("prompt missing PageType")
	}

	// And it cached the cleaned slice.
	if v, ok := c.Get(context.Background(), "blog.acme.io", "post"); !ok || !equalStringSlice(v, want) {
		t.Fatalf("cache after round-trip: (%v, %v), want (%v, true)", v, ok, want)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
