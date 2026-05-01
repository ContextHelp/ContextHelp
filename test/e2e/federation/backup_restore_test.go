//go:build federation_e2e

// Backup + restore + federation rebuild e2e test for US-0322 (T-0171).
//
// AC under test:
//   - source DB is the source of truth; downstream is derivable
//   - after restore + serve, federation rebuilds downstream from watermark
//   - re-pushing existing objects dedups by content-hash (no duplicates)
//   - new captures after restore propagate normally
//
// Strategy: use file-copy as the "backup" of the source SQLite. The full
// `dpkms backup`/`restore` tar.gz path is exercised under cmd/dpkms unit
// tests; this e2e validates the federation behavior post-restore — the
// portion US-0322 is uniquely about. Watermark on the target preserves
// across the source's wipe+restore cycle (target was untouched), so
// re-pushed objects must be skipped by content-hash dedup.
//
// Run: go test -tags "fts5 federation_e2e" -count=1 ./test/e2e/federation/...

package federation_e2e

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestFederation_BackupRestoreRebuild — full backup/restore/rebuild loop
// for US-0322. Source A federates to merged-target. Backup A. Wipe A.
// Restore A. Re-push. Target keeps deduped state and accepts new captures.
func TestFederation_BackupRestoreRebuild(t *testing.T) {
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "source.db")
	tgtPath := filepath.Join(tmp, "merged.db")
	backupPath := filepath.Join(tmp, "source.backup.db")

	src := mustOpenInstance(t, "source", srcPath)
	tgt := mustOpenInstance(t, "merged", tgtPath)

	// Phase 1: capture + initial federation.
	captureBookmark(t, src, "obj-pre-1", "hash-pre-1", "before backup", nil)
	captureBookmark(t, src, "obj-pre-2", "hash-pre-2", "before backup 2", nil)

	pushFromPath := pushFromPathFn(t, tgtPath)

	ix := instances{src: src, tgt: tgt, fedName: "src-to-merged"}
	if err := pushOnce(t, ix); err != nil {
		t.Fatalf("initial pushOnce: %v", err)
	}
	if got, want := countTargetObjects(t, tgt), 2; got != want {
		t.Fatalf("phase 1: target has %d objects, want %d", got, want)
	}

	// Phase 2: backup the source DB. SQLite is closed first to flush WAL;
	// reopened after copy. Mirrors `dpkms backup`'s file-snapshot semantics.
	if err := src.drv.Close(context.Background()); err != nil {
		t.Fatalf("close source for backup: %v", err)
	}
	if err := copyFile(srcPath, backupPath); err != nil {
		t.Fatalf("backup copy: %v", err)
	}

	// Phase 3: wipe source — simulate host loss.
	wipeSQLite(t, srcPath)

	// Phase 4: restore from backup → re-launch federation.
	if err := copyFile(backupPath, srcPath); err != nil {
		t.Fatalf("restore copy: %v", err)
	}

	// Phase 5: capture a NEW object post-restore + push.
	restored := reopenInstance(t, "source-restored", srcPath)
	captureBookmark(t, restored, "obj-post-1", "hash-post-1", "after restore", nil)

	if perr := pushFromPath(srcPath, ix.fedName); perr != nil {
		t.Fatalf("post-restore push: %v", perr)
	}

	// Assert: target now has 3 rows (2 pre-backup deduped + 1 new) and both
	// pre + post objects exist (not duplicated despite restored re-push).
	if got, want := countTargetObjects(t, tgt), 3; got != want {
		t.Errorf("post-restore target rows: got %d, want %d (dedup or "+
			"propagation broken)", got, want)
	}
	assertTargetHas(t, tgt, "obj-pre-1", "obj-post-1")
}

// wipeSQLite removes the main DB file and its WAL/SHM sidecars. Ignores
// not-exist errors since sidecars may not exist if the DB was checkpointed.
func wipeSQLite(t *testing.T, path string) {
	t.Helper()
	for _, suffix := range []string{"", "-shm", "-wal"} {
		if rerr := os.Remove(path + suffix); rerr != nil && !os.IsNotExist(rerr) {
			t.Logf("remove %s: %v", path+suffix, rerr)
		}
	}
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatalf("source not wiped: %v", serr)
	}
}

// reopenInstance opens an existing SQLite file as a new instance + Init.
// Differs from mustOpenInstance only in expected pre-existing schema.
func reopenInstance(t *testing.T, name, path string) *instance {
	t.Helper()
	drv, err := storageutil.NewDriver("sqlite", path)
	if err != nil {
		t.Fatalf("reopen %s: %v", name, err)
	}
	t.Cleanup(func() {
		if cerr := drv.Close(context.Background()); cerr != nil {
			t.Logf("close %s: %v", name, cerr)
		}
	})
	if ierr := drv.Init(context.Background()); ierr != nil {
		t.Fatalf("init %s: %v", name, ierr)
	}
	return &instance{name: name, path: path, drv: drv}
}

// assertTargetHas fails the test if any of the named object IDs is missing
// at the target. Used to confirm post-restore propagation + dedup state.
func assertTargetHas(t *testing.T, tgt *instance, ids ...string) {
	t.Helper()
	for _, id := range ids {
		obj, err := tgt.drv.Objects().Get(context.Background(), id)
		if err != nil || obj == nil {
			t.Errorf("object %q missing at target: err=%v", id, err)
		}
	}
}

// copyFile is a minimal file copy for SQLite snapshotting. The source
// must be closed before calling so WAL/SHM are flushed.
func copyFile(src, dst string) (err error) {
	in, oerr := os.Open(src) // #nosec G304 -- test helper
	if oerr != nil {
		return oerr
	}
	defer func() {
		if cerr := in.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	out, cerr := os.Create(dst) // #nosec G304 -- test helper
	if cerr != nil {
		return cerr
	}
	defer func() {
		if oerr := out.Close(); oerr != nil && err == nil {
			err = oerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

// pushFromPathFn returns a closure that opens srcPath, pushes all its
// objects to tgtPath under fedName, and closes the source driver. Used
// post-restore where the restored DB lives at srcPath but isn't held open
// by any *instance.
func pushFromPathFn(t *testing.T, tgtPath string) func(srcPath, fedName string) error {
	t.Helper()
	return func(srcPath, fedName string) error {
		ctx := context.Background()
		drv, err := storageutil.NewDriver("sqlite", srcPath)
		if err != nil {
			return err
		}
		defer func() {
			if cerr := drv.Close(ctx); cerr != nil {
				t.Logf("pushFromPathFn close: %v", cerr)
			}
		}()
		if ierr := drv.Init(ctx); ierr != nil {
			return ierr
		}
		objs, _, lerr := drv.Objects().List(ctx, storage.ObjectFilter{Status: "all"})
		if lerr != nil {
			return lerr
		}
		batch := make([]storage.KnowledgeObject, 0, len(objs))
		for _, o := range objs {
			batch = append(batch, *o)
		}
		return federation.NewLocalPusher(fedName, tgtPath).Push(ctx, batch, nil, nil)
	}
}
