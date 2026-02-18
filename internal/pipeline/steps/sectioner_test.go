package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestSectionByHeadings(t *testing.T) {
	step := NewSectioner()
	draft := &storage.KnowledgeObject{
		RawContent: "## Heading One\nContent one\n## Heading Two\nContent two",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("sections: got %d, want 2", len(got.Sections))
	}
	if got.Sections[0].Title != "Heading One" {
		t.Errorf("section 0 title: got %q", got.Sections[0].Title)
	}
	if got.Sections[1].Title != "Heading Two" {
		t.Errorf("section 1 title: got %q", got.Sections[1].Title)
	}
}

func TestSectionSingleBlock(t *testing.T) {
	step := NewSectioner()
	draft := &storage.KnowledgeObject{
		RawContent: "Just some text with no headings at all.",
	}

	got, _ := step.Run(context.Background(), draft)
	if len(got.Sections) != 1 {
		t.Fatalf("sections: got %d, want 1", len(got.Sections))
	}
	if got.Sections[0].Title != "" {
		t.Errorf("title should be empty: got %q", got.Sections[0].Title)
	}
}

func TestSectionPreservesOrder(t *testing.T) {
	step := NewSectioner()
	draft := &storage.KnowledgeObject{
		RawContent: "## A\ncontent a\n## B\ncontent b\n## C\ncontent c",
	}

	got, _ := step.Run(context.Background(), draft)
	for i, s := range got.Sections {
		if s.Order != i {
			t.Errorf("section %d order: got %d", i, s.Order)
		}
	}
}
