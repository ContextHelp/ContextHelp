package service

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeSearchObject builds a graph-canonical KO for search tests.
// The first element of summaries becomes the graph summary node content,
// driving FTS via ProjectIndex. rawContent is stored for flat-fallback tests.
func makeSearchObject(id string, summaries []string, rawContent string) *storage.KnowledgeObject {
	content := rawContent
	if len(summaries) > 0 && summaries[0] != "" {
		content = summaries[0]
	}
	obj := storageutil.BuildGraphKO(id, "text", content)
	// Preserve RawContent for tests that check it directly.
	if rawContent != "" {
		obj.RawContent = rawContent
	}
	return obj
}

func rebuildFTS(t *testing.T, svc *Service) {
	t.Helper()
	d, ok := svc.Store.(*sqlite.Driver)
	require.True(t, ok, "store must be *sqlite.Driver for FTS rebuild")
	_, err := d.DB().ExecContext(context.Background(), "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)
}

func TestHybridSearch_FTSOnly_WhenNoEmbeddingProvider(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("hs-1", []string{"distributed systems fault tolerance"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	results, err := svc.HybridSearch(ctx, "distributed", 10, retrieval.SemanticSource{}, cfg)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hs-1", results[0].ID)
}

func TestFindByText_UsesFTS(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("fbt-1", []string{"microservices resilience patterns"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	results, err := svc.FindByText(ctx, "resilience", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "fbt-1", results[0].ID)
}

// TestFindByText_HyphenatedQueryDoesNotCrash is the T-0565 service-level
// regression test: `ctxt find credit-eligible` previously surfaced
// "no such column: eligible" because the hyphen was passed unsanitised
// into FTS5 MATCH. With SafeFTSQuery wired into FindByTextFiltered, the
// hyphen becomes a token boundary and the query succeeds.
func TestFindByText_HyphenatedQueryDoesNotCrash(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("fbt-hy-1", []string{"documents that are credit eligible for activate"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	results, err := svc.FindByText(ctx, "credit-eligible", 10)
	require.NoError(t, err, "hyphenated query must not crash FTS")
	require.Len(t, results, 1)
	assert.Equal(t, "fbt-hy-1", results[0].ID)
}

// TestFindByText_GraphWithoutSummaryIndexesListItems is the second half of
// the T-0565 fix: a KO whose graph has only Tag / EntityMention nodes
// (e.g. a doc routed through text.short, which runs no markdown_parser /
// sectioner) used to produce an empty projected_fts_body — so
// `ctxt find SageMaker` against a 250-line bullet list returned 0 hits
// even though TextContent stored every item. ProjectIndex now falls back
// to flat fields when the graph lacks Summary/Section nodes; this test
// proves the round-trip works.
func TestFindByText_GraphWithoutSummaryIndexesListItems(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	bulletList := `Eligible AWS services for Activate credits:

- AWS SageMaker
- AWS Lambda
- VMware Cloud on AWS
- Amazon Bedrock`

	// KO with text.short-style graph: only Tag + EntityMention nodes,
	// no Summary or Section. Body lives in TextContent only.
	const koID = "fbt-list-1"
	obj := &storage.KnowledgeObject{
		ID:          koID,
		Type:        "text",
		Subtype:     "long",
		TextContent: bulletList,
		RawContent:  bulletList,
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID(koID, pluginapi.NodeTypeTag, 0),
					NodeType: pluginapi.NodeTypeTag,
					Label:    "aws",
					Content:  "aws",
				},
				{
					ID:       pluginapi.NewNodeID(koID, pluginapi.NodeTypeEntityMention, 0),
					NodeType: pluginapi.NodeTypeEntityMention,
					Content:  "@vendor.aws",
				},
			},
		},
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	for _, term := range []string{"SageMaker", "VMware", "Bedrock"} {
		results, err := svc.FindByText(ctx, term, 10)
		require.NoError(t, err, "FindByText(%q)", term)
		require.NotEmpty(t, results,
			"FindByText(%q) returned 0 results — list item not indexed (T-0565 regression)", term)
		assert.Equal(t, "fbt-list-1", results[0].ID, "term=%q", term)
	}
}

func TestHybridSearch_ErrorWhenNoProvider_FallbackDisabled(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		FallbackToFTS: false,
	}
	_, err := svc.HybridSearch(ctx, "anything", 10, retrieval.SemanticSource{}, cfg)
	require.Error(t, err)
}

func TestHybridSearchExplain_ReturnsBreakdown(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("exp-1", []string{"observability tracing distributed systems"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	results, err := svc.HybridSearchExplain(ctx, "observability", 10, retrieval.SemanticSource{}, cfg)
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Equal(t, "exp-1", r.Object.ID)

	// FTS leg should have contributed (no vector provider — fallback mode).
	assert.Greater(t, r.Breakdown.FTS, 0.0)
	// Vector leg should be zero — no embedding provider.
	assert.Equal(t, 0.0, r.Breakdown.Vector)
	// Total must equal sum of all signals.
	wantTotal := r.Breakdown.FTS + r.Breakdown.Vector + r.Breakdown.MentionBoost + r.Breakdown.GraphRelevance + r.Breakdown.WordOverlap
	assert.InDelta(t, wantTotal, r.Breakdown.Total, 1e-9)
}

func TestHybridSearchExplain_ErrorWhenNoProvider_FallbackDisabled(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		FallbackToFTS: false,
	}
	_, err := svc.HybridSearchExplain(ctx, "anything", 10, retrieval.SemanticSource{}, cfg)
	require.Error(t, err)
}

// TestHybridSearchExplainFilteredWithDiagnostics_BelowThresholdPopulated is the
// T-0574 regression test: when candidates surface from FTS but their total
// scores all fall below cfg.MinScore, the result envelope must report
// CandidateCount > 0, BelowThresholdCount == CandidateCount, and a
// non-zero TopBelowThresholdScore — letting the CLI distinguish "nothing
// indexed under any matching token" from "matches dropped under threshold".
func TestHybridSearchExplainFilteredWithDiagnostics_BelowThresholdPopulated(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Two candidates that should match the FTS leg but produce small RRF
	// totals (one term hit, fallback FTS-only via no embedding provider).
	for _, id := range []string{"thr-1", "thr-2"} {
		obj := makeSearchObject(id, []string{"resilient distributed-systems checkpointing"}, "")
		require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	}
	rebuildFTS(t, svc)

	// MinScore=10.0 is far above any plausible RRF total (RRF contributions
	// are 1/(k+rank+1) with k=60 → at most ~0.016 per leg).
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
		MinScore:      10.0,
	}

	envelope, err := svc.HybridSearchExplainFilteredWithDiagnostics(
		ctx, "resilient", storage.ObjectFilter{Limit: 10}, retrieval.SemanticSource{}, cfg,
	)
	require.NoError(t, err)
	require.NotNil(t, envelope)

	// All candidates must have been dropped — Results is empty.
	assert.Empty(t, envelope.Results, "all candidates should be below threshold")

	d := envelope.Diagnostics
	assert.Equal(t, 10.0, d.Threshold, "threshold must echo cfg.MinScore")
	assert.GreaterOrEqual(t, d.CandidateCount, 2, "FTS leg should surface both candidates")
	assert.Equal(t, d.CandidateCount, d.BelowThresholdCount, "every candidate is below 10.0")
	assert.Greater(t, d.TopBelowThresholdScore, 0.0, "top dropped score must be the highest scored candidate, not zero")
	assert.Less(t, d.TopBelowThresholdScore, d.Threshold, "top dropped score must be strictly below threshold")
}

// TestHybridSearchExplainFilteredWithDiagnostics_NoCandidates verifies the
// empty-truth shape: when no document matches any FTS token, the envelope
// reports CandidateCount=0 and BelowThresholdCount=0 — distinguishing this
// case from the all-below-threshold one above.
func TestHybridSearchExplainFilteredWithDiagnostics_NoCandidates(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
		MinScore:      0.0,
	}

	envelope, err := svc.HybridSearchExplainFilteredWithDiagnostics(
		ctx, "zzz_nothing_indexed_xyzzy", storage.ObjectFilter{Limit: 10}, retrieval.SemanticSource{}, cfg,
	)
	require.NoError(t, err)
	require.NotNil(t, envelope)

	assert.Empty(t, envelope.Results)
	assert.Equal(t, 0, envelope.Diagnostics.CandidateCount)
	assert.Equal(t, 0, envelope.Diagnostics.BelowThresholdCount)
	assert.Equal(t, 0.0, envelope.Diagnostics.TopBelowThresholdScore)
}

func TestHybridSearchExplain_TotalMatchesHybridSearchRRFScore(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("cmp-1", []string{"event sourcing CQRS architecture patterns"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	plain, err := svc.HybridSearch(ctx, "event sourcing", 10, retrieval.SemanticSource{}, cfg)
	require.NoError(t, err)
	require.Len(t, plain, 1)

	explained, err := svc.HybridSearchExplain(ctx, "event sourcing", 10, retrieval.SemanticSource{}, cfg)
	require.NoError(t, err)
	require.Len(t, explained, 1)

	// The rrf_score stored in Metadata by HybridSearch must equal the explain total.
	rrfScore, ok := plain[0].Metadata["rrf_score"].(float64)
	require.True(t, ok, "rrf_score must be float64")
	assert.InDelta(t, rrfScore, explained[0].Breakdown.Total, 1e-9)
}

// TestHybridSearch_StalenessWarning_PopulatedWhenStaleVersionPresent
// asserts that when the result set contains a candidate whose pipeline
// stamp parses to a version older than the registry's current version
// for the same family, SearchDiagnostics.StalenessWarning is populated
// with the stale count and a reason that points operators at
// `ctxt upgrade plan` (T-0581, ADR-070 §6).
func TestHybridSearch_StalenessWarning_PopulatedWhenStaleVersionPresent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Register text.short@v1 as the "current" version. The base test service
	// already registered "text.short" (legacy bare) — adding @v1 makes v1
	// the registry's installed version per CurrentVersionForFamily.
	svc.Pipes.Upsert("text.short@v1", &pipeline.Pipeline{PipelineName: "text.short@v1"})

	// Two stale (text.short@v0) hits + one current (text.short@v1) hit.
	stale1 := makeSearchObject("stale-1", []string{"distributed systems fault tolerance"}, "")
	stale1.Pipeline = "text.short@v0"
	require.NoError(t, svc.Store.Objects().Create(ctx, stale1))

	stale2 := makeSearchObject("stale-2", []string{"distributed coordination consensus"}, "")
	stale2.Pipeline = "text.short@v0"
	require.NoError(t, svc.Store.Objects().Create(ctx, stale2))

	current := makeSearchObject("current-1", []string{"distributed actor model resilience"}, "")
	current.Pipeline = "text.short@v1"
	require.NoError(t, svc.Store.Objects().Create(ctx, current))

	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	envelope, err := svc.HybridSearchExplainFilteredWithDiagnostics(
		ctx, "distributed", storage.ObjectFilter{Limit: 10}, retrieval.SemanticSource{}, cfg,
	)
	require.NoError(t, err)
	require.NotNil(t, envelope.Diagnostics.StalenessWarning, "expected staleness warning")
	assert.Equal(t, 2, envelope.Diagnostics.StalenessWarning.Count)
	assert.Contains(t, envelope.Diagnostics.StalenessWarning.Reason, "ctxt upgrade plan")
}

// TestHybridSearch_StalenessWarning_OmittedWhenAllCurrent confirms the
// warning is nil when every candidate's pipeline stamp matches the
// registry's currently-installed version.
func TestHybridSearch_StalenessWarning_OmittedWhenAllCurrent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Don't register a higher version — base "text.short" is the only
	// registration, so the registry's current version for the family
	// is v0 and all-v0 candidates count as current.
	obj := makeSearchObject("cur-1", []string{"distributed resilience"}, "")
	obj.Pipeline = "text.short@v0"
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	envelope, err := svc.HybridSearchExplainFilteredWithDiagnostics(
		ctx, "distributed", storage.ObjectFilter{Limit: 10}, retrieval.SemanticSource{}, cfg,
	)
	require.NoError(t, err)
	if envelope.Diagnostics.StalenessWarning != nil {
		t.Errorf("StalenessWarning should be nil when all candidates are current; got %+v", envelope.Diagnostics.StalenessWarning)
	}
}
