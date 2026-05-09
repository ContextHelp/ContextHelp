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

// TestCapturedEvent_HintsZeroValue pins T-0306's backward-compat contract:
// a CapturedEvent without explicit Hints carries the zero Hints, which
// strategies treat as "no page-side signals available."
func TestCapturedEvent_HintsZeroValue(t *testing.T) {
	ev := CapturedEvent{SourceURL: "https://example.com"}
	if ev.Hints != (Hints{}) {
		t.Fatalf("zero CapturedEvent has non-zero Hints: %#v", ev.Hints)
	}
}

// TestCapturedEvent_HintsRoundTrip pins that Hints set on the event
// reach a downstream consumer untouched.
func TestCapturedEvent_HintsRoundTrip(t *testing.T) {
	want := Hints{
		Generator:     "Substack",
		MetaPlatform:  "substack",
		CanonicalHost: "newsletter.example.com.substack.com",
	}
	ev := CapturedEvent{SourceURL: "https://example.com", Hints: want}
	if ev.Hints != want {
		t.Fatalf("Hints mutated through CapturedEvent: got %#v want %#v", ev.Hints, want)
	}
}
