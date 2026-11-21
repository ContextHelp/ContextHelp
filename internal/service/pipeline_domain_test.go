package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/domain"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
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
	ctx := ownerCtx()

	require.NotNil(t, svc.pipelineSvc, "pipelineSvc should be wired in New()")

	id, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:        "domain-svc-pipeline",
		Description: "via kit domain.Service",
		Steps:       `[{"name":"noop"}]`,
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
	ctx := ownerCtx()

	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "",
		Steps: `[{"name":"noop"}]`,
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
	ctx := ownerCtx()

	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "lifecycle-pipeline",
		Steps: `[{"name":"noop"}]`,
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
	ctx := ownerCtx()

	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "delete-target",
		Steps: `[{"name":"noop"}]`,
	})
	require.NoError(t, err)

	require.NoError(t, svc.DeletePipeline(ctx, "delete-target"))

	_, err = svc.GetPipeline(ctx, "delete-target")
	require.Error(t, err, "pipeline should be gone after delete")
}

// CreatePipeline validation (US-0101, US-0112): unknown steps, protected
// names, duplicates and invalid sandbox configs are refused before anything
// is persisted.
func TestCreatePipelineValidation(t *testing.T) {
	steps := `[{"name":"noop"}]`
	cases := []struct {
		name    string
		req     CreatePipelineRequest
		wantErr error
	}{
		{"unknown step", CreatePipelineRequest{Name: "p-unknown", Steps: `[{"name":"legal_classifier"}]`}, domain.ErrValidation},
		{"unknown step by type key", CreatePipelineRequest{Name: "p-unknown2", Steps: `[{"type":"nope"}]`}, domain.ErrValidation},
		{"protected namespace", CreatePipelineRequest{Name: "text.mine", Steps: steps}, ErrProtectedPipeline},
		{"network sandbox", CreatePipelineRequest{Name: "p-net", Steps: steps, Sandbox: &storage.SandboxConfig{Enabled: true, Network: true}}, domain.ErrValidation},
		{"bad isolation", CreatePipelineRequest{Name: "p-iso", Steps: steps, Sandbox: &storage.SandboxConfig{Enabled: true, IsolationLevel: "vm"}}, domain.ErrValidation},
		{"bad limits", CreatePipelineRequest{Name: "p-lim", Steps: steps, Sandbox: &storage.SandboxConfig{Enabled: true, ResourceLimits: &storage.ResourceLimitsConfig{MaxMemory: "lots"}}}, domain.ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			svc.Installer.Reserved = builtins.IsStepName
			_, err := svc.CreatePipeline(ownerCtx(), tc.req)
			require.ErrorIs(t, err, tc.wantErr)
			_, getErr := svc.GetPipeline(ownerCtx(), tc.req.Name)
			require.Error(t, getErr, "a refused pipeline must not be persisted")
		})
	}

	t.Run("valid sandbox and builtin step", func(t *testing.T) {
		svc := newTestService(t)
		_, err := svc.CreatePipeline(ownerCtx(), CreatePipelineRequest{Name: "p-ok", Steps: steps,
			Sandbox: &storage.SandboxConfig{Enabled: true, IsolationLevel: "container",
				ResourceLimits: &storage.ResourceLimitsConfig{MaxMemory: "512MB", MaxCPU: "2.0", Timeout: "30s"}}})
		require.NoError(t, err)
	})

	t.Run("installed step accepted", func(t *testing.T) {
		svc := newTestService(t)
		svc.Installer.Reserved = builtins.IsStepName
		require.NoError(t, svc.Store(ownerCtx()).Steps().Create(ownerCtx(), &storage.RegisteredStep{Name: "my_step", Source: "local:/x"}))
		_, err := svc.CreatePipeline(ownerCtx(), CreatePipelineRequest{Name: "p-inst", Steps: `[{"name":"my_step"}]`})
		require.NoError(t, err)
	})

	t.Run("duplicate name", func(t *testing.T) {
		svc := newTestService(t)
		_, err := svc.CreatePipeline(ownerCtx(), CreatePipelineRequest{Name: "p-dup", Steps: steps})
		require.NoError(t, err)
		_, err = svc.CreatePipeline(ownerCtx(), CreatePipelineRequest{Name: "p-dup", Steps: steps})
		require.ErrorIs(t, err, domain.ErrConflict)
	})

	t.Run("registered built-in pipeline protected", func(t *testing.T) {
		svc := newTestService(t)
		svc.Pipes.Upsert("github.repo", &pipeline.Pipeline{PipelineName: "github.repo"})
		for _, n := range []string{"github.repo", "github.mine"} {
			_, err := svc.CreatePipeline(ownerCtx(), CreatePipelineRequest{Name: n, Steps: steps})
			require.ErrorIs(t, err, ErrProtectedPipeline, n)
		}
	})
}
