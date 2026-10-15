package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ProviderAPIKey is the Provider name of API keys issued against the
// principal registry (ADR-079 §2).
const ProviderAPIKey = "api_key"

// Principal.Meta keys set on a principal that authenticated with an
// API key.
const (
	// MetaKeyID is the id of the key that authenticated the request.
	MetaKeyID = "key_id"
	// MetaIdentityID is the identity row of that key.
	MetaIdentityID = "identity_id"
)

// MintKey is what Keys.Mint issues a key for.
type MintKey struct {
	// PrincipalID is the principal the key acts for; it must be active.
	PrincipalID string
	// Name labels the key's identity row ("laptop CLI").
	Name string
	// ExpiresAt, when set, must be in the future; zero never expires.
	ExpiresAt time.Time
	// Scopes narrows the key; empty means the principal's role scopes.
	// A scope the principal's roles do not grant is refused.
	Scopes []Scope
}

// IssuedKey describes a minted key: everything but the secret.
type IssuedKey struct {
	ID          string    `json:"id"`
	PrincipalID string    `json:"principal_id"`
	IdentityID  string    `json:"identity_id"`
	Name        string    `json:"name,omitempty"`
	Scopes      []Scope   `json:"scopes"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at,omitzero"`
}

// Keys mints, verifies and revokes API keys over the principal
// registry: each key is a KeyIssuer record (hash, scopes, expiry,
// revocation) mirrored by an identity row (method api_key, subject =
// key id) that carries its name and last use and ties it to the
// principal's status. Verify refuses a key whose record, identity or
// principal is revoked, expired or suspended.
type Keys struct {
	issuer KeyIssuer
	auth   storage.AuthStore
	now    func() time.Time
}

// NewKeys returns Keys over issuer and the registry in auth. now is
// the clock; nil means time.Now.
func NewKeys(issuer KeyIssuer, auth storage.AuthStore, now func() time.Time) *Keys {
	if now == nil {
		now = time.Now
	}
	return &Keys{issuer: issuer, auth: auth, now: now}
}

// NewKeysOver returns Keys with a LocalKeys issuer over the registry's
// kv store.
func NewKeysOver(auth storage.AuthStore, now func() time.Time) *Keys {
	return NewKeys(NewLocalKeys(auth.KV(), now), auth, now)
}

// Mint issues a key for spec.PrincipalID and returns the secret, shown
// once, with the key's description. An unknown principal is
// storage.ErrNotFound; a suspended or revoked one is
// storage.ErrPrincipalInactive.
func (k *Keys) Mint(ctx context.Context, spec MintKey) (string, *IssuedKey, error) {
	if spec.PrincipalID == "" {
		return "", nil, errors.New("auth: an api key needs a principal")
	}
	p, err := k.auth.Principals().Get(ctx, spec.PrincipalID)
	if err != nil {
		return "", nil, err
	}
	if !p.Active() {
		return "", nil, fmt.Errorf("auth: principal %s is %s: %w", p.ID, p.Status, storage.ErrPrincipalInactive)
	}
	scopes, err := keyScopes(p, spec.Scopes)
	if err != nil {
		return "", nil, err
	}
	now := k.now().UTC()
	var ttl time.Duration
	if !spec.ExpiresAt.IsZero() {
		if ttl = spec.ExpiresAt.Sub(now); ttl <= 0 {
			return "", nil, fmt.Errorf("auth: expiry %s is not in the future", spec.ExpiresAt.UTC().Format(time.RFC3339))
		}
	}
	names := make([]string, len(scopes))
	for i, s := range scopes {
		names[i] = string(s)
	}
	secret, rec, err := k.issuer.Create(ctx, NewAPIKey{Principal: p.ID, Scopes: names, TTL: ttl})
	if err != nil {
		return "", nil, err
	}
	idn := &storage.Identity{PrincipalID: p.ID, Method: storage.IdentityMethodAPIKey, Subject: rec.ID,
		Name: spec.Name, CreatedAt: rec.CreatedAt}
	if err := k.auth.Identities().Create(ctx, idn); err != nil {
		// A key without an identity row would never verify; take the
		// record back out so it cannot linger half-made.
		if _, rerr := k.issuer.Revoke(ctx, rec.ID); rerr != nil {
			err = errors.Join(err, rerr)
		}
		return "", nil, fmt.Errorf("auth: bind api key %s: %w", rec.ID, err)
	}
	return secret, &IssuedKey{
		ID: rec.ID, PrincipalID: p.ID, IdentityID: idn.ID, Name: spec.Name, Scopes: scopes,
		CreatedAt: rec.CreatedAt, ExpiresAt: rec.ExpiresAt,
	}, nil
}

