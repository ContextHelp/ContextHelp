package registry_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// lifecycleDriver is what the lifecycle suite needs from a driver: the
// storage surface plus the raw handle the registry and signatures live in.
type lifecycleDriver interface {
	storage.StorageDriver
	DB() *sql.DB
}

// lifecycleEnv is one backend under test: a fresh, migrated database.
type lifecycleEnv struct {
	t       *testing.T
	ctx     context.Context
	reg     *registry.Store
	drv     lifecycleDriver
	dialect indexsig.Dialect
}

// runLifecycleSuite runs the set-default / deprecate / purge contract
// against a backend. newDriver returns a fresh, migrated database per
// subtest.
func runLifecycleSuite(t *testing.T, newDriver func(t *testing.T) lifecycleDriver) {
	for name, fn := range map[string]func(e *lifecycleEnv){
		"SetDefaultCoverageGuard":         testSetDefaultCoverageGuard,
		"SetDefaultRefusalChangesNothing": testSetDefaultRefusalChangesNothing,
		"SetDefaultPlanDoesNotFlip":       testSetDefaultPlanDoesNotFlip,
		"SetDefaultConcurrentOneDefault":  testSetDefaultConcurrentOneDefault,
		"DeprecateRefusesDefault":         testDeprecateRefusesDefault,
		"PopulatingDropsAfterDeprecation": testPopulatingDropsAfterDeprecation,
		"PurgeGuards":                     testPurgeGuards,
		"PurgeAfterGrace":                 testPurgeAfterGrace,
	} {
		t.Run(name, func(t *testing.T) {
			drv := newDriver(t)
			dialect := indexsig.DialectSQLite
			if d, ok := drv.(interface{ SQLDialect() string }); ok && d.SQLDialect() == "postgres" {
				dialect = indexsig.DialectPostgres
			}
			fn(&lifecycleEnv{
				t: t, ctx: context.Background(), drv: drv, dialect: dialect,
				reg: registry.NewFor(drv.DB(), string(dialect)),
			})
		})
	}
}

func TestLifecycle_SQLite(t *testing.T) {
	runLifecycleSuite(t, func(t *testing.T) lifecycleDriver {
		_, d := newRegistry(t)
		return d
	})
}

// --- fixture helpers -------------------------------------------------------

// model registers id at dimension 4 and builds its index.
func (e *lifecycleEnv) model(id string, makeDefault bool) {
	e.t.Helper()
	require.NoError(e.t, e.reg.Register(e.ctx, registry.Model{ModelID: id, Provider: "ollama", Dimension: 4}, makeDefault))
	require.NoError(e.t, e.drv.Embeddings().EnsureIndex(e.ctx,
		storage.EmbeddingModelSpec{ModelID: id, Provider: "ollama", Dimension: 4}))
}

// objects creates n objects named <prefix>-<i> and returns their IDs.
func (e *lifecycleEnv) objects(prefix string, n int) []string {
	e.t.Helper()
	ids := make([]string, 0, n)
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-%d", prefix, i)
		require.NoError(e.t, e.drv.Objects().Create(e.ctx, &storage.KnowledgeObject{
			ID: id, Type: "note", Status: "active", RawContent: "lifecycle fixture " + id,
			CreatedAt: now, UpdatedAt: now,
		}))
		ids = append(ids, id)
	}
	return ids
}

// embed writes a one-hot vector for each object under modelID.
func (e *lifecycleEnv) embed(modelID string, ids ...string) {
	e.t.Helper()
	for i, id := range ids {
		v := make([]float32, 4)
		v[i%4] = 1
		require.NoError(e.t, e.drv.Embeddings().Put(e.ctx, id, []storage.ObjectVector{{ModelID: modelID, Vector: v}}))
	}
}

func (e *lifecycleEnv) defaultID() string {
	e.t.Helper()
	m, err := e.reg.Default(e.ctx)
	if errors.Is(err, registry.ErrNoDefaultModel) {
		return ""
	}
	require.NoError(e.t, err)
	return m.ModelID
}

