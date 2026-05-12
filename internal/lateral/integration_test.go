package lateral_test

import (
	"context"
	"testing"
	"time"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/capgate"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/identity"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/materialize"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/scoring"
)

type fakeStrategy struct{ id string }

func (s *fakeStrategy) ID() string                     { return s.id }
func (s *fakeStrategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }

func (s *fakeStrategy) Applies(_ context.Context, _ lateral.CapturedEvent) lateral.AppliesResult {
	return lateral.AppliesResult{Matches: true, Specificity: 1}
}

func (s *fakeStrategy) Probe(_ context.Context, _ lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	return []lateral.Candidate{
		{URL: "https://example.com/sibling-1", CandidateType: "sibling_repo", Strategy: s.id, Preview: map[string]any{"topics": []string{"go"}}},
		{URL: "https://example.com/sibling-2", CandidateType: "sibling_repo", Strategy: s.id, Preview: map[string]any{"topics": []string{"go"}}},
	}, nil
}

func (s *fakeStrategy) Preconditions() []string { return nil }

type fakeEva struct{}

func (fakeEva) Cosine(_ context.Context, _, _ map[string]float64) (float64, error) {
	return 0.8, nil
}

// TODO: replace with kit's domain.MockRepository (kit/runtime/domain) once the
// real adapter lands; the fake will desynchronise otherwise.
type fakeGraph struct{}

func (fakeGraph) FindByURL(_ context.Context, _ string) (string, error) {
	return "", identity.ErrNotFound
}

func (fakeGraph) FindByIdentityKey(_ context.Context, _ string) (string, error) {
	return "", identity.ErrNotFound
}

// TODO: replace with kit's domain.MockRepository (kit/runtime/domain) once the
// real adapter lands; the fake will desynchronise otherwise.
type fakeStore struct{ records int }

func (s *fakeStore) WriteRecord(_ context.Context, _ map[string]any) (string, error) {
	s.records++
	return "o-lc", nil
}

func (s *fakeStore) WriteEdge(_ context.Context, _ materialize.Edge) error { return nil }

func TestE2E_DispatchScoreCapResolveMaterialize(t *testing.T) {
	reg := lateral.NewRegistry()
	reg.Register(&fakeStrategy{id: "fake"})

	ev := lateral.CapturedEvent{
		ObjectID:        "o-parent",
		Namespace:       "@x.repo",
		SourceURL:       "https://example.com/parent",
		CapturePipeline: "test",
	}
	chosen := reg.Dispatch(context.Background(), ev)
	if len(chosen) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(chosen))
	}

	ac := lateral.ActiveContext{
		SessionTopic:  map[string]float64{"go": 1},
		CaptureWindow: map[string]float64{"cli": 1},
	}
	candidates, err := chosen[0].Probe(context.Background(), ev, ac)
	if err != nil {
		t.Fatal(err)
	}

	scorer := scoring.NewScorer(fakeEva{}, scoring.DefaultWeights())
	gate := capgate.NewGate(capgate.Config{
		ThresholdsByType: map[string]float64{"sibling_repo": 0.45},
		CapKByType:       map[string]int{"sibling_repo": 1},
	})

	scored := make([]capgate.Scored, 0, len(candidates))
	for _, c := range candidates {
		r, err := scorer.Score(context.Background(), c, ac)
		if err != nil {
			t.Fatal(err)
		}
		scored = append(scored, capgate.Scored{
			URL:   c.URL,
			Type:  c.CandidateType,
			Score: r.Score,
		})
	}
	survivors := gate.Filter(scored)
	if len(survivors) != 1 {
		t.Fatalf("expected 1 survivor (cap_k=1), got %d", len(survivors))
	}

	resolver := identity.NewResolver(fakeGraph{})
	res, err := resolver.Resolve(context.Background(), identity.Candidate{URL: survivors[0].URL})
	if err != nil {
		t.Fatal(err)
	}

	st := &fakeStore{}
	m := materialize.New(st)
	out, err := m.Materialize(context.Background(), materialize.Input{
		ParentID:       "o-parent",
		URL:            survivors[0].URL,
		Title:          survivors[0].URL,
		CandidateType:  survivors[0].Type,
		Strategy:       "fake",
		Resolution:     res,
		ParentLifespan: 180 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.EdgeOnly {
		t.Fatal("expected probationary path")
	}
	if st.records != 1 {
		t.Fatalf("expected 1 record written, got %d", st.records)
	}
}
