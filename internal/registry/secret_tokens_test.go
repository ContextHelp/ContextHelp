package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"hop.top/kit/go/storage/secret"
	"hop.top/kit/go/storage/secret/memory"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
)

// secretsWith returns an in-memory secrets store holding name's token,
// bound to the origin of loginURL, under registry/<name>.
func secretsWith(t *testing.T, name, loginURL, token string) *memory.Store {
	t.Helper()
	origin, err := registry.CanonicalOrigin(loginURL)
	if err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(registry.StoredToken{Origin: origin, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	store := memory.New()
	if err := store.Set(context.Background(), registry.SecretTokenKey(name), value); err != nil {
		t.Fatal(err)
	}
	return store
}

func openStore(s secret.Store) func() (secret.Store, error) {
	return func() (secret.Store, error) { return s, nil }
}

func TestSecretTokens_SendsStoredToken(t *testing.T) {
	srv := protectedRegistry(t, "Authorization", "Bearer "+fakeToken)
	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL,
		Auth:       config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
		TrustLevel: config.RegistryTrustLevelTrusted,
	}
	tokens := registry.SecretTokens(openStore(secretsWith(t, "paid", srv.URL, fakeToken)))

	res, err := syncOne(t, cfg, tokens)
	if err != nil {
		t.Fatalf("Sync with a token in the secrets store: %v", err)
	}
	if res.Upserted != 1 {
		t.Errorf("Upserted = %d, want 1", res.Upserted)
	}
}

// A secret bound to another origin is withheld, as a keychain token is.
func TestSecretTokens_OriginBound(t *testing.T) {
	srv := protectedRegistry(t, "Authorization", "Bearer "+fakeToken)
	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL,
		Auth: config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
	}
	tokens := registry.SecretTokens(openStore(secretsWith(t, "paid", "https://registry.example.com", fakeToken)))
	var warn strings.Builder
	creds := registry.NewCredentials([]config.RegistryConfig{cfg}, tokens).WithWarnings(&warn)

	_, err := registry.New(&stubEntityStore{}).WithCredentials(creds).Sync(context.Background(), cfg)
	var authErr registry.ErrRegistryAuth
	if !errors.As(err, &authErr) || authErr.TokenSent {
		t.Fatalf("Sync = %v, want an unauthenticated ErrRegistryAuth", err)
	}
	if !strings.Contains(warn.String(), "bound to https://registry.example.com") {
		t.Errorf("warning = %q, want the bound origin", warn.String())
	}
	if !strings.Contains(warn.String(), "dpkms secret set registry/paid") {
		t.Errorf("warning = %q, want the dpkms remedy", warn.String())
	}
}

// No secret stored: the request goes out unauthenticated and the 401
// names the dpkms command that stores one, not `ctxt registry login`.
func TestSecretTokens_NoSecret_DpkmsHint(t *testing.T) {
	srv := protectedRegistry(t, "Authorization", "Bearer "+fakeToken)
	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL,
		Auth: config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
	}
	_, err := syncOne(t, cfg, registry.SecretTokens(openStore(memory.New())))
	if err == nil {
		t.Fatal("Sync without a stored secret succeeded against a protected registry")
	}
	if !strings.Contains(err.Error(), "run: dpkms secret set registry/paid") {
		t.Errorf("error %q lacks the dpkms remedy", err)
	}
	if strings.Contains(err.Error(), "ctxt registry login") {
		t.Errorf("error %q names ctxt registry login on a dpkms host", err)
	}
	var none registry.ErrNoToken
	if !errors.As(err, &none) {
		t.Errorf("error %v does not unwrap to ErrNoToken", err)
	}
}

func TestSecretTokens_MalformedSecretIsNoToken(t *testing.T) {
	store := memory.New()
	if err := store.Set(context.Background(), registry.SecretTokenKey("paid"), []byte(fakeToken)); err != nil {
		t.Fatal(err)
	}
	_, err := registry.SecretTokens(openStore(store)).Get("paid")
	var none registry.ErrNoToken
	if !errors.As(err, &none) {
		t.Fatalf("bare (non-JSON) secret: err = %v, want ErrNoToken", err)
	}
}

func TestSecretTokens_OpensStoreOnFirstLookupOnly(t *testing.T) {
	opened := 0
	tokens := registry.SecretTokens(func() (secret.Store, error) {
		opened++
		return secretsWith(t, "paid", "https://paid.example", fakeToken), nil
	})
	// A registry without auth.type never consults the store.
	creds := registry.NewCredentials([]config.RegistryConfig{{Name: "public", URL: "https://public.example"}}, tokens)
	_ = creds.CheckResponse(nil)
	if opened != 0 {
		t.Fatalf("store opened %d times before any lookup", opened)
	}
	for range 2 {
		if tok, err := tokens.Get("paid"); err != nil || tok.Token != fakeToken {
			t.Fatalf("Get = origin %q, %v", tok.Origin, err)
		}
	}
	if opened != 1 {
		t.Errorf("store opened %d times, want 1", opened)
	}
}

func TestSecretTokens_OpenErrorSurfaces(t *testing.T) {
	tokens := registry.SecretTokens(func() (secret.Store, error) { return nil, errors.New("no backend") })
	if _, err := tokens.Get("paid"); err == nil || !strings.Contains(err.Error(), "no backend") {
		t.Fatalf("Get = %v, want the open error", err)
	}
}