// defaults counts is_default = 1 rows straight from the table.
func (e *lifecycleEnv) defaults() int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.drv.DB().QueryRowContext(e.ctx,
		`SELECT COUNT(*) FROM embedding_models WHERE is_default = 1`).Scan(&n))
	return n
}

func (e *lifecycleEnv) rows(modelID string) int {
	e.t.Helper()
	q := `SELECT COUNT(*) FROM embeddings WHERE model_id = ?`
	if e.dialect == indexsig.DialectPostgres {
		q = `SELECT COUNT(*) FROM embeddings WHERE model_id = $1`
	}
	var n int
	require.NoError(e.t, e.drv.DB().QueryRowContext(e.ctx, q, modelID).Scan(&n))
	return n
}

func (e *lifecycleEnv) signature(modelID string) *indexsig.Row {
	e.t.Helper()
	row, err := indexsig.Load(e.ctx, e.drv.DB(), e.dialect, indexsig.EmbeddingSignatureID(modelID))
	require.NoError(e.t, err)
	return row
}

// searchable reports whether modelID's index answers a query.
func (e *lifecycleEnv) searchable(modelID string) bool {
	e.t.Helper()
	_, err := e.drv.Embeddings().Search(e.ctx, storage.VectorQuery{ModelID: modelID, Vector: []float32{1, 0, 0, 0}, TopK: 5})
	if errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		return false
	}
	require.NoError(e.t, err)
	return true
}

func (e *lifecycleEnv) snapshot() []registry.Model {
	e.t.Helper()
	models, err := e.reg.List(e.ctx)
	require.NoError(e.t, err)
	return models
}

// --- set-default -------------------------------------------------------------

func testSetDefaultCoverageGuard(e *lifecycleEnv) {
	t := e.t
	e.model("cov-a@1", true)
	e.model("cov-b@1", false)
	ids := e.objects("cov", 4)
	e.embed("cov-a@1", ids...)
	e.embed("cov-b@1", ids[:3]...)

	_, err := e.reg.SetDefault(e.ctx, "cov-b@1", 0.99)
	require.ErrorIs(t, err, registry.ErrBelowCoverage)
	var ce *registry.CoverageError
	require.True(t, errors.As(err, &ce), "refusal must carry the measured coverage: %v", err)
	assert.InDelta(t, 0.75, ce.Coverage, 1e-9)
	assert.InDelta(t, 0.99, ce.MinCoverage, 1e-9)
	assert.Equal(t, "cov-a@1", e.defaultID(), "a refused flip keeps the old default")

	p, err := e.reg.SetDefault(e.ctx, "cov-b@1", 0.75)
	require.NoError(t, err, "coverage equal to the threshold passes")
	assert.Equal(t, registry.Promotion{ModelID: "cov-b@1", Previous: "cov-a@1", Coverage: 0.75, MinCoverage: 0.75, Changed: true}, p)
	assert.Equal(t, "cov-b@1", e.defaultID())
	assert.Equal(t, 1, e.defaults())

	again, err := e.reg.SetDefault(e.ctx, "cov-b@1", 0.99)
	require.NoError(t, err, "re-promoting the default is a no-op, not a refusal")
	assert.False(t, again.Changed)
	assert.Equal(t, "cov-b@1", again.Previous)
	assert.Equal(t, "cov-b@1", e.defaultID())

	_, err = e.reg.SetDefault(e.ctx, "cov-a@1", 1.5)
	assert.Error(t, err, "a threshold above 1 is invalid")
}

func testSetDefaultRefusalChangesNothing(e *lifecycleEnv) {
	t := e.t
	e.model("keep-a@1", true)
	e.model("keep-low@1", false)
	e.model("keep-dep@1", false)
	require.NoError(t, e.reg.Register(e.ctx, registry.Model{ModelID: "keep-unmeasured@1", Provider: "ollama"}, false))
	ids := e.objects("keep", 2)
	e.embed("keep-a@1", ids...)
	e.embed("keep-dep@1", ids...)
	require.NoError(t, e.reg.Deprecate(e.ctx, "keep-dep@1", time.Now().Add(24*time.Hour)))
	before := e.snapshot()

	for id, want := range map[string]error{
		"keep-low@1":        registry.ErrBelowCoverage,
		"keep-dep@1":        registry.ErrModelDeprecated,
		"keep-unmeasured@1": registry.ErrModelNotMeasured,
		"keep-missing@1":    registry.ErrModelNotFound,
	} {
		_, err := e.reg.SetDefault(e.ctx, id, 0.5)
		assert.ErrorIs(t, err, want, id)
	}
	assert.Equal(t, before, e.snapshot(), "refused flips must leave the registry untouched")
	assert.Equal(t, "keep-a@1", e.defaultID())
}

