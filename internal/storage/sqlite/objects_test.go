package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeObject(id, typ string) *storage.KnowledgeObject {
	now := time.Now().Truncate(time.Second)
	return &storage.KnowledgeObject{
		ID:         id,
		Type:       typ,
		Subtype:    "short",
		RawContent: "test content for " + id,
		Tags:       []storage.Tag{{Label: "design", Weight: 1.0}},
		Mentions:   []string{"@ui.layout"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestCreateAndGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("obj-1", "article")
	if err := d.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := d.Objects().Get(ctx, "obj-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.ID != "obj-1" {
		t.Errorf("ID: got %q", got.ID)
	}
	if got.Type != "article" {
		t.Errorf("Type: got %q", got.Type)
	}
	if got.RawContent != obj.RawContent {
		t.Errorf("RawContent: got %q", got.RawContent)
	}
	if len(got.Tags) != 1 || got.Tags[0].Label != "design" {
		t.Errorf("Tags: got %v", got.Tags)
	}
}

func TestList(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		obj := makeObject(
			"obj-"+string(rune('a'+i)),
			"article",
		)
		if err := d.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	objs, total, err := d.Objects().List(ctx, storage.ObjectFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objs) != 2 {
		t.Errorf("count: got %d, want 2", len(objs))
	}
	if total != 3 {
		t.Errorf("total: got %d, want 3", total)
	}
}

func TestListFilterByType(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Objects().Create(ctx, makeObject("obj-1", "article"))
	d.Objects().Create(ctx, makeObject("obj-2", "note"))
	d.Objects().Create(ctx, makeObject("obj-3", "article"))

	objs, total, err := d.Objects().List(ctx, storage.ObjectFilter{Type: "article"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
	if len(objs) != 2 {
		t.Errorf("count: got %d, want 2", len(objs))
	}
}

func TestUpdate(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("obj-1", "article")
	d.Objects().Create(ctx, obj)

	obj.Tags = []storage.Tag{{Label: "updated", Weight: 2.0}}
	obj.UpdatedAt = time.Now().Truncate(time.Second)
	if err := d.Objects().Update(ctx, obj); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := d.Objects().Get(ctx, "obj-1")
	if len(got.Tags) != 1 || got.Tags[0].Label != "updated" {
		t.Errorf("Tags after update: got %v", got.Tags)
	}
}

func TestDelete(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("obj-1", "article")
	d.Objects().Create(ctx, obj)

	if err := d.Objects().Delete(ctx, "obj-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := d.Objects().Get(ctx, "obj-1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteNotFound(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	err := d.Objects().Delete(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent delete")
	}
}

func TestListBySQL_SimpleWhere(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, d.Objects().Create(ctx, makeObject("art-1", "article")))
	require.NoError(t, d.Objects().Create(ctx, makeObject("art-2", "article")))
	require.NoError(t, d.Objects().Create(ctx, makeObject("note-1", "note")))

	objs, total, err := d.Objects().ListBySQL(ctx, "type = ?", []any{"article"}, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, objs, 2)
	for _, o := range objs {
		assert.Equal(t, "article", o.Type)
	}
}

func TestListBySQL_Empty(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	objs, total, err := d.Objects().ListBySQL(ctx, "type = ?", []any{"nonexistent"}, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Len(t, objs, 0)
}

func TestListBySQL_Pagination(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		obj := makeObject(fmt.Sprintf("page-%d", i), "article")
		// Stagger creation times so ordering is deterministic.
		obj.CreatedAt = time.Now().Add(time.Duration(i) * time.Second).Truncate(time.Second)
		obj.UpdatedAt = obj.CreatedAt
		require.NoError(t, d.Objects().Create(ctx, obj))
	}

	objs, total, err := d.Objects().ListBySQL(ctx, "", nil, 2, 1)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, objs, 2)
}

func TestListBySQL_JSONExtract(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj1 := makeObject("meta-1", "article")
	obj1.Metadata = map[string]any{"key": "val"}
	require.NoError(t, d.Objects().Create(ctx, obj1))

	obj2 := makeObject("meta-2", "article")
	obj2.Metadata = map[string]any{"key": "other"}
	require.NoError(t, d.Objects().Create(ctx, obj2))

	obj3 := makeObject("meta-3", "article")
	obj3.Metadata = map[string]any{"different": "field"}
	require.NoError(t, d.Objects().Create(ctx, obj3))

	objs, total, err := d.Objects().ListBySQL(ctx, "json_extract(metadata, '$.key') = ?", []any{"val"}, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, objs, 1)
	assert.Equal(t, "meta-1", objs[0].ID)
}
