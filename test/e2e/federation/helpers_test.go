//go:build federation_e2e

// Package federation_e2e holds cross-instance e2e tests for ctxt federation.
//
// "Cross-instance" = two real SQLite DBs driven by federation.LocalPusher
// (the same code path the production async worker would use). Tests document
// US-0319 acceptance criteria. Some tests Skip with reference to the
// 2026-04-30 federation e2e audit when impl is missing (worker goroutine,
// watermark API, mentions table). Skipped tests still document the contract.
//
// Build tag: federation_e2e (excluded from default `go test`).
// Run: go test -tags "fts5 federation_e2e" -count=1 ./test/e2e/federation/...
package federation_e2e

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"

	_ "github.com/mattn/go-sqlite3"
)

// instance represents a minimal dpkms instance backed by a SQLite DB on disk.
// We don't boot the full HTTP server; LocalPusher operates DB-to-DB, which is
// the actual production code path the async worker invokes.
type instance struct {
	drv  storage.StorageDriver
	name string
	path string
}

// instances groups source + target + the federation name used to wire them.
type instances struct {
	src     *instance
	tgt     *instance
	fedName string
}

// setupTwoInstances creates two SQLite-backed dpkms instances (source A,
// target B) with migrations applied. Federation pushes flow A → B via
// federation.LocalPusher, the production code path.
func setupTwoInstances(t *testing.T) instances {
	t.Helper()
	tmp := t.TempDir()
	src := mustOpenInstance(t, "source", filepath.Join(tmp, "source.db"))
	tgt := mustOpenInstance(t, "target", filepath.Join(tmp, "target.db"))
	return instances{src: src, tgt: tgt, fedName: "test-fed"}
}

func mustOpenInstance(t *testing.T, name, path string) *instance {
	t.Helper()
	drv, err := storageutil.NewDriver("sqlite", path, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("setupTwoInstances: open %s: %v", name, err)
	}
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("setupTwoInstances: init %s: %v", name, err)
	}
	t.Cleanup(func() {
		if err := drv.Close(context.Background()); err != nil {
			t.Logf("close %s: %v", name, err)
		}
	})
	return &instance{name: name, path: path, drv: drv}
}

// captureBookmark inserts a KnowledgeObject into the given instance, simulating
// a capture event. Returns the populated object so the test can later check
// it appears at the federation target. mentions is wired in for the
// (currently skipped) MentionsPushed test; un-skip will exercise the
// non-nil branch.
//
//nolint:unparam // mentions slice is used by skipped MentionsPushed test
func captureBookmark(t *testing.T, inst *instance, id, contentHash, content string, mentions []string) *storage.KnowledgeObject {
	t.Helper()
	now := time.Now().UTC()
	obj := &storage.KnowledgeObject{
		ID:          id,
		Type:        "bookmark",
		RawContent:  content,
		ContentHash: contentHash,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if len(mentions) > 0 {
		obj.Metadata = map[string]any{"people": mentions}
	}
	if err := inst.drv.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("captureBookmark %q: %v", id, err)
	}
	return obj
}

// pushOnce drives one synchronous federation sync from src to tgt: what
// the async worker does on each tick (federation.Sync).
func pushOnce(t *testing.T, ix instances) error {
	t.Helper()
	pusher := federation.NewLocalPusher(ix.fedName, ix.tgt.path, nil, ix.src.drv.Blobs())
	_, err := federation.Sync(context.Background(), ix.src.drv, pusher, 0)
	return err
}

// waitForFederation polls the target until obj.ID appears or timeout fires.
// Mirrors the contract: "objects appear in merged DB within configured
// interval". Fails with a clear "doc says async push within interval; got 0
// objects in <timeout>" message so the audit-flagged gap surfaces loudly.
func waitForFederation(t *testing.T, tgt *instance, objectID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	ctx := context.Background()
	for time.Now().Before(deadline) {
		got, err := tgt.drv.Objects().Get(ctx, objectID)
		if err == nil && got != nil && got.ID == objectID {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf(
		"waitForFederation: doc says async push within interval; "+
			"got 0 objects matching %q in %s on target %s (%s)",
		objectID, timeout, tgt.name, tgt.path,
	)
}

// readWatermark reads federation_watermarks.last_change_seq via raw
// SQL, independent of the WatermarkStore under test. Returns (0, nil)
// if no row exists for the federation name.
func readWatermark(t *testing.T, dbPath, fedName string) (int64, error) {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Logf("readWatermark close %s: %v", dbPath, cerr)
		}
	}()
	var seq int64
	err = db.QueryRowContext(
		context.Background(),
		`SELECT last_change_seq FROM federation_watermarks WHERE federation_name = ?`,
		fedName,
	).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("query watermark: %w", err)
	}
	return seq, nil
}

// countTargetObjects returns how many rows exist in the target's objects
// table — independent of LocalPusher's dedup logic.
func countTargetObjects(t *testing.T, tgt *instance) int {
	t.Helper()
	objs, _, err := tgt.drv.Objects().List(context.Background(), storage.ObjectFilter{Status: "all"})
	if err != nil {
		t.Fatalf("countTargetObjects: %v", err)
	}
	return len(objs)
}
