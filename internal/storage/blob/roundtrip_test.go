package blob

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

func TestRoundTrip(t *testing.T) {
	bs, _ := local.New(t.TempDir())
	ctx := context.Background()

	original := strings.Repeat("large content ", 1000)
	draft := &storage.KnowledgeObject{
		RawContent:  original,
		ContentType: "text/plain",
		Source:      "test",
	}

	// Externalize.
	ext := steps.NewExternalizer(bs, 100)
	got, err := ext.Run(ctx, draft)
	if err != nil {
		t.Fatalf("externalize: %v", err)
	}

	if !IsBlobRef(got.RawContent) {
		t.Fatalf("expected blob ref, got %q", got.RawContent[:50])
	}

	// Resolve.
	rc, err := Resolve(ctx, bs, got)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer rc.Close()

	resolved, _ := io.ReadAll(rc)
	if !bytes.Equal(resolved, []byte(original)) {
		t.Errorf("round-trip mismatch: got %d bytes, want %d", len(resolved), len(original))
	}
}
