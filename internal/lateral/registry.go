package lateral

import (
	"context"
	"fmt"
)

// Registry holds all registered strategies and dispatches matching ones for
// a given CapturedEvent. Within FamilyPlatform, the highest-specificity
// match wins. Within FamilyShape, all matches fire. JIT fires only when
// zero Tier-A strategies claim.
//
// Registry is not safe for concurrent Register/Dispatch; callers serialize.
type Registry struct {
	platforms []LateralStrategy
	shapes    []LateralStrategy
	jit       []LateralStrategy
}

// NewRegistry returns an empty Registry. Strategies must be added via Register.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds s to the family-specific bucket selected by s.Family().
// Panics on unknown family — fail loud at registration so missing dispatches
// never manifest silently in production.
func (r *Registry) Register(s LateralStrategy) {
	switch s.Family() {
	case FamilyPlatform:
		r.platforms = append(r.platforms, s)
	case FamilyShape:
		r.shapes = append(r.shapes, s)
	case FamilyJIT:
		r.jit = append(r.jit, s)
	default:
		panic(fmt.Sprintf("lateral: unknown family %v for strategy %s", s.Family(), s.ID()))
	}
}

// Dispatch returns the strategies that claim ev, ordered platform → shape → JIT.
// JIT is consulted only when no Tier-A (platform/shape) strategy matches.
func (r *Registry) Dispatch(ctx context.Context, ev CapturedEvent) []LateralStrategy {
	var out []LateralStrategy

	// Platform: highest-specificity match wins.
	var bestPlatform LateralStrategy
	bestSpec := -1
	for _, s := range r.platforms {
		res := s.Applies(ctx, ev)
		if !res.Matches {
			continue
		}
		if res.Specificity > bestSpec {
			bestPlatform = s
			bestSpec = res.Specificity
		}
	}
	if bestPlatform != nil {
		out = append(out, bestPlatform)
	}

	// Shape: all matches fire.
	for _, s := range r.shapes {
		if s.Applies(ctx, ev).Matches {
			out = append(out, s)
		}
	}

	// JIT: only if zero Tier-A claimed.
	if len(out) == 0 {
		for _, s := range r.jit {
			if s.Applies(ctx, ev).Matches {
				out = append(out, s)
			}
		}
	}

	return out
}
