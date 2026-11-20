package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Reprocessing entity_extractor on a stored object persists the mentions
// it adds as the ingest worker does: thin entity rows and mention edges,
// tagged with the field they were extracted from, not "mentions".
func TestReprocessEntityExtractorPersistsMentionEdges(t *testing.T) {
	svc := newFanOutService(t)
	ctx := ownerCtx()
	require.NoError(t, svc.store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-reproc", Type: "text", RawContent: "Met @person.alice about @project.mobile",
	}))

	r := &jobs.Reprocessor{Objects: svc, LLM: providers.NewStubLLMProvider()}
	payload, err := json.Marshal(jobs.ReprocessPayload{ObjectID: "obj-reproc", Step: jobs.StepEntityExtractor})
	require.NoError(t, err)
	_, err = r.Handle(ctx, &storage.Job{ID: "j", Payload: string(payload)})
	require.NoError(t, err)

	edges, err := svc.store.Edges().ListFrom(ctx, "object", "obj-reproc")
	require.NoError(t, err)
	byTarget := edgesByTarget(edges, storage.EdgeTypeMentions)
	require.Len(t, byTarget, 2)
	for _, target := range []string{"ctxt://entity/person/alice", "ctxt://entity/project/mobile"} {
		if assert.Len(t, byTarget[target], 1, target) {
			assert.Equal(t, storage.MentionSourceRawContent, byTarget[target][0].Metadata[storage.ProvenanceSourceField], target)
		}
	}
	requireThinEntity(t, svc, "person/alice")
	requireThinEntity(t, svc, "project/mobile")
}
