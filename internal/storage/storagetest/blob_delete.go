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
// the object unless another object still refers to it. It also deletes
// the object's edges, which no foreign key cascades.
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

	// No foreign key ties edges to objects: the hard delete removes the
	// object's edges, both directions, and leaves other objects' edges.
	t.Run("edges", func(t *testing.T) {
		create(t, &storage.KnowledgeObject{ID: "bedge-gone", RawContent: "inline"})
		create(t, &storage.KnowledgeObject{ID: "bedge-kept", RawContent: "inline"})
		create(t, &storage.KnowledgeObject{ID: "bedge-other", RawContent: "inline"})
		for _, e := range []*storage.Edge{
			{ID: "bedge-out", FromType: "object", FromID: "bedge-gone", ToType: "object", ToID: "bedge-kept", EdgeType: "links"},
			{ID: "bedge-in", FromType: "object", FromID: "bedge-kept", ToType: "object", ToID: "bedge-gone", EdgeType: "links"},
			{ID: "bedge-unrelated", FromType: "object", FromID: "bedge-kept", ToType: "object", ToID: "bedge-other", EdgeType: "links"},
		} {
			e.Weight, e.CreatedAt = 1, fixtureTime()
			if err := drv.Edges().Create(ctx, e); err != nil {
				t.Fatalf("create edge %s: %v", e.ID, err)
			}
		}
		if _, err := blob.DeleteObject(ctx, drv, "bedge-gone"); err != nil {
			t.Fatalf("DeleteObject: %v", err)
		}
		for _, list := range []func() ([]*storage.Edge, error){
			func() ([]*storage.Edge, error) { return drv.Edges().ListFrom(ctx, "object", "bedge-gone") },
			func() ([]*storage.Edge, error) { return drv.Edges().ListTo(ctx, "object", "bedge-gone") },
		} {
			edges, err := list()
			if err != nil {
				t.Fatalf("list edges: %v", err)
			}
			if len(edges) != 0 {
				t.Errorf("deleted object still has %d edge(s): %s", len(edges), edges[0].ID)
			}
		}
		kept, err := drv.Edges().ListFrom(ctx, "object", "bedge-kept")
		if err != nil {
			t.Fatalf("list edges: %v", err)
		}
		if len(kept) != 1 || kept[0].ID != "bedge-unrelated" {
			t.Errorf("edges of other objects = %v, want [bedge-unrelated]", kept)
		}
	})

	t.Run("missing object", func(t *testing.T) {
		if _, err := blob.DeleteObject(ctx, drv, "bdel-none"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("DeleteObject of a missing object: %v, want ErrNotFound", err)
		}
	})
}
