package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/blobtest"
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

	err := s.Put(ctx, blobtest.KeyOf("abc123def456"), bytes.NewReader(data), meta)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, gotMeta, err := s.Get(ctx, blobtest.KeyOf("abc123def456"))
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

	ok, err := s.Exists(ctx, blobtest.KeyOf("missing"))
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if ok {
		t.Error("expected false for missing key")
	}

	data := []byte("test")
	_ = s.Put(ctx, blobtest.KeyOf("present"), bytes.NewReader(data), storage.BlobMeta{Size: int64(len(data))})

	ok, err = s.Exists(ctx, blobtest.KeyOf("present"))
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
	_ = s.Put(ctx, blobtest.KeyOf("todelete"), bytes.NewReader(data), storage.BlobMeta{Size: int64(len(data))})

	err := s.Delete(ctx, blobtest.KeyOf("todelete"))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	ok, _ := s.Exists(ctx, blobtest.KeyOf("todelete"))
	if ok {
		t.Error("expected false after delete")
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	keys := []string{blobtest.HexKey("aabb11"), blobtest.HexKey("aabb22"), blobtest.HexKey("ccdd33")}
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
	_ = s.Put(ctx, blobtest.KeyOf("urlkey1"), bytes.NewReader(data), storage.BlobMeta{Size: int64(len(data))})

	u, err := s.URL(ctx, blobtest.KeyOf("urlkey1"))
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

	_ = s.Put(ctx, blobtest.KeyOf("idem"), bytes.NewReader(data), meta)
	err := s.Put(ctx, blobtest.KeyOf("idem"), bytes.NewReader(data), meta)
	if err != nil {
		t.Fatalf("second put: %v", err)
	}
}

func TestKeySharding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key := blobtest.HexKey("abcdef1234567890")

	_ = s.Put(ctx, key, bytes.NewReader([]byte("x")), storage.BlobMeta{Size: 1})

	// Verify 2-level sharding: root/ab/cd/<key>
	expected := s.blobPath(key)
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected sharded path %q to exist: %v", expected, err)
	}
}

var _ storage.BlobStore = (*Store)(nil)

// Get of a missing key is storage.ErrBlobNotFound; a blob it cannot
// open for another reason is an error that is not.
func TestGetNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, _, err := s.Get(ctx, blobtest.KeyOf("abcd0000missing")); !errors.Is(err, storage.ErrBlobNotFound) {
		t.Errorf("Get missing key: err = %v, want ErrBlobNotFound", err)
	}

	if err := s.Put(ctx, blobtest.KeyOf("abcd0000locked"), bytes.NewReader([]byte("x")), storage.BlobMeta{}); err != nil {
		t.Fatal(err)
	}
	path := s.blobPath(blobtest.KeyOf("abcd0000locked"))
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if f, err := os.Open(path); err == nil {
		f.Close()
		t.Skip("running with permission to open a mode-000 file")
	}
	_, _, err := s.Get(ctx, blobtest.KeyOf("abcd0000locked"))
	if err == nil || errors.Is(err, storage.ErrBlobNotFound) {
		t.Errorf("Get unreadable blob: err = %v, want an error that is not ErrBlobNotFound", err)
	}
}

// TestListConformance runs the backend-shared List(prefix) contract.
func TestListConformance(t *testing.T) {
	blobtest.ListConformance(t, newTestStore(t))
}

// Blobs are instance data: the store root, its fan-out directories and
// each blob are closed to other users (0700 / 0600), so a protected
// instance passes `dpkms doctor --strict` as created.
func TestStore_ClosedToOtherUsers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not express access on Windows")
	}
	root := filepath.Join(t.TempDir(), "blobs")
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(context.Background(), blobtest.KeyOf("abcdef"), bytes.NewReader([]byte("x")), storage.BlobMeta{}); err != nil {
		t.Fatal(err)
	}
	err = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is mode %04o; want no group/other bits", p, fi.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
