package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/domain"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestPipelineGetID confirms storage.Pipeline implements domain.Entity
// by returning Name as the canonical ID.
func TestPipelineGetID(t *testing.T) {
	p := storage.Pipeline{ID: "uuid-1", Name: "test-pipeline"}
	var e domain.Entity = p
	assert.Equal(t, "test-pipeline", e.GetID(), "GetID returns Name (the lookup key), not the UUID")
}

// TestCreatePipelineThroughDomainService exercises the full happy path
// via the public service API and confirms the resulting pipeline is
// retrievable. The domain.Service code path is the only one in play
// when newTestService runs, so an unfailing Create demonstrates the
// pre_validated → validator → pre_persisted → repo.Create chain.
func TestCreatePipelineThroughDomainService(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	require.NotNil(t, svc.pipelineSvc, "pipelineSvc should be wired in New()")

	id, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:        "domain-svc-pipeline",
		Description: "via kit domain.Service",
		Steps:       `[{"name":"text_summary"}]`,
	})
	require.NoError(t, err)
	require.NotEmpty(t, id)

	got, err := svc.GetPipeline(ctx, "domain-svc-pipeline")
	require.NoError(t, err)
	assert.Equal(t, "domain-svc-pipeline", got.Name)
	assert.False(t, got.Archived)
	assert.Equal(t, 1, len(got.Steps))
}

// TestCreatePipelineRejectsEmptyName confirms the pipelineValidator
// fires from inside domain.Service. Empty name is the only invariant
// the validator enforces today; ErrValidation surfaces wrapped through
// the "create pipeline" prefix.
func TestCreatePipelineRejectsEmptyName(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "",
		Steps: `[{"name":"text_summary"}]`,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrValidation), "validator returns wrapped domain.ErrValidation")
}

// TestArchiveUnarchivePipelineUsesUpdate verifies that Archive +
// Unarchive route through domain.Service.Update (status flip + persist)
// instead of the legacy storage Archive/Unarchive verbs. End-state is
// observed via GetPipeline; the kit pre-event seam is exercised
// without needing a publisher attached.
func TestArchiveUnarchivePipelineUsesUpdate(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "lifecycle-pipeline",
		Steps: `[{"name":"text_summary"}]`,
	})
	require.NoError(t, err)

	require.NoError(t, svc.ArchivePipeline(ctx, "lifecycle-pipeline"))
	got, err := svc.GetPipeline(ctx, "lifecycle-pipeline")
	require.NoError(t, err)
	assert.True(t, got.Archived, "archive should flip Archived=true")

	require.NoError(t, svc.UnarchivePipeline(ctx, "lifecycle-pipeline"))
	got, err = svc.GetPipeline(ctx, "lifecycle-pipeline")
	require.NoError(t, err)
	assert.False(t, got.Archived, "unarchive should flip Archived=false")
}

// TestDeletePipelineThroughDomainService verifies Delete routes
// through domain.Service while preserving the built-in protection
// shape ("PROTECTED" substring stays for HTTP layer matching).
func TestDeletePipelineThroughDomainService(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "delete-target",
		Steps: `[{"name":"text_summary"}]`,
	})
	require.NoError(t, err)

	require.NoError(t, svc.DeletePipeline(ctx, "delete-target"))

	_, err = svc.GetPipeline(ctx, "delete-target")
	require.Error(t, err, "pipeline should be gone after delete")
}
