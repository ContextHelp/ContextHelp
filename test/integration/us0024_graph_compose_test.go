package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/uri"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock steps for US-0024 graph-compose tests.
// ---------------------------------------------------------------------------

// graphEntityMentionStep adds entity mentions (outbound edges) to the draft,
// simulating a root object that references related entities.
type graphEntityMentionStep struct {
	pipeline.BaseContract
	mentions []uri.URI
}

func (s *graphEntityMentionStep) Name() string { return "test-graph-entity-mention" }
func (s *graphEntityMentionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "article"
	draft.Mentions = append(draft.Mentions, s.mentions...)
	return draft, nil
}

// graphRelatedStep sets the type and a summary for a related (neighbor) object.
type graphRelatedStep struct {
	pipeline.BaseContract
	label string
}

func (s *graphRelatedStep) Name() string { return "test-graph-related" }
func (s *graphRelatedStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "article"
	draft.Summaries = []string{s.label + " related object summary"}
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0024 Tests
// ---------------------------------------------------------------------------

// TestUS0024_GraphEdgesCreatedForMentions verifies that ingesting an object with
// entity mentions creates the corresponding edges in the graph store.
func TestUS0024_GraphEdgesCreatedForMentions(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	entityURIs := []uri.URI{
		{Scheme: "ctxt", Space: "entity", ID: "team/backend"},
		{Scheme: "ctxt", Space: "entity", ID: "system/auth-service"},
	}

	env.svc.Pipes.Upsert("graph.root", &pipeline.Pipeline{
		PipelineName: "graph.root",
		Steps: []pipeline.PipelineStep{
			&graphEntityMentionStep{mentions: entityURIs},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "architecture overview mentioning backend team and auth service",
		Type:     "article",
		Pipeline: "graph.root",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Verify edges created from the root object.
	edges, err := env.svc.Store.Edges().ListFrom(ctx, "object", job.ResultID)
	require.NoError(t, err)
	assert.Len(t, edges, 2, "root object should have 2 outbound edges")
}

// TestUS0024_GraphTraversalDepth1ReturnsDirectNeighbors verifies that a root object
// with entity mentions produces edges retrievable from the store (depth-1 traversal).
func TestUS0024_GraphTraversalDepth1ReturnsDirectNeighbors(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("graph.depth1", &pipeline.Pipeline{
		PipelineName: "graph.depth1",
		Steps: []pipeline.PipelineStep{
			&graphEntityMentionStep{mentions: []uri.URI{
				{Scheme: "ctxt", Space: "entity", ID: "project/alpha"},
				{Scheme: "ctxt", Space: "entity", ID: "project/beta"},
				{Scheme: "ctxt", Space: "entity", ID: "project/gamma"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "multi-project overview content",
		Type:     "article",
		Pipeline: "graph.depth1",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Verify object has 3 mentions (depth-1 edges).
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Len(t, obj.Mentions, 3,
		"root object should have 3 entity mentions (depth-1 neighbors)")

	// Compose includes the root object content.
	brief, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{&obj}, "graph")
	require.NoError(t, err)
	assert.Contains(t, brief, obj.ID, "composition should reference root object")
}

// TestUS0024_GraphCompositionIncludesRelatedObjects verifies that when multiple
// related objects are composed together (simulating depth-2 traversal), all are
// included in the output.
func TestUS0024_GraphCompositionIncludesRelatedObjects(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	labels := []string{"root", "neighbor-A", "neighbor-B"}
	var objects []*storage.KnowledgeObject

	for i, label := range labels {
		pipeName := fmt.Sprintf("graph.related.%d", i)
		env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
			PipelineName: pipeName,
			Steps: []pipeline.PipelineStep{
				&graphRelatedStep{label: label},
			},
		})

		jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
			Content:  label + " content for graph traversal",
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
		require.Equal(t, gohttp.StatusOK, resp.StatusCode)

		var obj storage.KnowledgeObject
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
		objects = append(objects, &obj)
	}

	// Compose all objects (root + depth-2 neighbors) together.
	result, err := env.svc.Compose(ctx, objects, "graph")
	require.NoError(t, err)
	require.NotEmpty(t, result)

	// All 3 objects should appear in the composition.
	for _, obj := range objects {
		assert.Contains(t, result, obj.ID,
			"composition should include object %s", obj.ID)
	}

	// Check that object count is reflected.
	assert.Contains(t, result, "3 knowledge objects",
		"composition should note it was generated from 3 objects")
}

// TestUS0024_GraphBacklinksReachable verifies that entity backlinks are accessible
// after ingesting an object with entity mentions.
func TestUS0024_GraphBacklinksReachable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	env.svc.Pipes.Upsert("graph.backlink", &pipeline.Pipeline{
		PipelineName: "graph.backlink",
		Steps: []pipeline.PipelineStep{
			&graphEntityMentionStep{mentions: []uri.URI{
				{Scheme: "ctxt", Space: "entity", ID: "component/graph-engine"},
			}},
		},
	})

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content:  "graph engine usage documentation",
		Type:     "article",
		Pipeline: "graph.backlink",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Backlinks for the entity should include our object.
	backResp, err := gohttp.Get(fmt.Sprintf(
		"%s/api/v1/entities/@component.graph-engine/backlinks", env.URL))
	require.NoError(t, err)
	defer backResp.Body.Close()

	var body struct {
		Data []storage.KnowledgeObject `json:"data"`
	}
	require.NoError(t, json.NewDecoder(backResp.Body).Decode(&body))
	assert.GreaterOrEqual(t, len(body.Data), 1,
		"entity should have at least one backlink after object ingestion")
}
