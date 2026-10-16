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
	fx := &blobDeleteFx{ctx: context.Background(), drv: drv}
	t.Run("count", func(t *testing.T) { blobDeleteCount(t, fx) })

	// Blobs put by the subtests list as written long ago, outside the
	// grace period, unless a subtest touches them.
	fx.aged = NewAgedBlobs(drv, time.Now().Add(-48*time.Hour))

	t.Run("delete", func(t *testing.T) { blobDeleteLastRef(t, fx) })

	// An ingest of the content the deleted object holds puts the same
	// key again, then writes its row. Landing between the delete's
	// reference count (0) and its blob delete, it must keep its blob:
	// the put made it young, so the delete leaves it to the sweep.
	t.Run("ingest during delete", func(t *testing.T) { blobDeleteIngestDuring(t, fx) })

	// The same ingest landing after the delete has read the blob's age
	// (still old) and before it deletes the blob. Whichever of the two
	// acts last sees the other: the delete counts references again once
	// the blob is gone and puts it back if the ingest's row has landed;
	// the ingest checks its blob once its row is written and puts it back
	// if the delete has removed it (blob.Ensure).
	for _, tc := range []struct {
		name, key string
		// rowFirst lands the ingest's row and check inside the window,
		// before the blob delete; otherwise only its put does, and the
		// row and check follow the delete.
		rowFirst bool
	}{
		{name: "ingest row lands before the blob delete", key: "raceagerow", rowFirst: true},
		{name: "ingest row lands after the blob delete", key: "raceagelate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blobDeleteIngestRace(t, fx, tc.key, tc.rowFirst)
		})
	}

	// A blob written inside the grace period is not deleted with its
	// last object; the sweep deletes it once it is older.
	t.Run("young blob left to sweep", func(t *testing.T) { blobDeleteYoung(t, fx) })

	// No foreign key ties edges to objects: the hard delete removes the
	// object's edges, both directions, and leaves other objects' edges.
	t.Run("edges", func(t *testing.T) { blobDeleteEdges(t, fx) })

	// Aliases name an object by a foreign key that cascades: the hard
	// delete leaves no alias naming the deleted object, in any scope, and
	// leaves other objects' aliases.
	t.Run("aliases", func(t *testing.T) { blobDeleteAliases(t, fx) })

	t.Run("missing object", func(t *testing.T) { blobDeleteMissing(t, fx) })
}

// blobDeleteFx is the state the BlobDeleteConformance subtests share.
type blobDeleteFx struct {
	ctx  context.Context
	drv  storage.StorageDriver
	aged *AgedBlobs // set once "count" has run; see BlobDeleteConformance
}

func (fx *blobDeleteFx) put(t *testing.T, key string) {
	t.Helper()
	if err := fx.drv.Blobs().Put(fx.ctx, key, strings.NewReader("content of "+key), storage.BlobMeta{}); err != nil {
		t.Fatalf("put blob %s: %v", key, err)
	}
}

func (fx *blobDeleteFx) exists(t *testing.T, key string) bool {
	t.Helper()
	ok, err := fx.drv.Blobs().Exists(fx.ctx, key)
	if err != nil {
		t.Fatalf("exists %s: %v", key, err)
	}
	return ok
}

func (fx *blobDeleteFx) create(t *testing.T, obj *storage.KnowledgeObject) {
	t.Helper()
	obj.Type = "note"
	obj.CreatedAt, obj.UpdatedAt = fixtureTime(), fixtureTime()
	if err := fx.drv.Objects().Create(fx.ctx, obj); err != nil {
		t.Fatalf("Create %s: %v", obj.ID, err)
	}
}

func (fx *blobDeleteFx) count(t *testing.T, key string) int {
	t.Helper()
	n, err := fx.drv.Objects().CountBlobRefs(fx.ctx, key)
	if err != nil {
		t.Fatalf("CountBlobRefs %s: %v", key, err)
	}
	return n
}

