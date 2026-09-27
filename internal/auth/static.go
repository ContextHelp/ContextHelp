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
	// Roles are the role grants attached to the principal; each one
	// must be a known role (see Roles). They expand to the principal's
	// scopes.
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
	tokenHash []byte // HashSecret(token), for session revocation coupling
	principal Principal
}

// NewStatic builds a StaticProvider from the configured token table.
// Every entry needs a non-empty token, a principal and at least one
// known role; tokens must be unique so a credential resolves to exactly
// one identity.
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
		if err := ValidateRoles(t.Roles); err != nil {
			return nil, fmt.Errorf("auth: static token [%d]: %w", i, err)
		}
		if _, dup := seen[t.Token]; dup {
			return nil, fmt.Errorf("auth: static token [%d]: duplicate token (tokens must be unique)", i)
		}
		seen[t.Token] = struct{}{}
		entries = append(entries, staticEntry{
			token:     []byte(t.Token),
			tokenHash: []byte(HashSecret(t.Token)),
			principal: Principal{
				ID:       t.Principal,
				Name:     t.Principal,
				Provider: ProviderStatic,
				Roles:    append([]string(nil), t.Roles...),
				Scopes:   ScopesForRoles(t.Roles),
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
	// A session cookie is never a static token, even by accident.
	if cred.Scheme == SchemeSession {
		return nil, ErrInvalidCredential
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
	out.Scopes = append([]Scope(nil), match.principal.Scopes...)
	return &out, nil
}

// PrincipalForTokenHash implements TokenHashResolver: it returns the
// principal of the configured token whose HashSecret is hash. Every
// entry is compared in constant time.
func (p *StaticProvider) PrincipalForTokenHash(hash string) (*Principal, bool) {
	presented := []byte(hash)
	var match *staticEntry
	for i := range p.entries {
		e := &p.entries[i]
		if len(e.tokenHash) == len(presented) &&
			subtle.ConstantTimeCompare(e.tokenHash, presented) == 1 && match == nil {
			match = e
		}
	}
	if match == nil {
		return nil, false
	}
	out := match.principal
	out.Roles = append([]string(nil), match.principal.Roles...)
	out.Scopes = append([]Scope(nil), match.principal.Scopes...)
	return &out, true
}
