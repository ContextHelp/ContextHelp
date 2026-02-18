package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFind(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_find_1",
		Type:      "text",
		Summaries: []string{"authentication best practices for web apps"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("find", "authentication best practices")
	if err != nil {
		t.Fatalf("find should succeed: %v", err)
	}
	if !strings.Contains(out, "Search:") {
		t.Error("output should contain search header")
	}
	if !strings.Contains(out, "obj_find_1") {
		t.Error("output should contain matching object")
	}
}

func TestFindWithLimit(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	for i := 0; i < 3; i++ {
		obj := &storage.KnowledgeObject{
			ID:         "obj_onboard_" + string(rune('a'+i)),
			Type:       "text",
			Summaries:  []string{"onboarding flow design patterns"},
			RawContent: "onboarding details",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed object: %v", err)
		}
	}

	out, err := db.exec("find", "onboarding", "--limit", "2")
	if err != nil {
		t.Fatalf("find with --limit should succeed: %v", err)
	}
	if !strings.Contains(out, "Search:") {
		t.Error("output should contain search header")
	}
}

func TestFindNoResults(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("find", "nonexistent_query_xyz")
	if err != nil {
		t.Fatalf("find with no results should succeed: %v", err)
	}
	if !strings.Contains(out, "0 results") {
		t.Error("output should indicate zero results")
	}
}

func TestFindNoQueryError(t *testing.T) {
	_, err := executeCommand("find")
	if err == nil {
		t.Error("find without query should fail")
	}
}

func TestFindHelp(t *testing.T) {
	out, err := executeCommand("find", "--help")
	if err != nil {
		t.Fatalf("find --help should succeed: %v", err)
	}
	if !strings.Contains(out, "--limit") {
		t.Error("find help should list --limit flag")
	}
	if !strings.Contains(out, "search") {
		t.Error("find help should describe search functionality")
	}
}
