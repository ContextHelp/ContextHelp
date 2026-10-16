package registry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"hop.top/kit/go/storage/secret"
)

// SecretTokenKey is the secrets-store key that holds registry name's
// token on a dpkms host: "registry/<name>", with every character of name
// other than an ASCII letter, digit or "_" replaced by "_". kit's env
// backend reads key k from the variable upper(k) with "/" as "_", so the
// key of registry "team-ops" is the exportable REGISTRY_TEAM_OPS, not
// REGISTRY_TEAM-OPS. Every backend uses the same key.
func SecretTokenKey(registryName string) string {
	return "registry/" + strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		default:
			return '_'
		}
	}, registryName)
}

// SecretTokens returns a TokenSource over a secrets store (dpkms's
// secrets.backend, the store `dpkms secret set|get` use). A server has
// no login keychain: registry name's token is the secret
// SecretTokenKey(name), a JSON {"origin": "...", "token": "..."} value
// that Credentials release under the same origin and https/loopback rules
// as a `ctxt registry login` token. open runs on the first lookup only,
// and its error is returned by every lookup.
func SecretTokens(open func() (secret.Store, error)) TokenSource {
	return &secretTokens{open: open}
}

type secretTokens struct {
	once  sync.Once
	open  func() (secret.Store, error)
	store secret.Store
	err   error
}

func (s *secretTokens) Get(registryName string) (StoredToken, error) {
	s.once.Do(func() { s.store, s.err = s.open() })
	if s.err != nil {
		return StoredToken{}, fmt.Errorf("registry: open secrets store: %w", s.err)
	}
	key := SecretTokenKey(registryName)
	got, err := s.store.Get(context.Background(), key)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			return StoredToken{}, ErrNoToken{RegistryName: registryName}
		}
		return StoredToken{}, fmt.Errorf("registry: read secret %s: %w", key, err)
	}
	return decodeStoredToken(registryName, got.Value)
}

// LoginCommand names how an operator stores registryName's token here.
func (s *secretTokens) LoginCommand(registryName string) string {
	return "dpkms secret set " + SecretTokenKey(registryName)
}
