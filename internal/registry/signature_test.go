package registry_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
)

// generateKeyPair returns a fresh Ed25519 key pair as (privateKey, publicKeyHex).
func generateKeyPair(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return priv, hex.EncodeToString(pub)
}

func signBody(priv ed25519.PrivateKey, body []byte) string {
	sig := ed25519.Sign(priv, body)
	return base64.StdEncoding.EncodeToString(sig)
}

func makeRespWithHeader(header, value string) *http.Response {
	h := http.Header{}
	if header != "" {
		h.Set(header, value)
	}
	return &http.Response{Header: h}
}

// TestVerifyResponseSignature_ValidHeader verifies a correct signature in the
// Content-Signature response header.
func TestVerifyResponseSignature_ValidHeader(t *testing.T) {
	priv, pubHex := generateKeyPair(t)
	body := []byte(`{"name":"test-registry","version":"1.0"}`)
	sig := signBody(priv, body)

	resp := makeRespWithHeader("Content-Signature", sig)
	err := registry.VerifyResponseSignature("https://example.com", pubHex, body, resp, nil)
	if err != nil {
		t.Errorf("expected nil error for valid signature, got: %v", err)
	}
}

// TestVerifyResponseSignature_InvalidHeader verifies that a tampered body
// causes ErrSignatureInvalid.
func TestVerifyResponseSignature_InvalidHeader(t *testing.T) {
	priv, pubHex := generateKeyPair(t)
	body := []byte(`{"name":"test-registry","version":"1.0"}`)
	sig := signBody(priv, body)

	tampered := []byte(`{"name":"evil-registry","version":"99.0"}`)
	resp := makeRespWithHeader("Content-Signature", sig)
	err := registry.VerifyResponseSignature("https://example.com", pubHex, tampered, resp, nil)
	if err == nil {
		t.Fatal("expected error for invalid signature, got nil")
	}
	var sigErr registry.ErrSignatureInvalid
	if !isErrSignatureInvalid(err, &sigErr) {
		t.Errorf("expected ErrSignatureInvalid, got %T: %v", err, err)
	}
}

// TestVerifyResponseSignature_MissingSig verifies that a missing signature
// returns ErrSignatureMissing.
func TestVerifyResponseSignature_MissingSig(t *testing.T) {
	_, pubHex := generateKeyPair(t)
	body := []byte(`{"name":"test-registry","version":"1.0"}`)

	resp := makeRespWithHeader("", "")
	err := registry.VerifyResponseSignature("https://example.com", pubHex, body, resp, nil)
	if err == nil {
		t.Fatal("expected ErrSignatureMissing, got nil")
	}
	var missing registry.ErrSignatureMissing
	if !errors.As(err, &missing) {
		t.Errorf("expected ErrSignatureMissing, got %T: %v", err, err)
	}
}

// TestVerifyResponseSignature_ValidSidecar verifies a correct signature from a
// .sig sidecar (header absent).
func TestVerifyResponseSignature_ValidSidecar(t *testing.T) {
	priv, pubHex := generateKeyPair(t)
	body := []byte(`{"name":"test-registry","version":"1.0"}`)
	sig := signBody(priv, body)

	// No header present.
	resp := makeRespWithHeader("", "")
	err := registry.VerifyResponseSignature("https://example.com", pubHex, body, resp, []byte(sig))
	if err != nil {
		t.Errorf("expected nil error for valid sidecar signature, got: %v", err)
	}
}

// TestVerifyResponseSignature_InvalidSidecar verifies that a tampered body
// with a .sig sidecar causes ErrSignatureInvalid.
func TestVerifyResponseSignature_InvalidSidecar(t *testing.T) {
	priv, pubHex := generateKeyPair(t)
	body := []byte(`{"name":"test-registry","version":"1.0"}`)
	sig := signBody(priv, body)

	tampered := []byte(`{"name":"evil-registry","version":"99.0"}`)
	resp := makeRespWithHeader("", "")
	err := registry.VerifyResponseSignature("https://example.com", pubHex, tampered, resp, []byte(sig))
	if err == nil {
		t.Fatal("expected error for invalid sidecar signature, got nil")
	}
	var sigErr registry.ErrSignatureInvalid
	if !isErrSignatureInvalid(err, &sigErr) {
		t.Errorf("expected ErrSignatureInvalid, got %T: %v", err, err)
	}
}

// TestVerifyResponseSignature_WrongPublicKey verifies that a signature valid for
// key A is rejected when verified against key B.
func TestVerifyResponseSignature_WrongPublicKey(t *testing.T) {
	priv, _ := generateKeyPair(t)
	_, otherPubHex := generateKeyPair(t)

	body := []byte(`{"name":"test-registry","version":"1.0"}`)
	sig := signBody(priv, body)

	resp := makeRespWithHeader("Content-Signature", sig)
	err := registry.VerifyResponseSignature("https://example.com", otherPubHex, body, resp, nil)
	if err == nil {
		t.Fatal("expected error for wrong public key, got nil")
	}
}

// TestKeyFingerprint verifies that the fingerprint is stable and has the
// expected length (64 hex chars = 32 bytes SHA-256).
func TestKeyFingerprint(t *testing.T) {
	_, pubHex := generateKeyPair(t)
	fp, err := registry.KeyFingerprint(pubHex)
	if err != nil {
		t.Fatalf("KeyFingerprint: %v", err)
	}
	if len(fp) != 64 {
		t.Errorf("fingerprint length: got %d, want 64", len(fp))
	}
	// Stable.
	fp2, _ := registry.KeyFingerprint(pubHex)
	if fp != fp2 {
		t.Error("fingerprint is not deterministic")
	}
}

// TestFetchSigSidecar_NotFound verifies that a 404 response returns nil, nil.
func TestFetchSigSidecar_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := &http.Client{}
	data, err := registry.FetchSigSidecar(client, srv.URL+"/manifest.sig")
	if err != nil {
		t.Errorf("expected nil error for 404, got: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data for 404, got: %q", data)
	}
}

// TestFetchSigSidecar_Found verifies that a 200 response returns the body.
func TestFetchSigSidecar_Found(t *testing.T) {
	expected := "ABCDEF=="
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(expected))
	}))
	defer srv.Close()

	client := &http.Client{}
	data, err := registry.FetchSigSidecar(client, srv.URL+"/manifest.sig")
	if err != nil {
		t.Fatalf("FetchSigSidecar: %v", err)
	}
	if string(data) != expected {
		t.Errorf("got %q, want %q", data, expected)
	}
}

// isErrSignatureInvalid checks whether err is of type ErrSignatureInvalid.
func isErrSignatureInvalid(err error, out *registry.ErrSignatureInvalid) bool {
	return errors.As(err, out)
}
