package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestCodeBlockExtractorFindsBlocks(t *testing.T) {
	step := NewCodeBlockExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "Some text\n```go\nfmt.Println(\"hello\")\n```\nMore text\n```python\nprint('hi')\n```",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Errorf("expected 2 code block sections, got %d", len(got.Sections))
	}
	if got.Sections[0].Metadata["language"] != "go" {
		t.Errorf("language: %v", got.Sections[0].Metadata["language"])
	}
	if got.Metadata["code_block_count"] != 2 {
		t.Errorf("code_block_count: %v", got.Metadata["code_block_count"])
	}
}

func TestCodeBlockExtractorNoBlocks(t *testing.T) {
	step := NewCodeBlockExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "No code blocks here.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(got.Sections))
	}
}

func TestCodeBlockExtractorName(t *testing.T) {
	step := NewCodeBlockExtractor()
	if step.Name() != "code_block_extractor" {
		t.Errorf("name: %q", step.Name())
	}
}
