package registry_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// newRegistry stands up a real sqlite driver (per the no-mocks policy) and
// returns a registry.Store wired to it plus a cleanup callback the test
// driver auto-installs via t.Cleanup. The returned Store owns no resources of
// its own — closing the driver is the caller's responsibility (already
// handled by the t.Cleanup hook the migration framework installs).
func newRegistry(t testing.TB) (*registry.Store, *sqlite.Driver) {
	t.Helper()
	dir := t.TempDir()
	d, err := sqlite.New(filepath.Join(dir, "test.db"))
	require.NoError(t, err, "sqlite.New")
	require.NoError(t, d.Init(context.Background()), "driver.Init")
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return registry.New(d.DB()), d
}

func TestFreshInstall_HasNoModels(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()

	models, err := r.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, models, "fresh install must not seed a placeholder model")

	_, err = r.Default(ctx)
	assert.ErrorIs(t, err, registry.ErrNoDefaultModel, "no default until an operator registers one")
}

func TestRegister_RoundTrip(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()

	m := registry.Model{
		ModelID:    "openai-text-embedding-3-small@2025-01-15",
		Provider:   registry.ProviderOpenAI,
		Dimension:  1536,
		ConfigJSON: `{"endpoint":"https://api.openai.com/v1"}`,
	}
	require.NoError(t, r.Register(ctx, m, false))

	got, err := r.Get(ctx, m.ModelID)
	require.NoError(t, err)
	assert.Equal(t, m.ModelID, got.ModelID)
	assert.Equal(t, registry.ProviderOpenAI, got.Provider)
	assert.Equal(t, 1536, got.Dimension)
	assert.False(t, got.IsDefault)
	assert.Equal(t, m.ConfigJSON, got.ConfigJSON)
}

func TestRegister_DuplicateIsRejected(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()

	m := registry.Model{
		ModelID:   "voyage-large-2@2025-09-01",
		Provider:  registry.ProviderVoyage,
		Dimension: 1024,
	}
	require.NoError(t, r.Register(ctx, m, false))

	err := r.Register(ctx, m, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, registry.ErrModelAlreadyRegistered),
		"duplicate must return ErrModelAlreadyRegistered, got %v", err)
}

func TestPartialUniqueIndex_PreventsTwoDefaults(t *testing.T) {
	r, d := newRegistry(t)
	ctx := context.Background()

	// Register a default, then a second model with makeDefault=true and
	// confirm Register clears the previous default rather than violating
	// the unique index.
	first := "ollama-nomic-embed-text@2025-01-15"
	require.NoError(t, r.Register(ctx, registry.Model{
		ModelID:   first,
		Provider:  registry.ProviderOllama,
		Dimension: 768,
	}, true))
	require.NoError(t, r.Register(ctx, registry.Model{
		ModelID:   "openai-text-embedding-3-small@2025-01-15",
		Provider:  registry.ProviderOpenAI,
		Dimension: 1536,
	}, true))

	models, err := r.List(ctx)
	require.NoError(t, err)
	defaults := 0
	for _, m := range models {
		if m.IsDefault {
			defaults++
		}
	}
	assert.Equal(t, 1, defaults, "exactly one is_default = 1 row must exist")

	// Sanity: directly bypass the registry and try to mark two models
	// as default. The partial unique index must reject this.
	_, err = d.DB().ExecContext(ctx,
		`UPDATE embedding_models SET is_default = 1 WHERE model_id = ?`,
		first,
	)
	require.Error(t, err, "second is_default=1 must be rejected by partial unique index")
}

func TestSetDefault_FlipsAtomically(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()

	oldID := "ollama-nomic-embed-text@2025-01-15"
	require.NoError(t, r.Register(ctx, registry.Model{
		ModelID:   oldID,
		Provider:  registry.ProviderOllama,
		Dimension: 768,
	}, true))
	require.NoError(t, r.Register(ctx, registry.Model{
		ModelID:   "openai-text-embedding-3-small@2025-01-15",
		Provider:  registry.ProviderOpenAI,
		Dimension: 1536,
	}, false))

	// oldID is currently default. Flip to the new model.
	newID := "openai-text-embedding-3-small@2025-01-15"
	require.NoError(t, r.SetDefault(ctx, newID))

	got, err := r.Get(ctx, newID)
	require.NoError(t, err)
	assert.True(t, got.IsDefault, "newly-promoted model is default")

	previous, err := r.Get(ctx, oldID)
	require.NoError(t, err)
	assert.False(t, previous.IsDefault, "previous default must be demoted")
}

func TestSetDefault_UnknownModelIsRejected(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()

	err := r.SetDefault(ctx, "does-not-exist@2099-01-01")
	require.Error(t, err)
	assert.True(t, errors.Is(err, registry.ErrModelNotFound),
		"unknown model_id must return ErrModelNotFound, got %v", err)
}

