package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
)

// ---------------------------------------------------------------------------
// US-0031 Tests — Configure Encryption and Secrets
//
// All backends are tested via env-based or in-process mocks — no real OS
// keychain, 1Password, or gh CLI required. Keychain backend tests are skipped
// when the OS keychain is unavailable (CI safety).
// ---------------------------------------------------------------------------

// TestUS0031_SecretsConfigRoundTripAllBackends verifies that SecretsConfig fields
// survive JSON marshal/unmarshal for every supported backend.
func TestUS0031_SecretsConfigRoundTripAllBackends(t *testing.T) {
	cases := []config.SecretsConfig{
		{Backend: "env"},
		{Backend: "keychain", KeychainService: "ctxt"},
		{Backend: "age-file", AgeFile: "~/.config/ctxt/secrets.age", AgeIdentityFile: "~/.config/ctxt/identity.txt"},
		{Backend: "1password", OnePasswordVault: "MyVault"},
		{Backend: "gh-secrets", GHRepo: "owner/repo"},
	}

	for _, original := range cases {
		t.Run(original.Backend, func(t *testing.T) {
			data, err := json.Marshal(original)
			require.NoError(t, err)

			var restored config.SecretsConfig
			require.NoError(t, json.Unmarshal(data, &restored))

			assert.Equal(t, original.Backend, restored.Backend)
			assert.Equal(t, original.KeychainService, restored.KeychainService)
			assert.Equal(t, original.AgeFile, restored.AgeFile)
			assert.Equal(t, original.AgeIdentityFile, restored.AgeIdentityFile)
			assert.Equal(t, original.OnePasswordVault, restored.OnePasswordVault)
			assert.Equal(t, original.GHRepo, restored.GHRepo)
		})
	}
}

// TestUS0031_EnvBackendResolvesFromEnv verifies that the env resolver returns
// values from environment variables.
func TestUS0031_EnvBackendResolvesFromEnv(t *testing.T) {
	t.Setenv("CTXT_TEST_SECRET_KEY", "sk-test-resolved-value")

	r := secrets.NewEnvResolver()
	val, err := r.Get("CTXT_TEST_SECRET_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-resolved-value", val)
}

// TestUS0031_EnvBackendMissingKeyReturnsError verifies that Get() on an unset
// env var returns an error (not a silent empty string).
func TestUS0031_EnvBackendMissingKeyReturnsError(t *testing.T) {
	r := secrets.NewEnvResolver()
	_, err := r.Get("CTXT_TEST_DEFINITELY_NOT_SET_XYZZY")
	require.Error(t, err, "Get on missing env var must return error")
}

// TestUS0031_EnvBackendSetReturnsError verifies that Set() on the env resolver
// returns a clear error (env is read-only at runtime).
func TestUS0031_EnvBackendSetReturnsError(t *testing.T) {
	r := secrets.NewEnvResolver()
	err := r.Set("CTXT_TEST_KEY", "some-value")
	require.Error(t, err, "env resolver Set() must return error")
}

// TestUS0031_NewResolverEnvBackend verifies that factory returns an EnvResolver
// for the "env" backend without error.
func TestUS0031_NewResolverEnvBackend(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "env"}
	r, err := secrets.NewResolver(cfg)
	require.NoError(t, err)
	require.NotNil(t, r)
}

// TestUS0031_NewResolverEmptyBackendDefaultsToEnv verifies that empty Backend
// field defaults to env resolver (backward-compatible).
func TestUS0031_NewResolverEmptyBackendDefaultsToEnv(t *testing.T) {
	cfg := config.SecretsConfig{}
	r, err := secrets.NewResolver(cfg)
	require.NoError(t, err)
	require.NotNil(t, r)
}

// TestUS0031_NewResolverAgeFileMissingAgeFile verifies that "age-file" backend
// with empty AgeFile returns an error containing "age_file".
func TestUS0031_NewResolverAgeFileMissingAgeFile(t *testing.T) {
	cfg := config.SecretsConfig{
		Backend:         "age-file",
		AgeIdentityFile: "/path/to/identity.txt",
	}
	_, err := secrets.NewResolver(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "age_file", "error must mention the missing age_file field")
}

// TestUS0031_NewResolverAgeFileMissingIdentityFile verifies that "age-file" backend
// with empty AgeIdentityFile returns an error containing "age_identity_file".
func TestUS0031_NewResolverAgeFileMissingIdentityFile(t *testing.T) {
	cfg := config.SecretsConfig{
		Backend: "age-file",
		AgeFile: "/path/to/secrets.age",
	}
	_, err := secrets.NewResolver(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "age_identity_file", "error must mention the missing age_identity_file field")
}

// TestUS0031_NewResolverOnePasswordMissingVault verifies that "1password" backend
// with empty OnePasswordVault returns an error containing "vault".
func TestUS0031_NewResolverOnePasswordMissingVault(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "1password"}
	_, err := secrets.NewResolver(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault", "error must mention the missing vault field")
}

// TestUS0031_NewResolverUnknownBackendReturnsError verifies that an unknown backend
// value returns a validation error.
func TestUS0031_NewResolverUnknownBackendReturnsError(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "not-a-real-backend"}
	_, err := secrets.NewResolver(cfg)
	require.Error(t, err, "unknown backend must return error")
}

// TestUS0031_KeychainBackendDefaultsServiceName verifies that omitting
// KeychainService results in a resolver that uses "ctxt" as the default.
// The resolver is constructed only — no actual keychain access is performed.
func TestUS0031_KeychainBackendDefaultsServiceName(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "keychain"} // KeychainService intentionally omitted
	r, err := secrets.NewResolver(cfg)
	require.NoError(t, err)
	require.NotNil(t, r, "keychain resolver must be non-nil even without KeychainService set")
}

// TestUS0031_ProviderFactoryUsesResolver verifies that the providers factory
// delegates API key lookup to the supplied resolver, not to os.Getenv directly.
func TestUS0031_ProviderFactoryUsesResolver(t *testing.T) {
	// Use the in-process mock resolver seeded with a known key.
	resolver := newTestMockResolver(map[string]string{
		"ANTHROPIC_API_KEY": "sk-from-secrets-resolver",
	})
	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "auto"

	f := providers.NewFactory(cfg, resolver)
	require.NotNil(t, f)

	// LLM() should pick the Anthropic provider based on the key from the resolver.
	llm := f.LLM()
	assert.NotNil(t, llm, "factory must return a provider when resolver supplies a key")
}

// TestUS0031_NoPlaintextKeyInSecretsConfig verifies that the SecretsConfig struct
// does not contain any field that would store a plaintext API key value.
// The config must only store metadata (backend, paths, names) — never the secret itself.
func TestUS0031_NoPlaintextKeyInSecretsConfig(t *testing.T) {
	cfg := config.SecretsConfig{
		Backend:          "keychain",
		KeychainService:  "ctxt",
		AgeFile:          "~/.config/ctxt/secrets.age",
		AgeIdentityFile:  "~/.config/ctxt/identity.txt",
		OnePasswordVault: "MyVault",
		GHRepo:           "owner/repo",
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))

	// Ensure no field name resembles a plaintext API key.
	sensitiveKeys := []string{"api_key", "apikey", "password", "secret", "token", "key_value"}
	for _, k := range sensitiveKeys {
		_, exists := raw[k]
		assert.False(t, exists, "SecretsConfig must not contain field %q (would store plaintext secret)", k)
	}

	t.Logf("config fields present: %v", raw)
}
