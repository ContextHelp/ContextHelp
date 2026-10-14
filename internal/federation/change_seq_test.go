package federation

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// longAgo is an updated_at older than anything a target has
// acknowledged: what an import, a restore, or a row forwarded from a
// peer carries.
var longAgo = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// objectIDs lists the ids of every object in drv, sorted.
func objectIDs(t *testing.T, drv storage.StorageDriver) []string {
	t.Helper()
	objs, _, err := drv.Objects().List(context.Background(), storage.ObjectFilter{Status: "all"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := make([]string, 0, len(objs))
	for _, o := range objs {
		ids = append(ids, o.ID)
	}
	slices.Sort(ids)
	return ids
}

// openPath opens the SQLite database at path, as a second handle.
func openPath(t *testing.T, path string) storage.StorageDriver {
	t.Helper()
	drv, err := storageutil.NewDriver("sqlite", path, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("init %s: %v", path, err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	return drv
}

// An object stored after the last push with an updated_at older than
// the watermark — an import — is pushed on the next tick.
func TestSync_ImportedObjectWithOldUpdatedAtIsPushed(t *testing.T) {
	src := seedSource(t, "i-1", "i-2")
	_, target := newTestDB(t)
	p := NewLocalPusher("sync-import", target, nil, src.Blobs())
	mustSync(t, src, p, 0)

	imported := makeObject("i-imported", "hash-i-imported")
	imported.CreatedAt, imported.UpdatedAt = longAgo, longAgo
	if err := src.Objects().Create(context.Background(), &imported); err != nil {
		t.Fatal(err)
	}
	if n := mustSync(t, src, p, 0); n != 1 {
		t.Fatalf("sync after an import pushed %d objects, want 1", n)
	}
	if got, want := objectIDs(t, openPath(t, target)), []string{"i-1", "i-2", "i-imported"}; !slices.Equal(got, want) {
		t.Errorf("target holds %v, want %v", got, want)
	}
}

// An edit is pushed again whatever updated_at it stamps, older than
// the watermark included.
func TestSync_EditIsRePushed(t *testing.T) {
	src := seedSource(t, "r-1", "r-2", "r-3")
	spy := &spyPusher{Pusher: NewRemotePusherWithWatermarks("sync-edit", (&receiver{}).serve(t).URL, "", src.Watermarks(), src.Blobs())}
	mustSync(t, src, spy, 0)

	touch(t, src, "r-1", syncBase.Add(time.Minute))
	touch(t, src, "r-3", longAgo)
	if n := mustSync(t, src, spy, 0); n != 2 {
		t.Fatalf("sync after two edits pushed %d objects, want 2", n)
	}
	if got := spy.batches[len(spy.batches)-1]; !slices.Equal(got, []string{"r-1", "r-3"}) {
		t.Errorf("re-pushed %v, want [r-1 r-3] in edit order", got)
	}
}

// A → B → C: objects B receives from A after B last pushed to C reach
// C, though their updated_at is older than anything C acknowledged.
func TestSync_ChainForwardsToNextHop(t *testing.T) {
	ctx := context.Background()
	a := seedSource(t, "a-1", "a-2") // changed at syncBase
	b, bPath := newTestDB(t)
	c, cPath := newTestDB(t)

	own := makeObject("b-own", "hash-b-own") // changed just now, after A's
	if err := b.Objects().Create(ctx, &own); err != nil {
		t.Fatal(err)
	}
	bToC := NewLocalPusher("b-to-c", cPath, nil, b.Blobs())
	if n := mustSync(t, b, bToC, 0); n != 1 {
		t.Fatalf("B → C pushed %d, want b-own", n)
	}

	if n := mustSync(t, a, NewLocalPusher("a-to-b", bPath, nil, a.Blobs()), 0); n != 2 {
		t.Fatalf("A → B pushed %d, want 2", n)
	}
	if n := mustSync(t, b, bToC, 0); n != 2 {
		t.Errorf("B → C after A's push sent %d objects, want A's 2", n)
	}
	if got, want := objectIDs(t, c), []string{"a-1", "a-2", "b-own"}; !slices.Equal(got, want) {
		t.Errorf("C holds %v, want %v", got, want)
	}
}
