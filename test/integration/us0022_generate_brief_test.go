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
// Mock steps for US-0022 brief composition tests.
// ---------------------------------------------------------------------------

// briefObjectStep sets title, summary, and type to simulate a knowledge object
// ready for brief generation.
type briefObjectStep struct {
	pipeline.BaseContract
	title   string
	summary string
}

func (s *briefObjectStep) Name() string { return "test-brief-object" }
func (s *briefObjectStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "article"
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["title"] = s.title
	draft.Summaries = []string{s.summary}
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0022 Tests
// ---------------------------------------------------------------------------

// TestUS0022_BriefContainsObjectSummaries verifies that composing a brief from
// ingested objects includes each object's summary content.
func TestUS0022_BriefContainsObjectSummaries(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	// Ingest two objects with known summaries.
	titles := []string{"Project Alpha Kickoff", "Architecture Decision Record"}
	summaries := []string{
		"Alpha project kicked off with a focus on reliability.",
		"Decision to adopt event-driven architecture for scalability.",
	}

	var objects []*storage.KnowledgeObject
	for i, title := range titles {
		env.svc.Pipes.Upsert(fmt.Sprintf("brief.obj.%d", i), &pipeline.Pipeline{
			PipelineName: fmt.Sprintf("brief.obj.%d", i),
			Steps: []pipeline.PipelineStep{
				&briefObjectStep{title: title, summary: summaries[i]},
			},
		})

		jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
			Content:  fmt.Sprintf("content for %s", title),
			Type:     "article",
			Pipeline: fmt.Sprintf("brief.obj.%d", i),
			Source:   "e2e-test",
		})
		require.NoError(t, err)

		job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
		require.NotEmpty(t, job.ResultID)

		resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, gohttp.StatusOK, resp.StatusCode)

		var obj storage.KnowledgeObject
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
		objects = append(objects, &obj)
	}

	// Compose a brief from the two objects.
	brief, err := env.svc.Compose(ctx, objects, "brief")
	require.NoError(t, err)
	require.NotEmpty(t, brief)

	// Verify both summaries appear in the brief.
	for _, summary := range summaries {
		assert.Contains(t, brief, summary,
			"brief should contain summary: %q", summary)
	}

	// Verify the brief header is present.
	assert.Contains(t, brief, "# brief", "brief should have a title header")
}

// TestUS0022_BriefContainsObjectTitles verifies that each ingested object's
// metadata title appears as a section header in the brief.
func TestUS0022_BriefContainsObjectTitles(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("brief.titled", &pipeline.Pipeline{
		PipelineName: "brief.titled",
		Steps: []pipeline.PipelineStep{
			&briefObjectStep{
				title:   "Q1 Strategy Session",
				summary: "Discussed product roadmap priorities for Q1.",
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "Q1 strategy content",
		Type:     "article",
		Pipeline: "brief.titled",
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

	brief, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "brief")
	require.NoError(t, err)

	// Object ID should appear as a section header (## <id>).
	assert.Contains(t, brief, fmt.Sprintf("## %s", obj.ID),
		"brief should contain section header with object ID")
	assert.Contains(t, brief, "roadmap priorities", "brief should contain object summary content")
}

// TestUS0022_BriefWithCitationsIncludesRefTable verifies that ComposeWithCitations
// produces inline [ref:ID] markers and an appended reference table.
func TestUS0022_BriefWithCitationsIncludesRefTable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("brief.citations", &pipeline.Pipeline{
		PipelineName: "brief.citations",
		Steps: []pipeline.PipelineStep{
			&briefObjectStep{
				title:   "Infrastructure Review",
				summary: "Infrastructure capacity reviewed and approved.",
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "infra review content",
		Type:     "article",
		Pipeline: "brief.citations",
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

	result, err := env.svc.ComposeWithCitations(ctx, []*storage.KnowledgeObject{&obj}, "brief")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "brief", result.Type)
	assert.NotEmpty(t, result.Content)
	assert.NotEmpty(t, result.Citations, "brief with citations should have at least one citation")
	assert.Contains(t, result.SourceIDs, obj.ID, "source IDs should include the ingested object ID")

	// Inline citation marker [ref:<id>] should appear.
	assert.Contains(t, result.Content, fmt.Sprintf("[ref:%s]", obj.ID),
		"content should contain inline citation for object ID")

	// Reference table appended at end.
	assert.True(t, strings.Contains(result.Content, obj.ID),
		"reference table should include the object ID")
}

// TestUS0022_EmptyObjectListProducesEmptyBrief verifies composing from zero objects
// returns a brief header with a note about zero objects.
func TestUS0022_EmptyObjectListProducesEmptyBrief(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	brief, err := env.svc.Compose(context.Background(), []*storage.KnowledgeObject{}, "brief")
	require.NoError(t, err)
	assert.Contains(t, brief, "0 knowledge objects",
		"empty brief should note zero objects")
}
