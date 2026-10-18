package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/blobtest"
)

// nestedStore roots a store four levels under a temp dir, so a key that
// climbs out of the root (the case under test) still lands inside the
// test's own tree. base is the temp dir.
func nestedStore(t *testing.T) (s *Store, base string) {
	t.Helper()
	base = t.TempDir()
	s, err := New(filepath.Join(base, "a", "b", "c", "blobs"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return s, base
}

// TestKeyConformance runs the backend-shared key grammar contract.
func TestKeyConformance(t *testing.T) {
	s, _ := nestedStore(t)
	blobtest.KeyConformance(t, s)
}

// A key naming a path outside the root reads, writes and deletes
// nothing there: not through "../", not through the <k[:2]> shard
// directory a key starting ".." makes.
func TestKeyOutsideRootRefused(t *testing.T) {
	ctx := context.Background()
	s, base := nestedStore(t)
	c := filepath.Join(base, "a", "b", "c")
	secret := filepath.Join(c, "secret.txt")
	if err := os.WriteFile(secret, []byte("outside the blob root"), 0o600); err != nil {
		t.Fatal(err)
	}

	// "../secret.txt" is root/../secret.txt once sharded: c/secret.txt.
	rc, _, err := s.Get(ctx, "../secret.txt")
	if rc != nil {
		b, _ := io.ReadAll(rc)
		rc.Close()
		t.Errorf("Get read a file outside the root: %q", b)
	}
	if !errors.Is(err, storage.ErrInvalidBlobKey) {
		t.Errorf("Get(../secret.txt): err = %v, want ErrInvalidBlobKey", err)
	}
	if ok, err := s.Exists(ctx, "../secret.txt"); ok || !errors.Is(err, storage.ErrInvalidBlobKey) {
		t.Errorf("Exists(../secret.txt) = %v, %v; want false, ErrInvalidBlobKey", ok, err)
	}
	if err := s.Delete(ctx, "../secret.txt"); !errors.Is(err, storage.ErrInvalidBlobKey) {
		t.Errorf("Delete(../secret.txt): err = %v, want ErrInvalidBlobKey", err)
	}
	if b, err := os.ReadFile(secret); err != nil || string(b) != "outside the blob root" {
		t.Errorf("file outside the root changed: %q, %v", b, err)
	}

	// "..xx" shards as root/../xx/..xx: c/xx/..xx.
	if err := s.Put(ctx, "..xx", bytes.NewReader([]byte("x")), storage.BlobMeta{}); !errors.Is(err, storage.ErrInvalidBlobKey) {
		t.Errorf("Put(..xx): err = %v, want ErrInvalidBlobKey", err)
	}
	if _, err := os.Stat(filepath.Join(c, "xx")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Put(..xx) wrote outside the root: stat err = %v", err)
	}
}

// Files under the root that are not blobs (a .DS_Store, a stray note,
// a crashed write's temp file) are not listed: a sweep would otherwise
// try to delete them, and fail on their keys, every run.
func TestListSkipsNonBlobFiles(t *testing.T) {
	ctx := context.Background()
	s, _ := nestedStore(t)
	key := blobtest.HexKey("abcd")
	if err := s.Put(ctx, key, bytes.NewReader([]byte("x")), storage.BlobMeta{}); err != nil {
		t.Fatal(err)
	}
	shard := filepath.Dir(s.blobPath(key))
	for _, p := range []string{
		filepath.Join(s.root, ".DS_Store"),
		filepath.Join(shard, ".DS_Store"),
		filepath.Join(shard, "notes.txt"),
		filepath.Join(shard, "."+key+".tmp"),
	} {
		if err := os.WriteFile(p, []byte("not a blob"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, prefix := range []string{"", "abcd", key} {
		items, err := s.List(ctx, prefix)
		if err != nil {
			t.Fatalf("List(%q): %v", prefix, err)
		}
		if len(items) != 1 || items[0].Key != key {
			t.Errorf("List(%q) = %+v, want only %s", prefix, items, key)
		}
	}
}
