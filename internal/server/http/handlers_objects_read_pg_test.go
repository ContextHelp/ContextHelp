//go:build integration

package http_test

// Postgres parity for the object read surface. Reads the POSTGRES_* env
// block the other integration suites use:
//
//	POSTGRES_HOST=localhost POSTGRES_USER=contexthelp POSTGRES_PASSWORD=test_password \
//	  go test -tags integration,fts5 -count=1 -run 'Postgres' ./internal/server/http/

import (
	"testing"

	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
)

func TestObjectReads_Postgres(t *testing.T) {
	RunObjectReadSuite(t, httpserver.FreshPostgres)
}
