// Package keys defines the key-encryption-key (KEK) provider port used by
// dpkms storage encryption (SQLite, PostgreSQL, BlobStore, snapshots, spool).
// Callers wrap and unwrap 32-byte data-encryption keys (DEKs) bound to
// associated data (AAD); KEKs never leave the provider.
//
// Fail closed: when a provider is unconfigured, down, or refuses an operation,
// calls return an error wrapping a stable sentinel and never fall back to
// plaintext. Errors never carry key bytes, wrapped bytes, or AAD.
package keys

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// Sentinel errors mapped by callers to stable refusal codes.
var (
	ErrUnconfigured = errors.New("no key provider is configured")
	ErrUnavailable  = errors.New("key provider unavailable")
	ErrUnknownKey   = errors.New("unknown key id")
	ErrUnwrap       = errors.New("cannot unwrap: authentication failed")
)

// Stable error codes for operator diagnostics and API refusals.
const (
	CodeUnconfigured = "keys_unconfigured"
	CodeUnavailable  = "keys_unavailable"
	CodeUnknownKey   = "keys_unknown_key"
	CodeUnwrap       = "keys_unwrap_failed"
)

// MaterialSize is the required byte length of data keys and local KEKs (AES-256).
const MaterialSize = 32

var idRe = regexp.MustCompile(`^[A-Za-z0-9_:\-]{1,64}$`)

// ValidID reports whether id is a valid key identifier.
func ValidID(id string) bool { return idRe.MatchString(id) }

// Code returns the stable error code for err, or "" if err is not a keys error.
func Code(err error) string {
	switch {
	case errors.Is(err, ErrUnconfigured):
		return CodeUnconfigured
	case errors.Is(err, ErrUnknownKey):
		return CodeUnknownKey
	case errors.Is(err, ErrUnwrap):
		return CodeUnwrap
	case errors.Is(err, ErrUnavailable):
		return CodeUnavailable
	default:
		return ""
	}
}

// Provider wraps, unwraps, and rewraps small secret material (DEKs) under
// managed key-encryption keys.
type Provider interface {
	Name() string
	Current(ctx context.Context) (keyID string, err error)
	Wrap(ctx context.Context, keyID string, material, aad []byte) (wrapped []byte, usedID string, err error)
	Unwrap(ctx context.Context, keyID string, wrapped, aad []byte) ([]byte, error)
	Rewrap(ctx context.Context, keyID string, wrapped, aad []byte, toID string) (rewrapped []byte, usedID string, err error)
	Health(ctx context.Context) error
}

func fail(sentinel error, format string, args ...any) error {
	return fmt.Errorf("%w: %s", sentinel, fmt.Sprintf(format, args...))
}

type noneProvider struct{}

// NewNone returns a fail-closed provider used when no key provider is configured.
func NewNone() Provider { return noneProvider{} }

func (noneProvider) Name() string                            { return "none" }
func (noneProvider) Current(context.Context) (string, error) { return "", ErrUnconfigured }
func (noneProvider) Wrap(context.Context, string, []byte, []byte) ([]byte, string, error) {
	return nil, "", ErrUnconfigured
}
func (noneProvider) Unwrap(context.Context, string, []byte, []byte) ([]byte, error) {
	return nil, ErrUnconfigured
}
func (noneProvider) Rewrap(context.Context, string, []byte, []byte, string) ([]byte, string, error) {
	return nil, "", ErrUnconfigured
}
func (noneProvider) Health(context.Context) error { return ErrUnconfigured }

type failedProvider struct{ err error }

// NewFailed returns a provider that fails closed on every call with err.
func NewFailed(err error) Provider {
	if Code(err) == "" {
		err = fail(ErrUnavailable, "%v", err)
	}
	return failedProvider{err: err}
}

func (f failedProvider) Name() string                            { return "failed" }
func (f failedProvider) Current(context.Context) (string, error) { return "", f.err }
func (f failedProvider) Wrap(context.Context, string, []byte, []byte) ([]byte, string, error) {
	return nil, "", f.err
}
func (f failedProvider) Unwrap(context.Context, string, []byte, []byte) ([]byte, error) {
	return nil, f.err
}
func (f failedProvider) Rewrap(context.Context, string, []byte, []byte, string) ([]byte, string, error) {
	return nil, "", f.err
}
func (f failedProvider) Health(context.Context) error { return f.err }
