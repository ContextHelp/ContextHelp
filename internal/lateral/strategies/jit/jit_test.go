package jit_test

import (
	"context"
	"testing"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

func TestStrategy_IDStable(t *testing.T) {
	s := jit.New()
	if got, want := s.ID(), jit.StrategyID; got != want {
		t.Fatalf("ID() = %q, want %q", got, want)
	}
	if jit.StrategyID != "jit" {
		t.Fatalf("StrategyID = %q, want %q", jit.StrategyID, "jit")
	}
}

func TestStrategy_Family(t *testing.T) {
	s := jit.New()
	if got, want := s.Family(), lateral.FamilyJIT; got != want {
		t.Fatalf("Family() = %v, want %v", got, want)
	}
}

func TestStrategy_AppliesAllURLs(t *testing.T) {
	cases := []struct {
		name string
		ev   lateral.CapturedEvent
	}{
		{"empty", lateral.CapturedEvent{}},
		{"github_repo", lateral.CapturedEvent{SourceURL: "https://github.com/samber/lo"}},
		{"random_blog", lateral.CapturedEvent{SourceURL: "https://example.com/post/123"}},
		{"non_http_scheme", lateral.CapturedEvent{SourceURL: "file:///tmp/note.md"}},
		{"with_namespace", lateral.CapturedEvent{Namespace: "@x.repo", SourceURL: "https://anything"}},
	}

	s := jit.New()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := s.Applies(context.Background(), tc.ev)
			if !res.Matches {
				t.Fatalf("Applies(%v).Matches = false, want true", tc.ev)
			}
			if res.Specificity != 0 {
				t.Fatalf("Applies(%v).Specificity = %d, want 0", tc.ev, res.Specificity)
			}
		})
	}
}

func TestStrategy_ProbeReturnsNoCandidates(t *testing.T) {
	s := jit.New()
	candidates, err := s.Probe(context.Background(), lateral.CapturedEvent{}, lateral.ActiveContext{})
	if err != nil {
		t.Fatalf("Probe() err = %v, want nil", err)
	}
	if candidates != nil {
		t.Fatalf("Probe() candidates = %v, want nil (skeleton)", candidates)
	}
}

func TestStrategy_PreconditionsEmpty(t *testing.T) {
	s := jit.New()
	if got := s.Preconditions(); got != nil {
		t.Fatalf("Preconditions() = %v, want nil", got)
	}
}

// TestStrategy_RegistersAtFamilyJIT proves the concrete *jit.Strategy lands in
// the JIT bucket of the substrate registry and is surfaced as the fallback
// when no Tier-A strategy claims an event. Substrate-level dispatch invariants
// are covered by internal/lateral/registry_test.go using stubs; this test
// verifies the real type satisfies the contract through that machinery.
func TestStrategy_RegistersAtFamilyJIT(t *testing.T) {
	reg := lateral.NewRegistry()
	reg.Register(jit.New())

	chosen := reg.Dispatch(context.Background(), lateral.CapturedEvent{
		SourceURL: "https://example.com/anything",
	})
	if len(chosen) != 1 {
		t.Fatalf("Dispatch returned %d strategies, want 1", len(chosen))
	}
	if chosen[0].ID() != jit.StrategyID {
		t.Fatalf("Dispatch returned %q, want %q", chosen[0].ID(), jit.StrategyID)
	}
	if chosen[0].Family() != lateral.FamilyJIT {
		t.Fatalf("Dispatched strategy family = %v, want FamilyJIT", chosen[0].Family())
	}
}
