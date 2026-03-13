package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func watchCfg(id, path string) *storage.WatchConfig {
	now := time.Now().Truncate(time.Second)
	return &storage.WatchConfig{
		ID: id, Path: path, Mode: "generic",
		IncludePatterns: []string{"**/*.md"},
		ExcludePatterns: []string{"**/.git/**"},
		DebounceMS: 500, Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestWatchStore_CreateGet(t *testing.T) {
	ws := storageutil.NewTestDriver(t).Watches()
	ctx := context.Background()

	cfg := watchCfg("w1", "/tmp/vault1")
	if err := ws.CreateWatch(ctx, cfg); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := ws.GetWatch(ctx, "w1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Path != "/tmp/vault1" {
		t.Errorf("path: got %q", got.Path)
	}
	if len(got.IncludePatterns) != 1 || got.IncludePatterns[0] != "**/*.md" {
		t.Errorf("include_patterns: got %v", got.IncludePatterns)
	}
}

func TestWatchStore_List(t *testing.T) {
	ws := storageutil.NewTestDriver(t).Watches()
	ctx := context.Background()

	ws.CreateWatch(ctx, watchCfg("w1", "/tmp/a"))
	ws.CreateWatch(ctx, watchCfg("w2", "/tmp/b"))

	all, err := ws.ListWatches(ctx, "")
	if err != nil || len(all) != 2 {
		t.Errorf("list all: got %d, err %v", len(all), err)
	}
	active, err := ws.ListWatches(ctx, "active")
	if err != nil || len(active) != 2 {
		t.Errorf("list active: got %d, err %v", len(active), err)
	}
}

func TestWatchStore_UpdateDelete(t *testing.T) {
	ws := storageutil.NewTestDriver(t).Watches()
	ctx := context.Background()

	cfg := watchCfg("w1", "/tmp/vault")
	ws.CreateWatch(ctx, cfg)
	cfg.Status = "paused"
	if err := ws.UpdateWatch(ctx, cfg); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := ws.GetWatch(ctx, "w1")
	if got.Status != "paused" {
		t.Errorf("status after update: got %q", got.Status)
	}

	if err := ws.DeleteWatch(ctx, "w1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ws.GetWatch(ctx, "w1"); err == nil {
		t.Error("expected not-found error after delete")
	}
}

func TestWatchStore_FileRecord_RoundTrip(t *testing.T) {
	ws := storageutil.NewTestDriver(t).Watches()
	ctx := context.Background()
	ws.CreateWatch(ctx, watchCfg("w1", "/tmp/vault"))

	rec := &storage.WatchFileRecord{
		WatchID: "w1", FilePath: "/tmp/vault/note.md",
		ObjectID: "obj-abc", ContentHash: "deadbeef",
		LastSeen: time.Now().Truncate(time.Second),
	}
	if err := ws.UpsertFileRecord(ctx, rec); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := ws.GetFileRecord(ctx, "w1", "/tmp/vault/note.md")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ContentHash != "deadbeef" {
		t.Errorf("hash: got %q", got.ContentHash)
	}

	// Update via upsert.
	rec.ContentHash = "cafebabe"
	ws.UpsertFileRecord(ctx, rec)
	got, _ = ws.GetFileRecord(ctx, "w1", "/tmp/vault/note.md")
	if got.ContentHash != "cafebabe" {
		t.Errorf("hash after update: got %q", got.ContentHash)
	}

	// List.
	recs, _ := ws.ListFileRecords(ctx, "w1")
	if len(recs) != 1 {
		t.Errorf("list: got %d", len(recs))
	}

	// Delete.
	ws.DeleteFileRecord(ctx, "w1", "/tmp/vault/note.md")
	recs, _ = ws.ListFileRecords(ctx, "w1")
	if len(recs) != 0 {
		t.Errorf("after delete: got %d records", len(recs))
	}
}
