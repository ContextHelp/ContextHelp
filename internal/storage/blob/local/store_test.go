package local

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return s
}

func TestPutAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte("hello blob world")
	meta := storage.BlobMeta{
		ContentType: "text/plain",
		Size:        int64(len(data)),
		ContentHash: "abc123def456",
		Filename:    "test.txt",
	}

	err := s.Put(ctx, "abc123def456", bytes.NewReader(data), meta)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, gotMeta, err := s.Get(ctx, "abc123def456")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data: got %q, want %q", got, data)
	}
	if gotMeta.ContentType != "text/plain" {
		t.Errorf("content_type: got %q, want %q", gotMeta.ContentType, "text/plain")
	}
	if gotMeta.Filename != "test.txt" {
		t.Errorf("filename: got %q, want %q", gotMeta.Filename, "test.txt")
	}
}

func TestExists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ok, err := s.Exists(ctx, "missing")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if ok {
		t.Error("expected false for missing key")
	}

	data := []byte("test")
	_ = s.Put(ctx, "present", bytes.NewReader(data), storage.BlobMeta{Size: int64(len(data))})

	ok, err = s.Exists(ctx, "present")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !ok {
		t.Error("expected true for existing key")
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte("delete me")
	_ = s.Put(ctx, "todelete", bytes.NewReader(data), storage.BlobMeta{Size: int64(len(data))})

	err := s.Delete(ctx, "todelete")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	ok, _ := s.Exists(ctx, "todelete")
	if ok {
		t.Error("expected false after delete")
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	keys := []string{"aabb11", "aabb22", "ccdd33"}
	for _, k := range keys {
		_ = s.Put(ctx, k, bytes.NewReader([]byte("x")), storage.BlobMeta{Size: 1})
	}

	items, err := s.List(ctx, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte("url test")
	_ = s.Put(ctx, "urlkey1", bytes.NewReader(data), storage.BlobMeta{Size: int64(len(data))})

	u, err := s.URL(ctx, "urlkey1")
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if u == "" {
		t.Error("expected non-empty URL")
	}
	// Should be a file:// URL.
	if len(u) < 7 || u[:7] != "file://" {
		t.Errorf("expected file:// URL, got %q", u)
	}
}

func TestPutIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data := []byte("idempotent")
	meta := storage.BlobMeta{Size: int64(len(data)), ContentType: "text/plain"}

	_ = s.Put(ctx, "idem", bytes.NewReader(data), meta)
	err := s.Put(ctx, "idem", bytes.NewReader(data), meta)
	if err != nil {
		t.Fatalf("second put: %v", err)
	}
}

func TestKeySharding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key := "abcdef1234567890"

	_ = s.Put(ctx, key, bytes.NewReader([]byte("x")), storage.BlobMeta{Size: 1})

	// Verify 2-level sharding: root/ab/cd/abcdef1234567890
	expected := s.blobPath(key)
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected sharded path %q to exist: %v", expected, err)
	}
}

var _ storage.BlobStore = (*Store)(nil)
