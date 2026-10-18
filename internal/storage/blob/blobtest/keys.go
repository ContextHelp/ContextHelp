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
// separated paths, dotted names and bytes no key holds.
var InvalidKeys = []string{
	"", ".", "..", "../x", "../../etc/passwd", "..xx", "....", "ab..cd",
	"ab/cd/abcd", "/abs", `\abs`, `ab\..\cd`, "C:x", "x.meta.json",
	"a b", "a\x00b", "a\nb", "é",
}

// validKeys are keys a backend stores: a content hash and the shapes
// tests and fixtures use.
var validKeys = []string{
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	"sha256-visible-blob", "k_1", "x",
}

// KeyConformance pins the key grammar every backend enforces: each
// method refuses an invalid key with storage.ErrInvalidBlobKey, List
// an invalid non-empty prefix, and the keys ctxt writes round-trip.
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
			if key != "" {
				_, err = store.List(ctx, key)
				refused("List", err)
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
