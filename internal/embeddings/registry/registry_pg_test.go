//go:build integration

package registry_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
)

// pgIntegrationDSN mirrors the postgres package's env contract:
// POSTGRES_DSN wins; otherwise the POSTGRES_HOST/PORT/USER/PASSWORD/DB block.
func pgIntegrationDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	host := os.Getenv("POSTGRES_HOST")
	user := os.Getenv("POSTGRES_USER")
	if host == "" || user == "" {
		t.Skip("POSTGRES_DSN (or POSTGRES_HOST + POSTGRES_USER) not set; skipping Postgres integration test")
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	dbName := os.Getenv("POSTGRES_DB")
	if dbName == "" {
		dbName = user
	}
	cred := user
	if pw := os.Getenv("POSTGRES_PASSWORD"); pw != "" {
		cred = user + ":" + pw
	}
	return fmt.Sprintf("postgres://%s@%s/%s?sslmode=disable",
		cred, net.JoinHostPort(host, port), dbName)
}

// newPgRegistry provisions a fresh database, migrates the postgres driver on
// it, and returns a dialect-aware registry plus the driver.
func newPgRegistry(t *testing.T) (*registry.Store, *postgres.Driver) {
	t.Helper()
	baseDSN := pgIntegrationDSN(t)

	admin, err := sql.Open("postgres", baseDSN)
	require.NoError(t, err)
	t.Cleanup(func() { admin.Close() })

	name := fmt.Sprintf("ctxt_reg_%d_%04d", time.Now().UnixNano(), rand.Intn(10000))
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name)
	})

	u, err := url.Parse(baseDSN)
	require.NoError(t, err)
	u.Path = "/" + name

	drv, err := postgres.New(u.String())
	require.NoError(t, err)
	t.Cleanup(func() { drv.Close(context.Background()) })
	require.NoError(t, drv.Migrate(context.Background()))

	return registry.NewFor(drv.DB(), "postgres"), drv
}

// TestPgRegistry_CRUDRoundTrip pins the dialect-clean registry on Postgres:
// the same CRUD surface `ctxt embeddings list` uses on SQLite works against
// a hosted instance.
func TestPgRegistry_CRUDRoundTrip(t *testing.T) {
	r, _ := newPgRegistry(t)
	ctx := context.Background()

	m := registry.Model{
		ModelID:   "text-embedding-3-small",
		Provider:  registry.ProviderOpenAI,
		Dimension: 1536,
	}
	require.NoError(t, r.Register(ctx, m, false))

	got, err := r.Get(ctx, m.ModelID)
	require.NoError(t, err)
	assert.Equal(t, m.ModelID, got.ModelID)
	assert.Equal(t, registry.ProviderOpenAI, got.Provider)
	assert.Equal(t, 1536, got.Dimension)
	assert.False(t, got.RegisteredAt.IsZero(), "registered_at must round-trip from TIMESTAMPTZ")

	// Duplicate rejected.
	err = r.Register(ctx, m, false)
	assert.ErrorIs(t, err, registry.ErrModelAlreadyRegistered)

	// List includes the migration-seeded default plus ours.
	models, err := r.List(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(models), 2)

	// SetDefault flips atomically.
	require.NoError(t, r.SetDefault(ctx, m.ModelID))
	got, err = r.Get(ctx, m.ModelID)
	require.NoError(t, err)
	assert.True(t, got.IsDefault)

	// Deprecate stamps the timestamp.
	when := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, r.Deprecate(ctx, m.ModelID, when))
	got, err = r.Get(ctx, m.ModelID)
	require.NoError(t, err)
	require.NotNil(t, got.DeprecatedAt)

	// Coverage: empty corpus reports vacuous 1.0 for every model.
	withCov, err := r.ListWithCoverage(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, withCov)
	for _, mc := range withCov {
		assert.Equal(t, 1.0, mc.Coverage, "vacuous coverage on empty corpus for %s", mc.ModelID)
	}
}

// TestPgRegistry_DimensionCeiling pins loud failure at Register time: a
// dimension SQLite would happily brute-force is un-indexable under pgvector
// HNSW, and that asymmetry must surface at registration, not at first query.
func TestPgRegistry_DimensionCeiling(t *testing.T) {
	r, _ := newPgRegistry(t)
	err := r.Register(context.Background(), registry.Model{
		ModelID:   "giant-embedding",
		Provider:  registry.ProviderOpenAI,
		Dimension: 3000,
	}, false)
	require.Error(t, err, "dimension above the HNSW ceiling must fail at Register")
	assert.Contains(t, err.Error(), "2000")
	if !strings.Contains(err.Error(), "giant-embedding") {
		t.Errorf("error should name the model: %v", err)
	}
}
