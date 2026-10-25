//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A read-only Postgres handle reads, refuses DML and DDL, and never
// migrates: the same contract as the SQLite read-only open.
func TestPostgres_NewReadOnly(t *testing.T) {
	newIntegrationDriver(t) // skips without Postgres; migrates the schema
	ro, err := pgdrv.NewReadOnly(integrationDSN(), storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ro.Close(context.Background()) })
	ctx := context.Background()

	var n int
	if err := ro.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_version`).Scan(&n); err != nil {
		t.Fatalf("read through read-only handle: %v", err)
	}
	for _, stmt := range []string{
		`DELETE FROM jobs WHERE id = 'ro-probe'`,
		`CREATE TABLE ro_probe (a INTEGER)`,
	} {
		for range 3 {
			if _, err := ro.DB().ExecContext(ctx, stmt); err == nil {
				t.Fatalf("%s succeeded on a read-only handle", stmt)
			}
		}
	}
	if err := ro.Init(ctx); !errors.Is(err, pgdrv.ErrReadOnly) {
		t.Errorf("Init on a read-only handle = %v; want ErrReadOnly", err)
	}
}

// A connection string that already sets options keeps them on the
// read-only handle, and the handle is still read-only.
func TestPostgres_NewReadOnly_KeepsOptions(t *testing.T) {
	newIntegrationDriver(t) // skips without Postgres; migrates the schema
	u, err := url.Parse(integrationDSN())
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("options", "-c application_name=ro-probe")
	u.RawQuery = q.Encode()

	ro, err := pgdrv.NewReadOnly(u.String(), storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ro.Close(context.Background()) })
	ctx := context.Background()

	var app, readOnly string
	if err := ro.DB().QueryRowContext(ctx, `SELECT current_setting('application_name'), current_setting('default_transaction_read_only')`).Scan(&app, &readOnly); err != nil {
		t.Fatal(err)
	}
	if app != "ro-probe" || readOnly != "on" {
		t.Errorf("session application_name %q, default_transaction_read_only %q; want ro-probe, on", app, readOnly)
	}
	if _, err := ro.DB().ExecContext(ctx, `CREATE TABLE ro_probe (a INTEGER)`); err == nil {
		t.Fatal("DDL succeeded on a read-only handle with options")
	}
}
