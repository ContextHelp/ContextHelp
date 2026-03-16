package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGHSecretsResolver(t *testing.T) {
	r := NewGHSecretsResolver("owner/repo")
	assert.NotNil(t, r)
}

func TestGHSecretsResolverGetFallsBackToEnv(t *testing.T) {
	// GitHub secrets are write-only; Get must fall back to env.
	t.Setenv("GH_TEST_SECRET_XYZ", "from-env")
	r := NewGHSecretsResolver("owner/repo")
	v, err := r.Get("GH_TEST_SECRET_XYZ")
	require.NoError(t, err)
	assert.Equal(t, "from-env", v)
}

func TestGHSecretsResolverGetMissingReturnsError(t *testing.T) {
	r := NewGHSecretsResolver("owner/repo")
	_, err := r.Get("THIS_ENV_VAR_DOES_NOT_EXIST_XYZ_GH")
	assert.Error(t, err)
}
