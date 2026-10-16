package auth

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// keysFixture is Keys over a fresh SQLite registry with a controllable
// clock and one person principal beside the seeded owner.
type keysFixture struct {
	auth   storage.AuthStore
	keys   *Keys
	clock  *keyClock
	person *storage.Principal
}

func newKeysFixture(t *testing.T) *keysFixture {
	t.Helper()
	c := newKeyClock()
	auth := storageutil.NewTestDriver(t).Auth()
	p := &storage.Principal{ID: "prn_person", Kind: storage.PrincipalKindPerson, DisplayName: "Alice",
		Roles: []string{RoleReader}, CreatedAt: c.t}
	if err := auth.Principals().Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return &keysFixture{auth: auth, keys: NewKeysOver(auth, c.now), clock: c, person: p}
}

func TestKeysMint(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	_, issued, err := f.keys.Mint(ctx, MintKey{PrincipalID: storage.OwnerPrincipal, Name: "laptop"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	admin, _ := Bundle(RoleAdmin)
	if issued.PrincipalID != storage.OwnerPrincipal || issued.Name != "laptop" || !slices.Equal(issued.Scopes, admin) ||
		!issued.CreatedAt.Equal(f.clock.t) || !issued.ExpiresAt.IsZero() || issued.IdentityID == "" {
		t.Errorf("issued = %+v, want the owner's admin scopes, no expiry", issued)
	}
	idn, err := f.auth.Identities().Find(ctx, storage.IdentityMethodAPIKey, "", issued.ID)
	if err != nil {
		t.Fatalf("identity row: %v", err)
	}
	if idn.ID != issued.IdentityID || idn.PrincipalID != storage.OwnerPrincipal || idn.Name != "laptop" || idn.LastUsedAt != nil {
		t.Errorf("identity = %+v", idn)
	}
}

func TestKeysVerify(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	secret, issued, err := f.keys.Mint(ctx, MintKey{PrincipalID: storage.OwnerPrincipal, Name: "laptop"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	admin, _ := Bundle(RoleAdmin)
	f.clock.advance(time.Minute)
	p, idn, err := f.keys.Verify(ctx, secret)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if p.ID != storage.OwnerPrincipal || p.Provider != MethodAPIKey || p.Name == "" || !p.HasRole(RoleAdmin) ||
		!slices.Equal(p.Scopes, admin) || p.Meta[MetaKeyID] != issued.ID || p.Meta[MetaIdentityID] != issued.IdentityID {
		t.Errorf("principal = %+v", p)
	}
	if idn.ID != issued.IdentityID || idn.Name != "laptop" {
		t.Errorf("identity = %+v, want %s", idn, issued.IdentityID)
	}
	idn, _ = f.auth.Identities().Get(ctx, idn.ID)
	if idn.LastUsedAt == nil || !idn.LastUsedAt.Equal(f.clock.t) {
		t.Errorf("last used = %v, want %v (touched on verify)", idn.LastUsedAt, f.clock.t)
	}
}

func TestKeysMintScopesAndExpiry(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	secret, issued, err := f.keys.Mint(ctx, MintKey{PrincipalID: f.person.ID, Scopes: []Scope{ScopeReadMCP, ScopeReadObjects},
		ExpiresAt: f.clock.t.Add(time.Hour)})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if !slices.Equal(issued.Scopes, []Scope{ScopeReadObjects, ScopeReadMCP}) || !issued.ExpiresAt.Equal(f.clock.t.Add(time.Hour)) {
		t.Errorf("issued = %+v, want the two scopes in AllScopes order and a one hour expiry", issued)
	}
	p, _, err := f.keys.Verify(ctx, secret)
	if err != nil || !slices.Equal(p.Scopes, []Scope{ScopeReadObjects, ScopeReadMCP}) || !p.HasRole(RoleReader) {
		t.Errorf("Verify = %+v, %v; want the narrowed scopes", p, err)
	}
	f.clock.advance(time.Hour)
	if _, _, err := f.keys.Verify(ctx, secret); !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("expired key: %v, want ErrInvalidCredential", err)
	}

	for name, spec := range map[string]MintKey{
		"scope beyond the role": {PrincipalID: f.person.ID, Scopes: []Scope{ScopeWriteObjects}},
		"unknown scope":         {PrincipalID: f.person.ID, Scopes: []Scope{"fly:moon"}},
		"expiry in the past":    {PrincipalID: f.person.ID, ExpiresAt: f.clock.t.Add(-time.Second)},
		"no principal":          {},
	} {
		if _, _, err := f.keys.Mint(ctx, spec); err == nil {
			t.Errorf("%s: minted", name)
		}
	}
	if _, _, err := f.keys.Mint(ctx, MintKey{PrincipalID: "prn_missing"}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("unknown principal: %v, want ErrNotFound", err)
	}
	if err := f.auth.Principals().Suspend(ctx, f.person.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.keys.Mint(ctx, MintKey{PrincipalID: f.person.ID}); !errors.Is(err, storage.ErrPrincipalInactive) {
		t.Errorf("suspended principal: %v, want ErrPrincipalInactive", err)
	}
}

func TestKeysRevoke(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	secret, issued, err := f.keys.Mint(ctx, MintKey{PrincipalID: f.person.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(time.Minute)
	if err := f.keys.Revoke(ctx, issued.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, _, err := f.keys.Verify(ctx, secret); !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("revoked key verifies: %v", err)
	}
	idn, _ := f.auth.Identities().Get(ctx, issued.IdentityID)
	if idn == nil || idn.RevokedAt == nil || !idn.RevokedAt.Equal(f.clock.t) {
		t.Errorf("identity after Revoke = %+v, want revoked at %v", idn, f.clock.t)
	}
	if err := f.keys.Revoke(ctx, issued.ID); err != nil {
		t.Errorf("second Revoke: %v", err)
	}
	if err := f.keys.Revoke(ctx, "0000000000000000"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Revoke(unknown): %v, want ErrNotFound", err)
	}
}

// TestKeysVerifyRefusesRevokedIdentity pins that a key whose identity
// row was revoked (without its record) no longer verifies: revoking a
// principal cascades to identities, not to key records.
func TestKeysVerifyRefusesRevokedIdentity(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	secret, issued, err := f.keys.Mint(ctx, MintKey{PrincipalID: f.person.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.auth.Identities().Revoke(ctx, issued.IdentityID, f.clock.t); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.keys.Verify(ctx, secret); !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("key with a revoked identity verifies: %v", err)
	}
}

func TestKeysVerifyFollowsPrincipalStatus(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	secret, _, err := f.keys.Mint(ctx, MintKey{PrincipalID: f.person.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.auth.Principals().Suspend(ctx, f.person.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.keys.Verify(ctx, secret); !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("suspended principal's key verifies: %v", err)
	}
	// Revoking the principal cascades to the identity row; the key
	// record itself is untouched and the key still fails.
	if err := f.auth.Principals().Revoke(ctx, f.person.ID, f.clock.t); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.keys.Verify(ctx, secret); !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("revoked principal's key verifies: %v", err)
	}
}

func TestKeysVerifyTouchesAtMostOnceAMinute(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	secret, issued, err := f.keys.Mint(ctx, MintKey{PrincipalID: f.person.ID})
	if err != nil {
		t.Fatal(err)
	}
	lastUsed := func() time.Time {
		idn, err := f.auth.Identities().Get(ctx, issued.IdentityID)
		if err != nil || idn.LastUsedAt == nil {
			t.Fatalf("last used: %v %v", idn, err)
		}
		return *idn.LastUsedAt
	}
	f.clock.advance(time.Second)
	if _, _, err := f.keys.Verify(ctx, secret); err != nil {
		t.Fatal(err)
	}
	first := lastUsed()
	f.clock.advance(30 * time.Second)
	if _, _, err := f.keys.Verify(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if got := lastUsed(); !got.Equal(first) {
		t.Errorf("last used moved to %v within a minute, want %v", got, first)
	}
	f.clock.advance(30 * time.Second)
	if _, _, err := f.keys.Verify(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if got := lastUsed(); !got.Equal(f.clock.t) {
		t.Errorf("last used = %v after a minute, want %v", got, f.clock.t)
	}
}

// The api_key method is carried: no header is absent, a wrong, revoked
// or expired key is refused, a valid one matches through either header
// scheme, and a cookie is not its business.
func TestAPIKeyMethod(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	secret, issued, err := f.keys.Mint(ctx, MintKey{PrincipalID: storage.OwnerPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	var m Method = NewAPIKeyMethod(f.keys, nil)
	if m.Name() != MethodAPIKey || m.Ambient() {
		t.Errorf("Name = %q, Ambient = %v; want api_key, carried", m.Name(), m.Ambient())
	}
	if p, ok, err := m.Authenticate(ctx, Credential{}); p != nil || ok || err != nil {
		t.Errorf("no credential: %v, %v, %v; want absent", p, ok, err)
	}
	if p, ok, err := m.Authenticate(ctx, Credential{Cookie: secret}); p != nil || ok || err != nil {
		t.Errorf("a cookie: %v, %v, %v; want absent", p, ok, err)
	}
	if _, ok, err := m.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: "nope"}); ok || !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("wrong key: %v, %v; want refused", ok, err)
	}
	for _, scheme := range []string{SchemeBearer, SchemeAPIKey} {
		p, ok, err := m.Authenticate(ctx, Credential{Scheme: scheme, Token: secret})
		if err != nil || !ok || p.ID != storage.OwnerPrincipal || p.Provider != MethodAPIKey || p.Kind != storage.PrincipalKindOwner ||
			p.Meta[MetaKeyID] != issued.ID || p.Meta[MetaIdentityID] != issued.IdentityID {
			t.Errorf("%s: %+v, %v, %v", scheme, p, ok, err)
		}
	}
}

// A revoked registry key is refused by the method, like a wrong one.
func TestAPIKeyMethodRefusesRevokedKey(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	secret, issued, err := f.keys.Mint(ctx, MintKey{PrincipalID: storage.OwnerPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	m := NewAPIKeyMethod(f.keys, nil)
	if err := f.keys.Revoke(ctx, issued.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := m.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: secret}); ok || !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("revoked key: %v, %v; want refused", ok, err)
	}
}

// configKeysFor returns ConfigKeys for the SHA-256 of key.
func configKeysFor(t *testing.T, key, principal string, roles []string, expires string) []ConfigKey {
	t.Helper()
	cfg, err := ConfigKeysFrom([]config.AuthKeyConfig{{Hash: "sha256:" + HashSecret(key), Principal: principal, Roles: roles, Expires: expires}})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// A key declared in server.auth.keys authenticates by hash: it acts as
// the registry principal it names, narrowed by its roles, never past
// the principal's, and it has no identity row.
func TestAPIKeyMethodConfigKeys(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	ownerKey, personKey := cfgOwnerKey, cfgPersonKey
	cfg := append(configKeysFor(t, ownerKey, storage.OwnerPrincipal, []string{RoleReader}, ""),
		configKeysFor(t, personKey, f.person.ID, []string{RoleAdmin, RoleReader}, "")...)
	m := NewAPIKeyMethod(f.keys, cfg)

	p, ok, err := m.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: ownerKey})
	if err != nil || !ok {
		t.Fatalf("owner config key: %v, %v", ok, err)
	}
	if p.ID != storage.OwnerPrincipal || p.Kind != storage.PrincipalKindOwner || p.Provider != MethodAPIKey {
		t.Errorf("principal = %+v", p)
	}
	if want, _ := Bundle(RoleReader); !slices.Equal(p.Scopes, want) || !slices.Equal(p.Roles, []string{RoleReader}) {
		t.Errorf("owner key narrowed to reader: scopes %v roles %v", p.Scopes, p.Roles)
	}
	if !strings.HasPrefix(p.Meta[MetaKeyID], "config:") || p.Meta[MetaIdentityID] != "" {
		t.Errorf("meta = %v; want a config key id and no identity", p.Meta)
	}

	p, ok, err = m.Authenticate(ctx, Credential{Scheme: SchemeAPIKey, Token: personKey})
	if err != nil || !ok {
		t.Fatalf("person config key: %v, %v", ok, err)
	}
	if want, _ := Bundle(RoleReader); !slices.Equal(p.Scopes, want) || !slices.Equal(p.Roles, []string{RoleReader}) {
		t.Errorf("a reader's key asking for admin stays reader: scopes %v roles %v", p.Scopes, p.Roles)
	}

}

// The config-declared keys of configKeysTest, shared by the refusal
// cases.
const (
	cfgOwnerKey  = "owner-config-key-0123456789abcdef0123456789"
	cfgPersonKey = "person-config-key-0123456789abcdef0123456789"
)

// A near miss, the hash itself, an expired entry, an unknown or
// inactive principal: each is refused, never passed on to the registry.
func TestAPIKeyMethodConfigKeyRefusals(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	ownerKey, personKey := cfgOwnerKey, cfgPersonKey
	cfg := append(configKeysFor(t, ownerKey, storage.OwnerPrincipal, []string{RoleReader}, ""),
		configKeysFor(t, personKey, f.person.ID, []string{RoleAdmin, RoleReader}, "")...)
	m := NewAPIKeyMethod(f.keys, cfg)
	if _, ok, err := m.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: ownerKey + "x"}); ok || !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("near miss: %v, %v; want refused", ok, err)
	}
	if _, ok, err := m.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: "sha256:" + HashSecret(ownerKey)}); ok || !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("the hash itself: %v, %v; want refused", ok, err)
	}

	expired := NewAPIKeyMethod(f.keys, configKeysFor(t, ownerKey, storage.OwnerPrincipal, nil, f.clock.t.Add(-time.Hour).Format(time.RFC3339)))
	if _, ok, err := expired.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: ownerKey}); ok || !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("expired config key: %v, %v; want refused", ok, err)
	}
	future := NewAPIKeyMethod(f.keys, configKeysFor(t, ownerKey, storage.OwnerPrincipal, nil, f.clock.t.Add(time.Hour).Format(time.RFC3339)))
	if p, ok, err := future.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: ownerKey}); err != nil || !ok {
		t.Errorf("unexpired config key: %v, %v, %v", p, ok, err)
	} else if want, _ := Bundle(RoleAdmin); !slices.Equal(p.Scopes, want) {
		t.Errorf("no roles means the principal's: %v", p.Scopes)
	}

	ghost := NewAPIKeyMethod(f.keys, configKeysFor(t, ownerKey, "prn_nobody", nil, ""))
	if _, ok, err := ghost.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: ownerKey}); ok || !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("unknown principal: %v, %v; want refused", ok, err)
	}
	if err := f.auth.Principals().Suspend(ctx, f.person.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := m.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: personKey}); ok || !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("suspended principal: %v, %v; want refused", ok, err)
	}
	if _, err := ConfigKeysFrom([]config.AuthKeyConfig{{Hash: "plain", Principal: "owner"}}); err == nil {
		t.Error("a hash that is not sha256:<hex> must not parse")
	}
}

