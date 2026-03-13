package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestBatchEnqueuerCreatesSection(t *testing.T) {
	records := []map[string]any{
		{"content": "Short content"},
		{"content": strings.Repeat("x", 100)},
	}
	step := NewBatchEnqueuer()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"import_records": records},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(got.Sections))
	}
	if got.Sections[0].Title != "Short content" {
		t.Errorf("section[0].Title: got %q", got.Sections[0].Title)
	}
	// Long content: title should be truncated to 80 chars.
	if len(got.Sections[1].Title) != 80 {
		t.Errorf("section[1].Title length: got %d, want 80", len(got.Sections[1].Title))
	}
	if got.Metadata["batch_queued"] != 2 {
		t.Errorf("batch_queued: got %v", got.Metadata["batch_queued"])
	}
}

func TestBatchEnqueuerNoRecords(t *testing.T) {
	step := NewBatchEnqueuer()
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["batch_queued"] != 0 {
		t.Errorf("batch_queued: got %v", got.Metadata["batch_queued"])
	}
}

func TestBatchEnqueuerSectionOrder(t *testing.T) {
	records := []map[string]any{
		{"content": "a"},
		{"content": "b"},
		{"content": "c"},
	}
	step := NewBatchEnqueuer()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"import_records": records},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for i, s := range got.Sections {
		if s.Order != i {
			t.Errorf("section[%d].Order: got %d", i, s.Order)
		}
	}
}
