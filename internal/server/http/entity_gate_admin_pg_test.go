//go:build integration

package http

// Postgres parity for the admin entity-gate bypass: the metering rows the
// test asserts on live in the store. Reads the POSTGRES_* env block the
// other integration suites use:
//
//	POSTGRES_HOST=localhost POSTGRES_USER=contexthelp POSTGRES_PASSWORD=test_password \
//	  go test -tags integration,fts5 -count=1 -run 'Postgres' ./internal/server/http/

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
	name := fmt.Sprintf("ctxt_http_%d_%04d", time.Now().UnixNano(), rand.Intn(10000)) // #nosec G404 -- test database name
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name)
	})
	u, err := url.Parse(base)
	require.NoError(t, err)
	u.Path = "/" + name
	d, err := postgres.New(u.String())
	require.NoError(t, err)
	require.NoError(t, d.Init(context.Background()))
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return d
}

func TestAdminBypassesEntityGate_Postgres(t *testing.T) {
	runAdminEntityGateBypass(t, freshPostgres(t), false)
}
