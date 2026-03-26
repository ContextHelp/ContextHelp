package ranking_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ranking"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/uri"
)

// stubEdges is a minimal EdgeCounter for tests.
type stubEdges struct {
	// inbound maps object ID → inbound edge count
	inbound map[string]int
}

func (s *stubEdges) CountMentionsTo(_ context.Context, _, id string) (int, error) {
	return s.inbound[id], nil
}

func makeObj(id string, mentions ...string) *storage.KnowledgeObject {
	ms := make([]uri.URI, 0, len(mentions))
	for _, m := range mentions {
		// mentions are formatted as "space.slug" — split on first dot.
		space := "ns"
		slug := m
		for i, ch := range m {
			if ch == '.' {
				space = m[:i]
				slug = m[i+1:]
				break
			}
		}
		ms = append(ms, uri.URI{Scheme: "ctxt", Space: space, ID: slug})
	}
	return &storage.KnowledgeObject{
		ID:        id,
		Type:      "text",
		Mentions:  ms,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// TestRerank_EmptyCandidates returns empty result without error.
func TestRerank_EmptyCandidates(t *testing.T) {
	r := ranking.New(&stubEdges{}, ranking.DefaultWeights())
	results, err := r.Rerank(context.Background(), nil, 0)
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestRerank_FTSScorePreserved checks that RRF contributions pass through.
func TestRerank_FTSScorePreserved(t *testing.T) {
	edges := &stubEdges{inbound: map[string]int{}}
	r := ranking.New(edges, ranking.DefaultWeights())

	candidates := map[string]ranking.Candidate{
		"a": {Object: makeObj("a"), FTSScore: 0.3, VecScore: 0.0},
		"b": {Object: makeObj("b"), FTSScore: 0.1, VecScore: 0.0},
	}

	results, err := r.Rerank(context.Background(), candidates, 0)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// "a" should rank first (higher FTS score).
	assert.Equal(t, "a", results[0].Object.ID)
	assert.InDelta(t, 0.3, results[0].FTS, 1e-9)
	assert.Equal(t, "b", results[1].Object.ID)
}

// TestRerank_MentionBoostApplied verifies outbound mention bonus.
func TestRerank_MentionBoostApplied(t *testing.T) {
	edges := &stubEdges{inbound: map[string]int{}}
	w := ranking.DefaultWeights() // MentionBoost = 0.05 per mention
	r := ranking.New(edges, w)

	// "a" has 2 mentions → +0.10 bonus; "b" has none.
	candidates := map[string]ranking.Candidate{
		"a": {Object: makeObj("a", "ns.foo", "ns.bar"), FTSScore: 0.1},
		"b": {Object: makeObj("b"), FTSScore: 0.1},
	}

	results, err := r.Rerank(context.Background(), candidates, 0)
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, "a", results[0].Object.ID)
	assert.InDelta(t, 0.10, results[0].MentionBoost, 1e-9)
	assert.Equal(t, 0.0, results[1].MentionBoost)
}

// TestRerank_MentionBoostCapped verifies the MaxMentionBoost ceiling.
func TestRerank_MentionBoostCapped(t *testing.T) {
	edges := &stubEdges{inbound: map[string]int{}}
	w := ranking.WeightConfig{
		MentionBoost:    0.5,
		MaxMentionBoost: 1.0,
		DirectBacklink:  0.0,
		HopBacklink:     0.0,
	}
	r := ranking.New(edges, w)

	// 5 mentions × 0.5 = 2.5, but cap is 1.0.
	candidates := map[string]ranking.Candidate{
		"a": {Object: makeObj("a", "n.a", "n.b", "n.c", "n.d", "n.e"), FTSScore: 0.0},
	}

	results, err := r.Rerank(context.Background(), candidates, 0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 1.0, results[0].MentionBoost, 1e-9)
}

// TestRerank_DirectBacklinkBoost verifies inbound edge bonus.
func TestRerank_DirectBacklinkBoost(t *testing.T) {
	edges := &stubEdges{inbound: map[string]int{"x": 3}}
	w := ranking.DefaultWeights()
	r := ranking.New(edges, w)

	candidates := map[string]ranking.Candidate{
		"x": {Object: makeObj("x"), FTSScore: 0.1},
		"y": {Object: makeObj("y"), FTSScore: 0.1},
	}

	results, err := r.Rerank(context.Background(), candidates, 0)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// "x" has direct backlinks → ranks first.
	assert.Equal(t, "x", results[0].Object.ID)
	assert.InDelta(t, w.DirectBacklink, results[0].GraphRelevance, 1e-9)
	assert.Equal(t, 0.0, results[1].GraphRelevance)
}

// TestRerank_HopBacklinkBoost verifies 2-hop shared-entity boost.
func TestRerank_HopBacklinkBoost(t *testing.T) {
	// "direct" has a direct backlink; "hop" shares an entity URI with "direct".
	edges := &stubEdges{inbound: map[string]int{"direct": 1}}
	w := ranking.DefaultWeights()
	r := ranking.New(edges, w)

	candidates := map[string]ranking.Candidate{
		"direct": {Object: makeObj("direct", "ns.shared"), FTSScore: 0.1},
		"hop":    {Object: makeObj("hop", "ns.shared"), FTSScore: 0.05},
		"unrelated": {Object: makeObj("unrelated", "ns.other"), FTSScore: 0.05},
	}

	results, err := r.Rerank(context.Background(), candidates, 0)
	require.NoError(t, err)
	require.Len(t, results, 3)

	byID := make(map[string]ranking.Result)
	for _, res := range results {
		byID[res.Object.ID] = res
	}

	assert.InDelta(t, w.DirectBacklink, byID["direct"].GraphRelevance, 1e-9)
	assert.InDelta(t, w.HopBacklink, byID["hop"].GraphRelevance, 1e-9)
	assert.Equal(t, 0.0, byID["unrelated"].GraphRelevance)
}

// TestRerank_MinScoreFilters verifies results below minScore are dropped.
func TestRerank_MinScoreFilters(t *testing.T) {
	edges := &stubEdges{inbound: map[string]int{}}
	r := ranking.New(edges, ranking.DefaultWeights())

	candidates := map[string]ranking.Candidate{
		"high": {Object: makeObj("high"), FTSScore: 0.5},
		"low":  {Object: makeObj("low"), FTSScore: 0.01},
	}

	results, err := r.Rerank(context.Background(), candidates, 0.1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "high", results[0].Object.ID)
}

// TestRerank_TotalEqualsSumOfSignals validates Total == FTS+Vec+Mention+Graph.
func TestRerank_TotalEqualsSumOfSignals(t *testing.T) {
	edges := &stubEdges{inbound: map[string]int{"z": 2}}
	r := ranking.New(edges, ranking.DefaultWeights())

	candidates := map[string]ranking.Candidate{
		"z": {Object: makeObj("z", "ns.a"), FTSScore: 0.25, VecScore: 0.15},
	}

	results, err := r.Rerank(context.Background(), candidates, 0)
	require.NoError(t, err)
	require.Len(t, results, 1)

	res := results[0]
	want := res.FTS + res.Vector + res.MentionBoost + res.GraphRelevance
	assert.InDelta(t, want, res.Total, 1e-9)
}

// TestRerank_DeterministicOrderOnTie verifies stable sort on tied scores.
func TestRerank_DeterministicOrderOnTie(t *testing.T) {
	edges := &stubEdges{inbound: map[string]int{}}
	r := ranking.New(edges, ranking.DefaultWeights())

	// All same FTS score, no other signals → sort by ID ascending.
	candidates := map[string]ranking.Candidate{
		"c": {Object: makeObj("c"), FTSScore: 0.1},
		"a": {Object: makeObj("a"), FTSScore: 0.1},
		"b": {Object: makeObj("b"), FTSScore: 0.1},
	}

	results, err := r.Rerank(context.Background(), candidates, 0)
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "a", results[0].Object.ID)
	assert.Equal(t, "b", results[1].Object.ID)
	assert.Equal(t, "c", results[2].Object.ID)
}
