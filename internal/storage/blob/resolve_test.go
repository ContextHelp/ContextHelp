package blob

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func TestResolveInlineContent(t *testing.T) {
	s, _ := local.New(t.TempDir())
	obj := &storage.KnowledgeObject{RawContent: "just plain text"}

	rc, err := Resolve(context.Background(), s, obj)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != "just plain text" {
		t.Errorf("got %q, want %q", got, "just plain text")
	}
}

func TestResolveBlobReference(t *testing.T) {
	s, _ := local.New(t.TempDir())
	ctx := context.Background()

	original := []byte("blob content here")
	_ = s.Put(ctx, "abc123", bytes.NewReader(original), storage.BlobMeta{
		ContentType: "text/plain",
		Size:        int64(len(original)),
	})

	obj := &storage.KnowledgeObject{RawContent: "blob://abc123"}

	rc, err := Resolve(ctx, s, obj)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, original) {
		t.Errorf("got %q, want %q", got, original)
	}
}

func TestIsBlobRef(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"blob://abc123", true},
		{"blob://", false},
		{"regular text", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsBlobRef(tt.input); got != tt.want {
			t.Errorf("IsBlobRef(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBlobKey(t *testing.T) {
	key, ok := BlobKey("blob://abc123")
	if !ok || key != "abc123" {
		t.Errorf("BlobKey: got %q/%v, want %q/%v", key, ok, "abc123", true)
	}

	_, ok = BlobKey("regular text")
	if ok {
		t.Error("BlobKey should return false for non-blob ref")
	}
}
