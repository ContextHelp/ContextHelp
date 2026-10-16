package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// runIngest runs job through pipes on svc's store with fan-out wired as
// serve wires it, waits for the job to settle and returns the stored
// object's ID.
func runIngest(t *testing.T, svc *Service, pipes pipeline.Registry, job *storage.Job) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, svc.Queue.Enqueue(ctx, job))
	pool := jobs.NewWorkerPool(svc.Queue, pipes, svc.store, 1, nil, config.JobsConfig{
		PollInterval: 20 * time.Millisecond, StaleTimeout: 30 * time.Minute, MaxRetries: 3, MaxHops: 5,
	})
	pool.SetFanOut(func(_ context.Context, objectID string) error {
		_, err := svc.FanOut(ownerCtx(), objectID)
		return err
	})
	go func() {
		for ctx.Err() == nil {
			got, _ := svc.GetJob(ownerCtx(), job.ID)
			if got != nil && (got.Status == storage.JobCompleted || got.Status == storage.JobFailed) {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	pool.Start(ctx)
	got, err := svc.GetJob(ownerCtx(), job.ID)
	require.NoError(t, err)
	require.Equal(t, storage.JobCompleted, got.Status, "job error: %s", got.Error)
	require.NotEmpty(t, got.ResultID)
	return got.ResultID
}

// edgesByTarget groups the mention edges of one direction by the entity
// they name: ToID for mentions edges, FromID for mentioned_in edges.
func edgesByTarget(edges []*storage.Edge, edgeType string) map[string][]*storage.Edge {
	out := map[string][]*storage.Edge{}
	for _, e := range edges {
		if e.EdgeType != edgeType {
			continue
		}
		key := e.ToID
		if edgeType == "mentioned_in" {
			key = e.FromID
		}
		out[key] = append(out[key], e)
	}
	return out
}

// TestMentionEdges_OneRowPerEntityAcrossWriters ingests one object through
// every mention-edge writer in turn: entity_extractor and entity_resolver
// (with live stores) in the pipeline, the worker's edge write, then
// fan-out. The writers must agree: one mentions edge and one mentioned_in
// edge per entity, both naming the entity by the mention's canonical
// ctxt://entity/ URI, with the provenance of the mention intact, and the
// entity's backlinks list the object once. A mention the caller asserts
// twice is still one entity.
func TestMentionEdges_OneRowPerEntityAcrossWriters(t *testing.T) {
	svc := newFanOutService(t)
	ctx := ownerCtx()

	pipes := pipeline.NewRegistry()
	pipes.Register("test.mention-writers", &pipeline.Pipeline{
		PipelineName: "test.mention-writers",
		Steps: []pipeline.PipelineStep{
			steps.NewEntityExtractor(),
			steps.NewEntityResolverWithStores(svc.store.Entities(), svc.store.Edges()),
		},
	})
	now := time.Now().Truncate(time.Second)
	objID := runIngest(t, svc, pipes, &storage.Job{
		ID: "job-mention-writers", Type: "ingest:text", Status: storage.JobPending,
		Payload: "Met @person.alice about @project.mobile", Pipeline: "test.mention-writers",
		Source: "test", MaxRetries: 3, CreatedAt: now, UpdatedAt: now,
		UserMentions: []string{"@person.alice", "@person.alice"},
	})

	alice, ok := mentions.Parse("@person.alice")
	require.True(t, ok)
	mobile, ok := mentions.Parse("@project.mobile")
	require.True(t, ok)
	// draft.Mentions is [alice, alice, mobile]: the caller's first, then
	// the one extracted from the text, so alice's node is the first and
	// mobile's the third.
	wantProvenance := map[string]map[string]any{
		alice.String(): {
			storage.ProvenanceSourceNode:  storage.NewNodeID(objID, storage.NodeTypeEntityMention, 0),
			storage.ProvenanceSourceField: storage.MentionSourceMentions,
		},
		mobile.String(): {
			storage.ProvenanceSourceNode:  storage.NewNodeID(objID, storage.NodeTypeEntityMention, 2),
			storage.ProvenanceSourceField: storage.MentionSourceRawContent,
		},
	}

	fwd, err := svc.store.Edges().ListFrom(ctx, "object", objID)
	require.NoError(t, err)
	byTarget := edgesByTarget(fwd, "mentions")
	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	assert.ElementsMatch(t, []string{alice.String(), mobile.String()}, targets,
		"every mentions edge names the entity by its canonical URI")
	for target, want := range wantProvenance {
		if assert.Len(t, byTarget[target], 1, "mentions edges to %s", target) {
			assert.Equal(t, want, byTarget[target][0].Metadata, "provenance of the mentions edge to %s", target)
		}
	}

	rev, err := svc.store.Edges().ListTo(ctx, "object", objID)
	require.NoError(t, err)
	bySource := edgesByTarget(rev, "mentioned_in")
	for target, want := range wantProvenance {
		if assert.Len(t, bySource[target], 1, "mentioned_in edges from %s", target) {
			assert.Equal(t, want, bySource[target][0].Metadata, "provenance of the mentioned_in edge from %s", target)
		}
	}
	assert.Len(t, bySource, 2, "mentioned_in sources: %v", bySource)

	for _, slug := range []string{"person/alice", "project/mobile"} {
		objs, err := svc.EntityBacklinks(ctx, slug)
		require.NoError(t, err)
		ids := make([]string, 0, len(objs))
		for _, o := range objs {
			ids = append(ids, o.ID)
		}
		assert.Equal(t, []string{objID}, ids, "backlinks of %s", slug)
	}
}
