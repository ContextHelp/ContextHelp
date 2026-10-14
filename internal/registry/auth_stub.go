//go:build !darwin && !linux && !windows

// Package registry manages per-registry auth tokens stored in the OS keychain.
package registry

import (
	"errors"
	"fmt"
)

var errNotSupported = errors.New("registry: keychain auth not supported on this platform")

// ErrNoToken is returned when no token is found for a registry.
type ErrNoToken struct {
	RegistryName string
}

func (e ErrNoToken) Error() string {
	return fmt.Sprintf("registry: no token stored for %q — run: ctxt registry login %s",
		e.RegistryName, e.RegistryName)
}

// TokenStore is a no-op store for unsupported platforms.
type TokenStore struct{}

// NewTokenStore returns a stub TokenStore on unsupported platforms.
func NewTokenStore() *TokenStore { return &TokenStore{} }

// StoredToken is a registry token and the origin it was stored for.
type StoredToken struct {
	Origin string `json:"origin"`
	Token  string `json:"token"`
}

func (s *TokenStore) Set(_, _, _ string) error          { return errNotSupported }
func (s *TokenStore) Get(_ string) (StoredToken, error) { return StoredToken{}, errNotSupported }
func (s *TokenStore) Delete(_ string) error             { return errNotSupported }
