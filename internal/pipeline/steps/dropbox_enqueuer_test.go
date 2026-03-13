package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestDropboxEnqueuerSuccess(t *testing.T) {
	step := NewDropboxEnqueuer()

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"dropbox_files": []map[string]any{
				{
					"id":           "id:f1",
					"name":         "notes.md",
					"path":         "/notes.md",
					"size":         int64(100),
					"is_folder":    false,
					"content_hash": "hash1",
					"modified_time": "2026-01-15T12:00:00Z",
				},
				{
					"id":        "id:d1",
					"name":      "archive",
					"path":      "/archive",
					"is_folder": true,
				},
				{
					"id":           "id:f2",
					"name":         "plan.txt",
					"path":         "/plan.txt",
					"is_folder":    false,
					"content_hash": "hash2",
				},
			},
		},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Folder should be excluded; 2 file entries.
	if len(got.Sections) != 2 {
		t.Fatalf("Sections: got %d, want 2", len(got.Sections))
	}
	if got.Sections[0].Title != "notes.md" {
		t.Errorf("Sections[0].Title: got %q", got.Sections[0].Title)
	}
	if got.Sections[0].Content != "/notes.md" {
		t.Errorf("Sections[0].Content: got %q", got.Sections[0].Content)
	}

	pending, ok := got.Metadata["items_to_enqueue"].([]map[string]any)
	if !ok {
		t.Fatalf("items_to_enqueue type: %T", got.Metadata["items_to_enqueue"])
	}
	if len(pending) != 2 {
		t.Fatalf("items_to_enqueue: got %d, want 2", len(pending))
	}
}

func TestDropboxEnqueuerDeduplication(t *testing.T) {
	step := NewDropboxEnqueuer()

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"dropbox_files": []map[string]any{
				{"id": "id:f1", "name": "a.md", "path": "/a.md", "is_folder": false, "content_hash": "samehash"},
				{"id": "id:f2", "name": "b.md", "path": "/b.md", "is_folder": false, "content_hash": "samehash"},
				{"id": "id:f3", "name": "c.md", "path": "/c.md", "is_folder": false, "content_hash": "uniquehash"},
			},
		},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// First "samehash" kept, second deduplicated; "uniquehash" kept.
	if len(got.Sections) != 2 {
		t.Fatalf("Sections: got %d, want 2 (dedup)", len(got.Sections))
	}
}

func TestDropboxEnqueuerNoFiles(t *testing.T) {
	step := NewDropboxEnqueuer()

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	pending, ok := got.Metadata["items_to_enqueue"].([]map[string]any)
	if !ok {
		t.Fatalf("items_to_enqueue type: %T", got.Metadata["items_to_enqueue"])
	}
	if len(pending) != 0 {
		t.Errorf("expected empty pending, got %d", len(pending))
	}
}

func TestDropboxEnqueuerBadType(t *testing.T) {
	step := NewDropboxEnqueuer()

	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"dropbox_files": "not a slice",
		},
	}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestDropboxEnqueuerNilMetadata(t *testing.T) {
	step := NewDropboxEnqueuer()

	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Metadata == nil {
		t.Error("Metadata should be initialized")
	}
}

func TestDropboxEnqueuerName(t *testing.T) {
	step := NewDropboxEnqueuer()
	if step.Name() != "dropbox_enqueuer" {
		t.Errorf("Name: got %q", step.Name())
	}
}
