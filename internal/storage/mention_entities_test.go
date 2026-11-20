package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	uri "hop.top/cite/scheme"
)

// resolveStub is an EntityStore whose Resolve returns err and whose
// UpsertThin counts its calls.
type resolveStub struct {
	EntityStore
	err     error
	upserts int
}

func (s *resolveStub) Resolve(context.Context, string) (*Entity, error) { return nil, s.err }
func (s *resolveStub) UpsertThin(context.Context, *Entity) error        { s.upserts++; return nil }

func TestEnsureMentionEntityResolveErrors(t *testing.T) {
	u := uri.URI{Scheme: "ctxt", Namespace: "entity", ID: "a/b"}

	boom := errors.New("db down")
	st := &resolveStub{err: boom}
	if _, _, err := EnsureMentionEntity(context.Background(), st, u, time.Now()); !errors.Is(err, boom) {
		t.Fatalf("real Resolve error: got %v, want it propagated", err)
	}
	if st.upserts != 0 {
		t.Fatalf("a failed lookup wrote %d placeholder(s)", st.upserts)
	}

	st = &resolveStub{err: ErrNotFound}
	e, created, err := EnsureMentionEntity(context.Background(), st, u, time.Now())
	if err != nil || !created || e == nil || st.upserts != 1 {
		t.Fatalf("not found: got (%v, %v, %v), upserts %d; want a created placeholder", e, created, err, st.upserts)
	}
}
