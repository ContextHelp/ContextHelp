package registry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
)

// newEmptyRegistry is newRegistry minus whatever the migrations seeded, so
// these tests hold before and after the placeholder model is removed.
func newEmptyRegistry(t *testing.T) *registry.Store {
	t.Helper()
	r, d := newRegistry(t)
	ctx := context.Background()
	_, err := d.DB().ExecContext(ctx, `DELETE FROM embeddings`)
	require.NoError(t, err)
	_, err = d.DB().ExecContext(ctx, `DELETE FROM embedding_models`)
	require.NoError(t, err)
	return r
}

func TestDefault_NoDefault(t *testing.T) {
	r := newEmptyRegistry(t)
	ctx := context.Background()

	_, err := r.Default(ctx)
	assert.True(t, errors.Is(err, registry.ErrNoDefaultModel), "empty registry: got %v", err)

	require.NoError(t, r.Register(ctx, registry.Model{ModelID: "cand@1", Provider: "ollama", Dimension: 4}, false))
	_, err = r.Default(ctx)
	assert.True(t, errors.Is(err, registry.ErrNoDefaultModel), "non-default only: got %v", err)
}

func TestDefault_ReturnsFlaggedModel(t *testing.T) {
	r := newEmptyRegistry(t)
	ctx := context.Background()

	require.NoError(t, r.Register(ctx, registry.Model{ModelID: "a@1", Provider: "ollama", Dimension: 4}, true))
	require.NoError(t, r.Register(ctx, registry.Model{ModelID: "b@1", Provider: "ollama", Dimension: 8}, false))

	m, err := r.Default(ctx)
	require.NoError(t, err)
	assert.Equal(t, "a@1", m.ModelID)

	_, err = r.SetDefault(ctx, "b@1", 0)
	require.NoError(t, err)
	m, err = r.Default(ctx)
	require.NoError(t, err)
	assert.Equal(t, "b@1", m.ModelID, "Default must see a flip without caching")
	assert.Equal(t, 8, m.Dimension)
}

func TestPopulating(t *testing.T) {
	r := newEmptyRegistry(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	base := now.Add(-time.Hour)

	reg := func(id string, dim int, def bool, at time.Time) {
		t.Helper()
		require.NoError(t, r.Register(ctx,
			registry.Model{ModelID: id, Provider: "ollama", Dimension: dim, RegisteredAt: at}, def))
	}
	reg("cand@1", 8, false, base)
	reg("default@1", 4, true, base.Add(time.Minute))
	reg("retired@1", 8, false, base.Add(2*time.Minute))
	reg("retiring@1", 8, false, base.Add(3*time.Minute))
	reg("unprobed@1", 0, false, base.Add(4*time.Minute))
	require.NoError(t, r.Deprecate(ctx, "retired@1", now.Add(-time.Second)))
	require.NoError(t, r.Deprecate(ctx, "retiring@1", now.Add(24*time.Hour)))

	got, err := r.Populating(ctx, now)
	require.NoError(t, err)
	ids := make([]string, 0, len(got))
	for _, m := range got {
		ids = append(ids, m.ModelID)
	}
	assert.Equal(t, []string{"default@1", "cand@1", "retiring@1"}, ids,
		"default first; effective deprecation and unprobed dimension excluded")
}

// Deprecate refuses the default, so this state is not reachable through
// the registry; if the row says so anyway, the model queries read keeps
// receiving writes.
func TestPopulating_DeprecatedDefaultStillWritten(t *testing.T) {
	r, d := newRegistry(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

	require.NoError(t, r.Register(ctx, registry.Model{ModelID: "default@1", Provider: "ollama", Dimension: 4}, true))
	require.ErrorIs(t, r.Deprecate(ctx, "default@1", now.Add(-time.Hour)), registry.ErrIsDefault)
	_, err := d.DB().ExecContext(ctx, `UPDATE embedding_models SET deprecated_at = ? WHERE model_id = ?`,
		now.Add(-time.Hour).Format(time.RFC3339), "default@1")
	require.NoError(t, err)

	got, err := r.Populating(ctx, now)
	require.NoError(t, err)
	require.Len(t, got, 1, "the model queries read keeps receiving writes")
	assert.Equal(t, "default@1", got[0].ModelID)
}

func TestForDriver(t *testing.T) {
	_, d := newRegistry(t)
	ctx := context.Background()

	r, err := registry.ForDriver(d)
	require.NoError(t, err)
	require.NoError(t, r.Register(ctx, registry.Model{ModelID: "via-driver@1", Provider: "ollama", Dimension: 4}, false))
	got, err := r.Get(ctx, "via-driver@1")
	require.NoError(t, err)
	assert.Equal(t, 4, got.Dimension)

	_, err = registry.ForDriver(struct{}{})
	assert.Error(t, err, "a backend without a database handle must be refused")
}
