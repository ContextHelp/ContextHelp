package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
)

// StaticToken is one accepted credential and the identity it maps to.
type StaticToken struct {
	// Token is the shared secret presented by the caller.
	Token string
	// Principal is the stable identity assigned to callers of this token.
	Principal string
	// Roles are coarse role grants attached to the principal.
	Roles []string
}

// StaticProvider validates bearer tokens / API keys against a fixed
// table from server config. It is the first (and simplest) Provider
// implementation; richer backends (OIDC, mTLS) plug in behind the same
// interface.
type StaticProvider struct {
	entries []staticEntry
}

type staticEntry struct {
	token     []byte
	principal Principal
}

// NewStatic builds a StaticProvider from the configured token table.
// Every entry needs a non-empty token and principal; tokens must be
// unique so a credential resolves to exactly one identity.
func NewStatic(tokens []StaticToken) (*StaticProvider, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("auth: static provider requires at least one token")
	}
	seen := make(map[string]struct{}, len(tokens))
	entries := make([]staticEntry, 0, len(tokens))
	for i, t := range tokens {
		if t.Token == "" {
			return nil, fmt.Errorf("auth: static token [%d]: token must not be empty", i)
		}
		if t.Principal == "" {
			return nil, fmt.Errorf("auth: static token [%d]: principal must not be empty", i)
		}
		if _, dup := seen[t.Token]; dup {
			return nil, fmt.Errorf("auth: static token [%d]: duplicate token (tokens must be unique)", i)
		}
		seen[t.Token] = struct{}{}
		entries = append(entries, staticEntry{
			token: []byte(t.Token),
			principal: Principal{
				ID:       t.Principal,
				Name:     t.Principal,
				Provider: ProviderStatic,
				Roles:    append([]string(nil), t.Roles...),
			},
		})
	}
	return &StaticProvider{entries: entries}, nil
}

// Name implements Provider.
func (*StaticProvider) Name() string { return ProviderStatic }

// Authenticate implements Provider. It scans every entry with a
// constant-time comparison so response timing does not narrow down
// token prefixes.
func (p *StaticProvider) Authenticate(_ context.Context, cred Credential) (*Principal, error) {
	if cred.Empty() {
		return nil, ErrNoCredential
	}
	presented := []byte(cred.Token)
	var match *staticEntry
	for i := range p.entries {
		e := &p.entries[i]
		if len(e.token) == len(presented) &&
			subtle.ConstantTimeCompare(e.token, presented) == 1 && match == nil {
			match = e
		}
	}
	if match == nil {
		return nil, ErrInvalidCredential
	}
	// Copy so callers cannot mutate provider state through the result.
	out := match.principal
	out.Roles = append([]string(nil), match.principal.Roles...)
	return &out, nil
}
