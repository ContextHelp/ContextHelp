package blob

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFactoryLocal(t *testing.T) {
	dir := t.TempDir()
	cfg := config.BlobConfig{
		Backend: "local",
		Local:   config.BlobLocalConfig{Path: dir},
	}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new local: %v", err)
	}
	var _ storage.BlobStore = store
}

func TestFactoryStub(t *testing.T) {
	cfg := config.BlobConfig{Backend: "stub"}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new stub: %v", err)
	}
	var _ storage.BlobStore = store
}

func TestFactoryUnknown(t *testing.T) {
	cfg := config.BlobConfig{Backend: "unknown"}
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestFactoryDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := config.BlobConfig{
		Backend: "",
		Local:   config.BlobLocalConfig{Path: dir},
	}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("new default: %v", err)
	}
	var _ storage.BlobStore = store
}
