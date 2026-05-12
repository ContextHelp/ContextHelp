package lateral

import (
	"context"
	"testing"
)

type stubStrategy struct {
	id          string
	family      StrategyFamily
	matches     bool
	specificity int
}

func (s *stubStrategy) ID() string             { return s.id }
func (s *stubStrategy) Family() StrategyFamily { return s.family }
func (s *stubStrategy) Applies(ctx context.Context, ev CapturedEvent) AppliesResult {
	return AppliesResult{Matches: s.matches, Specificity: s.specificity}
}

func (s *stubStrategy) Probe(ctx context.Context, ev CapturedEvent, ac ActiveContext) ([]Candidate, error) {
	return nil, nil
}
func (s *stubStrategy) Preconditions() []string { return nil }

func TestRegistry_DispatchPlatformChildShadowsParent(t *testing.T) {
	parent := &stubStrategy{id: "github", family: FamilyPlatform, matches: true, specificity: 1}
	child := &stubStrategy{id: "github.gist", family: FamilyPlatform, matches: true, specificity: 2}

	reg := NewRegistry()
	reg.Register(parent)
	reg.Register(child)

	got := reg.Dispatch(context.Background(), CapturedEvent{})
	if len(got) != 1 || got[0].ID() != "github.gist" {
		t.Fatalf("expected only github.gist (highest specificity), got %v", strategyIDs(got))
	}
}

func TestRegistry_DispatchShapeFamilyAllFire(t *testing.T) {
	a := &stubStrategy{id: "product", family: FamilyShape, matches: true, specificity: 1}
	b := &stubStrategy{id: "landing", family: FamilyShape, matches: true, specificity: 1}
	c := &stubStrategy{id: "pricing", family: FamilyShape, matches: false}

	reg := NewRegistry()
	reg.Register(a)
	reg.Register(b)
	reg.Register(c)

	got := reg.Dispatch(context.Background(), CapturedEvent{})
	if len(got) != 2 {
		t.Fatalf("expected 2 shape strategies firing, got %d (%v)", len(got), strategyIDs(got))
	}
}

func TestRegistry_DispatchJITOnlyWhenZeroTierA(t *testing.T) {
	jit := &stubStrategy{id: "jit", family: FamilyJIT, matches: true, specificity: 0}
	reg := NewRegistry()
	reg.Register(jit)

	got := reg.Dispatch(context.Background(), CapturedEvent{})
	if len(got) != 1 || got[0].ID() != "jit" {
		t.Fatalf("expected jit to fire when no Tier-A claims, got %v", strategyIDs(got))
	}

	tierA := &stubStrategy{id: "github", family: FamilyPlatform, matches: true, specificity: 1}
	reg.Register(tierA)
	got = reg.Dispatch(context.Background(), CapturedEvent{})
	for _, s := range got {
		if s.Family() == FamilyJIT {
			t.Fatalf("jit must not fire when Tier-A claimed: %v", strategyIDs(got))
		}
	}
}

func strategyIDs(ss []LateralStrategy) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.ID()
	}
	return out
}
