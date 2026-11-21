// Package bosingest stores the records of an external authority (bos) as
// knowledge objects of source "bos", keyed by the record's address, and
// retracts them when the authority deletes or erases the record or the
// service principal can no longer read it. ctxt reads the authority only; it
// never connects to the tools the authority is connected to.
package bosingest

import (
	"context"
	"errors"

	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Source is the source name ingested records carry.
const Source = "bos"

// ErrNoAccess: the authority no longer lets the reading principal read the
// record; the record is retracted.
var ErrNoAccess = errors.New("bosingest: the record is no longer readable")

// Record is the content of a record as the authority's read operation
// returns it.
type Record struct {
	Content  string
	Metadata map[string]any
}

// ContentReader fetches a record from the authority, at the revision of the
// event that announced it, as the service principal ctxt reads under. It
// returns ErrNoAccess when the principal may not read it (any more).
type ContentReader interface {
	Read(ctx context.Context, address string, revision int64) (Record, error)
}

// Sink is a feed sink that ingests and retracts records.
type Sink struct {
	Reader ContentReader
	Store  storage.ObjectStore
	Runner *ingest.Runner
}

// Apply handles one change event: upserted and the rechecking changes
// (refreshed, scope_changed, access_changed, any unknown value) fetch the
// record and store it when it is not held; deleted and erased retract it;
// held and released change nothing.
func (s *Sink) Apply(ctx context.Context, ev bosaccess.Event) error {
	switch ev.Data.Change {
	case "held", "released":
		return nil
	case "deleted", "erased":
		return s.retract(ctx, ev.Subject)
	}
	rec, err := s.Reader.Read(ctx, ev.Subject, ev.Data.Revision)
	if errors.Is(err, ErrUnmapped) {
		return nil
	}
	if errors.Is(err, ErrNoAccess) {
		return s.retract(ctx, ev.Subject)
	}
	if err != nil {
		return err
	}
	meta := map[string]any{"bos_type": bosaccess.RecordTypeKey(ev.Subject), "bos_scope": ev.Data.Scope,
		"bos_sensitivity": ev.Data.Sensitivity}
	for k, v := range rec.Metadata {
		meta[k] = v
	}
	res, err := s.Runner.Run(ctx, one{obj: ingest.Object{
		ID: ev.Subject, SourceKey: ev.Subject, Type: bosaccess.RecordTypeKey(ev.Subject), Content: rec.Content, Metadata: meta,
	}})
	if err != nil {
		return err
	}
	if res.Errors > 0 {
		return errors.New("bosingest: the record could not be stored")
	}
	return nil
}

// retract removes the object held for the address, when there is one.
func (s *Sink) retract(ctx context.Context, address string) error {
	o, err := s.Store.GetBySourceKey(ctx, address, "")
	if err != nil || o == nil {
		return err
	}
	return s.Store.Delete(ctx, o.ID)
}

// one is an adapter that yields one object.
type one struct{ obj ingest.Object }

func (one) Name() string { return Source }

func (o one) Fetch(context.Context) ([]ingest.Object, error) { return []ingest.Object{o.obj}, nil }
