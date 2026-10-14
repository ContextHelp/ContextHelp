package storagetest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

// BlobUpdateConformance pins how an update treats the blob the stored
// content was externalized to: blob.UpdateObject writes the row, then
// deletes each blob the row referred to that the new content does not
// and no other object refers to. A blob that can't be deleted is
// returned as an orphan, with the row updated.
func BlobUpdateConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	put := func(t *testing.T, key string) {
		t.Helper()
		if err := drv.Blobs().Put(ctx, key, strings.NewReader("content of "+key), storage.BlobMeta{}); err != nil {
			t.Fatalf("put blob %s: %v", key, err)
		}
	}
	exists := func(t *testing.T, key string) bool {
		t.Helper()
		ok, err := drv.Blobs().Exists(ctx, key)
		if err != nil {
			t.Fatalf("exists %s: %v", key, err)
		}
		return ok
	}
	externalized := func(id, key string) *storage.KnowledgeObject {
		return &storage.KnowledgeObject{
			ID: id, Type: "note", RawContent: "blob://" + key, TextContent: "prefix of " + key,
			Metadata:  map[string]any{"blob_key": key},
			CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
		}
	}
	create := func(t *testing.T, obj *storage.KnowledgeObject) {
		t.Helper()
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", obj.ID, err)
		}
	}
	update := func(t *testing.T, d storage.StorageDriver, obj *storage.KnowledgeObject) []blob.Orphan {
		t.Helper()
		orphans, err := blob.UpdateObject(ctx, d, obj)
		if err != nil {
			t.Fatalf("UpdateObject %s: %v", obj.ID, err)
		}
		got, err := drv.Objects().Get(ctx, obj.ID)
		if err != nil {
			t.Fatalf("Get %s: %v", obj.ID, err)
		}
		if got.RawContent != obj.RawContent {
			t.Fatalf("stored RawContent = %q, want %q", got.RawContent, obj.RawContent)
		}
		return orphans
	}

	t.Run("replaced", func(t *testing.T) {
		put(t, "updold")
		put(t, "updnew")
		create(t, externalized("bupd-replace", "updold"))
		if orphans := update(t, drv, externalized("bupd-replace", "updnew")); len(orphans) != 0 {
			t.Fatalf("orphans = %v", orphans)
		}
		if exists(t, "updold") {
			t.Error("replacing externalized content left the old blob behind")
		}
		if !exists(t, "updnew") {
			t.Error("update deleted the blob its new content refers to")
		}
	})

	t.Run("inlined", func(t *testing.T) {
		put(t, "updinline")
		create(t, externalized("bupd-inline", "updinline"))
		if orphans := update(t, drv, &storage.KnowledgeObject{
			ID: "bupd-inline", Type: "note", RawContent: "small now",
			CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
		}); len(orphans) != 0 {
			t.Fatalf("orphans = %v", orphans)
		}
		if exists(t, "updinline") {
			t.Error("inlining the content left the old blob behind")
		}
	})

	t.Run("unchanged", func(t *testing.T) {
		put(t, "updsame")
		obj := externalized("bupd-same", "updsame")
		create(t, obj)
		obj.Summaries = []string{"edited"}
		update(t, drv, obj)
		if !exists(t, "updsame") {
			t.Error("an update that kept the content deleted its blob")
		}
	})

	t.Run("shared", func(t *testing.T) {
		put(t, "updshared")
		create(t, externalized("bupd-shared-a", "updshared"))
		create(t, externalized("bupd-shared-b", "updshared"))
		update(t, drv, &storage.KnowledgeObject{
			ID: "bupd-shared-a", Type: "note", RawContent: "small now",
			CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
		})
		if !exists(t, "updshared") {
			t.Error("update deleted a blob another object refers to")
		}
	})

	t.Run("orphan", func(t *testing.T) {
		put(t, "updstuck")
		create(t, externalized("bupd-stuck", "updstuck"))
		failing := failingDeleteDriver{StorageDriver: drv}
		orphans := update(t, failing, &storage.KnowledgeObject{
			ID: "bupd-stuck", Type: "note", RawContent: "small now",
			CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
		})
		if len(orphans) != 1 || orphans[0].Key != "updstuck" || !errors.Is(orphans[0].Err, errDeleteRefused) {
			t.Errorf("orphans = %v, want [updstuck: %v]", orphans, errDeleteRefused)
		}
		if !exists(t, "updstuck") {
			t.Error("the blob a failed delete reported is gone")
		}
	})

	t.Run("missing object", func(t *testing.T) {
		if _, err := blob.UpdateObject(ctx, drv, externalized("bupd-none", "updnone")); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("UpdateObject of a missing object: %v, want ErrNotFound", err)
		}
	})
}

var errDeleteRefused = errors.New("blob delete refused")

// failingDeleteDriver is drv with a blob store whose Delete fails.
type failingDeleteDriver struct{ storage.StorageDriver }

func (d failingDeleteDriver) Blobs() storage.BlobStore {
	return failingDeleteStore{BlobStore: d.StorageDriver.Blobs()}
}

type failingDeleteStore struct{ storage.BlobStore }

func (failingDeleteStore) Delete(context.Context, string) error { return errDeleteRefused }
