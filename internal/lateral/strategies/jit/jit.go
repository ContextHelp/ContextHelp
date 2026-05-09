// Package jit implements the catch-all FamilyJIT strategy. It fires only when
// no Tier-A platform/shape strategy claims a CapturedEvent (see Registry.Dispatch).
//
// The skeleton lives here at specificity 0 with always-true Applies. The real
// proposal/execute pipeline is wired in subsequent tasks:
//   - proposal prompt and recipe cache (T-0244, T-0245)
//   - sub-path executor + candidate emission (T-0246, T-0247)
//   - failure + LLM-outage event paths (T-0248, T-0249)
package jit

import (
	"context"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// StrategyID is the registry-stable identifier for the JIT strategy.
const StrategyID = "jit"

// Strategy is the FamilyJIT catch-all. It is registered last and consulted
// only when zero Tier-A strategies match the inbound event.
type Strategy struct{}

// New returns a zero-value Strategy. Real construction (with proposer, cache,
// executor) lands when those collaborators exist.
func New() *Strategy { return &Strategy{} }

func (s *Strategy) ID() string                     { return StrategyID }
func (s *Strategy) Family() lateral.StrategyFamily { return lateral.FamilyJIT }

// Applies always matches at specificity 0. Registry gating ensures JIT only
// fires when no platform/shape strategy claimed the event.
func (s *Strategy) Applies(_ context.Context, _ lateral.CapturedEvent) lateral.AppliesResult {
	return lateral.AppliesResult{Matches: true, Specificity: 0}
}

// Probe returns no candidates in the skeleton. Real implementation is wired
// in T-0246 once the proposer (T-0244) and recipe cache (T-0245) exist.
func (s *Strategy) Probe(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return nil, nil
}

// Preconditions: JIT runs from the captured event alone, no parent record fields required.
func (s *Strategy) Preconditions() []string { return nil }
