package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func ptr[T any](v T) *T { return &v }

// Each mode runs its own retrieval and reports its own diagnostics: fts
// never embeds and carries no semantic report, vector returns index hits
// only, hybrid fuses both legs. An empty mode is hybrid.
func TestFind_ModeSelection(t *testing.T) {
	f := newSemanticFixture(t)
	ctx := context.Background()

	for _, tc := range []struct {
		mode, want string
	}{{"", FindModeHybrid}, {FindModeHybrid, FindModeHybrid}, {FindModeVector, FindModeVector}, {FindModeFTS, FindModeFTS}} {
		t.Run("mode="+tc.mode, func(t *testing.T) {
			embeds := len(f.calls.EmbedURLs())
			res, err := f.svc.Find(ctx, FindRequest{Query: semanticQuery, Mode: tc.mode}, f.sem)
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Mode)
			assert.Equal(t, semanticQuery, res.Query)
			assert.Equal(t, len(res.Objects), res.Total)
			ids := resultIDs(res.Objects)
			d := res.Diagnostics

			switch tc.want {
			case FindModeFTS:
				assert.Equal(t, []string{"fts-only"}, ids)
				assert.Nil(t, d.Semantic, "fts reports no semantic leg")
				assert.Zero(t, d.CandidateCount, "fts leaves diagnostics at zero")
				assert.Len(t, f.calls.EmbedURLs(), embeds, "fts must not embed the query")
			case FindModeVector:
				assert.Contains(t, ids, "vec-a")
				assert.NotContains(t, ids, "fts-only", "vector returns index hits only")
				require.NotNil(t, d.Semantic)
				assert.Equal(t, retrieval.SemanticOK, d.Semantic.Status)
				assert.Equal(t, "sem-a", d.Semantic.ModelID)
				assert.Equal(t, len(ids), d.CandidateCount)
			case FindModeHybrid:
				assert.Contains(t, ids, "vec-a")
				assert.Contains(t, ids, "fts-only")
				require.NotNil(t, d.Semantic)
				assert.Equal(t, retrieval.SemanticOK, d.Semantic.Status)
				assert.Equal(t, "sem-a", d.Semantic.ModelID)
				assert.GreaterOrEqual(t, d.CandidateCount, len(ids))
				for _, o := range res.Objects {
					assert.Contains(t, o.Metadata, "rrf_score", o.ID)
				}
			}
			assert.Nil(t, res.Explain)
			assert.Nil(t, res.Facets)
		})
	}
}

// Explain applies to hybrid only: one breakdown per object, in order.
func TestFind_ExplainHybridOnly(t *testing.T) {
	f := newSemanticFixture(t)
	ctx := context.Background()

	res, err := f.svc.Find(ctx, FindRequest{Query: semanticQuery, Explain: true}, f.sem)
	require.NoError(t, err)
	require.NotEmpty(t, res.Objects)
	require.Len(t, res.Explain, len(res.Objects))
	for i, e := range res.Explain {
		assert.Equal(t, res.Objects[i].ID, e.ID)
		assert.Positive(t, e.Breakdown.Total, e.ID)
	}
	require.NotNil(t, res.Diagnostics.Semantic, "explain keeps the diagnostics")
	assert.Equal(t, retrieval.SemanticOK, res.Diagnostics.Semantic.Status)
	assert.Equal(t, len(res.Objects), res.Diagnostics.CandidateCount)

	res, err = f.svc.Find(ctx, FindRequest{Query: semanticQuery, Mode: FindModeFTS, Explain: true}, f.sem)
	require.NoError(t, err)
	assert.Nil(t, res.Explain, "explain is hybrid-only")
	assert.Equal(t, []string{"fts-only"}, resultIDs(res.Objects))
}

// The request's knobs reach the search: a min_score above every score
// drops all candidates, reported as below-threshold, with no suggestions.
func TestFind_ThresholdDiagnostics(t *testing.T) {
	f := newSemanticFixture(t)
	res, err := f.svc.Find(context.Background(), FindRequest{
		Query: semanticQuery, Search: FindSearch{MinScore: ptr(5.0)},
	}, f.sem)
	require.NoError(t, err)
	assert.Empty(t, res.Objects)
	assert.NotNil(t, res.Objects, "objects is never null")
	d := res.Diagnostics
	assert.InDelta(t, 5.0, d.Threshold, 1e-9)
	assert.Positive(t, d.CandidateCount)
	assert.Equal(t, d.CandidateCount, d.BelowThresholdCount)
	assert.Positive(t, d.TopBelowThresholdScore)
}

// A semantic leg that cannot run degrades to full-text with the reason in
// diagnostics; with fallback_to_fts off it is a SemanticUnavailableError.
func TestFind_SemanticUnavailable(t *testing.T) {
	f := newSemanticFixture(t)
	ctx := context.Background()
	_, err := f.db.Exec(`UPDATE embedding_models SET is_default = 0`)
	require.NoError(t, err)

	for _, mode := range []string{FindModeHybrid, FindModeVector} {
		res, err := f.svc.Find(ctx, FindRequest{Query: semanticQuery, Mode: mode}, f.sem)
		require.NoError(t, err, mode)
		assert.Equal(t, []string{"fts-only"}, resultIDs(res.Objects), mode)
		require.NotNil(t, res.Diagnostics.Semantic, mode)
		assert.Equal(t, retrieval.SemanticNoDefaultModel, res.Diagnostics.Semantic.Status, mode)
		assert.NotEmpty(t, res.Diagnostics.Semantic.Notice, mode)

		_, err = f.svc.Find(ctx, FindRequest{Query: semanticQuery, Mode: mode, Search: FindSearch{FallbackToFTS: ptr(false)}}, f.sem)
		var unavailable *SemanticUnavailableError
		require.ErrorAs(t, err, &unavailable, mode)
		assert.Equal(t, retrieval.SemanticNoDefaultModel, unavailable.Report.Status, mode)
	}
}

