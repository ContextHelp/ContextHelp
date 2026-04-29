package integration

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"hop.top/kit/go/storage/secret"
)

// ---------------------------------------------------------------------------
// US-0062 Tests — Manage Secrets via CLI
//
// These tests exercise the kit-backed secret stores selected by ctxt's thin
// internal/secrets factory. The CLI itself is not invoked in-process — these
// are unit-level integration tests over the same code path the CLI uses.
//
// Keychain backend tests skip when the OS keychain is unavailable. Use the
// env backend for all CI scenarios.
// ---------------------------------------------------------------------------

// TestUS0062_EnvBackendGetSet verifies the env backend's Get/Set behaviour.
// Set returns ErrNotSupported (read-only); Get reads from the environment.
func TestUS0062_EnvBackendGetSet(t *testing.T) {
	r, err := secrets.New(config.SecretsConfig{Backend: "env"})
	require.NoError(t, err)

	err = r.Set(context.Background(), "TEST_ENV_KEY_0062", []byte("my-value"))
	require.True(t, errors.Is(err, secret.ErrNotSupported), "want ErrNotSupported, got %v", err)

	t.Setenv("TEST_ENV_KEY_0062", "expected-value")
	got, err := r.Get(context.Background(), "TEST_ENV_KEY_0062")
	require.NoError(t, err)
	assert.Equal(t, "expected-value", string(got.Value))

	_, err = r.Get(context.Background(), "TEST_ENV_MISSING_KEY_XYZZY_0062")
	require.True(t, errors.Is(err, secret.ErrNotFound), "want ErrNotFound, got %v", err)
}

// TestUS0062_EnvBackendBareValue verifies the resolved value is bare bytes.
func TestUS0062_EnvBackendBareValue(t *testing.T) {
	t.Setenv("CTXT_TEST_JSON_KEY", "sk-bare-value")

	r, err := secrets.New(config.SecretsConfig{Backend: "env"})
	require.NoError(t, err)
	got, err := r.Get(context.Background(), "CTXT_TEST_JSON_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-bare-value", string(got.Value))
}

// TestUS0062_FactoryEnvBackend verifies the factory returns a working env store.
func TestUS0062_FactoryEnvBackend(t *testing.T) {
	r, err := secrets.New(config.SecretsConfig{Backend: "env"})
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Setenv("CTXT_FACTORY_TEST_KEY", "factory-value")
	got, err := r.Get(context.Background(), "CTXT_FACTORY_TEST_KEY")
	require.NoError(t, err)
	assert.Equal(t, "factory-value", string(got.Value))
}

// TestUS0062_KeychainBackendSkippedInCI skips keychain tests when on an
// unsupported OS or in a headless CI environment.
func TestUS0062_KeychainBackendSkippedInCI(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("keychain tests only run on macOS and Linux")
	}

	cfg := config.SecretsConfig{Backend: "keychain", KeychainService: "ctxt-test-e2e"}
	r, err := secrets.New(cfg)
	if err != nil {
		t.Skipf("keychain unavailable: %v", err)
	}
	require.NotNil(t, r)

	_, getErr := r.Get(context.Background(), "CTXT_E2E_NONEXISTENT_KEY_0062")
	if getErr != nil && !errors.Is(getErr, secret.ErrNotFound) {
		t.Skipf("keychain Get failed (expected in CI): %v", getErr)
	}
}

// TestUS0062_AgeFileBackendMissingFilesErrors verifies the age-file backend
// fails clearly when required file paths are not configured.
func TestUS0062_AgeFileBackendMissingFilesErrors(t *testing.T) {
	cases := []struct {
		name    string
		cfg     config.SecretsConfig
		wantErr string
	}{
		{
			name:    "missing age_file",
			cfg:     config.SecretsConfig{Backend: "age-file", AgeIdentityFile: "/tmp/id.txt"},
			wantErr: "age_file",
		},
		{
			name:    "missing age_identity_file",
			cfg:     config.SecretsConfig{Backend: "age-file", AgeFile: "/tmp/secrets.age"},
			wantErr: "age_identity_file",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := secrets.New(tc.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestUS0062_OnePasswordBackendMissingVaultErrors verifies the 1password
// backend fails clearly when onepassword_vault is not set.
func TestUS0062_OnePasswordBackendMissingVaultErrors(t *testing.T) {
	_, err := secrets.New(config.SecretsConfig{Backend: "1password"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault")
}

// TestUS0062_GHSecretsBackendEnvFallback verifies that gh-secrets constructs
// successfully and Get falls back to environment variables.
func TestUS0062_GHSecretsBackendEnvFallback(t *testing.T) {
	r, err := secrets.New(config.SecretsConfig{Backend: "gh-secrets", GHRepo: ""})
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Setenv("CTXT_GH_FALLBACK_KEY", "gh-env-fallback-value")
	got, err := r.Get(context.Background(), "CTXT_GH_FALLBACK_KEY")
	require.NoError(t, err)
	assert.Equal(t, "gh-env-fallback-value", string(got.Value))
}

// TestUS0062_UnknownBackendValidationError verifies an invalid backend value
// returns a validation error mentioning the bad name.
func TestUS0062_UnknownBackendValidationError(t *testing.T) {
	_, err := secrets.New(config.SecretsConfig{Backend: "does-not-exist"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}
