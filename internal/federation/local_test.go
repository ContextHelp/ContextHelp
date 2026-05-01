package federation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newTestDB opens an in-process SQLite DB at a temp path and runs migrations.
func newTestDB(t *testing.T) (storage.StorageDriver, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	drv, err := storageutil.NewDriver("sqlite", path)
	if err != nil {
		t.Fatalf("newTestDB: %v", err)
	}
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("newTestDB init: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	return drv, path
}

func makeObject(id, hash string) storage.KnowledgeObject {
	return storage.KnowledgeObject{
		ID:          id,
		Type:        "note",
		RawContent:  "content-" + id,
		ContentHash: hash,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
}

func makeEdge(id, fromID, toID string) storage.Edge {
	return storage.Edge{
		ID:        id,
		FromType:  "object",
		FromID:    fromID,
		ToType:    "object",
		ToID:      toID,
		EdgeType:  "related",
		CreatedAt: time.Now().UTC(),
	}
}

func countObjects(t *testing.T, drv storage.StorageDriver) int {
	t.Helper()
	objs, _, err := drv.Objects().List(context.Background(), storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("countObjects: %v", err)
	}
	return len(objs)
}

// TestLocalPusher_SingleObject pushes one object and verifies it appears at target.
func TestLocalPusher_SingleObject(t *testing.T) {
	_, targetPath := newTestDB(t)

	obj := makeObject("obj-1", "hash-1")
	pusher := NewLocalPusher("test-fed", targetPath)

	if err := pusher.Push(context.Background(), []storage.KnowledgeObject{obj}, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}

	targetDrv, _ := newTestDB(t) // open fresh handle to verify
	// Reopen via path directly to read back.
	targetDrv2, err := storageutil.NewDriver("sqlite", targetPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer targetDrv2.Close(context.Background())
	if err := targetDrv2.Init(context.Background()); err != nil {
		t.Fatalf("reopen init: %v", err)
	}
	_ = targetDrv

	got, err := targetDrv2.Objects().Get(context.Background(), "obj-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ContentHash != "hash-1" {
		t.Errorf("content_hash: got %q, want %q", got.ContentHash, "hash-1")
	}
}

// TestLocalPusher_Idempotent pushes the same object twice; target must have exactly one copy.
func TestLocalPusher_Idempotent(t *testing.T) {
	_, targetPath := newTestDB(t)

	obj := makeObject("obj-2", "hash-2")
	pusher := NewLocalPusher("test-fed", targetPath)

	for i := 0; i < 2; i++ {
		if err := pusher.Push(context.Background(), []storage.KnowledgeObject{obj}, nil); err != nil {
			t.Fatalf("Push[%d]: %v", i, err)
		}
	}

	targetDrv, err := storageutil.NewDriver("sqlite", targetPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer targetDrv.Close(context.Background())
	if err := targetDrv.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	n := countObjects(t, targetDrv)
	if n != 1 {
		t.Errorf("want 1 object after duplicate push, got %d", n)
	}
}

// TestLocalPusher_MultipleObjects pushes several objects; all must appear at target.
func TestLocalPusher_MultipleObjects(t *testing.T) {
	_, targetPath := newTestDB(t)

	objects := []storage.KnowledgeObject{
		makeObject("obj-a", "hash-a"),
		makeObject("obj-b", "hash-b"),
		makeObject("obj-c", "hash-c"),
	}
	pusher := NewLocalPusher("test-fed", targetPath)

	if err := pusher.Push(context.Background(), objects, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}

	targetDrv, err := storageutil.NewDriver("sqlite", targetPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer targetDrv.Close(context.Background())
	if err := targetDrv.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	n := countObjects(t, targetDrv)
	if n != 3 {
		t.Errorf("want 3 objects, got %d", n)
	}
}

// TestLocalPusher_AdvancesWatermarkAfterCommit verifies AC #3 of US-0319:
// after a successful Push, the federation_watermarks row at the target DB
// reflects last_synced_at > epoch. Drives the same code path the async
// worker invokes (federation.LocalPusher.Push).
func TestLocalPusher_AdvancesWatermarkAfterCommit(t *testing.T) {
	_, targetPath := newTestDB(t)

	objects := []storage.KnowledgeObject{
		makeObject("wm-1", "wm-hash-1"),
		makeObject("wm-2", "wm-hash-2"),
		makeObject("wm-3", "wm-hash-3"),
	}
	pusher := NewLocalPusher("wm-fed", targetPath)

	before := time.Now().UTC()
	if err := pusher.Push(context.Background(), objects, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	after := time.Now().UTC()

	// Reopen to read watermark + verify objects landed.
	drv, err := storageutil.NewDriver("sqlite", targetPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer drv.Close(context.Background())
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	if n := countObjects(t, drv); n != 3 {
		t.Errorf("source objects: want 3 at target, got %d", n)
	}

	got, err := drv.Watermarks().GetWatermark(context.Background(), "wm-fed")
	if err != nil {
		t.Fatalf("GetWatermark: %v", err)
	}
	epoch := time.Unix(0, 0).UTC()
	if !got.After(epoch) {
		t.Errorf("watermark not advanced: got %v, want > epoch", got)
	}
	// Sanity: watermark sits within the call's wall-clock window (small slack).
	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Errorf("watermark outside call window: got %v, before=%v after=%v",
			got, before, after)
	}
}

// TestLocalPusher_NoObjectsLeavesWatermarkUntouched verifies AC #7 of US-0319:
// "Zero objects since last watermark → tick is a no-op (no DB writes,
// no error)." The watermark MUST NOT advance when there is nothing to push.
func TestLocalPusher_NoObjectsLeavesWatermarkUntouched(t *testing.T) {
	_, targetPath := newTestDB(t)

	pusher := NewLocalPusher("noop-fed", targetPath)
	if err := pusher.Push(context.Background(), nil, nil); err != nil {
		t.Fatalf("Push on empty: %v", err)
	}

	drv, err := storageutil.NewDriver("sqlite", targetPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer drv.Close(context.Background())
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	got, err := drv.Watermarks().GetWatermark(context.Background(), "noop-fed")
	if err != nil {
		t.Fatalf("GetWatermark: %v", err)
	}
	epoch := time.Unix(0, 0).UTC()
	if !got.Equal(epoch) {
		t.Errorf("no-op tick advanced watermark: got %v, want epoch", got)
	}
}

// TestLocalPusher_EdgesTravelWithObjects verifies edges are inserted alongside their objects.
func TestLocalPusher_EdgesTravelWithObjects(t *testing.T) {
	_, targetPath := newTestDB(t)

	obj1 := makeObject("obj-x", "hash-x")
	obj2 := makeObject("obj-y", "hash-y")
	edge := makeEdge("edge-xy", "obj-x", "obj-y")

	pusher := NewLocalPusher("test-fed", targetPath)

	if err := pusher.Push(
		context.Background(),
		[]storage.KnowledgeObject{obj1, obj2},
		[]storage.Edge{edge},
	); err != nil {
		t.Fatalf("Push: %v", err)
	}

	targetDrv, err := storageutil.NewDriver("sqlite", targetPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer targetDrv.Close(context.Background())
	if err := targetDrv.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	edges, err := targetDrv.Edges().ListFrom(context.Background(), "object", "obj-x")
	if err != nil {
		t.Fatalf("ListFrom: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("want 1 edge, got %d", len(edges))
	}
	if edges[0].ID != "edge-xy" {
		t.Errorf("edge ID: got %q, want %q", edges[0].ID, "edge-xy")
	}
}
