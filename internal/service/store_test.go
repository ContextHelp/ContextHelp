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

// Store(ctx) hands the owner the driver itself and anyone else, a
// missing viewer included, a driver that refuses reads.
func TestStoreScopesByContextViewer(t *testing.T) {
	svc := newTestService(t)
	if got := svc.Store(ownerCtx()); got != svc.store {
		t.Fatalf("Store(owner) = %T, want the driver itself", got)
	}
	for name, ctx := range map[string]context.Context{
		"no viewer":   context.Background(),
		"zero viewer": sharing.WithViewer(context.Background(), sharing.Viewer{}),
	} {
		_, _, err := svc.ListObjects(ctx, storage.ObjectFilter{Limit: 1})
		if !errors.Is(err, storage.ErrRestrictedViewer) {
			t.Fatalf("%s: ListObjects err = %v, want ErrRestrictedViewer", name, err)
		}
		if _, err := svc.GetJob(ctx, "any"); !errors.Is(err, storage.ErrRestrictedViewer) {
			t.Fatalf("%s: GetJob err = %v, want ErrRestrictedViewer (job reads must not bypass the scoped driver)", name, err)
		}
		if _, _, err := svc.SearchObjects(ctx, "type==note", 10, 0); !errors.Is(err, storage.ErrRestrictedViewer) {
			t.Fatalf("%s: SearchObjects err = %v, want ErrRestrictedViewer (the engine reads the scoped store)", name, err)
		}
	}
}
