package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewOnePasswordResolver(t *testing.T) {
	r := NewOnePasswordResolver("MyVault")
	assert.NotNil(t, r)
}

func TestOnePasswordResolverSetNotSupported(t *testing.T) {
	r := NewOnePasswordResolver("MyVault")
	err := r.Set("KEY", "value")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "1Password")
}

func TestOnePasswordURIFormat(t *testing.T) {
	r := NewOnePasswordResolver("MyVault")
	// We can't run op in tests, but verify the URI it would use.
	uri := r.itemURI("MY_API_KEY")
	assert.Equal(t, "op://MyVault/MY_API_KEY/password", uri)
}

func TestOnePasswordResolverDoesNotImplementLister(t *testing.T) {
	r := NewOnePasswordResolver("MyVault")
	_, ok := any(r).(Lister)
	assert.False(t, ok, "OnePasswordResolver should not implement Lister")
}
