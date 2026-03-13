package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestHeadingSplitterDefaultDepth(t *testing.T) {
	step := NewHeadingSplitter()
	draft := &storage.KnowledgeObject{
		RawContent: "# H1\n\nContent\n\n## H2\n\nMore\n\n### H3 (ignored)\n\nDeep",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Default depth=2, so H3 is not a split point (it appears in H2's body).
	if len(got.Sections) != 2 {
		t.Errorf("expected 2 sections at depth 2, got %d", len(got.Sections))
	}
}

func TestHeadingSplitterCustomDepth(t *testing.T) {
	step := NewHeadingSplitter(WithMaxHeadingDepth(3))
	draft := &storage.KnowledgeObject{
		RawContent: "# H1\n\n## H2\n\n### H3\n\nContent",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 3 {
		t.Errorf("expected 3 sections at depth 3, got %d", len(got.Sections))
	}
}

func TestHeadingSplitterName(t *testing.T) {
	step := NewHeadingSplitter()
	if step.Name() != "heading_splitter" {
		t.Errorf("name: %q", step.Name())
	}
}
