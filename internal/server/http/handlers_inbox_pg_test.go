//go:build integration

package http_test

// Postgres parity for the inbox surface. Reads the POSTGRES_* env block
// the other integration suites use:
//
//	POSTGRES_HOST=localhost POSTGRES_USER=contexthelp POSTGRES_PASSWORD=test_password \
//	  go test -tags integration,fts5 -count=1 -run 'Postgres' ./internal/server/http/

import (
	"testing"

	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
)

func TestInboxAPI_Postgres(t *testing.T) {
	RunInboxSuite(t, httpserver.FreshPostgres)
}
