package storagetest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

// BlobDeleteConformance pins how a hard delete treats the blob an
// object's content is externalized to: CountBlobRefs sees every way an
// object refers to a key, and blob.DeleteObject deletes the blob with
// the object unless another object still refers to it. It also deletes
// the object's edges, which no foreign key cascades, and its aliases,
// which one does.
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

	// Blobs put by the subtests list as written long ago, outside the
	// grace period, unless a subtest touches them.
	aged := NewAgedBlobs(drv, time.Now().Add(-48*time.Hour))

	t.Run("delete", func(t *testing.T) {
		put(t, "ownkey")
		put(t, "sharedkey")
		create(t, &storage.KnowledgeObject{ID: "bdel-own", RawContent: "blob://ownkey", Metadata: map[string]any{"blob_key": "ownkey"}})
		create(t, &storage.KnowledgeObject{ID: "bdel-a", RawContent: "blob://sharedkey", Metadata: map[string]any{"blob_key": "sharedkey"}})
		create(t, &storage.KnowledgeObject{ID: "bdel-b", RawContent: "blob://sharedkey", Metadata: map[string]any{"blob_key": "sharedkey"}})

		for _, id := range []string{"bdel-own", "bdel-a"} {
			orphans, err := blob.DeleteObject(ctx, aged, id)
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
		if _, err := blob.DeleteObject(ctx, aged, "bdel-b"); err != nil {
			t.Fatalf("DeleteObject bdel-b: %v", err)
		}
		if exists(t, "sharedkey") {
			t.Error("deleting the last object referring to a blob left the blob behind")
		}
	})

	// An ingest of the content the deleted object holds puts the same
	// key again, then writes its row. Landing between the delete's
	// reference count (0) and its blob delete, it must keep its blob:
	// the put made it young, so the delete leaves it to the sweep.
	t.Run("ingest during delete", func(t *testing.T) {
		put(t, "racekey")
		create(t, &storage.KnowledgeObject{ID: "bdel-race-old", RawContent: "blob://racekey", Metadata: map[string]any{"blob_key": "racekey"}})
		ingested := false
		hooked := countHookDriver{StorageDriver: aged, after: func(key string) {
			if key != "racekey" || ingested {
				return
			}
			ingested = true
			put(t, "racekey")
			aged.Touch("racekey", time.Now())
			create(t, &storage.KnowledgeObject{ID: "bdel-race-new", RawContent: "blob://racekey", Metadata: map[string]any{"blob_key": "racekey"}})
		}}
		orphans, err := blob.DeleteObject(ctx, hooked, "bdel-race-old")
		if err != nil || len(orphans) != 0 {
			t.Fatalf("DeleteObject = %v, %v", orphans, err)
		}
		if !ingested {
			t.Fatal("the delete never counted racekey's references")
		}
		if !exists(t, "racekey") {
			t.Fatal("an ingest landing between the reference count and the blob delete lost its blob: bdel-race-new names a deleted key")
		}
		if _, err := blob.Sweep(ctx, aged, blob.SweepOptions{Now: time.Now().Add(2 * blob.DefaultSweepGrace)}); err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if !exists(t, "racekey") {
			t.Error("the sweep deleted a blob an object refers to")
		}
	})

	// A blob written inside the grace period is not deleted with its
	// last object; the sweep deletes it once it is older.
	t.Run("young blob left to sweep", func(t *testing.T) {
		put(t, "youngkey")
		aged.Touch("youngkey", time.Now())
		create(t, &storage.KnowledgeObject{ID: "bdel-young", RawContent: "blob://youngkey", Metadata: map[string]any{"blob_key": "youngkey"}})
		orphans, err := blob.DeleteObject(ctx, aged, "bdel-young")
		if err != nil || len(orphans) != 0 {
			t.Fatalf("DeleteObject = %v, %v", orphans, err)
		}
		if _, err := drv.Objects().Get(ctx, "bdel-young"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Get after delete: %v, want ErrNotFound", err)
		}
		if !exists(t, "youngkey") {
			t.Fatal("the delete removed a blob written inside the grace period")
		}
		if _, err := blob.Sweep(ctx, aged, blob.SweepOptions{Now: time.Now().Add(2 * blob.DefaultSweepGrace)}); err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if exists(t, "youngkey") {
			t.Error("the sweep left the blob once it was past the grace period")
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

	// Aliases name an object by a foreign key that cascades: the hard
	// delete leaves no alias naming the deleted object, in any scope, and
	// leaves other objects' aliases.
	t.Run("aliases", func(t *testing.T) {
		create(t, &storage.KnowledgeObject{ID: "balias-gone", RawContent: "inline"})
		create(t, &storage.KnowledgeObject{ID: "balias-kept", RawContent: "inline"})
		for _, a := range []*storage.Alias{
			{Alias: "gone-global", ObjectID: "balias-gone", Scope: "global"},
			{Alias: "gone-profile", ObjectID: "balias-gone", Scope: "profile", Profile: "work"},
			{Alias: "kept-global", ObjectID: "balias-kept", Scope: "global"},
		} {
			a.CreatedAt, a.UpdatedAt = fixtureTime(), fixtureTime()
			if err := drv.Aliases().Create(ctx, a); err != nil {
				t.Fatalf("create alias %s: %v", a.Alias, err)
			}
		}
		if _, err := blob.DeleteObject(ctx, drv, "balias-gone"); err != nil {
			t.Fatalf("DeleteObject: %v", err)
		}
		left, err := drv.Aliases().List(ctx, storage.AliasFilter{ObjectID: "balias-gone"})
		if err != nil {
			t.Fatalf("list aliases: %v", err)
		}
		if len(left) != 0 {
			t.Errorf("deleted object still has %d alias(es), first %q", len(left), left[0].Alias)
		}
		for _, alias := range []string{"gone-global", "gone-profile"} {
			if id, err := drv.Aliases().Resolve(ctx, alias, "work"); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Resolve(%s) after delete = %q, %v; want ErrNotFound", alias, id, err)
			}
		}
		if id, err := drv.Aliases().Resolve(ctx, "kept-global", ""); err != nil || id != "balias-kept" {
			t.Errorf("Resolve(kept-global) = %q, %v; want balias-kept", id, err)
		}
	})

	t.Run("missing object", func(t *testing.T) {
		if _, err := blob.DeleteObject(ctx, drv, "bdel-none"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("DeleteObject of a missing object: %v, want ErrNotFound", err)
		}
	})
}
