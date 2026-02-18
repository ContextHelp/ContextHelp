package search

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func seedObject(t *testing.T, driver storage.StorageDriver, id, typ string, tags []storage.Tag) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:        id,
		Type:      typ,
		Tags:      tags,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := driver.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("seed object %s: %v", id, err)
	}
}

func TestSearchByType(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	seedObject(t, driver, "obj-1", "article", nil)
	seedObject(t, driver, "obj-2", "note", nil)

	results, total, err := engine.Search(ctx, "type==article", 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
	if len(results) != 1 {
		t.Errorf("results: got %d", len(results))
	}
}

func TestSearchByTag(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	seedObject(t, driver, "obj-1", "article", []storage.Tag{{Label: "design", Weight: 1.0}})
	seedObject(t, driver, "obj-2", "article", []storage.Tag{{Label: "code", Weight: 1.0}})

	results, _, err := engine.Search(ctx, "tag==design", 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "obj-1" {
		t.Errorf("results: got %v", results)
	}
}

func TestSearchAnd(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	seedObject(t, driver, "obj-1", "article", []storage.Tag{{Label: "design"}})
	seedObject(t, driver, "obj-2", "note", []storage.Tag{{Label: "design"}})
	seedObject(t, driver, "obj-3", "article", []storage.Tag{{Label: "code"}})

	results, _, err := engine.Search(ctx, "type==article;tag==design", 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "obj-1" {
		t.Errorf("results: got %d", len(results))
	}
}

func TestSearchOr(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	seedObject(t, driver, "obj-1", "article", nil)
	seedObject(t, driver, "obj-2", "note", nil)
	seedObject(t, driver, "obj-3", "draft", nil)

	results, _, err := engine.Search(ctx, "type==article,type==note", 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("results: got %d, want 2", len(results))
	}
}

func TestSearchByMention(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	seedObject(t, driver, "obj-1", "article", nil)
	seedObject(t, driver, "obj-2", "note", nil)

	// Create edge: obj-1 mentions @ui.layout
	edge := &storage.Edge{
		ID:        "edge-1",
		FromType:  "object",
		FromID:    "obj-1",
		ToType:    "entity",
		ToID:      "@ui.layout",
		EdgeType:  "mentions",
		Weight:    1.0,
		CreatedAt: time.Now(),
	}
	driver.Edges().Create(ctx, edge)

	results, _, err := engine.Search(ctx, "mention==@ui.layout", 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "obj-1" {
		t.Errorf("results: got %v", results)
	}
}

func TestSearchPagination(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		seedObject(t, driver, fmt.Sprintf("obj-%d", i), "article", nil)
	}

	results, total, err := engine.Search(ctx, "type==article", 2, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 5 {
		t.Errorf("total: got %d, want 5", total)
	}
	if len(results) != 2 {
		t.Errorf("results: got %d, want 2", len(results))
	}
}

func TestSearchEmpty(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	results, total, err := engine.Search(ctx, "type==article", 10, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 0 {
		t.Errorf("total: got %d, want 0", total)
	}
	if len(results) != 0 {
		t.Errorf("results: got %d", len(results))
	}
}

func TestSearchInvalidQuery(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	_, _, err := engine.Search(ctx, "==bad", 10, 0)
	if err == nil {
		t.Error("expected error for invalid query")
	}
}
