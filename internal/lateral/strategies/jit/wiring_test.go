package jit_test

import (
	"context"
	"testing"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

func newTestDeps(reply []string) jit.Deps {
	return jit.Deps{
		Proposer:   &fakeProposer{reply: reply},
		Cache:      jit.NewMemoryProposalCache(),
		Fetcher:    &pipelineFetcher{bodies: map[string]string{}},
		Publisher:  &recordingPub{},
		Outage:     jit.NoOutage(),
		Classifier: fixedPageType{label: "Blog"},
	}
}

func TestRegister_DisabledSkipsRegistration(t *testing.T) {
	reg := lateral.NewRegistry()
	got, ok := jit.Register(reg, jit.Config{Enabled: false}, newTestDeps([]string{"/about"}))
	if ok {
		t.Fatal("ok = true on disabled config; want false")
	}
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}

	// Registry must be empty — Dispatch returns nothing.
	chosen := reg.Dispatch(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://x.example/y",
	})
	if len(chosen) != 0 {
		t.Fatalf("disabled JIT still dispatched %d strategies", len(chosen))
	}
}

func TestRegister_EnabledRegisters(t *testing.T) {
	reg := lateral.NewRegistry()
	deps := newTestDeps([]string{"/about", "/team"})

	// Pre-fill fetcher bodies so candidates emit.
	f := deps.Fetcher.(*pipelineFetcher)
	f.bodies = map[string]string{
		"https://x.example/about": "ok",
		"https://x.example/team":  "ok",
	}

	strategy, ok := jit.Register(reg, jit.Config{Enabled: true}, deps)
	if !ok {
		t.Fatal("ok = false on enabled config; want true")
	}
	if strategy == nil {
		t.Fatal("strategy = nil on successful registration")
	}
	if strategy.ID() != jit.StrategyID {
		t.Errorf("strategy.ID() = %q, want %q", strategy.ID(), jit.StrategyID)
	}

	// Registry now has one strategy that fires as JIT fallback.
	chosen := reg.Dispatch(context.Background(), lateral.CapturedEvent{
		ObjectID:  "obj-1",
		SourceURL: "https://x.example/post/1",
	})
	if len(chosen) != 1 || chosen[0].ID() != jit.StrategyID {
		t.Fatalf("Dispatch returned %v, want [jit]", chosen)
	}

	// Probe runs the wired pipeline end-to-end.
	cands, err := chosen[0].Probe(context.Background(), lateral.CapturedEvent{
		ObjectID:  "obj-1",
		SourceURL: "https://x.example/post/1",
	}, lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("Probe err = %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates = %d, want 2", len(cands))
	}
}

func TestRegister_OptionalDepsDefaultedSafely(t *testing.T) {
	reg := lateral.NewRegistry()
	deps := jit.Deps{
		Proposer: &fakeProposer{reply: []string{"/about"}},
		Cache:    jit.NewMemoryProposalCache(),
		Fetcher:  &pipelineFetcher{bodies: map[string]string{"https://x.example/about": "ok"}},
		// Publisher, Outage, Classifier all nil.
	}

	strategy, ok := jit.Register(reg, jit.Config{Enabled: true}, deps)
	if !ok {
		t.Fatal("ok = false")
	}

	// Probe must run without panicking even with nil optional deps.
	cands, err := strategy.Probe(context.Background(), lateral.CapturedEvent{
		ObjectID:  "obj-1",
		SourceURL: "https://x.example/post/1",
	}, lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("Probe err = %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
}

func TestRegister_PanicsOnMissingProposer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on missing Proposer")
		}
	}()
	reg := lateral.NewRegistry()
	deps := jit.Deps{
		Cache:   jit.NewMemoryProposalCache(),
		Fetcher: &pipelineFetcher{},
	}
	_, _ = jit.Register(reg, jit.Config{Enabled: true}, deps)
}

func TestRegister_PanicsOnMissingCache(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on missing Cache")
		}
	}()
	reg := lateral.NewRegistry()
	deps := jit.Deps{
		Proposer: &fakeProposer{},
		Fetcher:  &pipelineFetcher{},
	}
	_, _ = jit.Register(reg, jit.Config{Enabled: true}, deps)
}

func TestRegister_PanicsOnMissingFetcher(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on missing Fetcher")
		}
	}()
	reg := lateral.NewRegistry()
	deps := jit.Deps{
		Proposer: &fakeProposer{},
		Cache:    jit.NewMemoryProposalCache(),
	}
	_, _ = jit.Register(reg, jit.Config{Enabled: true}, deps)
}

func TestRegister_DisabledDoesNotPanicOnNilDeps(t *testing.T) {
	// When disabled, missing required deps are tolerated — the gate prevents
	// the deps from being touched.
	reg := lateral.NewRegistry()
	got, ok := jit.Register(reg, jit.Config{Enabled: false}, jit.Deps{})
	if ok {
		t.Fatal("ok = true on disabled config")
	}
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

func TestConfig_DefaultDisabled(t *testing.T) {
	// Zero-value Config must mean disabled — opt-in semantics.
	if (jit.Config{}).Enabled {
		t.Fatal("Config zero-value Enabled = true; want false (opt-in)")
	}
}
