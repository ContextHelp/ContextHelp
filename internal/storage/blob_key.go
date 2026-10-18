package storage

import (
	"errors"
	"fmt"
)

// ErrInvalidBlobKey is the cross-backend sentinel for a key outside the
// blob key grammar (CheckBlobKey). Every BlobStore method refuses such a
// key, and List such a non-empty prefix, before any I/O.
var ErrInvalidBlobKey = errors.New("invalid blob key")

// CheckBlobKey returns nil when key is a blob key: one or more ASCII
// letters, digits, '-' or '_'. The keys ctxt writes are content hashes
// (lowercase hex). A backend stores a key under its own shard
// directories (<k[:2]>/<k[2:4]>/<k>), so a key holds no separator and no
// dot: neither "../x" nor "..xx" (whose first shard is "..") can name a
// path outside the store, and no key is taken for a ".meta.json"
// sidecar. It returns an error wrapping ErrInvalidBlobKey otherwise.
func CheckBlobKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty", ErrInvalidBlobKey)
	}
	for i := 0; i < len(key); i++ {
		if !blobKeyByte(key[i]) {
			return fmt.Errorf("%w %q: want ASCII letters, digits, '-' or '_'", ErrInvalidBlobKey, key)
		}
	}
	return nil
}

func blobKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}
