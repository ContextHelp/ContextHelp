package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Principal.Meta keys set on a principal that authenticated with an
// API key.
const (
	// MetaKeyID is the id of the key that authenticated the request; for
	// a key declared in server.auth.keys it is "config:" and the first
	// twelve hex digits of its hash.
	MetaKeyID = "key_id"
	// MetaIdentityID is the identity row of that key. A key declared in
	// config has none, so the meta is absent.
	MetaIdentityID = "identity_id"
	// configKeyIDPrefix begins the MetaKeyID of a config-declared key.
	configKeyIDPrefix = "config:"
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
	out := registryPrincipal(p, scopesNamed(rec.Scopes), MethodAPIKey)
	out.Meta = map[string]string{MetaKeyID: rec.ID, MetaIdentityID: idn.ID}
	return out
}

// scopesNamed returns the known scopes among names, in AllScopes order.
func scopesNamed(names []string) []Scope {
	scopes := make([]Scope, 0, len(names))
	for _, s := range AllScopes {
		if slices.Contains(names, string(s)) {
			scopes = append(scopes, s)
		}
	}
	return scopes
}

// registryPrincipal is the Principal a registry row acts as when
// method authenticated it with the given effective scopes.
func registryPrincipal(p *storage.Principal, scopes []Scope, method string) *Principal {
	return &Principal{
		ID:       p.ID,
		Name:     p.DisplayName,
		Kind:     p.Kind,
		Provider: method,
		Roles:    slices.Clone(p.Roles),
		Scopes:   scopes,
	}
}

// ScopesOf returns the effective scopes of the credential behind idn:
// for an API key its record's scopes, else the principal's role scopes
// (p). A key whose record is gone has no scopes.
func (k *Keys) ScopesOf(ctx context.Context, p *storage.Principal, idn *storage.Identity) ([]Scope, error) {
	if idn.Method != storage.IdentityMethodAPIKey {
		return ScopesForRoles(p.Roles), nil
	}
	rec, err := k.issuer.Get(ctx, idn.Subject)
	if errors.Is(err, ErrAPIKeyNotFound) {
		return []Scope{}, nil
	}
	if err != nil {
		return nil, err
	}
	return scopesNamed(rec.Scopes), nil
}

