// Package authtest holds test doubles for the auth method chain: a
// method that resolves one fixed principal, and a method over a fixed
// token table. Route, scope and entitlement tests use them where who
// verified the credential is beside the point; tests of the chain, of
// keys and of sessions use the real methods over a registry. Import it
// from test files only.
package authtest

import (
	"context"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fixed authenticates any carried credential as one principal.
type fixed struct{ p *authn.Principal }

func (fixed) Name() string  { return "fixed" }
func (fixed) Ambient() bool { return false }

func (f fixed) Authenticate(_ context.Context, cred authn.Credential) (*authn.Principal, bool, error) {
	if cred.Token == "" {
		return nil, false, nil
	}
	out := *f.p
	return &out, true, nil
}

// Fixed returns a chain whose one carried method authenticates any
// presented token as p (a copy per request), and nothing when no token
// is presented.
func Fixed(p *authn.Principal) *authn.Chain { return authn.NewChain(fixed{p: p}) }

// Token is one entry of a Tokens table: the token and the principal it
// resolves to, with its roles' scopes.
type Token struct {
	Token     string
	Principal string
	// Kind is the principal kind; empty means person, or owner for
	// Principal storage.OwnerPrincipal.
	Kind  string
	Roles []string
}

// tokens authenticates a carried credential against a fixed table.
type tokens map[string]*authn.Principal

func (tokens) Name() string  { return authn.MethodAPIKey }
func (tokens) Ambient() bool { return false }

func (m tokens) Authenticate(_ context.Context, cred authn.Credential) (*authn.Principal, bool, error) {
	if cred.Token == "" {
		return nil, false, nil
	}
	p, ok := m[cred.Token]
	if !ok {
		return nil, false, authn.ErrInvalidCredential
	}
	out := *p
	out.Roles = append([]string(nil), p.Roles...)
	out.Scopes = append([]authn.Scope(nil), p.Scopes...)
	return &out, true, nil
}

// Tokens returns a carried method named api_key over entries: each
// token resolves to its principal with the scopes its roles grant; any
// other presented token is refused.
func Tokens(entries ...Token) authn.Method {
	m := tokens{}
	for _, e := range entries {
		kind := e.Kind
		if kind == "" {
			kind = storage.PrincipalKindPerson
			if e.Principal == storage.OwnerPrincipal {
				kind = storage.PrincipalKindOwner
			}
		}
		m[e.Token] = &authn.Principal{
			ID:       e.Principal,
			Name:     e.Principal,
			Kind:     kind,
			Provider: authn.MethodAPIKey,
			Roles:    e.Roles,
			Scopes:   authn.ScopesForRoles(e.Roles),
		}
	}
	return m
}

// TokenChain is Tokens as a chain of its own.
func TokenChain(entries ...Token) *authn.Chain { return authn.NewChain(Tokens(entries...)) }
