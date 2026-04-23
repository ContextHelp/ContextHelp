package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func seedTwoObjects(t *testing.T, db *testDB) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for _, id := range []string{"obj_a", "obj_b"} {
		if err := db.Driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "text", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

func TestLinkCreatesEdges(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	out, err := db.exec("link", "obj_a", "obj_b", "--type", "extends")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if !strings.Contains(out, "Linked") {
		t.Errorf("expected confirmation; got %q", out)
	}

	ctx := context.Background()
	fwd, _ := db.Driver.Edges().ListFrom(ctx, "object", "obj_a")
	found := false
	for _, e := range fwd {
		if e.ToID == "obj_b" && e.EdgeType == "extends" {
			found = true
		}
	}
	if !found {
		t.Error("forward edge (extends) not found")
	}

	rev, _ := db.Driver.Edges().ListFrom(ctx, "object", "obj_b")
	found = false
	for _, e := range rev {
		if e.ToID == "obj_a" && e.EdgeType == "extended-by" {
			found = true
		}
	}
	if !found {
		t.Error("reverse edge (extended-by) not found")
	}
}

func TestLinkSymmetricNoDuplicate(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	_, err := db.exec("link", "obj_a", "obj_b", "--type", "related-to")
	if err != nil {
		t.Fatalf("link: %v", err)
	}

	ctx := context.Background()
	fwd, _ := db.Driver.Edges().ListFrom(ctx, "object", "obj_a")
	count := 0
	for _, e := range fwd {
		if e.ToID == "obj_b" && e.EdgeType == "related-to" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 forward edge for symmetric type; got %d", count)
	}

	// symmetric type should NOT create a reverse edge
	rev, _ := db.Driver.Edges().ListFrom(ctx, "object", "obj_b")
	for _, e := range rev {
		if e.ToID == "obj_a" && e.EdgeType == "related-to" {
			t.Error("symmetric link should not create reverse edge")
		}
	}
}

func TestLinkInvalidType(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	_, err := db.exec("link", "obj_a", "obj_b", "--type", "imaginary")
	if err == nil {
		t.Error("expected error for invalid link type")
	}
}

func TestLinkMissingType(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	_, err := db.exec("link", "obj_a", "obj_b")
	if err == nil {
		t.Error("expected error when --type not provided")
	}
}

func TestLinkSourceNotFound(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	_, err := db.exec("link", "obj_missing", "obj_b", "--type", "extends")
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestLinkWithContext(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	_, err := db.exec("link", "obj_a", "obj_b",
		"--type", "contradicts", "--context", "newer data")
	if err != nil {
		t.Fatalf("link with context: %v", err)
	}

	ctx := context.Background()
	edges, _ := db.Driver.Edges().ListFrom(ctx, "object", "obj_a")
	for _, e := range edges {
		if e.EdgeType == "contradicts" {
			if e.Metadata["context"] != "newer data" {
				t.Errorf("expected context metadata; got %v", e.Metadata)
			}
		}
	}
}

func TestLinksListsAll(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	_, _ = db.exec("link", "obj_a", "obj_b", "--type", "extends")

	out, err := db.exec("links", "obj_a")
	if err != nil {
		t.Fatalf("links: %v", err)
	}
	if !strings.Contains(out, "extends") {
		t.Errorf("expected 'extends' in output; got %q", out)
	}
	if !strings.Contains(out, "obj_b") {
		t.Errorf("expected 'obj_b' in output; got %q", out)
	}
}

func TestLinksFilterByType(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	_, _ = db.exec("link", "obj_a", "obj_b", "--type", "extends")
	_, _ = db.exec("link", "obj_a", "obj_b", "--type", "supports")

	out, err := db.exec("links", "obj_a", "--type", "extends")
	if err != nil {
		t.Fatalf("links --type: %v", err)
	}
	if !strings.Contains(out, "extends") {
		t.Error("should contain extends")
	}
	if strings.Contains(out, "supports") {
		t.Error("should not contain supports when filtered")
	}
}

func TestLinksNoLinks(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	out, err := db.exec("links", "obj_a")
	if err != nil {
		t.Fatalf("links: %v", err)
	}
	if !strings.Contains(out, "No links found") {
		t.Errorf("expected no-links message; got %q", out)
	}
}

func TestLinksFollow(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// create chain: obj_1 -> obj_2 -> obj_3
	for _, id := range []string{"obj_1", "obj_2", "obj_3"} {
		_ = db.Driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "text", CreatedAt: now, UpdatedAt: now,
		})
	}

	_, _ = db.exec("link", "obj_1", "obj_2", "--type", "extends")
	_, _ = db.exec("link", "obj_2", "obj_3", "--type", "extends")

	out, err := db.exec("links", "obj_1", "--follow", "2")
	if err != nil {
		t.Fatalf("links --follow: %v", err)
	}
	if !strings.Contains(out, "obj_2") {
		t.Error("depth 1: should find obj_2")
	}
	if !strings.Contains(out, "obj_3") {
		t.Error("depth 2: should find obj_3")
	}
}

func TestUnlink(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	_, _ = db.exec("link", "obj_a", "obj_b", "--type", "extends")

	out, err := db.exec("unlink", "obj_a", "obj_b")
	if err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if !strings.Contains(out, "Removed") {
		t.Errorf("expected removal confirmation; got %q", out)
	}

	// verify edges gone
	ctx := context.Background()
	fwd, _ := db.Driver.Edges().ListFrom(ctx, "object", "obj_a")
	for _, e := range fwd {
		if e.ToID == "obj_b" && e.EdgeType == "extends" {
			t.Error("forward edge should have been removed")
		}
	}
}

func TestUnlinkNoLinks(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	out, err := db.exec("unlink", "obj_a", "obj_b")
	if err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if !strings.Contains(out, "No links found") {
		t.Errorf("expected no-links message; got %q", out)
	}
}

func TestDeleteCascadesLinks(t *testing.T) {
	db := setupTestDB(t)
	seedTwoObjects(t, db)

	_, _ = db.exec("link", "obj_a", "obj_b", "--type", "extends")

	// delete obj_a — should cascade via DeleteByObject
	_, err := db.exec("delete", "--id", "obj_a", "-y")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	ctx := context.Background()
	// reverse edges from obj_b referencing obj_a should be gone
	edges, _ := db.Driver.Edges().ListFrom(ctx, "object", "obj_b")
	for _, e := range edges {
		if e.ToID == "obj_a" {
			t.Errorf("edge to deleted object should have been removed: %+v", e)
		}
	}
}

func TestLinkHelp(t *testing.T) {
	out, err := executeCommand("link", "--help")
	if err != nil {
		t.Fatalf("link --help: %v", err)
	}
	for _, kw := range []string{"--type", "extends", "contradicts", "source_id"} {
		if !strings.Contains(out, kw) {
			t.Errorf("help should mention %q", kw)
		}
	}
}