// OwnerHasActiveKey reports whether the owner holds an API key that
// still verifies: an active api_key identity whose record is neither
// revoked nor expired at now.
func (k *Keys) OwnerHasActiveKey(ctx context.Context, now time.Time) (bool, error) {
	idns, err := k.auth.Identities().ListByPrincipal(ctx, storage.OwnerPrincipal)
	if err != nil {
		return false, err
	}
	for _, idn := range idns {
		if idn.Method != storage.IdentityMethodAPIKey || !idn.Active() {
			continue
		}
		rec, err := k.issuer.Get(ctx, idn.Subject)
		if errors.Is(err, ErrAPIKeyNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if rec.RevokedAt.IsZero() && (rec.ExpiresAt.IsZero() || now.Before(rec.ExpiresAt)) {
			return true, nil
		}
	}
	return false, nil
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

// ConfigKey is one API key declared by hash in server.auth.keys
// (ADR-079 §4): the api_key method accepts a presented key whose
// SHA-256 is Hash and resolves it to the registry principal named,
// with the principal's role scopes narrowed to Roles when set. A
// config key is config, not a registry row: it has no identity, so it
// cannot mint a browser session, and it is revoked by removing it and
// restarting.
type ConfigKey struct {
	// Hash is the raw SHA-256 of the key.
	Hash []byte
	// Principal is the registry id the key acts for.
	Principal string
	// Roles narrows the key; empty means the principal's own roles.
	Roles []string
	// ExpiresAt, when set, is when the key stops working.
	ExpiresAt time.Time
}

// ConfigKeysFrom parses server.auth.keys entries, which config
// validation has already checked for shape.
func ConfigKeysFrom(entries []config.AuthKeyConfig) ([]ConfigKey, error) {
	out := make([]ConfigKey, 0, len(entries))
	for i, e := range entries {
		raw, ok := strings.CutPrefix(e.Hash, "sha256:")
		if !ok {
			return nil, fmt.Errorf("auth: server.auth.keys[%d].hash: not a sha256: hash", i)
		}
		h, err := hex.DecodeString(raw)
		if err != nil || len(h) != sha256.Size {
			return nil, fmt.Errorf("auth: server.auth.keys[%d].hash: not 64 hex digits", i)
		}
		at, err := e.ExpiresAt()
		if err != nil {
			return nil, fmt.Errorf("auth: server.auth.keys[%d].%w", i, err)
		}
		out = append(out, ConfigKey{Hash: h, Principal: e.Principal, Roles: slices.Clone(e.Roles), ExpiresAt: at})
	}
	return out, nil
}

// APIKeyMethod is the api_key Method: a carried key in Authorization:
// Bearer or X-API-Key, verified against the keys declared in config by
// hash and then against the registry (Keys).
type APIKeyMethod struct {
	keys *Keys
	cfg  []ConfigKey
}

// NewAPIKeyMethod returns the api_key method over keys and the config
// keys cfg.
func NewAPIKeyMethod(keys *Keys, cfg []ConfigKey) *APIKeyMethod {
	return &APIKeyMethod{keys: keys, cfg: cfg}
}

// Name implements Method.
func (*APIKeyMethod) Name() string { return MethodAPIKey }

// Ambient implements Method: a key is chosen per request.
func (*APIKeyMethod) Ambient() bool { return false }

// Authenticate implements Method. No header credential is absent; a
// presented one either matches a config key, verifies as a registry
// key, or is refused.
func (m *APIKeyMethod) Authenticate(ctx context.Context, cred Credential) (*Principal, bool, error) {
	if cred.Token == "" {
		return nil, false, nil
	}
	if p, ok, err := m.configMatch(ctx, cred.Token); ok || err != nil {
		return p, ok, err
	}
	p, _, err := m.keys.Verify(ctx, cred.Token)
	if err != nil {
		if errors.Is(err, ErrInvalidCredential) || errors.Is(err, ErrNoCredential) {
			return nil, false, err
		}
		return nil, false, fmt.Errorf("auth: api key: %w", err)
	}
	return p, true, nil
}

// configMatch compares the presented key's hash with every config key
// in constant time and resolves the first match. A match whose key is
// expired or whose principal is unknown or inactive is refused, not
// passed on to the registry.
func (m *APIKeyMethod) configMatch(ctx context.Context, raw string) (*Principal, bool, error) {
	if len(m.cfg) == 0 {
		return nil, false, nil
	}
	sum := sha256.Sum256([]byte(raw))
	var hit *ConfigKey
	for i := range m.cfg {
		if subtle.ConstantTimeCompare(sum[:], m.cfg[i].Hash) == 1 && hit == nil {
			hit = &m.cfg[i]
		}
	}
	if hit == nil {
		return nil, false, nil
	}
	id := configKeyIDPrefix + hex.EncodeToString(hit.Hash[:6])
	if !hit.ExpiresAt.IsZero() && !m.keys.now().Before(hit.ExpiresAt) {
		return nil, false, fmt.Errorf("%w: key %s expired", ErrInvalidCredential, id)
	}
	p, err := m.keys.auth.Principals().Get(ctx, hit.Principal)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, false, fmt.Errorf("%w: key %s names unknown principal %s", ErrInvalidCredential, id, hit.Principal)
	}
	if err != nil {
		return nil, false, fmt.Errorf("auth: api key %s: %w", id, err)
	}
	if !p.Active() {
		return nil, false, fmt.Errorf("%w: principal %s is %s", ErrInvalidCredential, p.ID, p.Status)
	}
	roles := p.Roles
	scopes := ScopesForRoles(p.Roles)
	if len(hit.Roles) > 0 {
		roles = rolesWithin(hit.Roles, scopes)
		scopes = intersectScopes(ScopesForRoles(hit.Roles), scopes)
	}
	out := registryPrincipal(p, scopes, MethodAPIKey)
	out.Roles = slices.Clone(roles)
	out.Meta = map[string]string{MetaKeyID: id}
	return out, true, nil
}

// rolesWithin returns the roles in asked whose whole bundle granted
// covers: a key narrowed to reader on an admin principal acts as a
// reader; one asking for admin on a reader principal stays a reader.
func rolesWithin(asked []string, granted []Scope) []string {
	out := []string{}
	for _, r := range asked {
		bundle, ok := Bundle(r)
		if !ok {
			continue
		}
		covered := true
		for _, s := range bundle {
			if !slices.Contains(granted, s) {
				covered = false
				break
			}
		}
		if covered {
			out = append(out, r)
		}
	}
	return out
}

// intersectScopes returns the scopes in a that b also holds, in
// AllScopes order.
func intersectScopes(a, b []Scope) []Scope {
	out := []Scope{}
	for _, s := range AllScopes {
		if slices.Contains(a, s) && slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}

// RequireOwnerCredential is the startup check of a protected or public
// instance (ADR-079 §4): without an active owner credential, an
// unexpired and unrevoked API key bound to the owner in the registry or
// a server.auth.keys entry for the owner, it returns an error naming
// the command that creates one.
func RequireOwnerCredential(ctx context.Context, keys *Keys, cfg []ConfigKey, now time.Time) error {
	for _, k := range cfg {
		if k.Principal == storage.OwnerPrincipal && (k.ExpiresAt.IsZero() || now.Before(k.ExpiresAt)) {
			return nil
		}
	}
	ok, err := keys.OwnerHasActiveKey(ctx, now)
	if err != nil {
		return fmt.Errorf("auth: owner credential check: %w", err)
	}
	if !ok {
		return errors.New("auth: no active owner credential (an unexpired, unrevoked API key bound to owner, or a server.auth.keys entry for owner): " +
			"run `dpkms auth key create --principal owner` on this host, then start again")
	}
	return nil
}
