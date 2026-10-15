package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// API keys are opaque bearer credentials dpkms issues and checks
// itself: "ctxt_<id>_<secret>", where id is 8 random bytes in hex and
// secret 32 random bytes in base64url. The key store keeps, per id, the
// principal, scopes, expiry, revocation and a hash of the secret, never
// the secret. The hash is SHA-256 over a domain-separated encoding of
// the id and the secret, compared in constant time: a 256-bit random
// secret cannot be guessed at any speed, so a slow KDF would only add
// latency to every request.
//
// KeyIssuer is the method set of hop.top/kit's transport/authn.APIKeys
// (kit v0.5.0-alpha.25 and later) and LocalKeys is its stand-in over
// storage.KVStore, kit's kv.Store shape, until ctxt's kit pin reaches
// that version. The swap is then authn.NewAPIKeys(store.KV(), now) in
// place of NewLocalKeys; only the key prefix and hash domain differ,
// and ctxt is unreleased, so no key needs to survive it.
const (
	// APIKeyPrefix begins every API key, so secret scanners and people
	// recognize one.
	APIKeyPrefix = "ctxt"
	// apiKeyStorePrefix namespaces the records in the kv store.
	apiKeyStorePrefix = "apikey/"
	// hashDomain separates a key hash from any other SHA-256 of the
	// same bytes (a session or ticket hash, say).
	hashDomain        = "ctxt-apikey-v1"
	apiKeyIDBytes     = 8
	apiKeySecretBytes = 32
)

// ErrAPIKeyNotFound is a revoke or lookup of an id the store lacks.
var ErrAPIKeyNotFound = errors.New("auth: no such api key")

// APIKey is one stored key: everything but the secret.
type APIKey struct {
	// ID is the key's public identifier, the middle part of the key,
	// and the subject of its identity row.
	ID string `json:"id"`
	// Principal is the principal the key stands for.
	Principal string `json:"principal"`
	// Scopes are the key's effective scopes, never wider than the
	// principal's.
	Scopes []string `json:"scopes,omitempty"`
	// CreatedAt is when the key was issued.
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt, when set, is when the key stops working.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// RevokedAt, when set, is when the key was revoked.
	RevokedAt time.Time `json:"revoked_at,omitzero"`
	// Hash is the hex SHA-256 of the domain-separated id and secret.
	Hash string `json:"hash"`
}

// NewAPIKey is what KeyIssuer.Create issues a key for.
type NewAPIKey struct {
	Principal string
	Scopes    []string
	// TTL, when positive, sets the key's expiry; zero never expires.
	TTL time.Duration
}

// KeyIssuer issues, lists, revokes and verifies API keys: kit's
// authn.APIKeys method set.
type KeyIssuer interface {
	// Create issues a key and returns it with its record. The key is
	// shown once: only its hash is stored.
	Create(ctx context.Context, spec NewAPIKey) (string, *APIKey, error)
	// Get returns the record with id, or ErrAPIKeyNotFound.
	Get(ctx context.Context, id string) (*APIKey, error)
	// List returns every stored key, newest first.
	List(ctx context.Context) ([]APIKey, error)
	// Revoke marks the key with id revoked; it stops working at once.
	// The record stays. Revoking a revoked key is not an error; an
	// unknown id is ErrAPIKeyNotFound.
	Revoke(ctx context.Context, id string) (*APIKey, error)
	// Verify checks raw, a full API key, and returns its record.
	// Refusals wrap ErrInvalidCredential: a malformed key, an unknown
	// id, a secret that does not match, a revoked or expired key. An
	// empty raw is ErrNoCredential.
	Verify(ctx context.Context, raw string) (*APIKey, error)
}

// LocalKeys is the KeyIssuer over a storage.KVStore.
type LocalKeys struct {
	store storage.KVStore
	now   func() time.Time
}

// NewLocalKeys returns API keys over store. now is the clock; nil
// means time.Now.
func NewLocalKeys(store storage.KVStore, now func() time.Time) *LocalKeys {
	if now == nil {
		now = time.Now
	}
	return &LocalKeys{store: store, now: now}
}

// Create implements KeyIssuer.
func (k *LocalKeys) Create(ctx context.Context, spec NewAPIKey) (string, *APIKey, error) {
	if strings.TrimSpace(spec.Principal) == "" {
		return "", nil, errors.New("auth: an api key needs a principal")
	}
	for _, s := range spec.Scopes {
		if s == "" || strings.ContainsAny(s, " \t\r\n") {
			return "", nil, fmt.Errorf("auth: scope %q: a scope is one non-empty word with no spaces", s)
		}
	}
	idb := make([]byte, apiKeyIDBytes)
	secret := make([]byte, apiKeySecretBytes)
	if _, err := rand.Read(idb); err != nil {
		return "", nil, fmt.Errorf("auth: api key id: %w", err)
	}
	if _, err := rand.Read(secret); err != nil {
		return "", nil, fmt.Errorf("auth: api key secret: %w", err)
	}
	id, sec := hex.EncodeToString(idb), base64.RawURLEncoding.EncodeToString(secret)
	now := k.now().UTC()
	rec := &APIKey{
		ID:        id,
		Principal: spec.Principal,
		Scopes:    append([]string(nil), spec.Scopes...),
		CreatedAt: now,
		Hash:      apiKeyHash(id, sec),
	}
	if spec.TTL > 0 {
		rec.ExpiresAt = now.Add(spec.TTL)
	}
	if err := k.put(ctx, rec); err != nil {
		return "", nil, err
	}
	return APIKeyPrefix + "_" + id + "_" + sec, rec, nil
}