func testSetDefaultPlanDoesNotFlip(e *lifecycleEnv) {
	t := e.t
	e.model("plan-a@1", true)
	e.model("plan-b@1", false)
	ids := e.objects("plan", 2)
	e.embed("plan-b@1", ids...)

	p, err := e.reg.PlanSetDefault(e.ctx, "plan-b@1", 0.99)
	require.NoError(t, err)
	assert.True(t, p.Changed)
	assert.Equal(t, "plan-a@1", p.Previous)
	assert.Equal(t, "plan-a@1", e.defaultID(), "a plan must not flip the default")

	_, err = e.reg.PlanSetDefault(e.ctx, "plan-a@1", 0.99)
	assert.NoError(t, err, "planning a no-op on the default succeeds")
	e.model("plan-c@1", false)
	_, err = e.reg.PlanSetDefault(e.ctx, "plan-c@1", 0.99)
	assert.ErrorIs(t, err, registry.ErrBelowCoverage, "a plan runs the same guard")
}

func testSetDefaultConcurrentOneDefault(e *lifecycleEnv) {
	t := e.t
	const n = 8
	for i := 0; i < n; i++ {
		e.model(fmt.Sprintf("race-%d@1", i), i == 0)
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = e.reg.SetDefault(e.ctx, fmt.Sprintf("race-%d@1", i), 0.99)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		assert.NoError(t, err, "flip %d: concurrent flips must queue, not fail", i)
	}
	assert.Equal(t, 1, e.defaults(), "exactly one default after concurrent flips")
	assert.NotEmpty(t, e.defaultID())
}

// --- deprecate -----------------------------------------------------------------

func testDeprecateRefusesDefault(e *lifecycleEnv) {
	t := e.t
	e.model("dep-a@1", true)
	e.model("dep-b@1", false)

	err := e.reg.Deprecate(e.ctx, "dep-a@1", time.Now())
	require.ErrorIs(t, err, registry.ErrIsDefault)
	m, err := e.reg.Get(e.ctx, "dep-a@1")
	require.NoError(t, err)
	assert.Nil(t, m.DeprecatedAt, "a refused deprecation stamps nothing")

	require.ErrorIs(t, e.reg.PlanDeprecate(e.ctx, "dep-a@1", time.Now()), registry.ErrIsDefault)
	require.NoError(t, e.reg.PlanDeprecate(e.ctx, "dep-b@1", time.Now()))
	m, err = e.reg.Get(e.ctx, "dep-b@1")
	require.NoError(t, err)
	assert.Nil(t, m.DeprecatedAt, "a plan stamps nothing")

	require.ErrorIs(t, e.reg.Deprecate(e.ctx, "dep-missing@1", time.Now()), registry.ErrModelNotFound)
}

func testPopulatingDropsAfterDeprecation(e *lifecycleEnv) {
	t := e.t
	e.model("pop-a@1", true)
	e.model("pop-b@1", false)
	on := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	require.NoError(t, e.reg.Deprecate(e.ctx, "pop-b@1", on))

	ids := func(now time.Time) []string {
		models, err := e.reg.Populating(e.ctx, now)
		require.NoError(t, err)
		out := make([]string, 0, len(models))
		for _, m := range models {
			out = append(out, m.ModelID)
		}
		return out
	}
	assert.Equal(t, []string{"pop-a@1", "pop-b@1"}, ids(on.Add(-time.Minute)), "dual-write continues until the date")
	assert.Equal(t, []string{"pop-a@1"}, ids(on), "dual-write stops once the deprecation is effective")
	assert.Equal(t, []string{"pop-a@1"}, ids(on.Add(time.Hour)))
}

