//go:build integration

package jobs

// The task-job claim scenarios on Postgres: two drivers (two dpkms
// processes) on one throwaway database. Reads the POSTGRES_* env block the
// other integration suites use:
//
//	POSTGRES_HOST=localhost POSTGRES_USER=contexthelp POSTGRES_PASSWORD=test_password \
//	  go test -tags integration,fts5 -count=1 -run 'TestClaim_.*_Postgres' ./internal/jobs/

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
		t.Skip("POSTGRES_DSN (or POSTGRES_HOST + POSTGRES_USER) not set; skipping Postgres claim tests")
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

// sharedPostgres opens two drivers on one fresh database, dropped after
// the test.
func sharedPostgres(t *testing.T) (a, b storage.StorageDriver) {
	t.Helper()
	base := pgBaseDSN(t)
	admin, err := sql.Open("postgres", base)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("ctxt_jobs_%d_%04d", time.Now().UnixNano(), rand.Intn(10000)) // #nosec G404 -- test database name
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name)
	})
	u, err := url.Parse(base)
	require.NoError(t, err)
	u.Path = "/" + name
	open := func() storage.StorageDriver {
		d, err := postgres.New(u.String())
		require.NoError(t, err)
		require.NoError(t, d.Init(context.Background()))
		t.Cleanup(func() { _ = d.Close(context.Background()) })
		return d
	}
	return open(), open()
}

func TestClaim_StartupRecoveryDoesNotStealLiveTask_Postgres(t *testing.T) {
	a, b := sharedPostgres(t)
	runStartupRecoveryDoesNotStealLiveTask(t, a, b)
}

func TestClaim_LeaseStoreContract_Postgres(t *testing.T) {
	a, _ := sharedPostgres(t)
	runLeaseStoreContract(t, a.Jobs())
}

func TestClaim_ConcurrentAcquireRunsEachOnce_Postgres(t *testing.T) {
	a, b := sharedPostgres(t)
	runConcurrentAcquireRunsEachOnce(t, a, b)
}

func TestClaim_DeadHolderLeaseExpires_Postgres(t *testing.T) {
	a, b := sharedPostgres(t)
	runDeadHolderLeaseExpires(t, a, b)
}

func TestClaim_LostLeaseStopsFormerHolder_Postgres(t *testing.T) {
	a, b := sharedPostgres(t)
	runLostLeaseStopsFormerHolder(t, a, b)
}
