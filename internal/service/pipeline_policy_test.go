package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/domain"
	kitpolicy "hop.top/kit/go/runtime/policy"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	ctxtpolicy "github.com/ideacrafterslabs/ctxt/internal/policy"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newPolicyTestService builds a Service with a kit/bus + the bundled
// default policy engine attached, so destructive ops fire the
// pre_persisted topic and the policy engine vetoes when context.note
// is absent.
func newPolicyTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	t.Setenv("CTXT_POLICY_FILE", "../policy/policies_default.yaml")

	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := pipeline.DefaultRegistry()
	engine := search.NewEngine(driver)

	b := bus.New()
	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)

	svc := NewWithOptions(driver, q, pipes, engine, "", nil,
		[]Option{WithPolicyPublisher(pol.Publisher())},
	)
	cleanup := func() {
		pol.Close()
		_ = b.Close(context.Background())
	}
	return svc, cleanup
}

// TestPolicy_DeletePipeline_DeniesWithoutNote confirms the bundled
// delete-pipeline-requires-note rule vetoes a delete when nothing
// stuffed --note into ctx.
func TestPolicy_DeletePipeline_DeniesWithoutNote(t *testing.T) {
	svc, cleanup := newPolicyTestService(t)
	defer cleanup()

	ctx := context.Background()
	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "to-delete",
		Steps: `[{"name":"text_summary"}]`,
	})
	require.NoError(t, err)

	err = svc.DeletePipeline(ctx, "to-delete")
	require.Error(t, err)
	var pde *kitpolicy.PolicyDeniedError
	require.True(t, errors.As(err, &pde), "expected PolicyDeniedError, got %T: %v", err, err)
	assert.Equal(t, "delete-pipeline-requires-note", pde.PolicyName)
	assert.True(t, errors.Is(err, domain.ErrConflict))

	got, err := svc.GetPipeline(ctx, "to-delete")
	require.NoError(t, err, "veto must abort before storage.Delete")
	assert.NotNil(t, got)
}

// TestPolicy_DeletePipeline_AllowsWithNote confirms the same op
// succeeds when --note is plumbed.
func TestPolicy_DeletePipeline_AllowsWithNote(t *testing.T) {
	svc, cleanup := newPolicyTestService(t)
	defer cleanup()

	ctx := context.Background()
	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "to-delete",
		Steps: `[{"name":"text_summary"}]`,
	})
	require.NoError(t, err)

	noteCtx := context.WithValue(ctx, kitpolicy.ContextAttrsKey, map[string]any{
		"note": "rotating fixture pipelines per ops review 2026-Q2",
	})
	require.NoError(t, svc.DeletePipeline(noteCtx, "to-delete"))

	_, err = svc.GetPipeline(ctx, "to-delete")
	require.Error(t, err, "delete should remove the row after policy allows")
}

// TestPolicy_ArchivePipeline_DeniesWithoutNote_PassesUnarchive confirms
// the archive guard fires only when action=="archive". Unarchive shares
// the same Op="update" topic but the CEL rule short-circuits via
// context.request_attrs.action.
func TestPolicy_ArchivePipeline_DeniesWithoutNote_PassesUnarchive(t *testing.T) {
	svc, cleanup := newPolicyTestService(t)
	defer cleanup()

	ctx := context.Background()
	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "to-archive",
		Steps: `[{"name":"text_summary"}]`,
	})
	require.NoError(t, err)

	// Archive without note → policy denies.
	err = svc.ArchivePipeline(ctx, "to-archive")
	require.Error(t, err)
	var pde *kitpolicy.PolicyDeniedError
	require.True(t, errors.As(err, &pde))
	assert.Equal(t, "archive-pipeline-requires-note", pde.PolicyName)

	got, err := svc.GetPipeline(ctx, "to-archive")
	require.NoError(t, err)
	assert.False(t, got.Archived, "veto must abort before storage flips Archived=true")

	// Archive with note → succeeds. Then unarchive without a note —
	// the action!="archive" leg of the CEL OR keeps it allowed.
	noteCtx := context.WithValue(ctx, kitpolicy.ContextAttrsKey, map[string]any{
		"note": "compliance freeze for legal hold INC-1043",
	})
	require.NoError(t, svc.ArchivePipeline(noteCtx, "to-archive"))
	require.NoError(t, svc.UnarchivePipeline(ctx, "to-archive"))
}
