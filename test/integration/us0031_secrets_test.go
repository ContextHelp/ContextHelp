package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"hop.top/kit/go/storage/secret"
)

// ---------------------------------------------------------------------------
// US-0031 Tests — Configure Encryption and Secrets
//
// All backends are exercised through the kit secret.Store interface via the
// thin internal/secrets factory. No real OS keychain, 1Password, or gh CLI is
// invoked — keychain backend tests skip when the OS keyring is unavailable.
// ---------------------------------------------------------------------------

// TestUS0031_SecretsConfigRoundTripAllBackends verifies that SecretsConfig
// fields survive JSON marshal/unmarshal for every supported backend.
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

// TestUS0031_EnvBackendResolvesFromEnv verifies that the env backend returns
// values from environment variables.
func TestUS0031_EnvBackendResolvesFromEnv(t *testing.T) {
	t.Setenv("CTXT_TEST_SECRET_KEY", "sk-test-resolved-value")

	r, err := secrets.New(config.SecretsConfig{Backend: "env"})
	require.NoError(t, err)
	got, err := r.Get(context.Background(), "CTXT_TEST_SECRET_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-resolved-value", string(got.Value))
}

// TestUS0031_EnvBackendMissingKeyReturnsError verifies that Get on an unset
// env var returns ErrNotFound (not silent empty value).
func TestUS0031_EnvBackendMissingKeyReturnsError(t *testing.T) {
	r, err := secrets.New(config.SecretsConfig{Backend: "env"})
	require.NoError(t, err)
	_, err = r.Get(context.Background(), "CTXT_TEST_DEFINITELY_NOT_SET_XYZZY")
	require.Error(t, err)
	assert.True(t, errors.Is(err, secret.ErrNotFound), "want ErrNotFound, got %v", err)
}

// TestUS0031_EnvBackendSetReturnsNotSupported verifies that Set on the env
// backend returns ErrNotSupported (env is read-only at runtime).
func TestUS0031_EnvBackendSetReturnsNotSupported(t *testing.T) {
	r, err := secrets.New(config.SecretsConfig{Backend: "env"})
	require.NoError(t, err)
	err = r.Set(context.Background(), "CTXT_TEST_KEY", []byte("some-value"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, secret.ErrNotSupported), "want ErrNotSupported, got %v", err)
}

// TestUS0031_NewResolverEnvBackend verifies that the factory returns a working
// store for the "env" backend without error.
func TestUS0031_NewResolverEnvBackend(t *testing.T) {
	r, err := secrets.New(config.SecretsConfig{Backend: "env"})
	require.NoError(t, err)
	require.NotNil(t, r)
}

// TestUS0031_NewResolverEmptyBackendDefaultsToEnv verifies that empty Backend
// defaults to env (backward-compatible).
func TestUS0031_NewResolverEmptyBackendDefaultsToEnv(t *testing.T) {
	r, err := secrets.New(config.SecretsConfig{})
	require.NoError(t, err)
	require.NotNil(t, r)
}

// TestUS0031_NewResolverAgeFileMissingAgeFile verifies the age-file backend
// errors when AgeFile is empty.
func TestUS0031_NewResolverAgeFileMissingAgeFile(t *testing.T) {
	cfg := config.SecretsConfig{
		Backend:         "age-file",
		AgeIdentityFile: "/path/to/identity.txt",
	}
	_, err := secrets.New(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "age_file")
}

// TestUS0031_NewResolverAgeFileMissingIdentityFile verifies the age-file backend
// errors when AgeIdentityFile is empty.
func TestUS0031_NewResolverAgeFileMissingIdentityFile(t *testing.T) {
	cfg := config.SecretsConfig{
		Backend: "age-file",
		AgeFile: "/path/to/secrets.age",
	}
	_, err := secrets.New(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "age_identity_file")
}

// TestUS0031_NewResolverOnePasswordMissingVault verifies the 1password backend
// errors when OnePasswordVault is empty.
func TestUS0031_NewResolverOnePasswordMissingVault(t *testing.T) {
	cfg := config.SecretsConfig{Backend: "1password"}
	_, err := secrets.New(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vault")
}

// TestUS0031_NewResolverUnknownBackendReturnsError verifies an unknown backend
// value returns a validation error.
func TestUS0031_NewResolverUnknownBackendReturnsError(t *testing.T) {
	_, err := secrets.New(config.SecretsConfig{Backend: "not-a-real-backend"})
	require.Error(t, err)
}

// TestUS0031_KeychainBackendDefaultsServiceName verifies that omitting
// KeychainService still constructs a valid store (default "ctxt").
func TestUS0031_KeychainBackendDefaultsServiceName(t *testing.T) {
	r, err := secrets.New(config.SecretsConfig{Backend: "keychain"})
	require.NoError(t, err)
	require.NotNil(t, r)
}

// TestUS0031_ProviderFactoryUsesResolver verifies that the providers factory
// delegates API key lookup to the supplied store, not to os.Getenv directly.
func TestUS0031_ProviderFactoryUsesResolver(t *testing.T) {
	resolver := newTestMockResolver(map[string]string{
		"ANTHROPIC_API_KEY": "sk-from-secrets-resolver",
	})
	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "auto"

	f := providers.NewFactory(cfg, resolver)
	require.NotNil(t, f)

	llm := f.LLM()
	assert.NotNil(t, llm, "factory must return a provider when resolver supplies a key")
}

// TestUS0031_NoPlaintextKeyInSecretsConfig verifies that the SecretsConfig
// struct never carries plaintext secret values — only metadata.
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

	for _, k := range []string{"api_key", "apikey", "password", "secret", "token", "key_value"} {
		_, exists := raw[k]
		assert.False(t, exists, "SecretsConfig must not contain field %q (would store plaintext secret)", k)
	}
}