// --- purge ---------------------------------------------------------------------

const grace = 30 * 24 * time.Hour

func testPurgeGuards(e *lifecycleEnv) {
	t := e.t
	e.model("pg-a@1", true)
	e.model("pg-b@1", false)
	ids := e.objects("pg", 2)
	e.embed("pg-a@1", ids...)
	e.embed("pg-b@1", ids...)
	now := time.Now().UTC().Truncate(time.Second)

	_, err := e.reg.Purge(e.ctx, e.drv.Embeddings(), "pg-a@1", grace, now)
	require.ErrorIs(t, err, registry.ErrIsDefault)
	_, err = e.reg.Purge(e.ctx, e.drv.Embeddings(), "pg-b@1", grace, now)
	require.ErrorIs(t, err, registry.ErrNotDeprecated)
	_, err = e.reg.Purge(e.ctx, e.drv.Embeddings(), "pg-missing@1", grace, now)
	require.ErrorIs(t, err, registry.ErrModelNotFound)

	deprecated := now.Add(-grace + time.Hour) // one hour short of the grace period
	require.NoError(t, e.reg.Deprecate(e.ctx, "pg-b@1", deprecated))
	_, err = e.reg.Purge(e.ctx, e.drv.Embeddings(), "pg-b@1", grace, now)
	require.ErrorIs(t, err, registry.ErrGracePeriod)
	var ge *registry.GraceError
	require.True(t, errors.As(err, &ge), "refusal must carry the eligible date: %v", err)
	assert.True(t, ge.EligibleAt.Equal(deprecated.Add(grace)), "eligible at %s", ge.EligibleAt)

	_, err = e.reg.PlanPurge(e.ctx, "pg-b@1", grace, now)
	require.ErrorIs(t, err, registry.ErrGracePeriod, "a plan runs the same guard")

	// Nothing was touched by any refusal.
	assert.Equal(t, 2, e.rows("pg-b@1"))
	assert.True(t, e.searchable("pg-b@1"))
	assert.NotNil(t, e.signature("pg-b@1"))
	_, err = e.reg.Get(e.ctx, "pg-b@1")
	assert.NoError(t, err)
}

func testPurgeAfterGrace(e *lifecycleEnv) {
	t := e.t
	e.model("pa-a@1", true)
	e.model("pa-b@1", false)
	e.model("pa-c@1", false)
	ids := e.objects("pa", 3)
	e.embed("pa-a@1", ids...)
	e.embed("pa-b@1", ids...)
	e.embed("pa-c@1", ids...)
	now := time.Now().UTC().Truncate(time.Second)
	deprecated := now.Add(-grace)
	require.NoError(t, e.reg.Deprecate(e.ctx, "pa-b@1", deprecated))

	plan, err := e.reg.PlanPurge(e.ctx, "pa-b@1", grace, now)
	require.NoError(t, err, "grace period elapsed exactly")
	assert.Equal(t, int64(3), plan.Rows)
	assert.Equal(t, 3, e.rows("pa-b@1"), "a plan deletes nothing")

	res, err := e.reg.Purge(e.ctx, e.drv.Embeddings(), "pa-b@1", grace, now)
	require.NoError(t, err)
	assert.Equal(t, "pa-b@1", res.ModelID)
	assert.Equal(t, int64(3), res.Rows)
	assert.True(t, res.DeprecatedAt.Equal(deprecated))

	assert.Equal(t, 0, e.rows("pa-b@1"), "rows gone")
	assert.False(t, e.searchable("pa-b@1"), "index gone")
	assert.Nil(t, e.signature("pa-b@1"), "signature gone")
	_, err = e.reg.Get(e.ctx, "pa-b@1")
	assert.ErrorIs(t, err, registry.ErrModelNotFound, "registry row removed")

	for _, other := range []string{"pa-a@1", "pa-c@1"} {
		assert.Equal(t, 3, e.rows(other), "%s rows untouched", other)
		assert.True(t, e.searchable(other), "%s index untouched", other)
		assert.NotNil(t, e.signature(other), "%s signature untouched", other)
	}
	assert.Equal(t, "pa-a@1", e.defaultID())
}
