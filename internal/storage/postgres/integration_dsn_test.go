//go:build integration

package postgres_test

import (
	"testing"
)

// TestIntegrationDSN_BuildsFromEnvBlock verifies the DSN builder consumes
// the POSTGRES_HOST/PORT/USER/PASSWORD/DB env block exported by
// .github/workflows/ci.yml without falling back to the OS user (which
// produces FATAL: role "root" does not exist on GH Actions runners).
func TestIntegrationDSN_BuildsFromEnvBlock(t *testing.T) {
	for _, k := range []string{
		"POSTGRES_DSN", "POSTGRES_HOST", "POSTGRES_PORT",
		"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB",
	} {
		t.Setenv(k, "")
	}

	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_USER", "contexthelp")
	t.Setenv("POSTGRES_PASSWORD", "test_password")
	t.Setenv("POSTGRES_DB", "contexthelp_test")

	got := integrationDSN()
	want := "postgres://contexthelp:test_password@localhost:5432/contexthelp_test?sslmode=disable"
	if got != want {
		t.Errorf("integrationDSN()\n  got:  %q\n  want: %q", got, want)
	}
}

func TestIntegrationDSN_DSNOverridesBlock(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://override@h/db")
	t.Setenv("POSTGRES_HOST", "ignored")
	t.Setenv("POSTGRES_USER", "ignored")

	if got, want := integrationDSN(), "postgres://override@h/db"; got != want {
		t.Errorf("integrationDSN() = %q, want %q", got, want)
	}
}

func TestIntegrationDSN_EmptyWhenNoEnv(t *testing.T) {
	for _, k := range []string{
		"POSTGRES_DSN", "POSTGRES_HOST", "POSTGRES_PORT",
		"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB",
	} {
		t.Setenv(k, "")
	}
	if got := integrationDSN(); got != "" {
		t.Errorf("integrationDSN() = %q, want empty", got)
	}
}

func TestIntegrationDSN_NoPasswordOmitsCredentials(t *testing.T) {
	for _, k := range []string{
		"POSTGRES_DSN", "POSTGRES_PORT", "POSTGRES_PASSWORD", "POSTGRES_DB",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("POSTGRES_HOST", "localhost")
	t.Setenv("POSTGRES_USER", "contexthelp")

	got := integrationDSN()
	want := "postgres://contexthelp@localhost:5432/contexthelp?sslmode=disable"
	if got != want {
		t.Errorf("integrationDSN()\n  got:  %q\n  want: %q", got, want)
	}
}
