package registry_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The plans only read: they run, guards included, on a store opened
// read-only, as a --dry-run opens it.
func TestPlans_RunOnReadOnlyStore(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plan.db")
	d, err := sqlite.New(path, storageutil.NewTestBlobStore(t))
	require.NoError(t, err)
	require.NoError(t, d.Init(ctx))
	t.Cleanup(func() { _ = d.Close(ctx) })
	rw := registry.New(d.DB())
	require.NoError(t, rw.Register(ctx, registry.Model{ModelID: "ro-a@1", Provider: "ollama", Dimension: 4}, true))
	require.NoError(t, rw.Register(ctx, registry.Model{ModelID: "ro-b@1", Provider: "ollama", Dimension: 4}, false))

	ro, err := sqlite.NewReadOnly(path, storageutil.NewTestBlobStore(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close(ctx) })
	reg := registry.NewFor(ro.DB(), "sqlite")

	p, err := reg.PlanSetDefault(ctx, "ro-b@1", 0)
	require.NoError(t, err, "PlanSetDefault on a read-only store")
	require.True(t, p.Changed)
	require.Equal(t, "ro-a@1", p.Previous)
	_, err = reg.PlanSetDefault(ctx, "ro-x@1", 0)
	require.ErrorIs(t, err, registry.ErrModelNotFound, "the guards run read-only")

	require.NoError(t, reg.PlanDeprecate(ctx, "ro-b@1", time.Now()), "PlanDeprecate on a read-only store")
	require.ErrorIs(t, reg.PlanDeprecate(ctx, "ro-a@1", time.Now()), registry.ErrIsDefault)
	require.ErrorIs(t, reg.PlanDeprecate(ctx, "ro-x@1", time.Now()), registry.ErrModelNotFound)
}
