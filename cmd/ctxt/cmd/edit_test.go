package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestEditTitle(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_123",
		Type:      "text",
		Summaries: []string{"Old title"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("edit", "--id", "obj_123", "--title", "New title")
	if err != nil {
		t.Fatalf("edit with --title should succeed: %v", err)
	}
	if !strings.Contains(out, "Updated obj_123") {
		t.Error("output should confirm update")
	}
	if !strings.Contains(out, "title: New title") {
		t.Error("output should show updated field")
	}

	// Verify in storage
	updated, err := db.Driver.Objects().Get(ctx, "obj_123")
	if err != nil {
		t.Fatalf("get updated object: %v", err)
	}
	if len(updated.Summaries) == 0 || updated.Summaries[0] != "New title" {
		t.Errorf("title should be updated in storage, got %v", updated.Summaries)
	}
}

func TestEditMultipleFields(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_123",
		Type:      "text",
		Summaries: []string{"Old title"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("edit", "--id", "obj_123", "--title", "New title", "--tags", "ux,design", "--subtype", "article")
	if err != nil {
		t.Fatalf("edit with multiple fields should succeed: %v", err)
	}
	if !strings.Contains(out, "Updated obj_123") {
		t.Error("output should confirm update")
	}
}

func TestEditNoFieldsError(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("edit", "--id", "obj_123")
	if err == nil {
		t.Error("edit without any fields should fail")
	}
}

func TestEditMissingIDError(t *testing.T) {
	_, err := executeCommand("edit", "--title", "New title")
	if err == nil {
		t.Error("edit without --id should fail")
	}
}

func TestEditHelp(t *testing.T) {
	out, err := executeCommand("edit", "--help")
	if err != nil {
		t.Fatalf("edit --help should succeed: %v", err)
	}
	for _, flag := range []string{"--id", "--title", "--summary", "--tags", "--hints", "--mentions", "--decisions", "--subtype"} {
		if !strings.Contains(out, flag) {
			t.Errorf("edit help should list flag %s", flag)
		}
	}
}
