package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func seedRawObject(t *testing.T, db *testDB, id, content string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: content,
		Status:     "raw",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Driver.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("seed raw object: %v", err)
	}
}

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

func TestInboxListRawFlag(t *testing.T) {
	db := setupTestDB(t)
	seedRawObject(t, db, "raw_obj_01", "unenriched content")

	out, err := db.exec("inbox", "list", "--raw")
	if err != nil {
		t.Fatalf("inbox list --raw should succeed: %v", err)
	}
	if !strings.Contains(out, "raw_obj_01") {
		t.Errorf("output should contain raw object ID, got: %s", out)
	}
	if !strings.Contains(out, "raw") {
		t.Errorf("output should show raw status, got: %s", out)
	}
}

func TestInboxListPendingFlag(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_pend_01", "ingest:text", "text.short", storage.JobPending)

	out, err := db.exec("inbox", "list", "--pending")
	if err != nil {
		t.Fatalf("inbox list --pending should succeed: %v", err)
	}
	if !strings.Contains(out, "job_pend_01") {
		t.Errorf("output should contain pending job ID, got: %s", out)
	}
	if !strings.Contains(out, "pending") {
		t.Errorf("output should show pending status, got: %s", out)
	}
}

func TestInboxListFailedFlag(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_fail_01", "ingest:url", "url.generic", storage.JobFailed)

	out, err := db.exec("inbox", "list", "--failed")
	if err != nil {
		t.Fatalf("inbox list --failed should succeed: %v", err)
	}
	if !strings.Contains(out, "job_fail_01") {
		t.Errorf("output should contain failed job ID, got: %s", out)
	}
}

func TestInboxListQueueEmpty(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("inbox", "list", "--raw")
	if err != nil {
		t.Fatalf("inbox list --raw on empty db should succeed: %v", err)
	}
	if !strings.Contains(out, "0 total") && !strings.Contains(out, "No results") {
		t.Errorf("output should indicate empty result, got: %s", out)
	}
}

func TestInboxListQueueHeader(t *testing.T) {
	db := setupTestDB(t)
	seedRawObject(t, db, "raw_hdr_01", "header test")

	out, err := db.exec("inbox", "list", "--raw")
	if err != nil {
		t.Fatalf("inbox list --raw should succeed: %v", err)
	}
	if !strings.Contains(out, "Inbox queue") {
		t.Errorf("output should contain Inbox queue header, got: %s", out)
	}
}
