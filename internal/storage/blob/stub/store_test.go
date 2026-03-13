package stub

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestStubPut(t *testing.T) {
	s := New()
	err := s.Put(context.Background(), "abc123", strings.NewReader("data"), storage.BlobMeta{})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
}

func TestStubGetNotFound(t *testing.T) {
	s := New()
	_, _, err := s.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestStubExists(t *testing.T) {
	s := New()
	ok, err := s.Exists(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if ok {
		t.Error("stub should always return false for exists")
	}
}

func TestStubList(t *testing.T) {
	s := New()
	items, err := s.List(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestStubURL(t *testing.T) {
	s := New()
	_, err := s.URL(context.Background(), "abc123")
	if err == nil {
		t.Fatal("expected error for URL on stub")
	}
}

func TestStubDelete(t *testing.T) {
	s := New()
	err := s.Delete(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// Verify interface compliance.
var _ storage.BlobStore = (*Store)(nil)
