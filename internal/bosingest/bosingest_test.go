package bosingest_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/bosingest"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const addr = "bos://core.documents/documents_00000000000000000000000001"

type reader struct {
	rec bosingest.Record
	err error
}

func (r *reader) Read(context.Context, string, int64) (bosingest.Record, error) { return r.rec, r.err }

func event(change string) bosaccess.Event {
	return bosaccess.Event{ID: "1", Subject: addr, Data: bosaccess.EventData{Change: change, Scope: "bos://core.business_scopes/s", Sensitivity: "internal"}}
}

// A record is stored as an object of source bos keyed by its address, and
// retracted when the authority deletes or erases it or the reading principal
// can no longer read it; held and released change nothing.
func TestSinkIngestsAndRetracts(t *testing.T) {
	ctx := context.Background()
	d := storageutil.NewTestDriver(t)
	rd := &reader{rec: bosingest.Record{Content: "engagement letter", Metadata: map[string]any{"title": "Letter"}}}
	s := &bosingest.Sink{Reader: rd, Store: d.Objects(), Runner: ingest.NewRunner(d.Objects())}

	if err := s.Apply(ctx, event("upserted")); err != nil {
		t.Fatal(err)
	}
	o, err := d.Objects().GetBySourceKey(ctx, addr)
	if err != nil || o == nil {
		t.Fatalf("not stored: %v", err)
	}
	if o.Source != bosingest.Source || o.Type != "core.documents" || o.RawContent != "engagement letter" ||
		o.Metadata["bos_scope"] != "bos://core.business_scopes/s" || o.Metadata["title"] != "Letter" {
		t.Fatalf("object %+v", o)
	}
	// the same event again, and a recheck, store nothing more
	for _, ch := range []string{"upserted", "refreshed", "scope_changed"} {
		if err := s.Apply(ctx, event(ch)); err != nil {
			t.Fatal(err)
		}
	}
	for _, ch := range []string{"held", "released"} {
		if err := s.Apply(ctx, event(ch)); err != nil {
			t.Fatal(err)
		}
	}
	if o2, _ := d.Objects().GetBySourceKey(ctx, addr); o2 == nil || o2.ID != o.ID {
		t.Fatal("a recheck or a hold changed the object")
	}

}

// A record the reading principal can no longer read is retracted.
func TestSinkRetractsWhenAccessIsLost(t *testing.T) {
	ctx := context.Background()
	d := storageutil.NewTestDriver(t)
	rd := &reader{rec: bosingest.Record{Content: "engagement letter"}}
	s := &bosingest.Sink{Reader: rd, Store: d.Objects(), Runner: ingest.NewRunner(d.Objects())}
	if err := s.Apply(ctx, event("upserted")); err != nil {
		t.Fatal(err)
	}
	rd.err = bosingest.ErrNoAccess
	if err := s.Apply(ctx, event("access_changed")); err != nil {
		t.Fatal(err)
	}
	if o3, _ := d.Objects().GetBySourceKey(ctx, addr); o3 != nil {
		t.Fatal("record still held after the principal lost access")
	}

}

// erased and deleted retract; retracting what is not held is not an error.
func TestSinkRetractsOnDeleteAndErase(t *testing.T) {
	ctx := context.Background()
	d := storageutil.NewTestDriver(t)
	rd := &reader{rec: bosingest.Record{Content: "engagement letter"}}
	s := &bosingest.Sink{Reader: rd, Store: d.Objects(), Runner: ingest.NewRunner(d.Objects())}
	if err := s.Apply(ctx, event("deleted")); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(ctx, event("upserted")); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []string{"erased", "deleted"} {
		if err := s.Apply(ctx, event(ch)); err != nil {
			t.Fatal(err)
		}
		if o4, _ := d.Objects().GetBySourceKey(ctx, addr); o4 != nil {
			t.Fatalf("%s left the record held", ch)
		}
	}
}