// Facets count the objects the filter matches, independent of the limit.
func TestFind_Facets(t *testing.T) {
	f := newSemanticFixture(t)
	ctx := context.Background()

	res, err := f.svc.Find(ctx, FindRequest{Query: semanticQuery, Mode: FindModeFTS, Facets: true}, f.sem)
	require.NoError(t, err)
	want, err := f.svc.FacetCounts(ctx, resolvedFilter(t, FindRequest{Query: semanticQuery}))
	require.NoError(t, err)
	assert.Equal(t, want, res.Facets)
	assert.Equal(t, 3, sumCounts(res.Facets), "every stored object is counted")
}

func sumCounts(m map[string]int) int {
	n := 0
	for _, c := range m {
		n += c
	}
	return n
}

func resolvedFilter(t *testing.T, req FindRequest) storage.ObjectFilter {
	t.Helper()
	_, filter, _, err := resolveFind(req)
	require.NoError(t, err)
	return filter
}

func TestFind_Invalid(t *testing.T) {
	f := newSemanticFixture(t)
	for name, req := range map[string]FindRequest{
		"no query":          {},
		"blank query":       {Query: "  "},
		"unknown mode":      {Query: "q", Mode: "semantic"},
		"negative limit":    {Query: "q", Limit: -1},
		"bad since":         {Query: "q", Filter: FindFilter{Since: "April 1"}},
		"bad until":         {Query: "q", Filter: FindFilter{Until: "2026-13-01"}},
		"zero rrf_k":        {Query: "q", Search: FindSearch{RRFK: ptr(0)}},
		"negative pool":     {Query: "q", Search: FindSearch{VectorPool: ptr(-5)}},
		"negative weight":   {Query: "q", Search: FindSearch{FTSWeight: ptr(-0.1)}},
		"negative boost":    {Query: "q", Search: FindSearch{HopBacklinkBoost: ptr(-1.0)}},
		"negative minscore": {Query: "q", Search: FindSearch{MinScore: ptr(-1.0)}},
	} {
		_, err := f.svc.Find(context.Background(), req, f.sem)
		assert.True(t, errors.Is(err, ErrInvalidFind), "%s: %v", name, err)
	}
}

// Omitted knobs take the built-in defaults; each set knob lands on its
// own config field.
func TestFindSearch_Apply(t *testing.T) {
	base := config.DefaultSearchConfig()
	got, err := FindSearch{}.apply(base)
	require.NoError(t, err)
	assert.Equal(t, base, got)

	got, err = FindSearch{
		RRFK: ptr(7), FTSWeight: ptr(0.1), VectorWeight: ptr(0.9), FTSPool: ptr(11), VectorPool: ptr(13),
		MinScore: ptr(0.2), FallbackToFTS: ptr(false),
		MentionBoostPerMention: ptr(0.01), MaxMentionBoost: ptr(0.5), DirectBacklinkBoost: ptr(0.02), HopBacklinkBoost: ptr(0.04),
	}.apply(base)
	require.NoError(t, err)
	assert.Equal(t, config.SearchConfig{
		DefaultMode:   base.DefaultMode,
		RRF:           config.RRFConfig{K: 7, FTSWeight: 0.1, VectorWeight: 0.9},
		CandidatePool: config.CandidatePoolConfig{FTS: 11, Vector: 13},
		MinScore:      0.2,
		FallbackToFTS: false,
		Reranker: config.RerankerConfig{
			MentionBoostPerMention: 0.01, MaxMentionBoost: 0.5, DirectBacklinkBoost: 0.02, HopBacklinkBoost: 0.04,
		},
	}, got)
}

// The facet filter and limit resolve from the request; dates parse as
// YYYY-MM-DD.
func TestResolveFind_Filter(t *testing.T) {
	mode, filter, _, err := resolveFind(FindRequest{Query: "q", Filter: FindFilter{
		MetaType: "task", Topic: "k8s", Person: "alice", SourceType: "slack", Since: "2026-04-01", Until: "2026-04-30",
	}})
	require.NoError(t, err)
	assert.Equal(t, FindModeHybrid, mode)
	assert.Equal(t, DefaultFindLimit, filter.Limit)
	assert.Equal(t, "task", filter.MetadataType)
	assert.Equal(t, "k8s", filter.MetadataTopic)
	assert.Equal(t, "alice", filter.MetadataPerson)
	assert.Equal(t, "slack", filter.SourceType)
	require.NotNil(t, filter.MetadataSince)
	require.NotNil(t, filter.MetadataUntil)
	assert.Equal(t, "2026-04-01", filter.MetadataSince.Format("2006-01-02"))
	assert.Equal(t, "2026-04-30", filter.MetadataUntil.Format("2006-01-02"))

	_, filter, _, err = resolveFind(FindRequest{Query: "q", Limit: 3})
	require.NoError(t, err)
	assert.Equal(t, 3, filter.Limit)
}
