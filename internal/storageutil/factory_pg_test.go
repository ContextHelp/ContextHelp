//go:build integration

package storageutil

import (
	"context"
	"net"
	"net/url"
	"os"
	"testing"
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

func TestNewDriverPostgres(t *testing.T) {
	driver, err := NewDriver("postgres", pgBaseDSN(t), NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("new postgres driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.Init(context.Background()); err != nil {
		t.Fatalf("init postgres driver: %v", err)
	}
}
