package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func seedInboxItem(t *testing.T, db *testDB, id, content string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: content,
		Status:     "inbox",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Driver.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("seed inbox item: %v", err)
	}
}

func TestInboxList(t *testing.T) {
	db := setupTestDB(t)
	seedInboxItem(t, db, "inbox_001", "hello world")
	seedInboxItem(t, db, "inbox_002", "another item")

	out, err := db.exec("inbox", "list")
	if err != nil {
		t.Fatalf("inbox list should succeed: %v", err)
	}
	if !strings.Contains(out, "inbox_001") {
		t.Error("output should contain inbox_001")
	}
	if !strings.Contains(out, "inbox_002") {
		t.Error("output should contain inbox_002")
	}
}

func TestInboxListEmpty(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("inbox", "list")
	if err != nil {
		t.Fatalf("inbox list empty should succeed: %v", err)
	}
	if !strings.Contains(out, "0 total") {
		t.Errorf("output should show 0 total, got: %s", out)
	}
}

func TestInboxDiscard(t *testing.T) {
	db := setupTestDB(t)
	seedInboxItem(t, db, "inbox_dis_01", "to be discarded")

	out, err := db.exec("inbox", "discard", "inbox_dis_01")
	if err != nil {
		t.Fatalf("inbox discard should succeed: %v", err)
	}
	if !strings.Contains(out, "inbox_dis_01") {
		t.Errorf("output should mention the discarded ID, got: %s", out)
	}

	// Verify it no longer appears in inbox list
	listOut, err := db.exec("inbox", "list")
	if err != nil {
		t.Fatalf("inbox list should succeed: %v", err)
	}
	if strings.Contains(listOut, "inbox_dis_01") {
		t.Error("discarded item should not appear in inbox list")
	}
}

func TestInboxDiscardNotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("inbox", "discard", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent item")
	}
}

func TestInboxClear(t *testing.T) {
	db := setupTestDB(t)
	seedInboxItem(t, db, "inbox_clr_01", "item one")
	seedInboxItem(t, db, "inbox_clr_02", "item two")

	out, err := db.exec("inbox", "clear")
	if err != nil {
		t.Fatalf("inbox clear should succeed: %v", err)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("output should mention 2 cleared items, got: %s", out)
	}

	// Verify inbox is empty
	listOut, err := db.exec("inbox", "list")
	if err != nil {
		t.Fatalf("inbox list should succeed: %v", err)
	}
	if !strings.Contains(listOut, "0 total") {
		t.Errorf("inbox should be empty after clear, got: %s", listOut)
	}
}

func TestInboxHelp(t *testing.T) {
	out, err := executeCommand("inbox", "--help")
	if err != nil {
		t.Fatalf("inbox --help should succeed: %v", err)
	}
	for _, sub := range []string{"list", "triage", "discard", "clear"} {
		if !strings.Contains(out, sub) {
			t.Errorf("inbox help should list subcommand %s", sub)
		}
	}
}
