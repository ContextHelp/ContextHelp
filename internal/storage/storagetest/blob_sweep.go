package storagetest

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

// BlobSweepConformance pins the orphan-blob sweep: blob.Sweep lists the
// blob store and deletes each blob no object refers to that is older
// than the grace period. A younger one may belong to an ingest that has
// put its blob and not yet written its row, so it is kept; so is one of
// unknown age. A dry run deletes nothing and reports the same blobs.
func BlobSweepConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	const grace = time.Hour
	ages := map[string]time.Time{
		"swpreferenced": now.Add(-48 * time.Hour),
		"swpmetaref":    now.Add(-48 * time.Hour),
		"swporphan":     now.Add(-2 * time.Hour),
		"swpyoung":      now.Add(-time.Minute),
		"swpunknown":    {},
	}
	seedSweep(t, drv, ages)
	aged := agedDriver{StorageDriver: drv, ages: ages}
	exists := func(t *testing.T, key string) bool {
		t.Helper()
		return blobExists(t, drv, key)
	}
	check := func(t *testing.T, res blob.SweepResult) {
		t.Helper()
		checkSweep(t, res, len(ages))
	}

	t.Run("dry run", func(t *testing.T) {
		res, err := blob.Sweep(ctx, aged, blob.SweepOptions{Grace: grace, Now: now, DryRun: true})
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		check(t, res)
		for key := range ages {
			if !exists(t, key) {
				t.Errorf("dry run deleted %s", key)
			}
		}
	})
	t.Run("sweep", func(t *testing.T) {
		res, err := blob.Sweep(ctx, aged, blob.SweepOptions{Grace: grace, Now: now})
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		check(t, res)
		for key := range ages {
			if want := key != "swporphan"; exists(t, key) != want {
				t.Errorf("after sweep, %s exists = %v, want %v", key, !want, want)
			}
		}
	})
	t.Run("delete failure", func(t *testing.T) {
		if err := drv.Blobs().Put(ctx, "swporphan", strings.NewReader("again"), storage.BlobMeta{}); err != nil {
			t.Fatalf("put: %v", err)
		}
		res, err := blob.Sweep(ctx, failingDeleteDriver{StorageDriver: aged}, blob.SweepOptions{Grace: grace, Now: now})
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if len(res.Deleted) != 0 || len(res.Failed) != 1 || res.Failed[0].Key != "swporphan" {
			t.Errorf("Deleted = %v, Failed = %v, want swporphan failed", res.Deleted, res.Failed)
		}
	})

	// An ingest of an old orphan's content landing after the sweep has
	// listed it (old) and counted its references (none), before the
	// sweep deletes it: the sweep puts it back once it finds the
	// ingest's row, or the ingest does once it finds its blob gone.
	for _, tc := range []struct {
		name, key string
		rowFirst  bool // the ingest's row and check land before the delete
	}{
		{name: "ingest row lands before the blob delete", key: "swpracerow", rowFirst: true},
		{name: "ingest row lands after the blob delete", key: "swpracelate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const content = "content the ingest holds"
			if err := drv.Blobs().Put(ctx, tc.key, strings.NewReader(content), storage.BlobMeta{}); err != nil {
				t.Fatalf("put: %v", err)
			}
			row := ingested("swp-"+tc.key, tc.key, content)
			fired := false
			hooked := countHookDriver{StorageDriver: NewAgedBlobs(drv, now.Add(-48*time.Hour)), after: func(key string) {
				if key != tc.key || fired {
					return
				}
				fired = true
				if err := drv.Blobs().Put(ctx, tc.key, strings.NewReader(content), storage.BlobMeta{}); err != nil {
					t.Fatalf("ingest put: %v", err)
				}
				if tc.rowFirst {
					ingestRow(t, drv, row)
				}
			}}
			res, err := blob.Sweep(ctx, hooked, blob.SweepOptions{Grace: grace, Now: now})
			if err != nil {
				t.Fatalf("Sweep: %v", err)
			}
			if !fired {
				t.Fatal("the sweep never counted the blob's references")
			}
			if !tc.rowFirst {
				ingestRow(t, drv, row)
			}
			if !exists(t, tc.key) {
				t.Fatalf("an ingest racing the sweep lost its blob: %s names a deleted key", row.ID)
			}
			if got := blobContent(t, drv.Blobs(), tc.key); got != content {
				t.Errorf("blob content = %q, want %q", got, content)
			}
			if tc.rowFirst && slices.Contains(res.Deleted, tc.key) {
				t.Errorf("Deleted = %v lists %s, which the sweep put back", res.Deleted, tc.key)
			}
		})
	}
}

// agedDriver is drv with a blob store that lists each blob with the
// age in ages instead of its real modification time.
type agedDriver struct {
	storage.StorageDriver
	ages map[string]time.Time
}

func (d agedDriver) Blobs() storage.BlobStore {
	return agedStore{BlobStore: d.StorageDriver.Blobs(), ages: d.ages}
}

type agedStore struct {
	storage.BlobStore
	ages map[string]time.Time
}

func (s agedStore) List(ctx context.Context, prefix string) ([]storage.BlobInfo, error) {
	items, err := s.BlobStore.List(ctx, prefix)
	for i := range items {
		items[i].UpdatedAt = s.ages[items[i].Key]
	}
	return items, err
}

// seedSweep puts a blob per key of ages and the objects referring to
// swpreferenced (by raw ref) and swpmetaref (by metadata.blob_key).
func seedSweep(t *testing.T, drv storage.StorageDriver, ages map[string]time.Time) {
	t.Helper()
	ctx := context.Background()
	for key := range ages {
		if err := drv.Blobs().Put(ctx, key, strings.NewReader("content of "+key), storage.BlobMeta{}); err != nil {
			t.Fatalf("put blob %s: %v", key, err)
		}
	}
	for _, obj := range []*storage.KnowledgeObject{
		{ID: "swp-raw", RawContent: "blob://swpreferenced", TextContent: "prefix"},
		{ID: "swp-meta", RawContent: "inline", Metadata: map[string]any{"blob_key": "swpmetaref"}},
	} {
		obj.Type = "note"
		obj.CreatedAt, obj.UpdatedAt = fixtureTime(), fixtureTime()
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", obj.ID, err)
		}
	}
}

// checkSweep checks a sweep of the seedSweep store: only swporphan is
// deleted, swpyoung and swpunknown are kept as young.
func checkSweep(t *testing.T, res blob.SweepResult, scanned int) {
	t.Helper()
	if res.Scanned != scanned {
		t.Errorf("Scanned = %d, want %d", res.Scanned, scanned)
	}
	if !slices.Equal(res.Deleted, []string{"swporphan"}) {
		t.Errorf("Deleted = %v, want [swporphan]", res.Deleted)
	}
	if res.Young != 2 {
		t.Errorf("Young = %d, want 2 (swpyoung, swpunknown)", res.Young)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %v", res.Failed)
	}
}

func blobExists(t *testing.T, drv storage.StorageDriver, key string) bool {
	t.Helper()
	ok, err := drv.Blobs().Exists(context.Background(), key)
	if err != nil {
		t.Fatalf("exists %s: %v", key, err)
	}
	return ok
}
