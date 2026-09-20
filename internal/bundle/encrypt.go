// Package bundle — AES-256-GCM encrypted bundle support.
//
// Wire format (all fields big-endian):
//
//	[0]       version byte  (0x01)
//	[1..4]    argon2 time   (uint32)
//	[5..8]    argon2 memory (uint32, KiB)
//	[9]       argon2 threads (uint8)
//	[10..41]  salt (32 bytes)
//	[42..53]  nonce (12 bytes)
//	[54..]    ciphertext (GCM-sealed zip bytes)
package bundle

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/argon2"
)

const (
	// EncryptedMagicVersion is the version byte written at offset 0.
	EncryptedMagicVersion = 0x01

	argon2SaltLen  = 32
	argon2NonceLen = 12
	argon2KeyLen   = 32

	// Default Argon2id parameters (OWASP minimum for interactive use).
	defaultArgon2Time    = uint32(3)
	defaultArgon2Memory  = uint32(64 * 1024) // 64 MiB
	defaultArgon2Threads = uint8(4)

	// headerSize is the fixed-length prefix before the ciphertext.
	// 1 (version) + 4 (time) + 4 (memory) + 1 (threads) + 32 (salt) + 12 (nonce)
	headerSize = 1 + 4 + 4 + 1 + argon2SaltLen + argon2NonceLen
)

// EncryptedHeader holds the parsed header of an encrypted bundle.
type EncryptedHeader struct {
	Version uint8
	Time    uint32
	Memory  uint32
	Threads uint8
	Salt    [argon2SaltLen]byte
	Nonce   [argon2NonceLen]byte
}

// IsEncryptedBundle returns true if data begins with EncryptedMagicVersion.
// Used by restore to auto-detect encrypted bundles without reading the whole file.
func IsEncryptedBundle(data []byte) bool {
	return len(data) > headerSize && data[0] == EncryptedMagicVersion
}

// EncryptBundle seals plaintext (zip bytes) with AES-256-GCM derived from passphrase.
// Returns the self-describing encrypted blob.
func EncryptBundle(plaintext []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("bundle: encrypt: passphrase must not be empty")
	}

	var salt [argon2SaltLen]byte
	if _, err := io.ReadFull(rand.Reader, salt[:]); err != nil {
		return nil, fmt.Errorf("bundle: encrypt: generate salt: %w", err)
	}

	key := deriveKey([]byte(passphrase), salt[:], defaultArgon2Time, defaultArgon2Memory, defaultArgon2Threads)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("bundle: encrypt: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("bundle: encrypt: new gcm: %w", err)
	}

	var nonce [argon2NonceLen]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("bundle: encrypt: generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce[:], plaintext, nil)

	out := make([]byte, headerSize+len(ciphertext))
	off := 0
	out[off] = EncryptedMagicVersion
	off++
	binary.BigEndian.PutUint32(out[off:], defaultArgon2Time)
	off += 4
	binary.BigEndian.PutUint32(out[off:], defaultArgon2Memory)
	off += 4
	out[off] = defaultArgon2Threads
	off++
	copy(out[off:], salt[:])
	off += argon2SaltLen
	copy(out[off:], nonce[:])
	off += argon2NonceLen
	copy(out[off:], ciphertext)

	return out, nil
}

// DecryptBundle decrypts an encrypted bundle blob using passphrase.
// Returns the plaintext (zip bytes).
func DecryptBundle(blob []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("bundle: decrypt: passphrase must not be empty")
	}
	if len(blob) <= headerSize {
		return nil, fmt.Errorf("bundle: decrypt: blob too short (%d bytes)", len(blob))
	}
	if blob[0] != EncryptedMagicVersion {
		return nil, fmt.Errorf("bundle: decrypt: unknown version byte 0x%02x", blob[0])
	}

	off := 1
	t := binary.BigEndian.Uint32(blob[off:])
	off += 4
	m := binary.BigEndian.Uint32(blob[off:])
	off += 4
	p := blob[off]
	off++

	var salt [argon2SaltLen]byte
	copy(salt[:], blob[off:off+argon2SaltLen])
	off += argon2SaltLen

	var nonce [argon2NonceLen]byte
	copy(nonce[:], blob[off:off+argon2NonceLen])
	off += argon2NonceLen

	ciphertext := blob[off:]

	key := deriveKey([]byte(passphrase), salt[:], t, m, p)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("bundle: decrypt: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("bundle: decrypt: new gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce[:], ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("bundle: decrypt: open gcm (wrong passphrase?): %w", err)
	}
	return plaintext, nil
}

// ParseEncryptedHeader reads the self-describing header without decrypting.
func ParseEncryptedHeader(blob []byte) (EncryptedHeader, error) {
	if len(blob) < headerSize {
		return EncryptedHeader{}, fmt.Errorf("bundle: parse header: blob too short")
	}
	var h EncryptedHeader
	off := 0
	h.Version = blob[off]
	off++
	h.Time = binary.BigEndian.Uint32(blob[off:])
	off += 4
	h.Memory = binary.BigEndian.Uint32(blob[off:])
	off += 4
	h.Threads = blob[off]
	off++
	copy(h.Salt[:], blob[off:off+argon2SaltLen])
	off += argon2SaltLen
	copy(h.Nonce[:], blob[off:off+argon2NonceLen])
	return h, nil
}

// EncryptBundleFile reads the zip at path, encrypts its bytes with AES-256-GCM,
// and writes the encrypted blob back to the same path (replacing the plaintext zip).
func EncryptBundleFile(path string, passphrase string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("bundle: encrypt file: read %s: %w", path, err)
	}
	blob, err := EncryptBundle(data, passphrase)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, blob, 0600); err != nil {
		return fmt.Errorf("bundle: encrypt file: write %s: %w", path, err)
	}
	return nil
}

// deriveKey uses Argon2id to derive a 32-byte AES key from passphrase + salt.
func deriveKey(pass, salt []byte, t, m uint32, p uint8) []byte {
	return argon2.IDKey(pass, salt, t, m, p, argon2KeyLen)
}
