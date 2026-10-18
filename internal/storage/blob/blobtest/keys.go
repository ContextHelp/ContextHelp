package blobtest

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// InvalidKeys are keys outside storage.CheckBlobKey's grammar: paths up
// and out (directly, or through the <k[:2]> shard), absolute and
// separated paths, dotted names, bytes no key holds, reserved device
// names, hex of the wrong length or case, and the free-form shapes
// fixtures once used.
var InvalidKeys = append([]string{
	"", ".", "..", "../x", "../../etc/passwd", "..xx", "....", "ab..cd",
	"ab/cd/abcd", "/abs", `\abs`, `ab\..\cd`, "C:x", "x.meta.json",
	"a b", "a\x00b", "a\nb", "é", "nul", "con", "abc123", "sha256-visible-blob", "k_1",
	HexKey("ab")[:storage.BlobKeyLen-1], HexKey("ab") + "0", strings.ToUpper(HexKey("ab")),
	"../" + HexKey("ab")[3:],
}, invalidPrefixes...)

// invalidPrefixes are List prefixes outside storage.CheckBlobKeyPrefix:
// nothing that is not lowercase hex, and nothing longer than a key.
var invalidPrefixes = []string{
	"..", "../x", "ab/cd", "/abs", `\abs`, "x.meta.json", "nul", "AB", "g",
	"a b", "é", HexKey("ab") + "0",
}

// validKeys are keys a backend stores: content hashes.
var validKeys = []string{
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	KeyOf("valid"), HexKey("ab"), HexKey(""),
}

// KeyConformance pins the key grammar every backend enforces: each
// method refuses an invalid key with storage.ErrInvalidBlobKey, List
// an invalid prefix, and valid keys round-trip.
func KeyConformance(t *testing.T, store storage.BlobStore) {
	t.Helper()
	ctx := context.Background()
	for _, key := range InvalidKeys {
		t.Run("invalid="+key, func(t *testing.T) {
			refused := func(op string, err error) {
				t.Helper()
				if !errors.Is(err, storage.ErrInvalidBlobKey) {
					t.Errorf("%s(%q): err = %v, want ErrInvalidBlobKey", op, key, err)
				}
			}
			refused("Put", store.Put(ctx, key, strings.NewReader("x"), storage.BlobMeta{}))
			rc, _, err := store.Get(ctx, key)
			if rc != nil {
				rc.Close()
			}
			refused("Get", err)
			refused("Delete", store.Delete(ctx, key))
			_, err = store.Exists(ctx, key)
			refused("Exists", err)
			_, err = store.URL(ctx, key)
			refused("URL", err)
		})
	}
	for _, prefix := range invalidPrefixes {
		t.Run("invalid prefix="+prefix, func(t *testing.T) {
			if _, err := store.List(ctx, prefix); !errors.Is(err, storage.ErrInvalidBlobKey) {
				t.Errorf("List(%q): err = %v, want ErrInvalidBlobKey", prefix, err)
			}
		})
	}
	for _, key := range validKeys {
		t.Run("valid="+key, func(t *testing.T) {
			if err := store.Put(ctx, key, strings.NewReader("content of "+key), storage.BlobMeta{}); err != nil {
				t.Fatalf("Put(%q): %v", key, err)
			}
			rc, _, err := store.Get(ctx, key)
			if err != nil {
				t.Fatalf("Get(%q): %v", key, err)
			}
			defer rc.Close()
			if b, _ := io.ReadAll(rc); string(b) != "content of "+key {
				t.Errorf("Get(%q) = %q", key, b)
			}
		})
	}
}
