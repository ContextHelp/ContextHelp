package storagetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// scopedViewer is a storage.Viewer with a fixed answer: unrestricted,
// or restricted to predicate (? placeholders) as principal with every
// capability unless entities is false.
type scopedViewer struct {
	unrestricted bool
	predicate    string
	args         []any
	principal    string
	noEntities   bool
}

func (v scopedViewer) IsUnrestricted() bool                   { return v.unrestricted }
func (v scopedViewer) PrincipalID() string                    { return v.principal }
func (v scopedViewer) ObjectPredicate(string) (string, []any) { return v.predicate, v.args }
func (v scopedViewer) Allows(c string) bool {
	return c != storage.CapabilityEntities || !v.noEntities
}

// Viewers the suites use: the owner, a restricted viewer no grant
// applies to (every read answers nothing) and one that sees objects of
// a type.
var (
	viewerUnrestricted = scopedViewer{unrestricted: true}
	viewerNothing      = scopedViewer{predicate: "(1 = 0)", principal: "prn_nothing"}
)

// viewerOfType sees the active objects of type typ.
func viewerOfType(typ, principal string) scopedViewer {
	return scopedViewer{predicate: "type = ?", args: []any{typ}, principal: principal}
}

// ScopedDriverConformance pins storage.ScopedDriver's contract over drv
// (a fresh database): an unrestricted viewer reads the driver itself,
// results included; a nil viewer's every read fails closed with
// ErrRestrictedViewer and no rows; a restricted viewer's refused reads
// (storage.RestrictedReads) fail closed the same way while its scoped
// reads answer only what its predicate selects (a hidden row is
// ErrNotFound, exactly as a missing one) and its writes reach the
// driver. RestrictedViewerConformance pins what the scoped reads
// answer.
func ScopedDriverConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	owner := &storage.KnowledgeObject{
		ID: "scoped-owner-note", Type: "note", Status: storage.ObjectStatusActive,
		RawContent: "scoped conformance note", ContentHash: "sha256:scoped-owner-note",
		Tags: []storage.Tag{{Label: "scoped"}}, CreatedAt: now, UpdatedAt: now,
	}
	if err := drv.Objects().Create(ctx, owner); err != nil {
		t.Fatalf("seed object: %v", err)
	}
	entity := &storage.Entity{Slug: "scoped/entity", Title: "Scoped entity", Namespace: "scoped", CreatedAt: now, UpdatedAt: now}
	if err := drv.Entities().Upsert(ctx, entity); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
	edge := &storage.Edge{
		ID: "scoped-mention", FromType: "object", FromID: owner.ID,
		ToType: storage.EdgeEndpointEntity, ToID: "ctxt://entity/" + entity.Slug,
		EdgeType: "mentions", Weight: 1, CreatedAt: now,
	}
	if err := drv.Edges().Create(ctx, edge); err != nil {
		t.Fatalf("seed edge: %v", err)
	}

	t.Run("unrestricted viewer reads the driver itself", func(t *testing.T) {
		scopedUnrestrictedIsTheDriver(t, drv, owner.ID)
	})
	t.Run("nil viewer fails every read closed", func(t *testing.T) {
		scoped := storage.ScopedDriver(nil, drv)
		for _, table := range []map[string][]string{storage.RestrictedReads(), storage.ScopedReads(), storage.PrincipalReads()} {
			restrictedReadsFailClosed(t, scoped, table)
		}
	})
	t.Run("restricted viewer fails every refused read closed", func(t *testing.T) {
		restrictedReadsFailClosed(t, storage.ScopedDriver(viewerNothing, drv), storage.RestrictedReads())
	})
	t.Run("restricted viewer writes reach the driver", func(t *testing.T) {
		scopedRestrictedWritesLand(t, drv, now)
	})
	t.Run("restricted viewer loses optional capabilities", func(t *testing.T) {
		scoped := storage.ScopedDriver(viewerNothing, drv)
		if _, ok := scoped.Objects().(storage.ProjectionStore); ok {
			t.Fatal("restricted Objects must not expose ProjectionStore")
		}
		if _, ok := scoped.Embeddings().(storage.EmbeddingReader); ok {
			t.Fatal("restricted Embeddings must not expose EmbeddingReader")
		}
	})
}

