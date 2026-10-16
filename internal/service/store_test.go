package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ownerCtx is the context the tests act under: the owner's, unrestricted.
// A bare context carries no viewer and fails every read closed.
func ownerCtx() context.Context {
	return sharing.WithViewer(context.Background(), sharing.Unrestricted)
}

// Store(ctx) hands the owner the driver itself; a context with no
// viewer gets a driver that refuses every read (a forgotten attach is
// loud); the zero viewer, which an unknown or grant-less principal
// resolves to, reads through the scoped driver and sees nothing, while
// the owner-only stores still refuse it.
func TestStoreScopesByContextViewer(t *testing.T) {
	svc := newTestService(t)
	if got := svc.Store(ownerCtx()); got != svc.store {
		t.Fatalf("Store(owner) = %T, want the driver itself", got)
	}
	obj := &storage.KnowledgeObject{ID: "store-scope-note", Type: "note", Status: storage.ObjectStatusActive, RawContent: "scoped"}
	if err := svc.store.Objects().Create(ownerCtx(), obj); err != nil {
		t.Fatal(err)
	}

	bare := context.Background()
	if _, _, err := svc.ListObjects(bare, storage.ObjectFilter{Limit: 1}); !errors.Is(err, storage.ErrRestrictedViewer) {
		t.Fatalf("no viewer: ListObjects err = %v, want ErrRestrictedViewer", err)
	}
	if _, _, err := svc.SearchObjects(bare, "type==note", 10, 0); !errors.Is(err, storage.ErrRestrictedViewer) {
		t.Fatalf("no viewer: SearchObjects err = %v, want ErrRestrictedViewer (the engine reads the scoped store)", err)
	}

	zero := sharing.WithViewer(context.Background(), sharing.Viewer{})
	objs, total, err := svc.ListObjects(zero, storage.ObjectFilter{Limit: 1})
	if err != nil || len(objs) != 0 || total != 0 {
		t.Fatalf("zero viewer: ListObjects = %d objects, total %d, err %v; want nothing", len(objs), total, err)
	}
	if _, err := svc.GetObject(zero, obj.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("zero viewer: GetObject err = %v, want ErrNotFound (hidden means absent)", err)
	}
	objs, total, err = svc.SearchObjects(zero, "type==note", 10, 0)
	if err != nil || len(objs) != 0 || total != 0 {
		t.Fatalf("zero viewer: SearchObjects = %d objects, total %d, err %v; want nothing", len(objs), total, err)
	}

	for name, ctx := range map[string]context.Context{"no viewer": bare, "zero viewer": zero} {
		if _, err := svc.GetJob(ctx, "any"); !errors.Is(err, storage.ErrRestrictedViewer) {
			t.Fatalf("%s: GetJob err = %v, want ErrRestrictedViewer (jobs are owner-only)", name, err)
		}
	}
}
