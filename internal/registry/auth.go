//go:build darwin || linux

// Package registry manages per-registry auth tokens stored in the OS keychain.
// Tokens are NEVER written to the YAML config file; the keychain is the sole
// canonical store. The YAML auth block carries only type metadata and optional
// env-var hints (${VAR}) for CI/headless environments.
package registry

import (
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/secrets"
)

const (
	// keychainService is the service name used for all registry tokens.
	keychainService = "ctxt-registry"
)

// ErrNoToken is returned when no token is found for a registry.
type ErrNoToken struct {
	RegistryName string
}

func (e ErrNoToken) Error() string {
	return fmt.Sprintf("registry: no token stored for %q — run: ctxt registry login %s",
		e.RegistryName, e.RegistryName)
}

// TokenStore stores and retrieves registry auth tokens from the OS keychain.
// One KeychainResolver handles all registries; registry name is the account key.
type TokenStore struct {
	kc *secrets.KeychainResolver
}

// NewTokenStore creates a TokenStore backed by the OS keychain.
func NewTokenStore() *TokenStore {
	return &TokenStore{kc: secrets.NewKeychainResolver(keychainService)}
}

// Set stores token for registryName in the keychain.
// Previous token for the same registry is silently overwritten (-U flag).
func (s *TokenStore) Set(registryName, token string) error {
	if registryName == "" {
		return errors.New("registry: name must not be empty")
	}
	if token == "" {
		return errors.New("registry: token must not be empty")
	}
	if err := s.kc.Set(registryName, token); err != nil {
		return fmt.Errorf("registry: store token: %w", err)
	}
	return nil
}

// Get retrieves the token for registryName from the keychain.
// Returns ErrNoToken when nothing is stored.
func (s *TokenStore) Get(registryName string) (string, error) {
	token, err := s.kc.Get(registryName)
	if err != nil {
		var notFound secrets.ErrNotFound
		if errors.As(err, &notFound) {
			return "", ErrNoToken{RegistryName: registryName}
		}
		return "", fmt.Errorf("registry: retrieve token: %w", err)
	}
	return token, nil
}

// Delete removes the stored token for registryName from the keychain.
// Returns ErrNoToken when nothing was stored.
func (s *TokenStore) Delete(registryName string) error {
	// Verify existence first to surface a clear error.
	if _, err := s.kc.Get(registryName); err != nil {
		var notFound secrets.ErrNotFound
		if errors.As(err, &notFound) {
			return ErrNoToken{RegistryName: registryName}
		}
		return fmt.Errorf("registry: delete token: %w", err)
	}
	if err := s.kc.Delete(registryName); err != nil {
		return fmt.Errorf("registry: delete token: %w", err)
	}
	return nil
}
