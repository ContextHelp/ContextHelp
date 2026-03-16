package secrets

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResolverEnv(t *testing.T) {
	r, err := NewResolver(config.SecretsConfig{Backend: "env"})
	require.NoError(t, err)
	assert.IsType(t, &EnvResolver{}, r)
}

func TestNewResolverEmptyBackendDefaultsToEnv(t *testing.T) {
	r, err := NewResolver(config.SecretsConfig{})
	require.NoError(t, err)
	assert.IsType(t, &EnvResolver{}, r)
}

func TestNewResolverAgeFileMissingPath(t *testing.T) {
	_, err := NewResolver(config.SecretsConfig{Backend: "age-file"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "age_file")
}

func TestNewResolverAgeFileMissingIdentity(t *testing.T) {
	_, err := NewResolver(config.SecretsConfig{Backend: "age-file", AgeFile: "/tmp/secrets.age"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "age_identity_file")
}

func TestNewResolverUnknownBackend(t *testing.T) {
	_, err := NewResolver(config.SecretsConfig{Backend: "vault"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vault")
}

func TestNewResolverKeychain(t *testing.T) {
	r, err := NewResolver(config.SecretsConfig{Backend: "keychain", KeychainService: "ctxt"})
	require.NoError(t, err)
	assert.IsType(t, &KeychainResolver{}, r)
}
