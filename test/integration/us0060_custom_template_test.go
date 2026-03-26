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
// Mock steps for US-0060 custom template tests.
// ---------------------------------------------------------------------------

// templateSourceStep builds an object with sections matching a custom template structure.
type templateSourceStep struct {
	pipeline.BaseContract
	sections []storage.Section
	summary  string
}

func (s *templateSourceStep) Name() string { return "test-template-source" }
func (s *templateSourceStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "article"
	draft.Summaries = []string{s.summary}
	draft.Sections = append(draft.Sections, s.sections...)
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["template_sections"] = len(s.sections)
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0060 Tests
// ---------------------------------------------------------------------------

// TestUS0060_CustomTemplateSectionsRegisteredInPipeline verifies that a pipeline
// step can set custom template section metadata on an object, simulating registration
// of a custom template structure.
func TestUS0060_CustomTemplateSectionsRegisteredInPipeline(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	templateSections := []storage.Section{
		{Title: "Sprint Summary", Content: "Team completed auth module.", Order: 0},
		{Title: "Shipped", Content: "- Auth API v2\n- Token refresh", Order: 1},
		{Title: "Risks and Blockers", Content: "1. DB migration pending", Order: 2},
	}

	env.svc.Pipes.Upsert("template.sprint", &pipeline.Pipeline{
		PipelineName: "template.sprint",
		Steps: []pipeline.PipelineStep{
			&templateSourceStep{
				summary:  "Sprint review using custom sprint-review template.",
				sections: templateSections,
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "sprint review content for custom template",
		Type:     "article",
		Pipeline: "template.sprint",
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

	// Template sections preserved in order.
	require.Len(t, obj.Sections, 3, "should have 3 template sections")
	assert.Equal(t, "Sprint Summary", obj.Sections[0].Title)
	assert.Equal(t, "Shipped", obj.Sections[1].Title)
	assert.Equal(t, "Risks and Blockers", obj.Sections[2].Title)

	// Section order preserved.
	for i, sec := range obj.Sections {
		assert.Equal(t, i, sec.Order, "section %d order should be %d", i, i)
	}

	// Template section count in metadata.
	assert.Equal(t, 3.0, obj.Metadata["template_sections"],
		"metadata should record template section count")
}

// TestUS0060_CustomTemplateContentInterpolated verifies that section content
// set during pipeline execution is preserved and accessible in the composition.
func TestUS0060_CustomTemplateContentInterpolated(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("template.interpolate", &pipeline.Pipeline{
		PipelineName: "template.interpolate",
		Steps: []pipeline.PipelineStep{
			&templateSourceStep{
				summary: "Decision review with custom template variables interpolated.",
				sections: []storage.Section{
					{
						Title:   "Context",
						Content: "Project: Horizon | Owner: @person.alice | Status: IN_PROGRESS",
						Order:   0,
					},
					{
						Title:   "Key Points",
						Content: "- Milestone reached on schedule\n- Budget under by 5%",
						Order:   1,
					},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "project horizon status update",
		Type:     "article",
		Pipeline: "template.interpolate",
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

	// Verify interpolated variables in section content.
	require.Len(t, obj.Sections, 2)
	assert.Contains(t, obj.Sections[0].Content, "Project: Horizon",
		"context section should contain interpolated project name")
	assert.Contains(t, obj.Sections[0].Content, "@person.alice",
		"context section should contain interpolated owner reference")
	assert.Contains(t, obj.Sections[1].Content, "- Milestone reached",
		"key points section should use bullet list format")
}

// TestUS0060_CustomTemplateSectionOrderInComposition verifies that sections
// defined by a custom template appear in the declared order in the final composition.
func TestUS0060_CustomTemplateSectionOrderInComposition(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("template.order", &pipeline.Pipeline{
		PipelineName: "template.order",
		Steps: []pipeline.PipelineStep{
			&templateSourceStep{
				summary: "Custom template composition order test.",
				sections: []storage.Section{
					{Title: "Executive Summary", Content: "High-level status.", Order: 0},
					{Title: "Technical Details", Content: "Implementation specifics.", Order: 1},
					{Title: "Next Steps", Content: "1. Schedule review\n2. Update docs", Order: 2},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "technical review content for order test",
		Type:     "article",
		Pipeline: "template.order",
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

	// Compose using the custom template structure.
	result, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "brief")
	require.NoError(t, err)
	require.NotEmpty(t, result)

	// Composition references source object (template applied).
	assert.Contains(t, result, obj.ID,
		"composition should reference the source object with custom template sections")

	// Sections retained in object (order preserved for template rendering).
	require.Len(t, obj.Sections, 3)
	titles := []string{"Executive Summary", "Technical Details", "Next Steps"}
	for i, title := range titles {
		assert.Equal(t, title, obj.Sections[i].Title,
			"template section[%d] title should be %q", i, title)
	}
}

// TestUS0060_BulletListSectionFormatPreserved verifies that a section with bullet
// list content retains its format after ingestion (format validation for template).
func TestUS0060_BulletListSectionFormatPreserved(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	bulletContent := "- Feature A shipped\n- Feature B in progress\n- Feature C planned"

	env.svc.Pipes.Upsert("template.bullets", &pipeline.Pipeline{
		PipelineName: "template.bullets",
		Steps: []pipeline.PipelineStep{
			&templateSourceStep{
				summary: "Sprint shipped items in bullet list format.",
				sections: []storage.Section{
					{Title: "Shipped", Content: bulletContent, Order: 0},
				},
			},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "sprint shipped items",
		Type:     "article",
		Pipeline: "template.bullets",
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

	require.Len(t, obj.Sections, 1)
	assert.True(t, strings.HasPrefix(obj.Sections[0].Content, "- "),
		"bullet list section should start with '- ' marker")
	assert.Contains(t, obj.Sections[0].Content, "Feature A shipped",
		"bullet list content should be preserved verbatim")
	assert.Equal(t, "Shipped", obj.Sections[0].Title,
		"template section title should be preserved")
}
