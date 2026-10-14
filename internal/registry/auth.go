// Package registry manages per-registry auth tokens stored in the OS keychain.
// Tokens are NEVER written to the YAML config file; the keychain is the sole
// canonical store. The YAML auth block carries only type metadata.
//
// Each entry binds the token to the registry origin it was issued for at
// login (scheme + host + port); Credentials release it only to that origin.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"hop.top/kit/go/storage/secret"
	"hop.top/kit/go/storage/secret/keyring"
)

// keychainService is the service name used for all registry tokens.
const keychainService = "ctxt-registry"

// ErrNoToken is returned when no token is found for a registry.
type ErrNoToken struct {
	RegistryName string
}

func (e ErrNoToken) Error() string {
	return fmt.Sprintf("registry: no token stored for %q — run: ctxt registry login %s",
		e.RegistryName, e.RegistryName)
}

// TokenStore stores and retrieves registry auth tokens from the OS keychain.
// One keyring store handles all registries; registry name is the account key.
type TokenStore struct {
	kc secret.MutableStore
}

// NewTokenStore creates a TokenStore backed by the OS keychain.
func NewTokenStore() *TokenStore {
	return &TokenStore{kc: keyring.New(keychainService)}
}

// StoredToken is a registry token and the canonical origin
// (CanonicalOrigin) of the registry URL it was stored for.
type StoredToken struct {
	Origin string `json:"origin"`
	Token  string `json:"token"`
}

// Set stores token for registryName, bound to origin, in the keychain.
// Existing tokens are silently overwritten.
func (s *TokenStore) Set(registryName, origin, token string) error {
	if registryName == "" {
		return errors.New("registry: name must not be empty")
	}
	if origin == "" {
		return errors.New("registry: origin must not be empty")
	}
	if token == "" {
		return errors.New("registry: token must not be empty")
	}
	value, err := json.Marshal(StoredToken{Origin: origin, Token: token})
	if err != nil {
		return fmt.Errorf("registry: encode token: %w", err)
	}
	if err := s.kc.Set(context.Background(), registryName, value); err != nil {
		return fmt.Errorf("registry: store token: %w", err)
	}
	return nil
}

// Get retrieves the token for registryName from the keychain.
// Returns ErrNoToken when nothing is stored, or when the entry records no
// origin (nothing to bind it to).
func (s *TokenStore) Get(registryName string) (StoredToken, error) {
	got, err := s.kc.Get(context.Background(), registryName)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return StoredToken{}, ErrNoToken{RegistryName: registryName}
		}
		return StoredToken{}, fmt.Errorf("registry: retrieve token: %w", err)
	}
	var st StoredToken
	if json.Unmarshal(got.Value, &st) != nil || st.Origin == "" || st.Token == "" {
		return StoredToken{}, ErrNoToken{RegistryName: registryName}
	}
	return st, nil
}

// Delete removes the stored token for registryName from the keychain.
// Returns ErrNoToken when nothing was stored.
func (s *TokenStore) Delete(registryName string) error {
	if _, err := s.kc.Get(context.Background(), registryName); err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return ErrNoToken{RegistryName: registryName}
		}
		return fmt.Errorf("registry: delete token: %w", err)
	}
	if err := s.kc.Delete(context.Background(), registryName); err != nil {
		return fmt.Errorf("registry: delete token: %w", err)
	}
	return nil
}
