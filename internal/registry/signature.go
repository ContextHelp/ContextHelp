// Package registry — Ed25519 signature verification for registry sync responses.
package registry

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrSignatureRequired is returned when require_signatures is true but the
// registry does not declare a public key.
var ErrSignatureRequired = errors.New("registry signature required but registry declares no public_key")

// ErrSignatureInvalid is returned when a response carries a signature that
// fails Ed25519 verification.
type ErrSignatureInvalid struct {
	RegistryURL    string
	KeyFingerprint string
}

func (e ErrSignatureInvalid) Error() string {
	return fmt.Sprintf("registry %s: signature verification failed (key fingerprint: %s)",
		e.RegistryURL, e.KeyFingerprint)
}

// ErrSignatureMissing is returned when the registry declares a public key but
// the response carries no signature and require_signatures is true.
type ErrSignatureMissing struct {
	RegistryURL string
}

func (e ErrSignatureMissing) Error() string {
	return fmt.Sprintf("registry %s: signature required but no Content-Signature header present", e.RegistryURL)
}

// KeyFingerprint returns the hex-encoded SHA-256 digest of rawPublicKeyHex.
// rawPublicKeyHex is a hex-encoded Ed25519 public key (64 hex chars = 32 bytes).
func KeyFingerprint(rawPublicKeyHex string) (string, error) {
	keyBytes, err := hex.DecodeString(rawPublicKeyHex)
	if err != nil {
		return "", fmt.Errorf("decode public key hex: %w", err)
	}
	sum := sha256.Sum256(keyBytes)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyResponseSignature verifies the Ed25519 signature of body against the
// declared public key.
//
// Signature source priority:
//  1. Content-Signature HTTP response header (base64-encoded detached signature).
//  2. If header is absent and sigBody is non-nil, verifies against sigBody content
//     (used when a .sig sidecar was fetched separately).
//
// Returns nil on success.
// Returns ErrSignatureMissing when no signature is present.
// Returns ErrSignatureInvalid when the signature is present but invalid.
func VerifyResponseSignature(
	registryURL string,
	publicKeyHex string,
	body []byte,
	resp *http.Response,
	sigBody []byte,
) error {
	pubKeyBytes, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return fmt.Errorf("decode public key for %s: %w", registryURL, err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("registry %s: public key must be %d bytes, got %d",
			registryURL, ed25519.PublicKeySize, len(pubKeyBytes))
	}
	pubKey := ed25519.PublicKey(pubKeyBytes)

	fingerprint, _ := KeyFingerprint(publicKeyHex)

	// Prefer Content-Signature header.
	var sigBytes []byte
	if resp != nil {
		if hdr := resp.Header.Get("Content-Signature"); hdr != "" {
			hdr = strings.TrimSpace(hdr)
			sigBytes, err = base64.StdEncoding.DecodeString(hdr)
			if err != nil {
				// Try URL-safe base64 as a fallback.
				sigBytes, err = base64.RawURLEncoding.DecodeString(hdr)
				if err != nil {
					return fmt.Errorf("registry %s: decode Content-Signature header: %w",
						registryURL, err)
				}
			}
		}
	}

	// Fall back to .sig sidecar if provided.
	if len(sigBytes) == 0 && len(sigBody) > 0 {
		trimmed := strings.TrimSpace(string(sigBody))
		sigBytes, err = base64.StdEncoding.DecodeString(trimmed)
		if err != nil {
			sigBytes, err = base64.RawURLEncoding.DecodeString(trimmed)
			if err != nil {
				return fmt.Errorf("registry %s: decode .sig sidecar: %w", registryURL, err)
			}
		}
	}

	if len(sigBytes) == 0 {
		return ErrSignatureMissing{RegistryURL: registryURL}
	}

	if !ed25519.Verify(pubKey, body, sigBytes) {
		return ErrSignatureInvalid{
			RegistryURL:    registryURL,
			KeyFingerprint: fingerprint,
		}
	}

	return nil
}

// FetchSigSidecar fetches <manifestURL>.sig and returns its body.
// Returns (nil, nil) when the sidecar is absent (404).
func FetchSigSidecar(client interface {
	Do(*http.Request) (*http.Response, error)
}, sigURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, sigURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create .sig request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil // network error → treat as absent
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read .sig body: %w", err)
	}
	return data, nil
}
