package integration

// US-0052: Graph-Based Entity Search
// Ingests objects with entity mentions (edges), queries by entity slug via
// backlinks and RelatedObjects, verifies graph traversal returns related objects.

import (
	"context"
	"fmt"
	"testing"

	uri "hop.top/cite/scheme"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entityMentionStep is a pipeline step that adds entity mentions to the draft.
type entityMentionStep struct {
	pipeline.BaseContract
	mentions []uri.URI
}

func (s *entityMentionStep) Name() string { return "test-entity-mention" }
func (s *entityMentionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Mentions = append(draft.Mentions, s.mentions...)
	return draft, nil
}

// TestUS0052_EntityBacklinksReturnDirectMentions verifies that objects mentioning
// an entity appear in EntityBacklinks results.
func TestUS0052_EntityBacklinksReturnDirectMentions(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	entitySlug := "auth-service"
	entityURI := uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "auth-service"}

	env.svc.Pipes.Upsert("test.entity.mention", &pipeline.Pipeline{
		PipelineName: "test.entity.mention",
		Steps: []pipeline.PipelineStep{
			&entityMentionStep{mentions: []uri.URI{entityURI}},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "authentication flow using auth-service token exchange",
		Type:     "text",
		Pipeline: "test.entity.mention",
		Source:   "e2e-test",
	})
	require.NoError(t, err)
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	backlinks, err := env.svc.EntityBacklinks(context.Background(), entitySlug)
	require.NoError(t, err)
	require.NotEmpty(t, backlinks, "entity backlinks must contain the ingested object")
	assert.True(t, containsIDPtr(backlinks, job.ResultID),
		"ingested object %s must appear in @%s backlinks", job.ResultID, entitySlug)
}

// TestUS0052_GraphEdgeCreatedAfterIngestion verifies that an edge record is
// created in the store after an object mentioning the entity is ingested.
func TestUS0052_GraphEdgeCreatedAfterIngestion(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	entityURI := uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "payment-gateway"}
	env.svc.Pipes.Upsert("test.edge.check", &pipeline.Pipeline{
		PipelineName: "test.edge.check",
		Steps:        []pipeline.PipelineStep{&entityMentionStep{mentions: []uri.URI{entityURI}}},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "payment gateway integration charge flow",
		Type:     "text",
		Pipeline: "test.edge.check",
		Source:   "e2e-test",
	})
	require.NoError(t, err)
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	edges, err := env.svc.Store.Edges().ListFrom(context.Background(), "object", job.ResultID)
	require.NoError(t, err)
	assert.Greater(t, len(edges), 0, "edge must exist after ingestion with entity mention")
}

// TestUS0052_RelatedObjectsViaSharedEntityMention verifies that two objects
// mentioning the same entity are discovered as related via graph traversal.
func TestUS0052_RelatedObjectsViaSharedEntityMention(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	entityURI := uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "kafka-broker"}
	env.svc.Pipes.Upsert("test.related", &pipeline.Pipeline{
		PipelineName: "test.related",
		Steps:        []pipeline.PipelineStep{&entityMentionStep{mentions: []uri.URI{entityURI}}},
	})

	var resultIDs []string
	for i := 0; i < 2; i++ {
		jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
			Content:  fmt.Sprintf("kafka consumer group partition rebalance doc %d", i),
			Type:     "text",
			Pipeline: "test.related",
			Source:   "e2e-test",
		})
		require.NoError(t, err)
		job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
		require.NotEmpty(t, job.ResultID)
		resultIDs = append(resultIDs, job.ResultID)
	}

	require.Len(t, resultIDs, 2)
	// RelatedObjects from obj[0] must include obj[1] (linked via shared entity edge).
	related, err := env.svc.RelatedObjects(context.Background(), resultIDs[0], 2, 10)
	require.NoError(t, err)
	assert.True(t, containsIDPtr(related, resultIDs[1]),
		"objects sharing the same entity mention must be related via graph traversal")
}

// TestUS0052_EntityWithNoConnectionsReturnsEmptyNotError verifies that querying
// backlinks for an entity with no connections returns empty list, not an error.
func TestUS0052_EntityWithNoConnectionsReturnsEmptyNotError(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	backlinks, err := env.svc.EntityBacklinks(context.Background(), "nonexistent-entity-slug")
	require.NoError(t, err)
	assert.Empty(t, backlinks)
}
