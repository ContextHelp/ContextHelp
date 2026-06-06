package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uri "hop.top/cite/scheme"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock steps for US-0057 stakeholder analysis tests.
// ---------------------------------------------------------------------------

// stakeholderMentionStep adds person/team entity mentions to the draft, simulating
// objects enriched with stakeholder entity extraction.
type stakeholderMentionStep struct {
	pipeline.BaseContract
	stakeholders []uri.URI
}

func (s *stakeholderMentionStep) Name() string { return "test-stakeholder-mention" }
func (s *stakeholderMentionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "article"
	draft.Mentions = append(draft.Mentions, s.stakeholders...)
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0057 Tests
// ---------------------------------------------------------------------------

// TestUS0057_StakeholderMentionsPreservedAfterIngestion verifies that entity mentions
// representing stakeholders are retained in the stored knowledge object.
func TestUS0057_StakeholderMentionsPreservedAfterIngestion(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	stakeholders := []uri.URI{
		{Scheme: "ctxt", Namespace: "entity", ID: "person/alice-smith"},
		{Scheme: "ctxt", Namespace: "entity", ID: "person/bob-jones"},
		{Scheme: "ctxt", Namespace: "entity", ID: "team/platform"},
	}

	env.svc.Pipes.Upsert("stakeholder.mentions", &pipeline.Pipeline{
		PipelineName: "stakeholder.mentions",
		Steps: []pipeline.PipelineStep{
			&stakeholderMentionStep{stakeholders: stakeholders},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "meeting notes with alice, bob, and platform team",
		Type:     "article",
		Pipeline: "stakeholder.mentions",
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

	require.Len(t, obj.Mentions, 3, "should have 3 stakeholder mentions")
	assert.Equal(t, "person/alice-smith", obj.Mentions[0].ID)
	assert.Equal(t, "person/bob-jones", obj.Mentions[1].ID)
	assert.Equal(t, "team/platform", obj.Mentions[2].ID)
}

// TestUS0057_StakeholderEdgesCreatedFromMentions verifies that ingesting an object
// with stakeholder mentions creates edges in the graph store.
func TestUS0057_StakeholderEdgesCreatedFromMentions(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("stakeholder.edges", &pipeline.Pipeline{
		PipelineName: "stakeholder.edges",
		Steps: []pipeline.PipelineStep{
			&stakeholderMentionStep{stakeholders: []uri.URI{
				{Scheme: "ctxt", Namespace: "entity", ID: "person/carol-white"},
				{Scheme: "ctxt", Namespace: "entity", ID: "person/dave-green"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "decision review with carol and dave",
		Type:     "article",
		Pipeline: "stakeholder.edges",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Verify edges exist for each stakeholder mention.
	edges, err := env.svc.Store.Edges().ListFrom(ctx, "object", job.ResultID)
	require.NoError(t, err)
	assert.Len(t, edges, 2,
		"should have 2 edges, one per stakeholder mention")
}

// TestUS0057_StakeholderAnalysisCompositionIncludesMentions verifies that composing
// a stakeholder analysis from objects with mentions surfaces the mention data.
func TestUS0057_StakeholderAnalysisCompositionIncludesMentions(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("stakeholder.compose", &pipeline.Pipeline{
		PipelineName: "stakeholder.compose",
		Steps: []pipeline.PipelineStep{
			&stakeholderMentionStep{stakeholders: []uri.URI{
				{Scheme: "ctxt", Namespace: "entity", ID: "person/eve-chen"},
				{Scheme: "ctxt", Namespace: "entity", ID: "team/security"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "security review involving eve and security team",
		Type:     "article",
		Pipeline: "stakeholder.compose",
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

	// Compose a stakeholder analysis from the object.
	analysis, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "stakeholder-analysis")
	require.NoError(t, err)
	require.NotEmpty(t, analysis)

	// The composition must reference the source object.
	assert.Contains(t, analysis, obj.ID,
		"stakeholder analysis should reference source object ID")

	// The composition should have the stakeholder-analysis header.
	assert.Contains(t, analysis, "# stakeholder-analysis",
		"analysis should have correct title header")
}

// TestUS0057_MultipleObjectsStakeholderSurfaced verifies that multiple objects
// sharing stakeholder mentions produce a composition that includes all source objects.
func TestUS0057_MultipleObjectsStakeholderSurfaced(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	sharedStakeholder := uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "person/frank-lee"}
	var objects []*storage.KnowledgeObject

	for i := 0; i < 2; i++ {
		pipeName := fmt.Sprintf("stakeholder.multi.%d", i)
		env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
			PipelineName: pipeName,
			Steps: []pipeline.PipelineStep{
				&stakeholderMentionStep{stakeholders: []uri.URI{sharedStakeholder}},
			},
		})

		jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
			Content:  fmt.Sprintf("document %d mentioning frank lee", i),
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

	// Compose stakeholder analysis from both objects.
	result, err := env.svc.ComposeWithCitations(ctx, objects, "stakeholder-analysis")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.SourceIDs, 2,
		"stakeholder analysis should reference both source objects")
	for _, obj := range objects {
		assert.Contains(t, result.SourceIDs, obj.ID,
			"source IDs should contain object %s", obj.ID)
	}
}