// ScopesOf is what a session inherits: the key's scopes for an api_key
// identity, the role scopes for any other, nothing for a key whose
// record is gone.
func TestKeysScopesOf(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	_, issued, err := f.keys.Mint(ctx, MintKey{PrincipalID: f.person.ID, Scopes: []Scope{ScopeReadObjects}})
	if err != nil {
		t.Fatal(err)
	}
	idn, err := f.auth.Identities().Get(ctx, issued.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	p := f.person
	if got, err := f.keys.ScopesOf(ctx, p, idn); err != nil || !slices.Equal(got, []Scope{ScopeReadObjects}) {
		t.Errorf("api_key identity: %v, %v", got, err)
	}
	other := &storage.Identity{PrincipalID: p.ID, Method: storage.IdentityMethodPasskey, Subject: "cred"}
	if err := f.auth.Identities().Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	if got, err := f.keys.ScopesOf(ctx, p, other); err != nil || !slices.Equal(got, ScopesForRoles(p.Roles)) {
		t.Errorf("passkey identity: %v, %v", got, err)
	}
	gone := &storage.Identity{PrincipalID: p.ID, Method: storage.IdentityMethodAPIKey, Subject: "0123456789abcdef"}
	if got, err := f.keys.ScopesOf(ctx, p, gone); err != nil || len(got) != 0 {
		t.Errorf("identity of a vanished key record: %v, %v; want no scopes", got, err)
	}
}

// A protected instance needs an active owner credential to start: a
// registry key that is neither revoked nor expired, or a config key for
// the owner that has not expired. The refusal names the command.
func TestRequireOwnerCredential(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	now := f.clock.t
	err := RequireOwnerCredential(ctx, f.keys, nil, now)
	if err == nil || !strings.Contains(err.Error(), "dpkms auth key create --principal owner") {
		t.Fatalf("fresh registry: %v; want a refusal naming the command", err)
	}
	if _, _, err := f.keys.Mint(ctx, MintKey{PrincipalID: f.person.ID}); err != nil {
		t.Fatal(err)
	}
	if err := RequireOwnerCredential(ctx, f.keys, nil, now); err == nil {
		t.Error("another principal's key is not the owner's")
	}
	_, short, err := f.keys.Mint(ctx, MintKey{PrincipalID: storage.OwnerPrincipal, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireOwnerCredential(ctx, f.keys, nil, now); err != nil {
		t.Errorf("unexpired owner key: %v", err)
	}
	if err := RequireOwnerCredential(ctx, f.keys, nil, now.Add(2*time.Hour)); err == nil {
		t.Error("an expired owner key is no credential")
	}
	_, long, err := f.keys.Mint(ctx, MintKey{PrincipalID: storage.OwnerPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.keys.Revoke(ctx, long.ID); err != nil {
		t.Fatal(err)
	}
	if err := RequireOwnerCredential(ctx, f.keys, nil, now.Add(2*time.Hour)); err == nil {
		t.Error("a revoked owner key is no credential")
	}
	if err := f.keys.Revoke(ctx, short.ID); err != nil {
		t.Fatal(err)
	}
	cfg := configKeysFor(t, "cfg-owner-key-0123456789abcdef0123456789", storage.OwnerPrincipal, nil, "")
	if err := RequireOwnerCredential(ctx, f.keys, cfg, now); err != nil {
		t.Errorf("config key for the owner: %v", err)
	}
	stale := configKeysFor(t, "cfg-owner-key-0123456789abcdef0123456789", storage.OwnerPrincipal, nil, "2020-01-01")
	if err := RequireOwnerCredential(ctx, f.keys, stale, now); err == nil {
		t.Error("an expired config key is no credential")
	}
	person := configKeysFor(t, "cfg-person-key-0123456789abcdef0123456789", f.person.ID, nil, "")
	if err := RequireOwnerCredential(ctx, f.keys, person, now); err == nil {
		t.Error("a config key of another principal is not the owner's")
	}
}