func blobDeleteCount(t *testing.T, fx *blobDeleteFx) {
	ctx := fx.ctx
	drv := fx.drv
	fx.create(t, &storage.KnowledgeObject{ID: "bref-raw", RawContent: "blob://cntkey", TextContent: "text"})
	// Create never stores a ref as text; a row written before it
	// did can still carry one, so Update stores it as given.
	textRef := &storage.KnowledgeObject{ID: "bref-text", RawContent: "inline"}
	fx.create(t, textRef)
	textRef.TextContent = "blob://cntkey"
	if err := drv.Objects().Update(ctx, textRef); err != nil {
		t.Fatalf("Update bref-text: %v", err)
	}
	fx.create(t, &storage.KnowledgeObject{ID: "bref-meta", RawContent: "inline", Metadata: map[string]any{"blob_key": "cntkey"}})
	fx.create(t, &storage.KnowledgeObject{ID: "bref-other", RawContent: "blob://cntkeyx", Metadata: map[string]any{"blob_key": "cntkeyx"}})
	if n := fx.count(t, "cntkey"); n != 3 {
		t.Errorf("CountBlobRefs(cntkey) = %d, want 3 (raw, text and metadata refs)", n)
	}
	if n := fx.count(t, "absent"); n != 0 {
		t.Errorf("CountBlobRefs(absent) = %d, want 0", n)
	}
}

func blobDeleteLastRef(t *testing.T, fx *blobDeleteFx) {
	ctx := fx.ctx
	drv := fx.drv
	aged := fx.aged
	fx.put(t, "ownkey")
	fx.put(t, "sharedkey")
	fx.create(t, &storage.KnowledgeObject{ID: "bdel-own", RawContent: "blob://ownkey", Metadata: map[string]any{"blob_key": "ownkey"}})
	fx.create(t, &storage.KnowledgeObject{ID: "bdel-a", RawContent: "blob://sharedkey", Metadata: map[string]any{"blob_key": "sharedkey"}})
	fx.create(t, &storage.KnowledgeObject{ID: "bdel-b", RawContent: "blob://sharedkey", Metadata: map[string]any{"blob_key": "sharedkey"}})

	for _, id := range []string{"bdel-own", "bdel-a"} {
		orphans, err := blob.DeleteObject(ctx, aged, id)
		if err != nil || len(orphans) != 0 {
			t.Fatalf("DeleteObject %s = %v, %v", id, orphans, err)
		}
		if _, err := drv.Objects().Get(ctx, id); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Get %s after delete: %v, want ErrNotFound", id, err)
		}
	}
	if fx.exists(t, "ownkey") {
		t.Error("deleting the only object referring to a blob left the blob behind")
	}
	if !fx.exists(t, "sharedkey") {
		t.Fatal("deleting one of two objects sharing a blob deleted the blob")
	}
	if _, err := blob.DeleteObject(ctx, aged, "bdel-b"); err != nil {
		t.Fatalf("DeleteObject bdel-b: %v", err)
	}
	if fx.exists(t, "sharedkey") {
		t.Error("deleting the last object referring to a blob left the blob behind")
	}
}

func blobDeleteIngestDuring(t *testing.T, fx *blobDeleteFx) {
	ctx := fx.ctx
	aged := fx.aged
	fx.put(t, "racekey")
	fx.create(t, &storage.KnowledgeObject{ID: "bdel-race-old", RawContent: "blob://racekey", Metadata: map[string]any{"blob_key": "racekey"}})
	ingested := false
	hooked := countHookDriver{StorageDriver: aged, after: func(key string) {
		if key != "racekey" || ingested {
			return
		}
		ingested = true
		fx.put(t, "racekey")
		aged.Touch("racekey", time.Now())
		fx.create(t, &storage.KnowledgeObject{ID: "bdel-race-new", RawContent: "blob://racekey", Metadata: map[string]any{"blob_key": "racekey"}})
	}}
	orphans, err := blob.DeleteObject(ctx, hooked, "bdel-race-old")
	if err != nil || len(orphans) != 0 {
		t.Fatalf("DeleteObject = %v, %v", orphans, err)
	}
	if !ingested {
		t.Fatal("the delete never counted racekey's references")
	}
	if !fx.exists(t, "racekey") {
		t.Fatal("an ingest landing between the reference count and the blob delete lost its blob: bdel-race-new names a deleted key")
	}
	if _, err := blob.Sweep(ctx, aged, blob.SweepOptions{Now: time.Now().Add(2 * blob.DefaultSweepGrace)}); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !fx.exists(t, "racekey") {
		t.Error("the sweep deleted a blob an object refers to")
	}
}

