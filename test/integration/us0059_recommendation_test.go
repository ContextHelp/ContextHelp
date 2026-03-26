package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock steps for US-0059 recommendation document tests.
// ---------------------------------------------------------------------------

// recommendationSourceStep enriches an object with decisions and a summary,
// making it suitable as a source for recommendation generation.
type recommendationSourceStep struct {
	pipeline.BaseContract
	summary   string
	decisions []storage.Decision
}

func (s *recommendationSourceStep) Name() string { return "test-recommendation-source" }
func (s *recommendationSourceStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "article"
	draft.Summaries = []string{s.summary}
	draft.Decisions = append(draft.Decisions, s.decisions...)
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0059 Tests
// ---------------------------------------------------------------------------

// TestUS0059_RecommendationCompositionHasCorrectHeader verifies that composing a
// recommendation document from source objects produces output with the expected header.
func TestUS0059_RecommendationCompositionHasCorrectHeader(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("rec.header", &pipeline.Pipeline{
		PipelineName: "rec.header",
		Steps: []pipeline.PipelineStep{
			&recommendationSourceStep{
				summary: "Deployment risk analysis shows three failure modes.",
				decisions: []storage.Decision{
					{Title: "Adopt canary releases", Status: "OPEN", Impact: "HIGH"},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "deployment risk analysis content",
		Type:     "article",
		Pipeline: "rec.header",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	rec, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "recommendation")
	require.NoError(t, err)
	require.NotEmpty(t, rec)

	assert.True(t, strings.HasPrefix(rec, "# recommendation"),
		"recommendation should start with # recommendation header")
}

// TestUS0059_RecommendationContainsSourceObjectReference verifies that each source
// object is referenced in the composed recommendation.
func TestUS0059_RecommendationContainsSourceObjectReference(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("rec.reference", &pipeline.Pipeline{
		PipelineName: "rec.reference",
		Steps: []pipeline.PipelineStep{
			&recommendationSourceStep{
				summary: "Three incidents traced to deployment rollbacks.",
				decisions: []storage.Decision{
					{Title: "Adopt blue-green deployment", Status: "OPEN", Impact: "HIGH"},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "incident post-mortem content",
		Type:     "article",
		Pipeline: "rec.reference",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	// ComposeWithCitations embeds provenance references.
	result, err := env.svc.ComposeWithCitations(ctx, []*storage.KnowledgeObject{&obj}, "recommendation")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Source IDs — provenance linking each recommendation to its source.
	assert.Contains(t, result.SourceIDs, obj.ID,
		"recommendation provenance should list source object ID")

	// Inline citation marker present.
	assert.Contains(t, result.Content, fmt.Sprintf("[ref:%s]", obj.ID),
		"recommendation should embed inline citation for source provenance")
}

// TestUS0059_MultipleSourcesProduceSingleRecommendation verifies that composing
// a recommendation from multiple objects produces a single cohesive output that
// references all sources.
func TestUS0059_MultipleSourcesProduceSingleRecommendation(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	summaries := []string{
		"Security audit found 5 vulnerabilities in auth layer.",
		"Performance review shows 40% latency increase under load.",
	}

	var objects []*storage.KnowledgeObject
	for i, summary := range summaries {
		pipeName := fmt.Sprintf("rec.multi.%d", i)
		env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
			PipelineName: pipeName,
			Steps: []pipeline.PipelineStep{
				&recommendationSourceStep{
					summary: summary,
					decisions: []storage.Decision{
						{Title: fmt.Sprintf("Recommendation source %d", i), Status: "OPEN", Impact: "HIGH"},
					},
				},
			},
		})

		jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
			Content:  summary,
			Type:     "article",
			Pipeline: pipeName,
			Source:   "e2e-test",
		})
		require.NoError(t, err)

		job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
		require.NotEmpty(t, job.ResultID)

		resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
		require.NoError(t, err)
		defer resp.Body.Close()

		var obj storage.KnowledgeObject
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
		objects = append(objects, &obj)
	}

	result, err := env.svc.ComposeWithCitations(ctx, objects, "recommendation")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.SourceIDs, 2,
		"recommendation should reference both source objects")
	assert.NotEmpty(t, result.Citations,
		"recommendation should have inline citations for both sources")
}

// TestUS0059_OpenDecisionsGenerateActionableRecommendations verifies that objects
// containing OPEN decisions are stored correctly for use in recommendation generation.
func TestUS0059_OpenDecisionsGenerateActionableRecommendations(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("rec.open", &pipeline.Pipeline{
		PipelineName: "rec.open",
		Steps: []pipeline.PipelineStep{
			&recommendationSourceStep{
				summary: "Two open decisions require immediate action.",
				decisions: []storage.Decision{
					{Title: "Rotate TLS certificates", Status: "OPEN", Impact: "HIGH"},
					{Title: "Enable audit logging", Status: "OPEN", Impact: "MEDIUM"},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "security posture review with open decisions",
		Type:     "article",
		Pipeline: "rec.open",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	// Both decisions are OPEN (actionable).
	require.Len(t, obj.Decisions, 2)
	for _, d := range obj.Decisions {
		assert.Equal(t, "OPEN", d.Status,
			"decision %q should be OPEN to generate actionable recommendations", d.Title)
	}
}
