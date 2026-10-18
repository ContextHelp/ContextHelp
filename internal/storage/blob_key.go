package storage

import (
	"errors"
	"fmt"
)

// ErrInvalidBlobKey is the cross-backend sentinel for a key outside the
// blob key grammar (CheckBlobKey). Every BlobStore method refuses such a
// key, and List a prefix outside CheckBlobKeyPrefix, before any I/O.
var ErrInvalidBlobKey = errors.New("invalid blob key")

// BlobKeyLen is the length of every blob key: a sha256, hex-encoded.
const BlobKeyLen = 64

// CheckBlobKey returns nil when key is a blob key: exactly BlobKeyLen
// lowercase hex digits, the content hash every blob is stored under
// (storageutil.ContentHash). A backend stores a key under its own shard
// directories (<k[:2]>/<k[2:4]>/<k>), so a key that is a path ("../x"),
// that makes a path of its shards ("..xx"), that a case-insensitive
// filesystem folds onto another key, that a platform reserves (nul,
// con) or that names a ".meta.json" sidecar is not one. It returns an
// error wrapping ErrInvalidBlobKey otherwise.
func CheckBlobKey(key string) error {
	if len(key) != BlobKeyLen || !lowerHex(key) {
		return fmt.Errorf("%w %q: want %d lowercase hex digits (a sha256)", ErrInvalidBlobKey, key, BlobKeyLen)
	}
	return nil
}

// CheckBlobKeyPrefix returns nil when prefix can start a blob key: 1 to
// BlobKeyLen lowercase hex digits. List takes such a prefix, or none.
func CheckBlobKeyPrefix(prefix string) error {
	if len(prefix) == 0 || len(prefix) > BlobKeyLen || !lowerHex(prefix) {
		return fmt.Errorf("%w prefix %q: want 1 to %d lowercase hex digits", ErrInvalidBlobKey, prefix, BlobKeyLen)
	}
	return nil
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
