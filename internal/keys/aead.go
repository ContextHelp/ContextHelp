package keys

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	gcmNonceSize = 12
	// EnvelopeVersion is the wire format version for SealEnvelope/OpenEnvelope.
	EnvelopeVersion = 0x01
)

// EnvelopeHeader holds the unencrypted metadata prefix of a sealed envelope.
type EnvelopeHeader struct {
	Version    byte
	KeyID      string
	WrappedDEK []byte
}

func sealAEAD(key, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fail(ErrUnavailable, "cipher init")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fail(ErrUnavailable, "gcm init")
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fail(ErrUnavailable, "random nonce")
	}
	out := make([]byte, gcmNonceSize, gcmNonceSize+len(plaintext)+gcm.Overhead())
	copy(out, nonce)
	return gcm.Seal(out, nonce, plaintext, aad), nil
}

func openAEAD(key, ciphertext, aad []byte) ([]byte, error) {
	if len(ciphertext) < gcmNonceSize {
		return nil, ErrUnwrap
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fail(ErrUnavailable, "cipher init")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fail(ErrUnavailable, "gcm init")
	}
	pt, err := gcm.Open(nil, ciphertext[:gcmNonceSize], ciphertext[gcmNonceSize:], aad)
	if err != nil {
		return nil, ErrUnwrap
	}
	return pt, nil
}

// SealEnvelope encrypts plaintext with a fresh random 32-byte DEK wrapped by p
// under its current KEK, binding both DEK wrap and payload encryption to aad.
//
// Wire format (big-endian):
//
//	[0]       version (0x01)
//	[1..2]    keyID len (uint16)
//	[..]      keyID bytes
//	[..+2]    wrappedDEK len (uint16)
//	[..]      wrappedDEK bytes
//	[..]      nonce (12B) || AES-256-GCM ciphertext
func SealEnvelope(ctx context.Context, p Provider, plaintext, aad []byte) ([]byte, error) {
	if p == nil {
		return nil, ErrUnconfigured
	}
	dek := make([]byte, MaterialSize)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fail(ErrUnavailable, "generate dek")
	}
	defer zeroBytes(dek)

	wrappedDEK, keyID, err := p.Wrap(ctx, "", dek, aad)
	if err != nil {
		return nil, err
	}
	ct, err := sealAEAD(dek, plaintext, aad)
	if err != nil {
		return nil, err
	}
	return encodeEnvelope(keyID, wrappedDEK, ct)
}

// OpenEnvelope unwraps the DEK from envelope via p and decrypts the payload.
func OpenEnvelope(ctx context.Context, p Provider, envelope, aad []byte) ([]byte, error) {
	if p == nil {
		return nil, ErrUnconfigured
	}
	hdr, ct, err := parseEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	dek, err := p.Unwrap(ctx, hdr.KeyID, hdr.WrappedDEK, aad)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(dek)
	return openAEAD(dek, ct, aad)
}

// RewrapEnvelope rotates the wrapped DEK inside envelope to toID ("" = current)
// without decrypting the payload.
func RewrapEnvelope(ctx context.Context, p Provider, envelope, aad []byte, toID string) ([]byte, string, error) {
	if p == nil {
		return nil, "", ErrUnconfigured
	}
	hdr, ct, err := parseEnvelope(envelope)
	if err != nil {
		return nil, "", err
	}
	newWrapped, usedID, err := p.Rewrap(ctx, hdr.KeyID, hdr.WrappedDEK, aad, toID)
	if err != nil {
		return nil, "", err
	}
	out, err := encodeEnvelope(usedID, newWrapped, ct)
	if err != nil {
		return nil, "", err
	}
	return out, usedID, nil
}

// InspectEnvelope reads the unencrypted header of envelope without decrypting.
func InspectEnvelope(envelope []byte) (EnvelopeHeader, error) {
	hdr, _, err := parseEnvelope(envelope)
	return hdr, err
}

// IsEnvelope reports whether data starts with a well-formed envelope header.
func IsEnvelope(data []byte) bool {
	_, _, err := parseEnvelope(data)
	return err == nil
}

func encodeEnvelope(keyID string, wrappedDEK, ct []byte) ([]byte, error) {
	if len(keyID) == 0 || len(keyID) > 0xffff {
		return nil, errors.New("keys: invalid envelope key id length")
	}
	if len(wrappedDEK) == 0 || len(wrappedDEK) > 0xffff {
		return nil, errors.New("keys: invalid wrapped dek length")
	}
	out := make([]byte, 1+2+len(keyID)+2+len(wrappedDEK)+len(ct))
	off := 0
	out[off] = EnvelopeVersion
	off++
	// #nosec G115 -- keyID length is verified <= 0xffff above.
	binary.BigEndian.PutUint16(out[off:], uint16(len(keyID))) //nolint:gosec // length checked <= 0xffff above
	off += 2
	copy(out[off:], keyID)
	off += len(keyID)
	// #nosec G115 -- wrappedDEK length is verified <= 0xffff above.
	binary.BigEndian.PutUint16(out[off:], uint16(len(wrappedDEK))) //nolint:gosec // length checked <= 0xffff above
	off += 2
	copy(out[off:], wrappedDEK)
	off += len(wrappedDEK)
	copy(out[off:], ct)
	return out, nil
}

func parseEnvelope(data []byte) (EnvelopeHeader, []byte, error) {
	if len(data) < 1+2+2+gcmNonceSize {
		return EnvelopeHeader{}, nil, fmt.Errorf("%w: envelope truncated", ErrUnwrap)
	}
	if data[0] != EnvelopeVersion {
		return EnvelopeHeader{}, nil, fmt.Errorf("%w: unsupported envelope version", ErrUnwrap)
	}
	off := 1
	idLen := int(binary.BigEndian.Uint16(data[off:]))
	off += 2
	if idLen == 0 || off+idLen+2 > len(data) {
		return EnvelopeHeader{}, nil, fmt.Errorf("%w: malformed key id", ErrUnwrap)
	}
	keyID := string(data[off : off+idLen])
	off += idLen
	dekLen := int(binary.BigEndian.Uint16(data[off:]))
	off += 2
	if dekLen == 0 || off+dekLen+gcmNonceSize > len(data) {
		return EnvelopeHeader{}, nil, fmt.Errorf("%w: malformed wrapped dek", ErrUnwrap)
	}
	wrappedDEK := append([]byte(nil), data[off:off+dekLen]...)
	off += dekLen
	return EnvelopeHeader{Version: EnvelopeVersion, KeyID: keyID, WrappedDEK: wrappedDEK}, data[off:], nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