// Get implements KeyIssuer.
func (k *LocalKeys) Get(ctx context.Context, id string) (*APIKey, error) {
	rec, err := k.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("%w: %s", ErrAPIKeyNotFound, id)
	}
	return rec, nil
}

// List implements KeyIssuer.
func (k *LocalKeys) List(ctx context.Context) ([]APIKey, error) {
	keys, err := k.store.List(ctx, apiKeyStorePrefix)
	if err != nil {
		return nil, fmt.Errorf("auth: list api keys: %w", err)
	}
	out := make([]APIKey, 0, len(keys))
	for _, key := range keys {
		rec, err := k.get(ctx, strings.TrimPrefix(key, apiKeyStorePrefix))
		if err != nil {
			return nil, err
		}
		if rec != nil {
			out = append(out, *rec)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Revoke implements KeyIssuer.
func (k *LocalKeys) Revoke(ctx context.Context, id string) (*APIKey, error) {
	rec, err := k.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.RevokedAt.IsZero() {
		rec.RevokedAt = k.now().UTC()
		if err := k.put(ctx, rec); err != nil {
			return nil, err
		}
	}
	return rec, nil
}

// Verify implements KeyIssuer.
func (k *LocalKeys) Verify(ctx context.Context, raw string) (*APIKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrNoCredential
	}
	id, sec, ok := splitAPIKey(raw)
	if !ok {
		return nil, fmt.Errorf("%w: not a %s api key", ErrInvalidCredential, APIKeyPrefix)
	}
	rec, err := k.get(ctx, id)
	if err != nil {
		return nil, err
	}
	want := []byte(apiKeyHash(id, sec))
	if rec == nil {
		// Spend the comparison anyway, so an unknown id and a wrong
		// secret take the same time.
		subtle.ConstantTimeCompare(want, want)
		return nil, fmt.Errorf("%w: unknown api key", ErrInvalidCredential)
	}
	if subtle.ConstantTimeCompare(want, []byte(rec.Hash)) != 1 {
		return nil, fmt.Errorf("%w: unknown api key", ErrInvalidCredential)
	}
	now := k.now()
	if !rec.RevokedAt.IsZero() {
		return nil, fmt.Errorf("%w: api key %s was revoked", ErrInvalidCredential, id)
	}
	if !rec.ExpiresAt.IsZero() && !now.Before(rec.ExpiresAt) {
		return nil, fmt.Errorf("%w: api key %s expired", ErrInvalidCredential, id)
	}
	return rec, nil
}

// splitAPIKey parses "ctxt_<id>_<secret>". The secret is base64url,
// whose alphabet holds "_", so the split takes the first two.
func splitAPIKey(raw string) (id, secret string, ok bool) {
	parts := strings.SplitN(raw, "_", 3)
	if len(parts) != 3 || parts[0] != APIKeyPrefix || !isHex(parts[1], apiKeyIDBytes) {
		return "", "", false
	}
	if b, err := base64.RawURLEncoding.DecodeString(parts[2]); err != nil || len(b) != apiKeySecretBytes {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func (k *LocalKeys) get(ctx context.Context, id string) (*APIKey, error) {
	data, ok, err := k.store.Get(ctx, apiKeyStorePrefix+id)
	if err != nil {
		return nil, fmt.Errorf("auth: api key %s: %w", id, err)
	}
	if !ok {
		return nil, nil
	}
	var rec APIKey
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("auth: api key %s: %w", id, err)
	}
	return &rec, nil
}

func (k *LocalKeys) put(ctx context.Context, rec *APIKey) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("auth: api key %s: %w", rec.ID, err)
	}
	if err := k.store.Put(ctx, apiKeyStorePrefix+rec.ID, data); err != nil {
		return fmt.Errorf("auth: api key %s: %w", rec.ID, err)
	}
	return nil
}

// apiKeyHash is the stored hash of a key's secret.
func apiKeyHash(id, secret string) string {
	h := sha256.New()
	h.Write([]byte(hashDomain))
	h.Write([]byte{0})
	h.Write([]byte(id))
	h.Write([]byte{0})
	h.Write([]byte(secret))
	return hex.EncodeToString(h.Sum(nil))
}

// isHex reports whether s is n bytes of lowercase hex.
func isHex(s string, n int) bool {
	if len(s) != 2*n {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
