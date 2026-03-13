package steps

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func TestExternalizeUnderThreshold(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 1024)

	draft := &storage.KnowledgeObject{
		RawContent: "short content",
		Source:     "test",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent != "short content" {
		t.Errorf("content should be unchanged, got %q", got.RawContent)
	}
}

func TestExternalizeOverThreshold(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 10) // low threshold for testing

	bigContent := strings.Repeat("x", 100)
	draft := &storage.KnowledgeObject{
		RawContent:  bigContent,
		ContentType: "text/plain",
		Source:      "test",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !blob.IsBlobRef(got.RawContent) {
		t.Fatalf("expected blob:// reference, got %q", got.RawContent)
	}

	// Verify metadata was set.
	if got.Metadata["blob_key"] == nil {
		t.Error("expected blob_key in metadata")
	}
	if got.Metadata["blob_original_size"] == nil {
		t.Error("expected blob_original_size in metadata")
	}

	// Verify content is retrievable from blob store.
	key, _ := blob.BlobKey(got.RawContent)
	rc, _, err := bs.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}
	defer rc.Close()
	retrieved, _ := io.ReadAll(rc)
	if !bytes.Equal(retrieved, []byte(bigContent)) {
		t.Errorf("retrieved content mismatch: got %d bytes, want %d", len(retrieved), len(bigContent))
	}
}

func TestExternalizeExactThreshold(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 10)

	// Exactly at threshold — should stay inline.
	draft := &storage.KnowledgeObject{
		RawContent: strings.Repeat("x", 10),
		Source:     "test",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if blob.IsBlobRef(got.RawContent) {
		t.Error("content at threshold should stay inline")
	}
}

func TestExternalizeZeroThreshold(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 0) // 0 = never externalize

	draft := &storage.KnowledgeObject{
		RawContent: strings.Repeat("x", 1000),
		Source:     "test",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if blob.IsBlobRef(got.RawContent) {
		t.Error("threshold 0 should never externalize")
	}
}

func TestExternalizeContract(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 1024)

	c := step.Contract()
	if len(c.Requires) == 0 || c.Requires[0] != "RawContent" {
		t.Errorf("requires: got %v, want [RawContent]", c.Requires)
	}
	found := false
	for _, cap := range c.Capabilities {
		if cap == "blob-externalize" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected blob-externalize capability, got %v", c.Capabilities)
	}
}

func TestExternalizeName(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	step := NewExternalizer(bs, 1024)
	if step.Name() != "externalize_content" {
		t.Errorf("name: got %q, want %q", step.Name(), "externalize_content")
	}
}
