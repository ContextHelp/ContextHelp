// Package blobtest holds the contract every storage.BlobStore backend
// runs in its own tests, so the backends answer alike.
package blobtest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// listKeys are the blobs ListConformance puts: long keys share their
// first two and four characters (the backends shard keys into
// <k[:2]>/<k[2:4]>/<k> paths), and short keys are stored unsharded.
var listKeys = []string{"abcdef01", "abcdff02", "abzz03", "ef0123", "ab9", "x"}

// ListConformance pins List(prefix) on a store holding nothing but the
// blobs it puts: every key with that prefix, matched against the key
// (never the shard directories a backend stores it under), and no
// metadata entries. An empty prefix lists every blob. Each item carries
// the blob's size and a modification time.
func ListConformance(t *testing.T, store storage.BlobStore) {
	t.Helper()
	ctx := context.Background()
	for _, key := range listKeys {
		if err := store.Put(ctx, key, strings.NewReader("content of "+key), storage.BlobMeta{ContentType: "text/plain"}); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	for _, tc := range []struct {
		prefix string
		want   []string
	}{
		{"", []string{"ab9", "abcdef01", "abcdff02", "abzz03", "ef0123", "x"}},
		{"abcdef01", []string{"abcdef01"}},
		{"abcdef", []string{"abcdef01"}},
		{"abcd", []string{"abcdef01", "abcdff02"}},
		{"abc", []string{"abcdef01", "abcdff02"}},
		{"ab", []string{"ab9", "abcdef01", "abcdff02", "abzz03"}},
		{"a", []string{"ab9", "abcdef01", "abcdff02", "abzz03"}},
		{"ef0123", []string{"ef0123"}},
		{"x", []string{"x"}},
		// cd and 01 name shard directories and key suffixes, not keys.
		{"cd", nil},
		{"01", nil},
		{"abcdef012", nil},
		{"zz", nil},
	} {
		t.Run("prefix="+tc.prefix, func(t *testing.T) {
			items, err := store.List(ctx, tc.prefix)
			if err != nil {
				t.Fatalf("List(%q): %v", tc.prefix, err)
			}
			var got []string
			for _, item := range items {
				got = append(got, item.Key)
				if want := int64(len("content of " + item.Key)); item.Size != want {
					t.Errorf("List(%q): %s size = %d, want %d", tc.prefix, item.Key, item.Size, want)
				}
				if item.UpdatedAt.IsZero() {
					t.Errorf("List(%q): %s has no modification time", tc.prefix, item.Key)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("List(%q) = %v, want %v", tc.prefix, got, tc.want)
			}
		})
	}
}
