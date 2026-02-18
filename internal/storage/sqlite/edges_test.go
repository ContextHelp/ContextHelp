package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func makeEdge(id, fromID, toID string) *storage.Edge {
	return &storage.Edge{
		ID:        id,
		FromType:  "object",
		FromID:    fromID,
		ToType:    "entity",
		ToID:      toID,
		EdgeType:  "mentions",
		Weight:    1.0,
		CreatedAt: time.Now().Truncate(time.Second),
	}
}

func TestCreateAndListFrom(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Edges().Create(ctx, makeEdge("e1", "obj-1", "ui.layout"))
	d.Edges().Create(ctx, makeEdge("e2", "obj-1", "ui.color"))

	edges, err := d.Edges().ListFrom(ctx, "object", "obj-1")
	if err != nil {
		t.Fatalf("list from: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("count: got %d, want 2", len(edges))
	}
}

func TestListTo(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Edges().Create(ctx, makeEdge("e1", "obj-1", "ui.layout"))
	d.Edges().Create(ctx, makeEdge("e2", "obj-2", "ui.layout"))
	d.Edges().Create(ctx, makeEdge("e3", "obj-3", "api.auth"))

	edges, err := d.Edges().ListTo(ctx, "entity", "ui.layout")
	if err != nil {
		t.Fatalf("list to: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("count: got %d, want 2", len(edges))
	}
}

func TestDeleteByObject(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Edges().Create(ctx, makeEdge("e1", "obj-1", "ui.layout"))
	d.Edges().Create(ctx, makeEdge("e2", "obj-1", "ui.color"))

	if err := d.Edges().DeleteByObject(ctx, "obj-1"); err != nil {
		t.Fatalf("delete by object: %v", err)
	}

	edges, _ := d.Edges().ListFrom(ctx, "object", "obj-1")
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}
