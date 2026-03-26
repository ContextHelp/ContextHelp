//go:build darwin || linux

package registry

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hasKeychain reports whether the OS keychain CLI is available.
func hasKeychain() bool {
	_, err := exec.LookPath("security")
	return err == nil
}

func TestNewTokenStore(t *testing.T) {
	s := NewTokenStore()
	assert.NotNil(t, s)
	assert.NotNil(t, s.kc)
}

func TestTokenStore_SetGetDelete(t *testing.T) {
	if !hasKeychain() {
		t.Skip("security binary not available — skip live keychain test")
	}

	// Use a unique service to isolate test data.
	origSvc := keychainService
	_ = origSvc // ensure constant is accessible

	s := &TokenStore{kc: newTestResolver(t)}

	const regName = "test-registry"
	const token = "tok-abc123"

	// Store
	require.NoError(t, s.Set(regName, token))

	// Retrieve
	got, err := s.Get(regName)
	require.NoError(t, err)
	assert.Equal(t, token, got)

	// Delete
	require.NoError(t, s.Delete(regName))

	// Gone
	_, err = s.Get(regName)
	var noToken ErrNoToken
	assert.True(t, errors.As(err, &noToken), "expected ErrNoToken after delete")
	assert.Equal(t, regName, noToken.RegistryName)
}

func TestTokenStore_GetMissing(t *testing.T) {
	if !hasKeychain() {
		t.Skip("security binary not available — skip live keychain test")
	}

	s := &TokenStore{kc: newTestResolver(t)}
	_, err := s.Get("definitely-not-stored-registry")
	var noToken ErrNoToken
	require.True(t, errors.As(err, &noToken))
	assert.Contains(t, err.Error(), "ctxt registry login")
}

func TestTokenStore_DeleteMissing(t *testing.T) {
	if !hasKeychain() {
		t.Skip("security binary not available — skip live keychain test")
	}

	s := &TokenStore{kc: newTestResolver(t)}
	err := s.Delete("definitely-not-stored-registry-del")
	var noToken ErrNoToken
	assert.True(t, errors.As(err, &noToken))
}

func TestTokenStore_SetEmptyName(t *testing.T) {
	s := NewTokenStore()
	err := s.Set("", "sometoken")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name must not be empty")
}

func TestTokenStore_SetEmptyToken(t *testing.T) {
	s := NewTokenStore()
	err := s.Set("myregistry", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token must not be empty")
}

func TestErrNoToken_Error(t *testing.T) {
	e := ErrNoToken{RegistryName: "myreg"}
	assert.Contains(t, e.Error(), "myreg")
	assert.Contains(t, e.Error(), "ctxt registry login")
}

func TestTokenStore_SetOverwrites(t *testing.T) {
	if !hasKeychain() {
		t.Skip("security binary not available — skip live keychain test")
	}

	s := &TokenStore{kc: newTestResolver(t)}
	const reg = "overwrite-reg"

	require.NoError(t, s.Set(reg, "first-token"))
	require.NoError(t, s.Set(reg, "second-token"))

	got, err := s.Get(reg)
	require.NoError(t, err)
	assert.Equal(t, "second-token", got)

	// Cleanup
	_ = s.Delete(reg)
}
