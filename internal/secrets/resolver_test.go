package secrets

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvResolverGet(t *testing.T) {
	os.Setenv("TEST_SECRET_KEY", "myvalue")
	defer os.Unsetenv("TEST_SECRET_KEY")

	r := NewEnvResolver()
	v, err := r.Get("TEST_SECRET_KEY")
	require.NoError(t, err)
	assert.Equal(t, "myvalue", v)
}

func TestEnvResolverGetMissing(t *testing.T) {
	r := NewEnvResolver()
	_, err := r.Get("THIS_VAR_DOES_NOT_EXIST_XYZ")
	assert.Error(t, err)
}

func TestEnvResolverSetIsReadOnly(t *testing.T) {
	r := NewEnvResolver()
	err := r.Set("SOME_KEY", "value")
	assert.Error(t, err)
}

func TestErrNotFoundMessage(t *testing.T) {
	err := ErrNotFound{Key: "MY_KEY"}
	assert.Contains(t, err.Error(), "MY_KEY")
}

func TestEnvResolverDoesNotImplementLister(t *testing.T) {
	r := NewEnvResolver()
	_, ok := any(r).(Lister)
	assert.False(t, ok, "EnvResolver should not implement Lister")
}