// scopedUnrestrictedIsTheDriver checks an unrestricted viewer gets drv
// itself and reads the seeded object and its edges through it exactly
// as drv answers them.
func scopedUnrestrictedIsTheDriver(t *testing.T, drv storage.StorageDriver, ownerID string) {
	t.Helper()
	ctx := context.Background()
	scoped := storage.ScopedDriver(viewerUnrestricted, drv)
	if scoped != drv {
		t.Fatalf("ScopedDriver(unrestricted) = %T, want the driver", scoped)
	}
	got, err := scoped.Objects().Get(ctx, ownerID)
	if err != nil {
		t.Fatalf("Get through scoped: %v", err)
	}
	want, err := drv.Objects().Get(ctx, ownerID)
	if err != nil {
		t.Fatalf("Get through driver: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped Get = %+v, driver Get = %+v", got, want)
	}
	gotEdges, err := scoped.Edges().ListFrom(ctx, "object", ownerID)
	if err != nil {
		t.Fatalf("ListFrom through scoped: %v", err)
	}
	wantEdges, err := drv.Edges().ListFrom(ctx, "object", ownerID)
	if err != nil {
		t.Fatalf("ListFrom through driver: %v", err)
	}
	if !reflect.DeepEqual(gotEdges, wantEdges) {
		t.Fatalf("scoped ListFrom = %+v, driver ListFrom = %+v", gotEdges, wantEdges)
	}
}

// scopedRestrictedWritesLand checks a restricted viewer's writes reach
// drv while its reads of the same rows, outside its predicate, answer
// as if the rows did not exist (ADR-080 §4: hidden means absent).
func scopedRestrictedWritesLand(t *testing.T, drv storage.StorageDriver, now time.Time) {
	t.Helper()
	ctx := context.Background()
	scoped := storage.ScopedDriver(viewerNothing, drv)
	written := &storage.KnowledgeObject{
		ID: "scoped-restricted-write", Type: "note", Status: storage.ObjectStatusActive,
		RawContent: "written through a restricted driver", ContentHash: "sha256:scoped-restricted-write",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := scoped.Objects().Create(ctx, written); err != nil {
		t.Fatalf("Create through restricted: %v", err)
	}
	if _, err := drv.Objects().Get(ctx, written.ID); err != nil {
		t.Fatalf("the restricted write did not reach the driver: %v", err)
	}
	if _, err := scoped.Objects().Get(ctx, written.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get of the own write outside the predicate = %v, want ErrNotFound", err)
	}
	alias := &storage.Alias{Alias: "scoped-alias", ObjectID: written.ID, Scope: "global", CreatedAt: now, UpdatedAt: now}
	if err := scoped.Aliases().Create(ctx, alias); err != nil {
		t.Fatalf("alias Create through restricted: %v", err)
	}
	if id, err := drv.Aliases().Resolve(ctx, alias.Alias, ""); err != nil || id != written.ID {
		t.Fatalf("alias Resolve through driver = (%q, %v), want %q", id, err, written.ID)
	}
	if _, err := scoped.Aliases().Resolve(ctx, alias.Alias, ""); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("alias Resolve of a hidden object through restricted = %v, want ErrNotFound", err)
	}
}

// restrictedReadsFailClosed calls every read of table on scoped, with
// zero arguments, and checks each refuses with ErrRestrictedViewer and
// returns nothing else.
func restrictedReadsFailClosed(t *testing.T, scoped storage.StorageDriver, table map[string][]string) {
	t.Helper()
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	sv := reflect.ValueOf(scoped)
	for accessor, reads := range table {
		store := sv.MethodByName(accessor).Call(nil)[0]
		if store.IsNil() {
			t.Errorf("%s: scoped accessor returned nil", accessor)
			continue
		}
		for _, name := range reads {
			m, ok := store.Type().MethodByName(name)
			if !ok {
				t.Errorf("%s.%s: not a method of the scoped store", accessor, name)
				continue
			}
			args := make([]reflect.Value, 0, m.Type.NumIn())
			for i := 0; i < m.Type.NumIn(); i++ {
				if m.Type.In(i) == ctxType {
					args = append(args, reflect.ValueOf(context.Background()))
				} else {
					args = append(args, reflect.Zero(m.Type.In(i)))
				}
			}
			out := store.MethodByName(name).Call(args)
			err, _ := out[len(out)-1].Interface().(error)
			if !errors.Is(err, storage.ErrRestrictedViewer) {
				t.Errorf("%s.%s: err = %v, want ErrRestrictedViewer", accessor, name, err)
			}
			for i, v := range out[:len(out)-1] {
				if !v.IsZero() {
					t.Errorf("%s.%s: result %d = %v, want nothing on a refused read", accessor, name, i, v.Interface())
				}
			}
		}
	}
}
