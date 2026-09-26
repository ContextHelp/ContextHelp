//go:build integration

package cmd

// The embeddings journey on Postgres (pgvector): the same scenario, cassettes
// and binaries as TestEmbeddingsJourney_SQLite, against a fresh database.
// Reads the POSTGRES_* env block the other integration suites use:
//
//	POSTGRES_HOST=localhost POSTGRES_USER=contexthelp POSTGRES_PASSWORD=test_password \
//	  go test -tags integration,fts5 -count=1 -run TestEmbeddingsJourney_Postgres ./cmd/ctxt/cmd/

import (
	"database/sql"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// journeyPostgresBaseDSN mirrors the integration env contract: POSTGRES_DSN
// wins, otherwise the POSTGRES_HOST/PORT/USER/PASSWORD/DB block.
func journeyPostgresBaseDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	host, user := os.Getenv("POSTGRES_HOST"), os.Getenv("POSTGRES_USER")
	if host == "" || user == "" {
		t.Skip("POSTGRES_DSN (or POSTGRES_HOST + POSTGRES_USER) not set; skipping the Postgres journey")
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

// postgresJourneyStore creates a throwaway database, dropped after the test.
func postgresJourneyStore(t *testing.T) *journeyStore {
	t.Helper()
	base := journeyPostgresBaseDSN(t)
	admin, err := sql.Open("postgres", base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("ctxt_journey_%d_%04d", time.Now().UnixNano(), rand.Intn(10000)) // #nosec G404 -- test database name
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name)
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	dsn := u.String()
	return &journeyStore{
		kind: "postgres", storageType: "postgres", storagePath: dsn, dialect: indexsig.DialectPostgres,
		open: func() (*sql.DB, func(), error) {
			db, err := sql.Open("postgres", dsn)
			if err != nil {
				return nil, nil, err
			}
			return db, func() { _ = db.Close() }, nil
		},
	}
}

func TestEmbeddingsJourney_Postgres(t *testing.T) {
	runEmbeddingsJourney(t, newJourneyEnv(t, postgresJourneyStore(t)))
}
