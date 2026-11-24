package keys

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hop.top/kit/go/storage/secret"
)

func TestMemoryAndEnvelopeContract(t *testing.T) {
	ctx := context.Background()
	p, err := NewMemory("k1")
	if err != nil {
		t.Fatalf("NewMemory: %v", err)
	}
	if err := p.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
	aad := []byte("blob:abc123")
	plain := []byte("personal-information-sentinel")

	env, err := SealEnvelope(ctx, p, plain, aad)
	if err != nil {
		t.Fatalf("SealEnvelope: %v", err)
	}
	if bytes.Contains(env, plain) {
		t.Fatal("envelope leaked plaintext")
	}
	if !IsEnvelope(env) {
		t.Fatal("IsEnvelope returned false")
	}
	got, err := OpenEnvelope(ctx, p, env, aad)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("OpenEnvelope = (%q, %v), want %q", got, err, plain)
	}
	// Tampered AAD fails with ErrUnwrap.
	if _, err := OpenEnvelope(ctx, p, env, []byte("blob:other")); !errors.Is(err, ErrUnwrap) {
		t.Fatalf("wrong AAD err = %v, want ErrUnwrap", err)
	}

	// Rotate -> old envelope still opens -> rewrap -> retire k1 -> still opens!
	if _, err := p.Rotate("k2"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	rewrapped, usedID, err := RewrapEnvelope(ctx, p, env, aad, "")
	if err != nil || usedID != "k2" {
		t.Fatalf("RewrapEnvelope = (%q, %v), want k2", usedID, err)
	}
	if err := p.Retire("k1"); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if _, err := OpenEnvelope(ctx, p, env, aad); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("retired key open err = %v, want ErrUnknownKey", err)
	}
	got, err = OpenEnvelope(ctx, p, rewrapped, aad)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("rewrapped open = (%q, %v)", got, err)
	}

	// Down switch fails closed with ErrUnavailable.
	p.SetDown(true)
	if _, err := OpenEnvelope(ctx, p, rewrapped, aad); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("down provider err = %v, want ErrUnavailable", err)
	}
}

func TestLocalFilePermissionsAndEnv(t *testing.T) {
	ctx := context.Background()
	raw := make([]byte, MaterialSize)
	_, _ = rand.Read(raw)
	line := "k1=" + base64.StdEncoding.EncodeToString(raw) + "\n"

	dir := t.TempDir()
	goodPath := filepath.Join(dir, "keys.txt")
	if err := os.WriteFile(goodPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewLocalFromFile(goodPath, "")
	if err != nil {
		t.Fatalf("NewLocalFromFile(0600): %v", err)
	}
	if err := p.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}

	if runtime.GOOS != "windows" {
		badPath := filepath.Join(dir, "world.txt")
		if err := os.WriteFile(badPath, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := NewLocalFromFile(badPath, ""); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected ErrUnavailable for 0644 key file, got %v", err)
		}
	}

	pEnv, err := NewLocalFromEnv(strings.TrimSpace(line), "")
	if err != nil || pEnv.Health(ctx) != nil {
		t.Fatalf("NewLocalFromEnv failed: %v", err)
	}
}

type fakeSecretStore struct {
	vals map[string][]byte
}

func (f fakeSecretStore) Get(_ context.Context, key string) (*secret.Secret, error) {
	v, ok := f.vals[key]
	if !ok {
		return nil, secret.ErrNotFound
	}
	return &secret.Secret{Key: key, Value: v}, nil
}
func (f fakeSecretStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (f fakeSecretStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := f.vals[key]
	return ok, nil
}

func TestFromSecretStore(t *testing.T) {
	ctx := context.Background()
	raw := make([]byte, MaterialSize)
	_, _ = rand.Read(raw)
	st := fakeSecretStore{vals: map[string][]byte{
		"storage_kek": []byte("k1=" + base64.StdEncoding.EncodeToString(raw)),
	}}
	p, err := NewFromSecretStore(ctx, st, "storage_kek", "")
	if err != nil || p.Health(ctx) != nil {
		t.Fatalf("NewFromSecretStore failed: %v", err)
	}
	if _, err := NewFromSecretStore(ctx, st, "missing", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing secret err = %v, want ErrUnavailable", err)
	}
}

type fakeTransitState struct {
	tok     string
	derived bool
	sealed  bool
	ver     int
	k1, k2  []byte
}

func newFakeTransitServer(t *testing.T, st *fakeTransitState) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != st.tok {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if st.sealed {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		st.handle(w, r)
	}))
}

