package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/blobtest"
)

// TestKeyConformance runs the backend-shared key grammar contract.
func TestKeyConformance(t *testing.T) {
	t.Run("no store prefix", func(t *testing.T) {
		_, s := newFakeS3(t, "test-bucket", "")
		blobtest.KeyConformance(t, s)
	})
	t.Run("store prefix", func(t *testing.T) {
		_, s := newFakeS3(t, "test-bucket", "data")
		blobtest.KeyConformance(t, s)
	})
}

// A key naming an object outside the store prefix reads and writes
// nothing there: keyPath cleans "..", so "../other/secret" under prefix
// "data" is the bucket's "other/secret", and "..ab" is "ab/..ab".
func TestKeyOutsidePrefixRefused(t *testing.T) {
	ctx := context.Background()
	f, s := newFakeS3(t, "test-bucket", "data")
	f.put("other/secret", []byte("outside the store prefix"))

	rc, _, err := s.Get(ctx, "../other/secret")
	if rc != nil {
		b, _ := io.ReadAll(rc)
		rc.Close()
		t.Errorf("Get read an object outside the prefix: %q", b)
	}
	if !errors.Is(err, storage.ErrInvalidBlobKey) {
		t.Errorf("Get(../other/secret): err = %v, want ErrInvalidBlobKey", err)
	}
	if err := s.Delete(ctx, "../other/secret"); !errors.Is(err, storage.ErrInvalidBlobKey) {
		t.Errorf("Delete(../other/secret): err = %v, want ErrInvalidBlobKey", err)
	}
	if err := s.Put(ctx, "..ab", bytes.NewReader([]byte("x")), storage.BlobMeta{}); !errors.Is(err, storage.ErrInvalidBlobKey) {
		t.Errorf("Put(..ab): err = %v, want ErrInvalidBlobKey", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.objects["other/secret"]; !ok {
		t.Error("object outside the prefix deleted")
	}
	for key := range f.objects {
		if key != "other/secret" && !strings.HasPrefix(key, "data/") {
			t.Errorf("store wrote outside its prefix: %q", key)
		}
	}
}
