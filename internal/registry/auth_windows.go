//go:build windows

// Package registry manages per-registry auth tokens via Windows Credential Manager.
package registry

import (
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/secrets"
)

const keychainService = "ctxt-registry"

// ErrNoToken is returned when no token is found for a registry.
type ErrNoToken struct {
	RegistryName string
}

func (e ErrNoToken) Error() string {
	return fmt.Sprintf("registry: no token stored for %q — run: ctxt registry login %s",
		e.RegistryName, e.RegistryName)
}

// TokenStore stores and retrieves registry auth tokens from Windows Credential Manager.
type TokenStore struct {
	kc *secrets.KeychainResolver
}

// NewTokenStore creates a TokenStore backed by Windows Credential Manager.
func NewTokenStore() *TokenStore {
	return &TokenStore{kc: secrets.NewKeychainResolver(keychainService)}
}

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

func (s *TokenStore) Delete(registryName string) error {
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
