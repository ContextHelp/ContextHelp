package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock pipeline steps for summary generation tests.
// ---------------------------------------------------------------------------

// summaryGenerationStep simulates summary generation and section decomposition.
type summaryGenerationStep struct {
	pipeline.BaseContract
	summary  string
	sections []storage.Section
}

func (s *summaryGenerationStep) Name() string { return "test-summary-generation" }
func (s *summaryGenerationStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if s.summary != "" {
		draft.Summaries = append(draft.Summaries, s.summary)
	}
	draft.Sections = append(draft.Sections, s.sections...)
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["sections_created"] = float64(len(s.sections))
	draft.Metadata["summarization_method"] = "instructor"
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0012 Tests
// ---------------------------------------------------------------------------

// TestUS0012_SummarySectionCreatedForLongDoc verifies a summary is generated and
// stored for a long document.
func TestUS0012_SummarySectionCreatedForLongDoc(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	wantSummary := "Alice Smith presented the mobile redesign architecture, deciding to adopt a micro-frontend approach."

	env.svc.Pipes.Upsert("text.summarize", &pipeline.Pipeline{
		PipelineName: "text.summarize",
		Steps: []pipeline.PipelineStep{
			&summaryGenerationStep{
				summary: wantSummary,
				sections: []storage.Section{
					{Title: "Background", Content: "The current mobile app was built in 2019...", Order: 0},
					{Title: "Architecture Decision", Content: "Micro-frontends chosen for scalability.", Order: 1},
					{Title: "Next Steps", Content: "Migration plan to be drafted by Q2.", Order: 2},
				},
			},
		},
	})

	longContent := `Alice Smith presented the mobile redesign architecture.
The current mobile app was built in 2019 with a monolithic frontend.
After extensive research, the team decided to adopt a micro-frontend approach.
This will improve scalability and allow independent deployment of features.
Next steps include drafting a migration plan by Q2 and piloting with the settings module.`

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  longContent,
		Type:     "text",
		Pipeline: "text.summarize",
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

	require.NotEmpty(t, obj.Summaries, "summaries must be non-empty after generation")
	assert.Equal(t, wantSummary, obj.Summaries[0], "summary must match generated text")
}

// TestUS0012_SectionCountMatchesStructure verifies section count in Metadata matches
// the actual number of Sections on the object.
func TestUS0012_SectionCountMatchesStructure(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	sections := []storage.Section{
		{Title: "Introduction", Content: "Overview of the system.", Order: 0},
		{Title: "Design", Content: "Key design decisions.", Order: 1},
		{Title: "Implementation", Content: "Implementation details.", Order: 2},
		{Title: "Testing", Content: "Test strategy.", Order: 3},
		{Title: "Conclusion", Content: "Summary and next steps.", Order: 4},
	}

	env.svc.Pipes.Upsert("text.section-count", &pipeline.Pipeline{
		PipelineName: "text.section-count",
		Steps: []pipeline.PipelineStep{
			&summaryGenerationStep{
				summary:  "Five-section technical document.",
				sections: sections,
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "A long technical document with five major sections covering introduction through conclusion.",
		Type:     "text",
		Pipeline: "text.section-count",
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

	assert.Len(t, obj.Sections, 5, "section count should match structure")
	assert.Equal(t, float64(5), obj.Metadata["sections_created"],
		"sections_created metadata must match actual section count")
}

// TestUS0012_SectionsHaveTitleAndContent verifies each section has title and content fields.
func TestUS0012_SectionsHaveTitleAndContent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("text.section-fields", &pipeline.Pipeline{
		PipelineName: "text.section-fields",
		Steps: []pipeline.PipelineStep{
			&summaryGenerationStep{
				summary: "Document with structured sections.",
				sections: []storage.Section{
					{Title: "Background", Content: "Historical context here.", Order: 0},
					{Title: "Goals", Content: "Primary objectives described.", Order: 1},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Background section covers history. Goals section covers objectives.",
		Type:     "text",
		Pipeline: "text.section-fields",
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

	for i, sec := range obj.Sections {
		assert.NotEmpty(t, sec.Title, "section %d must have a title", i)
		assert.NotEmpty(t, sec.Content, "section %d must have content", i)
		assert.Equal(t, i, sec.Order, "section order must be sequential")
	}
}

// TestUS0012_SummarizationMethodRecordedInMetadata verifies the summarization method
// is persisted in Metadata.
func TestUS0012_SummarizationMethodRecordedInMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("text.summarize-method", &pipeline.Pipeline{
		PipelineName: "text.summarize-method",
		Steps: []pipeline.PipelineStep{
			&summaryGenerationStep{
				summary:  "Short summary.",
				sections: []storage.Section{{Title: "Main", Content: "Content here.", Order: 0}},
			},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Content to summarize with method tracking",
		Type:     "text",
		Pipeline: "text.summarize-method",
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

	assert.Equal(t, "instructor", obj.Metadata["summarization_method"],
		"summarization_method must be recorded in Metadata")
}
