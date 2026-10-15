package auth

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

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
	if p.ID != storage.OwnerPrincipal || p.Provider != ProviderAPIKey || p.Name == "" || !p.HasRole(RoleAdmin) ||
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

func TestAPIKeyProvider(t *testing.T) {
	f := newKeysFixture(t)
	ctx := context.Background()
	secret, _, err := f.keys.Mint(ctx, MintKey{PrincipalID: storage.OwnerPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	var provider Provider = NewAPIKeyProvider(f.keys)
	if provider.Name() != ProviderAPIKey {
		t.Errorf("Name = %q", provider.Name())
	}
	if _, err := provider.Authenticate(ctx, Credential{}); !errors.Is(err, ErrNoCredential) {
		t.Errorf("empty: %v, want ErrNoCredential", err)
	}
	if _, err := provider.Authenticate(ctx, Credential{Scheme: SchemeSession, Token: secret}); !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("session scheme: %v, want ErrInvalidCredential", err)
	}
	if _, err := provider.Authenticate(ctx, Credential{Scheme: SchemeBearer, Token: "nope"}); !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("bad token: %v, want ErrInvalidCredential", err)
	}
	for _, scheme := range []string{SchemeBearer, SchemeAPIKey} {
		p, err := provider.Authenticate(ctx, Credential{Scheme: scheme, Token: secret})
		if err != nil || p.ID != storage.OwnerPrincipal || p.Provider != ProviderAPIKey {
			t.Errorf("%s: %+v, %v; want the owner", scheme, p, err)
		}
	}
}
