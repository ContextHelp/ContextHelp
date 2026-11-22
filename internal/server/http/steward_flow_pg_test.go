//go:build integration

package http

import "testing"

// Postgres parity for the steward flow: request, approval, elevation,
// idle, revocation, the off switch and the rules, each on a fresh
// Postgres database. Reads the POSTGRES_* env block the other
// integration suites use:
//
//	POSTGRES_HOST=localhost POSTGRES_USER=contexthelp POSTGRES_PASSWORD=test_password \
//	  go test -tags integration,fts5 -count=1 -run 'StewardFlow' ./internal/server/http/
func TestStewardFlow_Postgres(t *testing.T) { runStewardFlowSuite(t, freshPostgres) }

// Postgres parity for the elevated-read audit over a real elevated
// session.
func TestStewardFlowAudit_Postgres(t *testing.T) { runStewardFlowAuditSuite(t, freshPostgres) }
