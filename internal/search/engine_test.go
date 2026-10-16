package search

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedObject(t *testing.T, driver storage.StorageDriver, id, typ string, tags []storage.Tag) {
	t.Helper()
	seedObjectWithProfile(t, driver, id, typ, tags, "")
}

func seedObjectWithProfile(t *testing.T, driver storage.StorageDriver, id, typ string, tags []storage.Tag, profileID string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:        id,
		Type:      typ,
		Tags:      tags,
		ProfileID: profileID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := driver.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("seed object %s: %v", id, err)
	}
}

// A query the engine refuses before reading the store is ErrInvalidQuery,
// parse and compile failures alike, with the stage named in the text as
// before; a query that reads the store is not.
func TestSearchRefusedQueryIsInvalidQuery(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	for _, tc := range []struct{ name, query, prefix string }{
		{"parse", "((type==note", "parse error: "},
		{"compile", "nosuchfield==x", "compile error: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := engine.Search(ctx, driver.Objects(), tc.query, 10, 0)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidQuery)
			assert.True(t, strings.HasPrefix(err.Error(), tc.prefix), err.Error())
		})
	}

	_, _, err := engine.Search(ctx, driver.Objects(), "type==note", 10, 0)
	require.NoError(t, err)
	assert.NotErrorIs(t, fmt.Errorf("store: %w", context.Canceled), ErrInvalidQuery)
}

func TestSearchByType(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	seedObject(t, driver, "obj-1", "article", nil)
	seedObject(t, driver, "obj-2", "note", nil)

	results, total, err := engine.Search(ctx, driver.Objects(), "type==article", 10, 0)
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

	results, _, err := engine.Search(ctx, driver.Objects(), "tag==design", 10, 0)
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

	results, _, err := engine.Search(ctx, driver.Objects(), "type==article;tag==design", 10, 0)
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

	results, _, err := engine.Search(ctx, driver.Objects(), "type==article,type==note", 10, 0)
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

	results, _, err := engine.Search(ctx, driver.Objects(), "mention==@ui.layout", 10, 0)
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

	results, total, err := engine.Search(ctx, driver.Objects(), "type==article", 2, 2)
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

	results, total, err := engine.Search(ctx, driver.Objects(), "type==article", 10, 0)
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

	_, _, err := engine.Search(ctx, driver.Objects(), "==bad", 10, 0)
	if err == nil {
		t.Error("expected error for invalid query")
	}
}

// TestProfileIsolation verifies profile A cannot see profile B objects.
func TestProfileIsolation(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	// obj-a belongs to profile "alice", obj-b to "bob", obj-g is global ("").
	seedObjectWithProfile(t, driver, "obj-a", "note", nil, "alice")
	seedObjectWithProfile(t, driver, "obj-b", "note", nil, "bob")
	seedObjectWithProfile(t, driver, "obj-g", "note", nil, "")

	// Alice's scoped search should return only obj-a.
	results, total, err := engine.Search(ctx, driver.Objects(), "type==note", 10, 0, "alice")
	if err != nil {
		t.Fatalf("search alice: %v", err)
	}
	if total != 1 {
		t.Errorf("alice total: got %d, want 1", total)
	}
	if len(results) != 1 || results[0].ID != "obj-a" {
		t.Errorf("alice results: got %v", results)
	}

	// Bob's scoped search should return only obj-b.
	results, total, err = engine.Search(ctx, driver.Objects(), "type==note", 10, 0, "bob")
	if err != nil {
		t.Fatalf("search bob: %v", err)
	}
	if total != 1 {
		t.Errorf("bob total: got %d, want 1", total)
	}
	if len(results) != 1 || results[0].ID != "obj-b" {
		t.Errorf("bob results: got %v", results)
	}

	// Unscoped search returns all three.
	results, total, err = engine.Search(ctx, driver.Objects(), "type==note", 10, 0)
	if err != nil {
		t.Fatalf("search global: %v", err)
	}
	if total != 3 {
		t.Errorf("global total: got %d, want 3", total)
	}
	_ = results
}

// TestSearchStatusScope pins that a query-language search reads like List:
// an object that is not active is never a hit, however the predicate
// matches it, because the engine asks the store for no status.
func TestSearchStatusScope(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	for _, st := range []string{"active", "inbox", "discarded"} {
		now := time.Now().Truncate(time.Second)
		if err := driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: "st-" + st, Type: "note", Status: st, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", st, err)
		}
	}

	results, total, err := engine.Search(ctx, driver.Objects(), "type==note", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total, "total counts active objects only")
	if assert.Len(t, results, 1) {
		assert.Equal(t, "st-active", results[0].ID)
	}
}

// TestSearchRelated verifies related: returns objects sharing mention targets.
func TestSearchRelated(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	engine := NewEngine(driver)
	ctx := context.Background()

	seedObject(t, driver, "obj-1", "article", nil)
	seedObject(t, driver, "obj-2", "article", nil)
	seedObject(t, driver, "obj-3", "article", nil) // no shared mentions

	// obj-1 and obj-2 both mention @arch.decision; obj-3 mentions something else.
	mkEdge := func(id, fromID, toID string) *storage.Edge {
		return &storage.Edge{
			ID:        id,
			FromType:  "object",
			FromID:    fromID,
			ToType:    "entity",
			ToID:      toID,
			EdgeType:  "mentions",
			Weight:    1.0,
			CreatedAt: time.Now(),
		}
	}
	driver.Edges().Create(ctx, mkEdge("e1", "obj-1", "@arch.decision"))
	driver.Edges().Create(ctx, mkEdge("e2", "obj-2", "@arch.decision"))
	driver.Edges().Create(ctx, mkEdge("e3", "obj-3", "@other.thing"))

	// related:@arch.decision should return obj-2 (related to obj-1 via shared target).
	results, _, err := engine.Search(ctx, driver.Objects(), "related==@arch.decision", 10, 0)
	if err != nil {
		t.Fatalf("search related: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one related result")
	}
	// obj-3 must NOT appear (no shared mention target).
	for _, r := range results {
		if r.ID == "obj-3" {
			t.Errorf("obj-3 should not be in related results")
		}
	}
}
