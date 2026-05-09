// internal/lateral/properties_test.go
package lateral_test

import (
	"context"
	"testing"
	"testing/quick"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// matchAll is a minimal Strategy that always matches with caller-controlled
// family + specificity. Used for testing the registry's dispatch invariants
// without bringing in real strategy code.
type matchAll struct {
	id     string
	family lateral.StrategyFamily
	spec   int
}

func (s *matchAll) ID() string                     { return s.id }
func (s *matchAll) Family() lateral.StrategyFamily { return s.family }

func (s *matchAll) Applies(_ context.Context, _ lateral.CapturedEvent) lateral.AppliesResult {
	return lateral.AppliesResult{Matches: true, Specificity: s.spec}
}

func (s *matchAll) Probe(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}

func (s *matchAll) Preconditions() []string { return nil }

// Within FamilyPlatform, the highest-specificity matching strategy wins
// regardless of registration order. testing/quick generates the parent's
// specificity and the child's offset; child specificity = parent + offset + 1
// (always strictly greater).
func TestProperty_ChildAlwaysShadowsParent(t *testing.T) {
	if err := quick.Check(func(parentSpec, childExtra uint8) bool {
		ps := int(parentSpec) + 1
		cs := ps + int(childExtra) + 1
		reg := lateral.NewRegistry()
		reg.Register(&matchAll{id: "parent", family: lateral.FamilyPlatform, spec: ps})
		reg.Register(&matchAll{id: "child", family: lateral.FamilyPlatform, spec: cs})
		got := reg.Dispatch(context.Background(), lateral.CapturedEvent{})
		return len(got) == 1 && got[0].ID() == "child"
	}, nil); err != nil {
		t.Fatal(err)
	}
}

// FamilyShape strategies dispatch in parallel: registering N matching shapes
// should produce N dispatched strategies.
func TestProperty_AllShapesFire(t *testing.T) {
	if err := quick.Check(func(n uint8) bool {
		nshapes := int(n%5) + 1
		reg := lateral.NewRegistry()
		for i := 0; i < nshapes; i++ {
			reg.Register(&matchAll{id: "s", family: lateral.FamilyShape, spec: 1})
		}
		got := reg.Dispatch(context.Background(), lateral.CapturedEvent{})
		return len(got) == nshapes
	}, nil); err != nil {
		t.Fatal(err)
	}
}

// FamilyJIT only fires when no Tier-A strategy claimed the event. With a
// JIT registered, dispatch should include it iff no Platform/Shape strategy
// also matched.
func TestProperty_JITOnlyWhenNoTierA(t *testing.T) {
	if err := quick.Check(func(hasTierA bool) bool {
		reg := lateral.NewRegistry()
		reg.Register(&matchAll{id: "jit", family: lateral.FamilyJIT, spec: 0})
		if hasTierA {
			reg.Register(&matchAll{id: "tier-a", family: lateral.FamilyPlatform, spec: 1})
		}
		got := reg.Dispatch(context.Background(), lateral.CapturedEvent{})
		jitFired := false
		for _, s := range got {
			if s.ID() == "jit" {
				jitFired = true
			}
		}
		// JIT fires iff there's no Tier-A — so jitFired should be the
		// negation of hasTierA.
		return jitFired != hasTierA
	}, nil); err != nil {
		t.Fatal(err)
	}
}