func TestDeprecate_StampsTimestamp(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()

	id := "openai-text-embedding-3-large@2026-05-01"
	require.NoError(t, r.Register(ctx, registry.Model{
		ModelID:   id,
		Provider:  registry.ProviderOpenAI,
		Dimension: 3072,
	}, false))

	when := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	require.NoError(t, r.Deprecate(ctx, id, when))

	got, err := r.Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got.DeprecatedAt, "deprecated_at must be set")
	assert.Equal(t, when, got.DeprecatedAt.UTC())
}

func TestDeprecate_UnknownModelIsRejected(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()

	err := r.Deprecate(ctx, "does-not-exist@2099-01-01", time.Time{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, registry.ErrModelNotFound))
}

func TestList_OrdersByRegisteredAt(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()

	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, id := range []string{"a@2025-01-01", "b@2025-02-01", "c@2025-03-01"} {
		require.NoError(t, r.Register(ctx, registry.Model{
			ModelID:      id,
			Provider:     registry.ProviderOpenAI,
			Dimension:    1536,
			RegisteredAt: t0.AddDate(0, i, 0),
		}, false))
	}

	models, err := r.List(ctx)
	require.NoError(t, err)
	got := []string{}
	for _, m := range models {
		got = append(got, m.ModelID)
	}
	assert.Equal(t, []string{"a@2025-01-01", "b@2025-02-01", "c@2025-03-01"}, got)
}

// TestListWithCoverage_EmptyCorpus verifies that on a fresh DB with no
// objects, a registered model reports coverage = 1.0 (the
// vacuous-coverage convention — operators reading `ctxt embeddings list`
// should not see 0.0 just because nothing has been ingested yet).
func TestListWithCoverage_EmptyCorpus(t *testing.T) {
	r, _ := newRegistry(t)
	ctx := context.Background()
	require.NoError(t, r.Register(ctx, registry.Model{
		ModelID: "ollama-nomic-embed-text@2025-01-15", Provider: registry.ProviderOllama, Dimension: 768,
	}, true))

	models, err := r.ListWithCoverage(ctx)
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.InDelta(t, 1.0, models[0].Coverage, 1e-9, "empty corpus → vacuous full coverage")
}

// TestListWithCoverage_PartialCoverage seeds a small corpus and a candidate
// model, embeds half the objects under the candidate, and verifies the
// reported coverage matches the fraction. This is the operator-facing
// recall guard's primary input — getting it wrong means Phase 3
// (T-0584) ships a guard that lies.
func TestListWithCoverage_PartialCoverage(t *testing.T) {
	r, d := newRegistry(t)
	ctx := context.Background()

	// Seed 4 objects so the math (1/4, 2/4) is exact in float64.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"o1", "o2", "o3", "o4"} {
		_, err := d.DB().ExecContext(ctx,
			`INSERT INTO objects (id, type, created_at, updated_at) VALUES (?, 'note', ?, ?)`,
			id, now, now,
		)
		require.NoError(t, err, "seed object %s", id)
	}

	// Register a default and a candidate; embed 2 of the 4 objects under
	// the candidate.
	require.NoError(t, r.Register(ctx, registry.Model{
		ModelID: "ollama-nomic-embed-text@2025-01-15", Provider: registry.ProviderOllama, Dimension: 768,
	}, true))
	candidate := registry.Model{
		ModelID:   "test-candidate@2026-05-07",
		Provider:  "test",
		Dimension: 8,
	}
	require.NoError(t, r.Register(ctx, candidate, false))

	for _, id := range []string{"o1", "o2"} {
		_, err := d.DB().ExecContext(ctx,
			`INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, created_at)
			 VALUES (?, ?, 0, ?, ?)`,
			id, candidate.ModelID, []byte{0, 1, 2, 3}, time.Now().UTC().Format(time.RFC3339),
		)
		require.NoError(t, err)
	}

	models, err := r.ListWithCoverage(ctx)
	require.NoError(t, err)
	require.Len(t, models, 2, "default + candidate")

	var defaultCov, candidateCov float64
	for _, m := range models {
		switch m.ModelID {
		case candidate.ModelID:
			candidateCov = m.Coverage
		default:
			defaultCov = m.Coverage
		}
	}
	assert.InDelta(t, 0.5, candidateCov, 1e-9, "2 of 4 objects covered under candidate")
	assert.InDelta(t, 0.0, defaultCov, 1e-9, "default model has no embeddings rows under it yet")
}

// TestRegister_LargeDimensionAllowedOnSQLite pins the asymmetry contract:
// SQLite brute-forces any dimension, so no indexability ceiling applies —
// the ceiling is a Postgres/HNSW property enforced only there.
func TestRegister_LargeDimensionAllowedOnSQLite(t *testing.T) {
	r, _ := newRegistry(t)
	err := r.Register(context.Background(), registry.Model{
		ModelID:   "giant-embedding",
		Provider:  registry.ProviderOpenAI,
		Dimension: 3000,
	}, false)
	require.NoError(t, err, "sqlite has no indexability ceiling")
}
