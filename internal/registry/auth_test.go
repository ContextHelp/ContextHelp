//go:build darwin || linux

package registry

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTokenStore(t *testing.T) {
	s := NewTokenStore()
	assert.NotNil(t, s)
	assert.NotNil(t, s.kc)
}

func TestTokenStore_SetGetDelete(t *testing.T) {
	s := &TokenStore{kc: newTestResolver(t)}

	const regName = "test-registry"
	const token = "tok-abc123"
	const origin = "https://registry.example"

	// Store
	require.NoError(t, s.Set(regName, origin, token))

	// Retrieve
	got, err := s.Get(regName)
	require.NoError(t, err)
	assert.Equal(t, StoredToken{Origin: origin, Token: token}, got)

	// Delete
	require.NoError(t, s.Delete(regName))

	// Gone
	_, err = s.Get(regName)
	var noToken ErrNoToken
	assert.True(t, errors.As(err, &noToken), "expected ErrNoToken after delete")
	assert.Equal(t, regName, noToken.RegistryName)
}

func TestTokenStore_GetMissing(t *testing.T) {
	s := &TokenStore{kc: newTestResolver(t)}
	_, err := s.Get("definitely-not-stored-registry")
	var noToken ErrNoToken
	require.True(t, errors.As(err, &noToken))
	assert.Contains(t, err.Error(), "ctxt registry login")
}

func TestTokenStore_DeleteMissing(t *testing.T) {
	s := &TokenStore{kc: newTestResolver(t)}
	err := s.Delete("definitely-not-stored-registry-del")
	var noToken ErrNoToken
	assert.True(t, errors.As(err, &noToken))
}

func TestTokenStore_SetEmptyName(t *testing.T) {
	s := &TokenStore{kc: newTestResolver(t)}
	err := s.Set("", "https://registry.example", "sometoken")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name must not be empty")
}

func TestTokenStore_SetEmptyToken(t *testing.T) {
	s := &TokenStore{kc: newTestResolver(t)}
	err := s.Set("myregistry", "https://registry.example", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token must not be empty")
}

func TestErrNoToken_Error(t *testing.T) {
	e := ErrNoToken{RegistryName: "myreg"}
	assert.Contains(t, e.Error(), "myreg")
	assert.Contains(t, e.Error(), "ctxt registry login")
}

func TestTokenStore_SetOverwrites(t *testing.T) {
	s := &TokenStore{kc: newTestResolver(t)}
	const reg = "overwrite-reg"

	require.NoError(t, s.Set(reg, "https://registry.example", "first-token"))
	require.NoError(t, s.Set(reg, "https://registry.example", "second-token"))

	got, err := s.Get(reg)
	require.NoError(t, err)
	assert.Equal(t, "second-token", got.Token)

	// Cleanup
	_ = s.Delete(reg)
}

func TestTokenStore_SetRequiresOrigin(t *testing.T) {
	s := &TokenStore{kc: newTestResolver(t)}
	err := s.Set("myregistry", "", "sometoken")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "origin must not be empty")
}

// An entry written without an origin (a bare token) is no token: there is
// no recorded origin to bind it to.
func TestTokenStore_EntryWithoutOriginIsNoToken(t *testing.T) {
	kc := newTestResolver(t)
	s := &TokenStore{kc: kc}
	for i, raw := range []string{"bare-token", `{"token":"tok"}`, `{"origin":"https://registry.example"}`} {
		name := fmt.Sprintf("legacy-%d", i)
		require.NoError(t, kc.Set(context.Background(), name, []byte(raw)))
		_, err := s.Get(name)
		var noToken ErrNoToken
		assert.True(t, errors.As(err, &noToken), "entry %q: err = %v", raw, err)
	}
}
