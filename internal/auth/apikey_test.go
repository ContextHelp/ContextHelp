package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// keyClock is a settable clock for the key tests.
type keyClock struct{ t time.Time }

func (c *keyClock) now() time.Time          { return c.t }
func (c *keyClock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newKeyClock() *keyClock                { return &keyClock{t: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)} }

// keyFixture is a LocalKeys issuer over a fresh SQLite registry with a
// controllable clock.
type keyFixture struct {
	auth  storage.AuthStore
	keys  *LocalKeys
	clock *keyClock
}

func newKeyFixture(t *testing.T) *keyFixture {
	t.Helper()
	c := newKeyClock()
	auth := storageutil.NewTestDriver(t).Auth()
	return &keyFixture{auth: auth, keys: NewLocalKeys(auth.KV(), c.now), clock: c}
}

func TestLocalKeysCreateFormat(t *testing.T) {
	f := newKeyFixture(t)
	ctx := context.Background()
	secret, rec, err := f.keys.Create(ctx, NewAPIKey{Principal: "owner", Scopes: []string{"read:objects"}, TTL: time.Hour})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	parts := strings.SplitN(secret, "_", 3)
	if len(parts) != 3 || parts[0] != APIKeyPrefix || parts[1] != rec.ID || !isHex(rec.ID, apiKeyIDBytes) {
		t.Fatalf("key %q: want %s_<16 hex>_<secret>", secret, APIKeyPrefix)
	}
	if raw, err := base64.RawURLEncoding.DecodeString(parts[2]); err != nil || len(raw) != apiKeySecretBytes {
		t.Errorf("secret part %q: %d bytes, %v; want 32 random bytes base64url", parts[2], len(raw), err)
	}
	if rec.Principal != "owner" || rec.Scopes[0] != "read:objects" || !rec.CreatedAt.Equal(f.clock.t) ||
		!rec.ExpiresAt.Equal(f.clock.t.Add(time.Hour)) || !rec.RevokedAt.IsZero() {
		t.Errorf("record = %+v", rec)
	}
	if rec.Hash == "" || strings.Contains(rec.Hash, parts[2]) || rec.Hash != apiKeyHash(rec.ID, parts[2]) {
		t.Errorf("hash %q does not bind id and secret, or leaks the secret", rec.Hash)
	}
}

func TestLocalKeysStoresHashOnly(t *testing.T) {
	f := newKeyFixture(t)
	ctx := context.Background()
	secret, rec, err := f.keys.Create(ctx, NewAPIKey{Principal: "owner"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, sec, _ := splitAPIKey(secret)
	stored, ok, err := f.auth.KV().Get(ctx, apiKeyStorePrefix+rec.ID)
	if err != nil || !ok {
		t.Fatalf("record not stored: %v %v", ok, err)
	}
	if strings.Contains(string(stored), sec) {
		t.Error("the stored record carries the secret")
	}
	got, err := f.keys.Get(ctx, rec.ID)
	if err != nil || got.Hash != rec.Hash {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if _, err := f.keys.Get(ctx, "0000000000000000"); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Errorf("Get(unknown): %v, want ErrAPIKeyNotFound", err)
	}
}

func TestLocalKeysCreateRefusals(t *testing.T) {
	f := newKeyFixture(t)
	ctx := context.Background()
	if _, _, err := f.keys.Create(ctx, NewAPIKey{}); err == nil {
		t.Error("no principal: created")
	}
	if _, _, err := f.keys.Create(ctx, NewAPIKey{Principal: "owner", Scopes: []string{"read objects"}}); err == nil {
		t.Error("scope with a space: created")
	}
}

func TestLocalKeysVerify(t *testing.T) {
	f := newKeyFixture(t)
	ctx := context.Background()
	secret, rec, err := f.keys.Create(ctx, NewAPIKey{Principal: "owner", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.keys.Verify(ctx, " "+secret+"\n")
	if err != nil || got.ID != rec.ID {
		t.Fatalf("Verify = %+v, %v; want the record", got, err)
	}

	id, sec, _ := splitAPIKey(secret)
	wrong := base64.RawURLEncoding.EncodeToString(make([]byte, apiKeySecretBytes))
	for name, raw := range map[string]string{
		"empty secret part": APIKeyPrefix + "_" + id + "_",
		"wrong secret":      APIKeyPrefix + "_" + id + "_" + wrong,
		"unknown id":        APIKeyPrefix + "_0123456789abcdef_" + sec,
		"other prefix":      "kit_" + id + "_" + sec,
		"short id":          APIKeyPrefix + "_abc_" + sec,
		"static token":      "tok-ops-fixture-0123456789abcdef0123456789",
		"no separators":     strings.ReplaceAll(secret, "_", ""),
	} {
		if _, err := f.keys.Verify(ctx, raw); !errors.Is(err, ErrInvalidCredential) {
			t.Errorf("%s: %v, want ErrInvalidCredential", name, err)
		}
	}
	if _, err := f.keys.Verify(ctx, ""); !errors.Is(err, ErrNoCredential) {
		t.Errorf("empty: %v, want ErrNoCredential", err)
	}

	f.clock.advance(time.Hour)
	if _, err := f.keys.Verify(ctx, secret); !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("at expiry: %v, want ErrInvalidCredential", err)
	}
}

func TestLocalKeysRevokeAndList(t *testing.T) {
	f := newKeyFixture(t)
	ctx := context.Background()
	first, a, err := f.keys.Create(ctx, NewAPIKey{Principal: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(time.Minute)
	_, b, err := f.keys.Create(ctx, NewAPIKey{Principal: "prn_x"})
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(time.Minute)
	rec, err := f.keys.Revoke(ctx, a.ID)
	if err != nil || !rec.RevokedAt.Equal(f.clock.t) {
		t.Fatalf("Revoke = %+v, %v", rec, err)
	}
	if _, err := f.keys.Verify(ctx, first); !errors.Is(err, ErrInvalidCredential) {
		t.Errorf("revoked key verifies: %v", err)
	}
	f.clock.advance(time.Minute)
	again, err := f.keys.Revoke(ctx, a.ID)
	if err != nil || !again.RevokedAt.Equal(rec.RevokedAt) {
		t.Errorf("second Revoke = %+v, %v; want the first revocation kept", again, err)
	}
	if _, err := f.keys.Revoke(ctx, "0000000000000000"); !errors.Is(err, ErrAPIKeyNotFound) {
		t.Errorf("Revoke(unknown): %v, want ErrAPIKeyNotFound", err)
	}
	list, err := f.keys.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID != b.ID || list[1].ID != a.ID {
		t.Errorf("List = %+v, %v; want newest first", list, err)
	}
}
