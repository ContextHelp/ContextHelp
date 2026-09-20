package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The CLI tells a missing record apart from a failed query by matching
// storage.ErrNotFound, and answers the first with a NOT_FOUND envelope
// naming a recovery action. That only holds while the driver's own
// not-found errors wrap the sentinel: a driver that returns a bare
// error instead degrades every missing ref to an uncharacterized
// GENERIC, silently and without failing anything else.

func TestObjectStore_Get_MissingWrapsErrNotFound(t *testing.T) {
	drv := newTestDriver(t)

	_, err := drv.Objects().Get(context.Background(), "obj_deadbeef")
	if err == nil {
		t.Fatal("Get of a missing object returned no error")
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get error %v does not wrap storage.ErrNotFound", err)
	}
}

func TestEntityStore_Resolve_MissingWrapsErrNotFound(t *testing.T) {
	drv := newTestDriver(t)

	_, err := drv.Entities().Resolve(context.Background(), "nosuchentity")
	if err == nil {
		t.Fatal("Resolve of a missing entity returned no error")
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Resolve error %v does not wrap storage.ErrNotFound", err)
	}
}
