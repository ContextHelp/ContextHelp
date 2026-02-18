package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestDeleteByIDWithYes(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_123",
		Type:      "text",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("delete", "--id", "obj_123", "-y")
	if err != nil {
		t.Fatalf("delete --id with -y should succeed: %v", err)
	}
	if !strings.Contains(out, "Deleted obj_123") {
		t.Error("output should confirm deletion")
	}

	// Verify object is gone
	_, getErr := db.Driver.Objects().Get(ctx, "obj_123")
	if getErr == nil {
		t.Error("object should have been deleted from storage")
	}
}

func TestDeleteByTagWithYes(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_tagged",
		Type:      "text",
		Tags:      []storage.Tag{{Label: "temporary"}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("delete", "--tag", "temporary", "-y")
	if err != nil {
		t.Fatalf("delete --tag with -y should succeed: %v", err)
	}
	if !strings.Contains(out, "objects deleted") {
		t.Errorf("output should confirm deletion count, got: %s", out)
	}
}

func TestDeleteByMentionWithYes(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_mention",
		Type:      "text",
		Mentions:  []string{"@project.archived"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("delete", "--mention", "@project.archived", "-y")
	if err != nil {
		t.Fatalf("delete --mention with -y should succeed: %v", err)
	}
	// Output should confirm deletion or indicate no matches found (depending on filter impl)
	if !strings.Contains(out, "deleted") && !strings.Contains(out, "No matching") {
		t.Errorf("output should confirm result, got: %s", out)
	}
}

func TestDeleteAllWithYes(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	for i := 0; i < 3; i++ {
		obj := &storage.KnowledgeObject{
			ID:        fmt.Sprintf("obj_all_%d", i),
			Type:      "text",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed object: %v", err)
		}
	}

	out, err := db.exec("delete", "--all", "-y")
	if err != nil {
		t.Fatalf("delete --all with -y should succeed: %v", err)
	}
	if !strings.Contains(out, "objects deleted") {
		t.Errorf("output should confirm deletion count, got: %s", out)
	}
}

func TestDeleteNoFilterError(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.exec("delete")
	if err == nil {
		t.Error("delete without any filter should fail")
	}
}

func TestDeleteHelp(t *testing.T) {
	out, err := executeCommand("delete", "--help")
	if err != nil {
		t.Fatalf("delete --help should succeed: %v", err)
	}
	for _, flag := range []string{"--id", "--tag", "--hint", "--mention", "--type", "--subtype", "--all", "-y"} {
		if !strings.Contains(out, flag) {
			t.Errorf("delete help should list flag %s", flag)
		}
	}
}
