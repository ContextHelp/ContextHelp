package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"hop.top/uri"
)

func TestList(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	for _, id := range []string{"obj_001", "obj_002", "obj_003"} {
		obj := &storage.KnowledgeObject{
			ID:        id,
			Type:      "text",
			Summaries: []string{"Summary for " + id},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed object: %v", err)
		}
	}

	out, err := db.exec("list")
	if err != nil {
		t.Fatalf("list should succeed: %v", err)
	}
	if !strings.Contains(out, "Knowledge Objects") {
		t.Error("output should contain 'Knowledge Objects'")
	}
	if !strings.Contains(out, "obj_001") {
		t.Error("output should contain object IDs")
	}
}

func TestListWithFilters(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "obj_url_1",
		Type:      "url",
		Tags:      []storage.Tag{{Label: "ux"}},
		Mentions: []uri.URI{{Scheme: "ctxt", Space: "entity", ID: "ui/best-practice"}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Driver.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	out, err := db.exec("list", "--type", "url")
	if err != nil {
		t.Fatalf("list with filters should succeed: %v", err)
	}
	if !strings.Contains(out, "obj_url_1") {
		t.Error("output should contain filtered object")
	}
}

func TestListWithDisplayFlags(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("list", "--limit", "10", "--sort", "recent", "--dir", "asc")
	if err != nil {
		t.Fatalf("list with display flags should succeed: %v", err)
	}
	if !strings.Contains(out, "Knowledge Objects") {
		t.Error("output should contain header")
	}
}

func TestListHelp(t *testing.T) {
	out, err := executeCommand("list", "--help")
	if err != nil {
		t.Fatalf("list --help should succeed: %v", err)
	}
	for _, flag := range []string{"--type", "--tagged", "--hint", "--mention", "--pipeline", "--subtype", "--before", "--after", "--q", "--limit", "--start", "--sort", "--dir", "--no-track"} {
		if !strings.Contains(out, flag) {
			t.Errorf("list help should list flag %s", flag)
		}
	}
}
