//go:build integration

package sharechange_test

// Postgres parity for the share.change tests: the same fixtures and
// assertions over a fresh Postgres database each. Reads the POSTGRES_*
// env block the other integration suites use:
//
//	POSTGRES_HOST=localhost POSTGRES_USER=contexthelp POSTGRES_PASSWORD=test_password \
//	  go test -tags integration,fts5 -count=1 -run 'Postgres' ./internal/sharechange/

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// pgBaseDSN mirrors the integration env contract: POSTGRES_DSN wins,
// otherwise the POSTGRES_HOST/PORT/USER/PASSWORD/DB block.
func pgBaseDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	host, user := os.Getenv("POSTGRES_HOST"), os.Getenv("POSTGRES_USER")
	if host == "" || user == "" {
		t.Skip("POSTGRES_DSN (or POSTGRES_HOST + POSTGRES_USER) not set; skipping Postgres tests")
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	db := os.Getenv("POSTGRES_DB")
	if db == "" {
		db = user
	}
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + db, RawQuery: "sslmode=disable"}
	if pw := os.Getenv("POSTGRES_PASSWORD"); pw != "" {
		u.User = url.UserPassword(user, pw)
	} else {
		u.User = url.User(user)
	}
	return u.String()
}

// freshPostgres opens a driver on a new database, dropped after the test.
func freshPostgres(t *testing.T) storage.StorageDriver {
	t.Helper()
	base := pgBaseDSN(t)
	admin, err := sql.Open("postgres", base)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("ctxt_shch_%d_%04d", time.Now().UnixNano(), rand.Intn(10000)) // #nosec G404 -- test database name
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name)
	})
	u, err := url.Parse(base)
	require.NoError(t, err)
	u.Path = "/" + name
	d, err := postgres.New(u.String(), storageutil.NewTestBlobStore(t))
	require.NoError(t, err)
	require.NoError(t, d.Init(context.Background()))
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return d
}

func TestPostgres_OwnerChangesAreNeverPending(t *testing.T) { ownerNeverPending(t, freshPostgres) }
func TestPostgres_WideningByAdminIsPendingAndChangesNothing(t *testing.T) {
	widening(t, freshPostgres)
}
func TestPostgres_NarrowingAndNeutralApplyAtOnceFromAdmin(t *testing.T) {
	narrowing(t, freshPostgres)
}
func TestPostgres_UnclassifiableIsWidening(t *testing.T)            { unclassifiable(t, freshPostgres) }
func TestPostgres_UnreadableRegistryWritesNothing(t *testing.T)     { unreadable(t, freshPostgres) }
func TestPostgres_ApproveAppliesTheFrozenChange(t *testing.T)       { applies(t, freshPostgres) }
func TestPostgres_SeparationOfRequesterAndBeneficiary(t *testing.T) { separation(t, freshPostgres) }
func TestPostgres_PreconditionMovedIsStale(t *testing.T)            { stale(t, freshPostgres) }
func TestPostgres_ConcurrentChangeCannotSlipBetweenClassifyAndWrite(t *testing.T) {
	concurrentSlip(t, freshPostgres)
}
func TestPostgres_ConcurrentEditWaitsForTheApproval(t *testing.T) { approveRace(t, freshPostgres) }
