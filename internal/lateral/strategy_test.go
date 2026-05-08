package lateral

import (
	"context"
	"testing"
)

func TestLateralStrategy_InterfaceContract(t *testing.T) {
	var _ LateralStrategy = (*fakeStrategy)(nil)
}

type fakeStrategy struct{}

func (s *fakeStrategy) ID() string             { return "fake" }
func (s *fakeStrategy) Family() StrategyFamily { return FamilyPlatform }
func (s *fakeStrategy) Applies(ctx context.Context, ev CapturedEvent) AppliesResult {
	return AppliesResult{Matches: false, Specificity: 0}
}

func (s *fakeStrategy) Probe(ctx context.Context, ev CapturedEvent, ac ActiveContext) ([]Candidate, error) {
	return nil, nil
}
func (s *fakeStrategy) Preconditions() []string { return nil }
