package blobtest

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// KeyOf returns a blob key derived from name (its sha256), so a fixture
// can name its blobs readably and still store them under valid keys.
func KeyOf(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// HexKey returns prefix, lowercase hex, padded with zeros to a full blob
// key: for fixtures whose keys must share a prefix (List, sharding).
func HexKey(prefix string) string {
	return prefix + strings.Repeat("0", storage.BlobKeyLen-len(prefix))
}
