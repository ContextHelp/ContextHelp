//go:build e2e && integration && unix

package searchgraph_test

// The endpoint scenarios on Postgres (pgvector), against throwaway
// databases on the server the POSTGRES_* env block names, as the other
// integration suites read it. Without it the test skips:
//
//	POSTGRES_HOST=localhost POSTGRES_USER=contexthelp POSTGRES_PASSWORD=test_password \
//	  go test -count=1 -tags 'fts5 e2e integration' -run TestDpkmsGraph_Postgres ./test/e2e/searchgraph/

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestDpkmsGraph_Postgres(t *testing.T) {
	base := postgresBaseDSN(t)
	runEndpointSuite(t, "postgres", func(t *testing.T, name string) string {
		return postgresDatabase(t, base, name)
	})
}

// postgresBaseDSN is POSTGRES_DSN, or the DSN the POSTGRES_HOST/PORT/
// USER/PASSWORD/DB block names; it skips the test when neither is set.
func postgresBaseDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	host, user := os.Getenv("POSTGRES_HOST"), os.Getenv("POSTGRES_USER")
	if host == "" || user == "" {
		t.Skip("POSTGRES_DSN (or POSTGRES_HOST + POSTGRES_USER) not set; skipping the Postgres run")
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

// postgresDatabase creates an empty database on base's server and
// returns its DSN; the database is dropped when the test ends, after
// every process that could hold a connection to it is gone.
func postgresDatabase(t *testing.T, base, name string) string {
	t.Helper()
	admin, err := sql.Open("postgres", base)
	if err != nil {
		t.Fatal(err)
	}
	db := fmt.Sprintf("ctxt_graph_e2e_%s_%d_%04d", name, time.Now().UnixNano(), rand.IntN(10000)) //nolint:gosec // test database name
	if _, err := admin.Exec("CREATE DATABASE " + db); err != nil {
		_ = admin.Close()
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, db)
		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + db); err != nil {
			t.Errorf("drop database %s: %v", db, err)
		}
		_ = admin.Close()
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + db
	return u.String()
}
