package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestMarkdownParserCreatesHeadingSections(t *testing.T) {
	step := NewMarkdownParser()
	draft := &storage.KnowledgeObject{
		RawContent: "# Title\n\nSome content\n\n## Section\n\nMore content",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(got.Sections))
	}
	if got.Sections[0].Title != "Title" {
		t.Errorf("first section title: %q", got.Sections[0].Title)
	}
	if got.Metadata["markdown_heading_count"] != 2 {
		t.Errorf("heading count: %v", got.Metadata["markdown_heading_count"])
	}
}

func TestMarkdownParserNoHeadings(t *testing.T) {
	step := NewMarkdownParser()
	draft := &storage.KnowledgeObject{
		RawContent: "Just plain text, no headings.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(got.Sections))
	}
}

func TestMarkdownParserName(t *testing.T) {
	step := NewMarkdownParser()
	if step.Name() != "markdown_parser" {
		t.Errorf("name: %q", step.Name())
	}
}