func blobDeleteIngestRace(t *testing.T, fx *blobDeleteFx, key string, rowFirst bool) {
	ctx := fx.ctx
	drv := fx.drv
	aged := fx.aged
	const content = "content the ingest holds"
	fx.put(t, key)
	fx.create(t, &storage.KnowledgeObject{ID: "bdel-" + key + "-old", RawContent: "blob://" + key, Metadata: map[string]any{"blob_key": key}})
	row := ingested("bdel-"+key+"-new", key, content)
	fired := false
	hooked := listHookDriver{StorageDriver: aged, after: func(prefix string) {
		if prefix != key || fired {
			return
		}
		fired = true
		if err := drv.Blobs().Put(ctx, key, strings.NewReader(content), storage.BlobMeta{}); err != nil {
			t.Fatalf("ingest put: %v", err)
		}
		if rowFirst {
			ingestRow(t, drv, row)
		}
	}}
	orphans, err := blob.DeleteObject(ctx, hooked, "bdel-"+key+"-old")
	if err != nil || len(orphans) != 0 {
		t.Fatalf("DeleteObject = %v, %v", orphans, err)
	}
	if !fired {
		t.Fatal("the delete never read the blob's age")
	}
	if !rowFirst {
		ingestRow(t, drv, row)
	}
	if !fx.exists(t, key) {
		t.Fatalf("an ingest racing the blob delete lost its blob: %s names a deleted key", row.ID)
	}
	if got := blobContent(t, drv.Blobs(), key); got != content {
		t.Errorf("blob content = %q, want %q", got, content)
	}
}

func blobDeleteYoung(t *testing.T, fx *blobDeleteFx) {
	ctx := fx.ctx
	drv := fx.drv
	aged := fx.aged
	fx.put(t, "youngkey")
	aged.Touch("youngkey", time.Now())
	fx.create(t, &storage.KnowledgeObject{ID: "bdel-young", RawContent: "blob://youngkey", Metadata: map[string]any{"blob_key": "youngkey"}})
	orphans, err := blob.DeleteObject(ctx, aged, "bdel-young")
	if err != nil || len(orphans) != 0 {
		t.Fatalf("DeleteObject = %v, %v", orphans, err)
	}
	if _, err := drv.Objects().Get(ctx, "bdel-young"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get after delete: %v, want ErrNotFound", err)
	}
	if !fx.exists(t, "youngkey") {
		t.Fatal("the delete removed a blob written inside the grace period")
	}
	if _, err := blob.Sweep(ctx, aged, blob.SweepOptions{Now: time.Now().Add(2 * blob.DefaultSweepGrace)}); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if fx.exists(t, "youngkey") {
		t.Error("the sweep left the blob once it was past the grace period")
	}
}

func blobDeleteEdges(t *testing.T, fx *blobDeleteFx) {
	ctx := fx.ctx
	drv := fx.drv
	fx.create(t, &storage.KnowledgeObject{ID: "bedge-gone", RawContent: "inline"})
	fx.create(t, &storage.KnowledgeObject{ID: "bedge-kept", RawContent: "inline"})
	fx.create(t, &storage.KnowledgeObject{ID: "bedge-other", RawContent: "inline"})
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
}

func blobDeleteAliases(t *testing.T, fx *blobDeleteFx) {
	ctx := fx.ctx
	drv := fx.drv
	fx.create(t, &storage.KnowledgeObject{ID: "balias-gone", RawContent: "inline"})
	fx.create(t, &storage.KnowledgeObject{ID: "balias-kept", RawContent: "inline"})
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
}

func blobDeleteMissing(t *testing.T, fx *blobDeleteFx) {
	ctx := fx.ctx
	drv := fx.drv
	if _, err := blob.DeleteObject(ctx, drv, "bdel-none"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("DeleteObject of a missing object: %v, want ErrNotFound", err)
	}
}

// ingestRow writes row, then checks its blob the way ingest does.
func ingestRow(t *testing.T, drv storage.StorageDriver, row *storage.KnowledgeObject) {
	t.Helper()
	ctx := context.Background()
	if err := drv.Objects().Create(ctx, row); err != nil {
		t.Fatalf("ingest Create %s: %v", row.ID, err)
	}
	if err := blob.Ensure(ctx, drv.Blobs(), row); err != nil {
		t.Fatalf("ingest Ensure %s: %v", row.ID, err)
	}
}