func (st *fakeTransitState) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/transit/keys/ctxt":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"latest_version": st.ver, "derived": st.derived},
		})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/transit/encrypt/ctxt":
		st.handleEncrypt(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/transit/decrypt/ctxt":
		st.handleDecrypt(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/transit/rewrap/ctxt":
		st.handleRewrap(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (st *fakeTransitState) handleEncrypt(w http.ResponseWriter, r *http.Request) {
	var req struct{ Plaintext, Context string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	pt, _ := base64.StdEncoding.DecodeString(req.Plaintext)
	aad, _ := base64.StdEncoding.DecodeString(req.Context)
	k := st.k1
	if st.ver == 2 {
		k = st.k2
	}
	ct, _ := sealAEAD(k, pt, aad)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"ciphertext": "vault:v" + string(rune('0'+st.ver)) + ":" + base64.StdEncoding.EncodeToString(ct)},
	})
}

func (st *fakeTransitState) handleDecrypt(w http.ResponseWriter, r *http.Request) {
	var req struct{ Ciphertext, Context string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	aad, _ := base64.StdEncoding.DecodeString(req.Context)
	parts := strings.SplitN(req.Ciphertext, ":", 3)
	rawCT, _ := base64.StdEncoding.DecodeString(parts[2])
	k := st.k1
	if parts[1] == "v2" {
		k = st.k2
	}
	pt, err := openAEAD(k, rawCT, aad)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"plaintext": base64.StdEncoding.EncodeToString(pt)},
	})
}

func (st *fakeTransitState) handleRewrap(w http.ResponseWriter, r *http.Request) {
	var req struct{ Ciphertext, Context string }
	_ = json.NewDecoder(r.Body).Decode(&req)
	aad, _ := base64.StdEncoding.DecodeString(req.Context)
	parts := strings.SplitN(req.Ciphertext, ":", 3)
	rawCT, _ := base64.StdEncoding.DecodeString(parts[2])
	pt, err := openAEAD(st.k1, rawCT, aad)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	ct, _ := sealAEAD(st.k2, pt, aad)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"ciphertext": "vault:v2:" + base64.StdEncoding.EncodeToString(ct)},
	})
}

func TestTransitProvider(t *testing.T) {
	ctx := context.Background()
	st := &fakeTransitState{
		tok:     "transit-tok-" + "part-two",
		derived: true,
		ver:     1,
		k1:      make([]byte, MaterialSize),
		k2:      make([]byte, MaterialSize),
	}
	_, _ = rand.Read(st.k1)
	_, _ = rand.Read(st.k2)
	srv := newFakeTransitServer(t, st)
	defer srv.Close()

	// Plain HTTP to non-loopback refused.
	if _, err := NewTransit(TransitConfig{Addr: "http://vault.example:8200", KeyName: "ctxt", Token: st.tok}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected non-loopback http refusal, got %v", err)
	}

	p, err := NewTransit(TransitConfig{Addr: srv.URL, KeyName: "ctxt", Token: st.tok})
	if err != nil {
		t.Fatalf("NewTransit: %v", err)
	}
	env, err := SealEnvelope(ctx, p, []byte("secret-doc"), []byte("row:1"))
	if err != nil {
		t.Fatalf("SealEnvelope: %v", err)
	}
	got, err := OpenEnvelope(ctx, p, env, []byte("row:1"))
	if err != nil || string(got) != "secret-doc" {
		t.Fatalf("OpenEnvelope = (%q, %v)", got, err)
	}
	if _, err := OpenEnvelope(ctx, p, env, []byte("row:wrong")); !errors.Is(err, ErrUnwrap) {
		t.Fatalf("wrong AAD err = %v, want ErrUnwrap", err)
	}

	// Rotate & rewrap.
	st.ver = 2
	rw, usedID, err := RewrapEnvelope(ctx, p, env, []byte("row:1"), "")
	if err != nil || usedID != "ctxt:v2" {
		t.Fatalf("RewrapEnvelope = (%q, %v)", usedID, err)
	}
	got, err = OpenEnvelope(ctx, p, rw, []byte("row:1"))
	if err != nil || string(got) != "secret-doc" {
		t.Fatalf("OpenEnvelope after rewrap = (%q, %v)", got, err)
	}

	// Non-derived key refused.
	st.derived = false
	if err := p.Health(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("underived key err = %v, want ErrUnavailable", err)
	}
	st.derived = true

	// Sealed vault fails closed.
	st.sealed = true
	if _, err := OpenEnvelope(ctx, p, rw, []byte("row:1")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("sealed vault err = %v, want ErrUnavailable", err)
	}
}
