package storagetest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

// BlobDeleteConformance pins how a hard delete treats the blob an
// object's content is externalized to: CountBlobRefs sees every way an
// object refers to a key, and blob.DeleteObject deletes the blob with
// the object unless another object still refers to it.
func BlobDeleteConformance(t *testing.T, drv storage.StorageDriver) {
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
	create := func(t *testing.T, obj *storage.KnowledgeObject) {
		t.Helper()
		obj.Type = "note"
		obj.CreatedAt, obj.UpdatedAt = fixtureTime(), fixtureTime()
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", obj.ID, err)
		}
	}
	count := func(t *testing.T, key string) int {
		t.Helper()
		n, err := drv.Objects().CountBlobRefs(ctx, key)
		if err != nil {
			t.Fatalf("CountBlobRefs %s: %v", key, err)
		}
		return n
	}

	t.Run("count", func(t *testing.T) {
		create(t, &storage.KnowledgeObject{ID: "bref-raw", RawContent: "blob://cntkey", TextContent: "text"})
		// Create never stores a ref as text; a row written before it
		// did can still carry one, so Update stores it as given.
		textRef := &storage.KnowledgeObject{ID: "bref-text", RawContent: "inline"}
		create(t, textRef)
		textRef.TextContent = "blob://cntkey"
		if err := drv.Objects().Update(ctx, textRef); err != nil {
			t.Fatalf("Update bref-text: %v", err)
		}
		create(t, &storage.KnowledgeObject{ID: "bref-meta", RawContent: "inline", Metadata: map[string]any{"blob_key": "cntkey"}})
		create(t, &storage.KnowledgeObject{ID: "bref-other", RawContent: "blob://cntkeyx", Metadata: map[string]any{"blob_key": "cntkeyx"}})
		if n := count(t, "cntkey"); n != 3 {
			t.Errorf("CountBlobRefs(cntkey) = %d, want 3 (raw, text and metadata refs)", n)
		}
		if n := count(t, "absent"); n != 0 {
			t.Errorf("CountBlobRefs(absent) = %d, want 0", n)
		}
	})

	t.Run("delete", func(t *testing.T) {
		put(t, "ownkey")
		put(t, "sharedkey")
		create(t, &storage.KnowledgeObject{ID: "bdel-own", RawContent: "blob://ownkey", Metadata: map[string]any{"blob_key": "ownkey"}})
		create(t, &storage.KnowledgeObject{ID: "bdel-a", RawContent: "blob://sharedkey", Metadata: map[string]any{"blob_key": "sharedkey"}})
		create(t, &storage.KnowledgeObject{ID: "bdel-b", RawContent: "blob://sharedkey", Metadata: map[string]any{"blob_key": "sharedkey"}})

		for _, id := range []string{"bdel-own", "bdel-a"} {
			orphans, err := blob.DeleteObject(ctx, drv, id)
			if err != nil || len(orphans) != 0 {
				t.Fatalf("DeleteObject %s = %v, %v", id, orphans, err)
			}
			if _, err := drv.Objects().Get(ctx, id); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Get %s after delete: %v, want ErrNotFound", id, err)
			}
		}
		if exists(t, "ownkey") {
			t.Error("deleting the only object referring to a blob left the blob behind")
		}
		if !exists(t, "sharedkey") {
			t.Fatal("deleting one of two objects sharing a blob deleted the blob")
		}
		if _, err := blob.DeleteObject(ctx, drv, "bdel-b"); err != nil {
			t.Fatalf("DeleteObject bdel-b: %v", err)
		}
		if exists(t, "sharedkey") {
			t.Error("deleting the last object referring to a blob left the blob behind")
		}
	})

	t.Run("missing object", func(t *testing.T) {
		if _, err := blob.DeleteObject(ctx, drv, "bdel-none"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("DeleteObject of a missing object: %v, want ErrNotFound", err)
		}
	})
}
