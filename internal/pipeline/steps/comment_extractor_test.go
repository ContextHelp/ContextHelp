package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestCommentExtractorFindsAnnotations(t *testing.T) {
	step := NewCommentExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "package main\n\n// TODO: fix this later\nfunc main() {\n\t// FIXME: this crashes\n\t// HACK: workaround\n\t// NOTE: important\n}",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["annotation_count"] != 4 {
		t.Errorf("annotation_count: %v", got.Metadata["annotation_count"])
	}
	if len(got.Sections) != 4 {
		t.Errorf("expected 4 sections, got %d", len(got.Sections))
	}
	for _, sec := range got.Sections {
		if sec.Metadata["type"] != "comment" {
			t.Errorf("section type: %v", sec.Metadata["type"])
		}
	}
}

func TestCommentExtractorNoAnnotations(t *testing.T) {
	step := NewCommentExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "// regular comment\nvar x = 1",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["annotation_count"] != 0 {
		t.Errorf("annotation_count: %v", got.Metadata["annotation_count"])
	}
}

func TestCommentExtractorName(t *testing.T) {
	step := NewCommentExtractor()
	if step.Name() != "comment_extractor" {
		t.Errorf("name: %q", step.Name())
	}
}
