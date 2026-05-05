package policy_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/domain"
	kitpolicy "hop.top/kit/go/runtime/policy"

	ctxtpolicy "github.com/ideacrafterslabs/ctxt/internal/policy"
)

// TestInit_DefaultBundle_RejectsDeleteWithoutNote loads the bundled
// default policy via Init and confirms the delete-pipeline-requires-note
// rule denies a kit pre_persisted publish whose payload op is "delete"
// and whose context.note is empty.
func TestInit_DefaultBundle_RejectsDeleteWithoutNote(t *testing.T) {
	t.Setenv("CTXT_POLICY_FILE", filepath.Join("policies_default.yaml"))

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	pub := pol.Publisher()
	require.NotNil(t, pub)

	// Empty context.note: rule must veto.
	err = pub.Publish(context.Background(), "kit.runtime.entity.pre_persisted", "test", domain.PreEntityPayload{
		Op:       domain.OpDelete,
		Phase:    domain.PhasePrePersisted,
		EntityID: "p-1",
	})
	require.Error(t, err)
	var pde *kitpolicy.PolicyDeniedError
	require.True(t, errors.As(err, &pde), "expected PolicyDeniedError, got %T: %v", err, err)
	assert.Equal(t, "delete-pipeline-requires-note", pde.PolicyName)
	assert.True(t, errors.Is(err, domain.ErrConflict))
}

// TestInit_DefaultBundle_AllowsDeleteWithNote confirms the same rule
// passes when the host stuffs a non-empty note into ctx via
// policy.ContextAttrsKey before publishing.
func TestInit_DefaultBundle_AllowsDeleteWithNote(t *testing.T) {
	t.Setenv("CTXT_POLICY_FILE", filepath.Join("policies_default.yaml"))

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	ctx := context.WithValue(context.Background(), kitpolicy.ContextAttrsKey, map[string]any{
		"note": "rotating fixture pipeline",
	})
	err = pol.Publisher().Publish(ctx, "kit.runtime.entity.pre_persisted", "test", domain.PreEntityPayload{
		Op:       domain.OpDelete,
		Phase:    domain.PhasePrePersisted,
		EntityID: "p-1",
	})
	require.NoError(t, err)
}

// TestInit_DefaultBundle_AllowsCreateWithoutNote confirms create ops
// (a non-destructive op) pass without a note. The default bundle only
// gates archive (Update + action=archive) and delete; everything else
// stays open.
func TestInit_DefaultBundle_AllowsCreateWithoutNote(t *testing.T) {
	t.Setenv("CTXT_POLICY_FILE", filepath.Join("policies_default.yaml"))

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	err = pol.Publisher().Publish(context.Background(), "kit.runtime.entity.pre_persisted", "test", domain.PreEntityPayload{
		Op:       domain.OpCreate,
		Phase:    domain.PhasePrePersisted,
		EntityID: "p-1",
	})
	require.NoError(t, err)
}

// TestInit_DefaultBundle_RejectsArchiveWithoutNote confirms the archive
// rule fires on Update with action=archive and empty note. Update
// without action=archive (e.g. unarchive) must NOT trigger this rule.
func TestInit_DefaultBundle_RejectsArchiveWithoutNote(t *testing.T) {
	t.Setenv("CTXT_POLICY_FILE", filepath.Join("policies_default.yaml"))

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	// Archive without note → deny.
	archiveCtx := context.WithValue(context.Background(), kitpolicy.ContextAttrsKey, map[string]any{
		"request_attrs": map[string]any{"action": "archive"},
	})
	err = pol.Publisher().Publish(archiveCtx, "kit.runtime.entity.pre_persisted", "test", domain.PreEntityPayload{
		Op:       domain.OpUpdate,
		Phase:    domain.PhasePrePersisted,
		EntityID: "p-1",
	})
	require.Error(t, err)
	var pde *kitpolicy.PolicyDeniedError
	require.True(t, errors.As(err, &pde))
	assert.Equal(t, "archive-pipeline-requires-note", pde.PolicyName)

	// Unarchive (action != "archive") without note → allow. The CEL
	// short-circuits before even reading context.note.
	unarchiveCtx := context.WithValue(context.Background(), kitpolicy.ContextAttrsKey, map[string]any{
		"request_attrs": map[string]any{"action": "unarchive"},
	})
	require.NoError(t, pol.Publisher().Publish(unarchiveCtx, "kit.runtime.entity.pre_persisted", "test", domain.PreEntityPayload{
		Op:       domain.OpUpdate,
		Phase:    domain.PhasePrePersisted,
		EntityID: "p-1",
	}))
}

// TestInit_NilBus rejects Init with a clear error.
func TestInit_NilBus(t *testing.T) {
	_, err := ctxtpolicy.Init(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bus is required")
}

// TestInit_UnknownTopicFailsLoud confirms misconfig at startup is
// surfaced as an error instead of silently accepting a no-op rule.
func TestInit_UnknownTopicFailsLoud(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	require.NoError(t, writeFile(bad, []byte(
		"policies:\n  - name: bogus\n    on: not.a.real.topic\n    when: 'true'\n    effect: allow\n",
	)))
	t.Setenv("CTXT_POLICY_FILE", bad)

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	_, err := ctxtpolicy.Init(b)
	require.Error(t, err)
}

// writeFile is a test-only helper to avoid importing os in every test.
func writeFile(path string, data []byte) error {
	return writeFileImpl(path, data)
}
