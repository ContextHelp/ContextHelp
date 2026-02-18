package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
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
