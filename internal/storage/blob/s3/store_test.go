package s3

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestNewMissingBucket(t *testing.T) {
	cfg := config.BlobS3Config{
		Region: "us-east-1",
	}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for missing bucket")
	}
}

func TestKeyPath(t *testing.T) {
	s := &Store{prefix: "data"}
	got := s.keyPath("abcdef1234")
	want := "data/ab/cd/abcdef1234"
	if got != want {
		t.Errorf("keyPath: got %q, want %q", got, want)
	}
}

func TestKeyPathNoPrefix(t *testing.T) {
	s := &Store{}
	got := s.keyPath("abcdef1234")
	want := "ab/cd/abcdef1234"
	if got != want {
		t.Errorf("keyPath: got %q, want %q", got, want)
	}
}

var _ storage.BlobStore = (*Store)(nil)
