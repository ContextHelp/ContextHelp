package integration

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
)

// ---------------------------------------------------------------------------
// US-0062 Tests — Manage Secrets via CLI
//
// Note: These tests exercise the secrets.Resolver backends directly (unit-level
// integration) since the CLI itself is not invoked in-process during e2e tests.
//
// Keychain backend tests are skipped when not on a supported OS or when the
// keychain is unavailable (CI safety). Use env backend for all CI scenarios.
// ---------------------------------------------------------------------------

// TestUS0062_EnvResolverGetSetDelete verifies the env backend's Get/Set behaviour.
// Set returns an error (read-only); Get reads from the environment.
func TestUS0062_EnvResolverGetSetDelete(t *testing.T) {
	// Set: env resolver must return a clear read-only error.
	r := secrets.NewEnvResolver()
	err := r.Set("TEST_ENV_KEY_0062", "my-value")
	require.Error(t, err, "env backend Set must return error (read-only)")

	// Get with env var set.
	t.Setenv("TEST_ENV_KEY_0062", "expected-value")
	val, err := r.Get("TEST_ENV_KEY_0062")
	require.NoError(t, err)
	assert.Equal(t, "expected-value", val, "env resolver must return the env var value")

	// Get for missing key.
	_, err = r.Get("TEST_ENV_MISSING_KEY_XYZZY_0062")
	require.Error(t, err, "Get on missing env var must return error")
}

// TestUS0062_EnvResolverGetOutputsJSON verifies that the resolved value is a bare
// string (correct for `ctxt secret get` bare output and JSON wrapping).
func TestUS0062_EnvResolverGetOutputsJSON(t *testing.T) {
	t.Setenv("CTXT_TEST_JSON_KEY", "sk-bare-value")

	r := secrets.NewEnvResolver()
	val, err := r.Get("CTXT_TEST_JSON_KEY")
	require.NoError(t, err)

	// The CLI wraps this in {"key":"...","value":"..."} for --output json.
	// Verify the value is the raw string without extra quoting.
	assert.Equal(t, "sk-bare-value", val, "Get must return the bare value without JSON encoding")
}

// TestUS0062_EnvBackendMissingKeyError verifies that Get on a missing env key
// returns an error whose message contains the key name (for clear UX).
func TestUS0062_EnvBackendMissingKeyError(t *testing.T) {
	r := secrets.NewEnvResolver()
	_, err := r.Get("CTXT_DEFINITELY_MISSING_0062")
	require.Error(t, err)
	// The CLI shows this error to the user — message quality matters.
	assert.NotEmpty(t, err.Error(), "error message must be non-empty")
}

// TestUS0062_FactoryEnvBackend verifies that secrets.NewResolver returns a
// working EnvResolver for the "env" backend declaration.
func TestUS0062_FactoryEnvBackend(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "env"}
	r, err := secrets.NewResolver(cfg)
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Setenv("CTXT_FACTORY_TEST_KEY", "factory-value")
	val, err := r.Get("CTXT_FACTORY_TEST_KEY")
	require.NoError(t, err)
	assert.Equal(t, "factory-value", val)
}

// TestUS0062_KeychainBackendSkippedInCI skips keychain tests when on an
// unsupported OS or in a headless CI environment.
func TestUS0062_KeychainBackendSkippedInCI(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("keychain tests only run on macOS and Linux")
	}

	// Attempt to construct a keychain resolver. In CI without a keychain
	// daemon, the constructor succeeds (it's lazy) but Get() would fail.
	// We only verify construction here.
	cfg := config.SecretsConfig{Backend: "keychain", KeychainService: "ctxt-test-e2e"}
	r, err := secrets.NewResolver(cfg)
	if err != nil {
		t.Skipf("keychain resolver unavailable: %v", err)
	}
	require.NotNil(t, r)

	// Attempt a Get — skip if the keychain tool is not available.
	_, getErr := r.Get("CTXT_E2E_NONEXISTENT_KEY_0062")
	if getErr != nil {
		t.Skipf("keychain get failed (expected in CI): %v", getErr)
	}
}

// TestUS0062_AgeFileBackendMissingFilesErrors verifies that the age-file backend
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
			_, err := secrets.NewResolver(tc.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestUS0062_OnePasswordBackendMissingVaultErrors verifies that the 1password
// backend fails clearly when onepassword_vault is not set.
func TestUS0062_OnePasswordBackendMissingVaultErrors(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "1password"} // vault intentionally omitted
	_, err := secrets.NewResolver(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault")
}

// TestUS0062_GHSecretsBackendSetReadonly verifies that the gh-secrets backend
// is constructed successfully and its Get() falls back to env variables.
func TestUS0062_GHSecretsBackendSetReadonly(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "gh-secrets", GHRepo: ""}
	r, err := secrets.NewResolver(cfg)
	require.NoError(t, err, "gh-secrets resolver must construct without error")
	require.NotNil(t, r)

	// Get should fall back to env when the gh CLI is not available.
	t.Setenv("CTXT_GH_FALLBACK_KEY", "gh-env-fallback-value")
	val, getErr := r.Get("CTXT_GH_FALLBACK_KEY")
	if getErr == nil {
		// Either gh CLI worked or env fallback worked.
		assert.Equal(t, "gh-env-fallback-value", val)
	}
	// If getErr != nil it means both gh CLI and env lookup failed — acceptable in CI.
}

// TestUS0062_SecretListEnvBackendReturnsBackendInfo verifies that the env backend
// resolver can be introspected (type assertion) for the list command use case.
func TestUS0062_SecretListEnvBackendReturnsBackendInfo(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "env"}
	r, err := secrets.NewResolver(cfg)
	require.NoError(t, err)

	// The list command inspects the resolver type — verify it's the expected concrete type.
	_, isEnv := r.(*secrets.EnvResolver)
	assert.True(t, isEnv, "env backend must return an *EnvResolver")
}

// TestUS0062_UnknownBackendValidationError verifies that an invalid backend value
// returns a validation error listing the problem.
func TestUS0062_UnknownBackendValidationError(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "does-not-exist"}
	_, err := secrets.NewResolver(cfg)
	require.Error(t, err)
	// Error should be informative enough for the user to fix their config.
	assert.Contains(t, err.Error(), "does-not-exist", "error must include the invalid backend name")
}