// keyScopes returns the scopes a key for p gets: the principal's role
// scopes when none are asked for, else the asked ones, each of which
// the roles must grant.
func keyScopes(p *storage.Principal, asked []Scope) ([]Scope, error) {
	granted := ScopesForRoles(p.Roles)
	if len(asked) == 0 {
		return granted, nil
	}
	out := make([]Scope, 0, len(asked))
	for _, s := range AllScopes {
		if !slices.Contains(asked, s) {
			continue
		}
		if !slices.Contains(granted, s) {
			return nil, fmt.Errorf("auth: scope %s is not granted to principal %s (roles %v)", s, p.ID, p.Roles)
		}
		out = append(out, s)
	}
	for _, s := range asked {
		if !slices.Contains(AllScopes, s) {
			return nil, fmt.Errorf("auth: unknown scope %q", s)
		}
	}
	return out, nil
}

// Verify resolves raw, a presented API key, to the principal it acts
// for and the identity row of the key, and records the use. Refusals
// wrap ErrInvalidCredential: a malformed or unknown key, a wrong
// secret, an expired or revoked key, a revoked identity, a suspended
// or revoked principal. An empty raw is ErrNoCredential.
func (k *Keys) Verify(ctx context.Context, raw string) (*Principal, *storage.Identity, error) {
	rec, err := k.issuer.Verify(ctx, raw)
	if err != nil {
		return nil, nil, err
	}
	idn, err := k.auth.Identities().Find(ctx, storage.IdentityMethodAPIKey, "", rec.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil, fmt.Errorf("%w: api key %s has no identity", ErrInvalidCredential, rec.ID)
	}
	if err != nil {
		return nil, nil, err
	}
	if !idn.Active() {
		return nil, nil, fmt.Errorf("%w: api key %s was revoked", ErrInvalidCredential, rec.ID)
	}
	if idn.PrincipalID != rec.Principal {
		return nil, nil, fmt.Errorf("%w: api key %s is bound to another principal", ErrInvalidCredential, rec.ID)
	}
	p, err := k.auth.Principals().Get(ctx, idn.PrincipalID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil, fmt.Errorf("%w: api key %s has no principal", ErrInvalidCredential, rec.ID)
	}
	if err != nil {
		return nil, nil, err
	}
	if !p.Active() {
		return nil, nil, fmt.Errorf("%w: principal %s is %s", ErrInvalidCredential, p.ID, p.Status)
	}
	if err := k.auth.Identities().Touch(ctx, idn.ID, k.now().UTC()); err != nil {
		return nil, nil, err
	}
	return keyPrincipal(p, rec, idn), idn, nil
}

// keyPrincipal is the Principal a verified key acts as.
func keyPrincipal(p *storage.Principal, rec *APIKey, idn *storage.Identity) *Principal {
	scopes := make([]Scope, 0, len(rec.Scopes))
	for _, s := range AllScopes {
		if slices.Contains(rec.Scopes, string(s)) {
			scopes = append(scopes, s)
		}
	}
	return &Principal{
		ID:       p.ID,
		Name:     p.DisplayName,
		Provider: ProviderAPIKey,
		Roles:    slices.Clone(p.Roles),
		Scopes:   scopes,
		Meta:     map[string]string{MetaKeyID: rec.ID, MetaIdentityID: idn.ID},
	}
}

// Revoke ends the key with id: its record stops verifying and its
// identity row is revoked. An unknown id is storage.ErrNotFound;
// revoking a revoked key changes nothing.
func (k *Keys) Revoke(ctx context.Context, id string) error {
	if _, err := k.issuer.Revoke(ctx, id); err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return fmt.Errorf("api key %s: %w", id, storage.ErrNotFound)
		}
		return err
	}
	idn, err := k.auth.Identities().Find(ctx, storage.IdentityMethodAPIKey, "", id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return k.auth.Identities().Revoke(ctx, idn.ID, k.now().UTC())
}

// APIKeyProvider is the Provider over Keys: bearer tokens and X-API-Key
// headers that carry an API key. It is built here for Phase 1b to wire
// into the method chain; nothing selects it from config yet.
type APIKeyProvider struct{ keys *Keys }

// NewAPIKeyProvider returns a Provider that resolves API keys with keys.
func NewAPIKeyProvider(keys *Keys) *APIKeyProvider { return &APIKeyProvider{keys: keys} }

// Name implements Provider.
func (*APIKeyProvider) Name() string { return ProviderAPIKey }

// Authenticate implements Provider. A session cookie is never an API
// key; a connection that carries only a client certificate presents no
// credential to this provider.
func (p *APIKeyProvider) Authenticate(ctx context.Context, cred Credential) (*Principal, error) {
	if cred.Scheme == SchemeSession {
		return nil, ErrInvalidCredential
	}
	if cred.Token == "" {
		return nil, ErrNoCredential
	}
	principal, _, err := p.keys.Verify(ctx, cred.Token)
	if err != nil {
		if errors.Is(err, ErrInvalidCredential) || errors.Is(err, ErrNoCredential) {
			return nil, err
		}
		return nil, fmt.Errorf("auth: api key: %w", err)
	}
	return principal, nil
}
