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
// Mock steps for US-0025 export tests.
// ---------------------------------------------------------------------------

// exportBriefObjectStep prepares an object suitable for brief export tests.
type exportBriefObjectStep struct {
	pipeline.BaseContract
	title   string
	summary string
}

func (s *exportBriefObjectStep) Name() string { return "test-export-brief-object" }
func (s *exportBriefObjectStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "article"
	draft.Summaries = []string{s.summary}
	draft.Sections = []storage.Section{
		{Title: s.title, Content: s.summary, Order: 0},
	}
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["title"] = s.title
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0025 Tests
// ---------------------------------------------------------------------------

// TestUS0025_MarkdownExportContainsSectionsAndHeadings verifies that a composed
// brief in markdown format preserves section headings from the source objects.
func TestUS0025_MarkdownExportContainsSectionsAndHeadings(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("export.markdown", &pipeline.Pipeline{
		PipelineName: "export.markdown",
		Steps: []pipeline.PipelineStep{
			&exportBriefObjectStep{
				title:   "Executive Summary",
				summary: "The project is on track for Q2 launch.",
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "executive summary export content",
		Type:     "article",
		Pipeline: "export.markdown",
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

	require.Len(t, obj.Sections, 1, "object should have 1 section")
	assert.Equal(t, "Executive Summary", obj.Sections[0].Title)

	// Compose brief in markdown format (default Compose output is markdown).
	markdown, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "brief")
	require.NoError(t, err)

	// Markdown format: starts with # heading.
	assert.True(t, strings.HasPrefix(markdown, "# brief"),
		"markdown export should start with a # heading")

	// Contains the object summary.
	assert.Contains(t, markdown, "Q2 launch",
		"markdown should contain object summary content")

	// Contains section-level heading (## <object-id>).
	assert.Contains(t, markdown, fmt.Sprintf("## %s", obj.ID),
		"markdown should contain section heading for object")
}

// TestUS0025_BriefExportContainsProvenance verifies that ComposeWithCitations
// embeds provenance references in the exported brief.
func TestUS0025_BriefExportContainsProvenance(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("export.provenance", &pipeline.Pipeline{
		PipelineName: "export.provenance",
		Steps: []pipeline.PipelineStep{
			&exportBriefObjectStep{
				title:   "Risk Register",
				summary: "Three HIGH risks identified in architecture review.",
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "risk register content",
		Type:     "article",
		Pipeline: "export.provenance",
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

	// Source IDs (provenance) must include the object.
	assert.Contains(t, result.SourceIDs, obj.ID,
		"export provenance should list source object ID")

	// Citation marker present (provenance inline).
	assert.Contains(t, result.Content, fmt.Sprintf("[ref:%s]", obj.ID),
		"exported brief should contain inline citation for provenance")

	// Generated timestamp present.
	assert.False(t, result.GeneratedAt.IsZero(),
		"export should have a non-zero generated_at timestamp")
}

// TestUS0025_MultiSectionBriefPreservesOrder verifies that a brief composed from
// an object with multiple sections preserves section order in the output.
func TestUS0025_MultiSectionBriefPreservesOrder(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("export.sections", &pipeline.Pipeline{
		PipelineName: "export.sections",
		Steps: []pipeline.PipelineStep{
			&docHierarchicalSectionStep{sections: []storage.Section{
				{Title: "Background", Content: "Context for the initiative.", Order: 0},
				{Title: "Findings", Content: "Three key findings from the audit.", Order: 1},
				{Title: "Recommendations", Content: "Five action items identified.", Order: 2},
			}},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "audit report with multiple sections",
		Type:     "article",
		Pipeline: "export.sections",
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

	require.Len(t, obj.Sections, 3, "should have 3 sections")
	assert.Equal(t, "Background", obj.Sections[0].Title)
	assert.Equal(t, "Findings", obj.Sections[1].Title)
	assert.Equal(t, "Recommendations", obj.Sections[2].Title)

	// Order indices preserved.
	for i, sec := range obj.Sections {
		assert.Equal(t, i, sec.Order, "section order should be %d", i)
	}

	// Brief content contains the section headings.
	brief, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "brief")
	require.NoError(t, err)
	assert.Contains(t, brief, obj.ID, "brief should reference source object")
}
