package bosaccess

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const (
	tScope  = "bos://core.business_scopes/business_scopes_00000000000000000000000001"
	tPerson = "bos://core.principals/principals_00000000000000000000000001"
	tEng    = "bos://projects.engagements/engagements_00000000000000000000000001"
	tDoc    = "bos://core.documents/documents_00000000000000000000000001"
)

var t0 = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

func str(s string) *string { return &s }

func baseSet(epoch int64, entries ...RecordEntry) Set {
	s := Set{
		Contract: Contract, Epoch: epoch, Mode: ModeFull, Scope: tScope, GeneratedAt: t0.Format(time.RFC3339),
		Policy:       Policy{MaxStalenessSeconds: 86400, ReconcileIntervalSeconds: 3600, RecordBasisMaxStalenessSeconds: 3600},
		Scopes:       []Scope{{ID: tScope}},
		Principals:   []Principal{{ID: tPerson, Kind: "person", Status: "active"}},
		RecordAccess: entries,
	}
	s.Normalize()
	s.Digest = ContentDigest(s)
	return s
}

func entry(id, via string) RecordEntry {
	return RecordEntry{ID: id, Subject: tPerson, Anchor: tEng, Scope: tScope, TypeKey: str("core.documents"), Via: via, Grant: "g"}
}

func docEvent(under ...string) Event {
	return Event{Subject: tDoc, Data: EventData{Change: "upserted", Scope: tScope, Sensitivity: "internal", Under: under}}
}

// The declared profile lists the feature and the shapes entries need, and
// no type grant: a grant is one ctxt does not enforce and says so.
func TestDeclaredProfile(t *testing.T) {
	p := DeclaredProfile("1.2.3")
	if !p.HasFeature(Contract, FeatureRecordAccess) || !p.Supports(FeatureRecordAccess) {
		t.Fatalf("the feature is not declared: %+v", p.Contracts)
	}
	for _, s := range []string{ShapeRowRelation, ShapeRecordGrant} {
		if !p.Expresses(s) {
			t.Errorf("shape %s not declared", s)
		}
	}
	for _, s := range []string{ShapeTypeGrant, ShapeOperation} {
		if p.Expresses(s) {
			t.Errorf("shape %s declared; ctxt does not enforce it", s)
		}
	}
}

// What ctxt cannot enforce it denies and names in the acknowledgement: an
// entry with a property it does not know (by entry id), a grant of a shape it
// does not list and an operation grant (by id).
func TestAckNamesWhatIsNotRepresentable(t *testing.T) {
	p, err := Open(DeclaredProfile("t"), nil, func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	var unknown RecordEntry
	raw := []byte(`{"id":"rec_unknown","subject":"` + tPerson + `","anchor":"` + tEng + `","scope":"` + tScope + `","type_key":"core.documents","via":"assignment","grant":"g","from_the_future":1}`)
	if err := json.Unmarshal(raw, &unknown); err != nil {
		t.Fatal(err)
	}
	s := baseSet(1, entry("rec_ok", ViaAssignment), unknown)
	s.Grants = []Grant{{ID: "grant_type", Subject: tPerson, Target: Target{Scope: tScope}, Permission: "read"}}
	s.Operations = []Operation{{ID: "op_1", Subject: tPerson, Scope: tScope, Operation: "core.Run"}}
	s.Digest = ContentDigest(s)
	ack, err := p.Apply(s)
	if err != nil {
		t.Fatal(err)
	}
	var grants []string
	for _, u := range ack.Unrepresentable {
		grants = append(grants, u.Grant)
	}
	if !slices.Equal(grants, []string{"grant_type", "op_1"}) {
		t.Errorf("unrepresentable grants = %v", grants)
	}
	if ack.RecordAccess == nil || ack.RecordAccess.Count != 2 || len(ack.RecordAccess.Unrepresentable) != 1 || ack.RecordAccess.Unrepresentable[0].Entry != "rec_unknown" {
		t.Errorf("record access ack = %+v", ack.RecordAccess)
	}
	if err := Validate(SchemaAck, mustJSON(ack)); err != nil {
		t.Errorf("ack is not a valid document: %v", err)
	}
	// the entry it cannot express reads nothing, the one it can reads
	for _, e := range []Event{docEvent(tEng)} {
		if err := p.Consume(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.CatchUp(); err != nil {
		t.Fatal(err)
	}
	if got := p.Readable(tPerson); !slices.Equal(got, []string{tDoc}) {
		t.Errorf("readable = %v", got)
	}
}

// A feature-less profile represents no entry: every entry is denied.
func TestPeerWithoutTheFeatureDeniesEveryEntry(t *testing.T) {
	prof := DeclaredProfile("t")
	prof.Contracts = []Listed{{ID: Contract, Level: "complete"}}
	p, _ := Open(prof, nil, func() time.Time { return t0 })
	ack, err := p.Apply(baseSet(1, entry("rec_1", ViaAssignment)))
	if err != nil {
		t.Fatal(err)
	}
	if len(ack.RecordAccess.Unrepresentable) != 1 {
		t.Fatalf("ack = %+v", ack.RecordAccess)
	}
	_ = p.Consume(docEvent(tEng))
	_ = p.CatchUp()
	if got := p.Readable(tPerson); got != nil {
		t.Errorf("readable = %v, want nothing", got)
	}
}

// The state survives a restart through the file store, and a refused write
// is not acknowledged.
func TestFileStoreKeepsTheStateAcrossARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "bosaccess.json")
	clk := &clock{t: t0}
	p, err := Open(DeclaredProfile("t"), FileStore{Path: path}, clk.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Apply(baseSet(1, entry("rec_1", ViaAssignment))); err != nil {
		t.Fatal(err)
	}
	_ = p.Consume(docEvent(tEng))
	_ = p.CatchUp()

	again, err := Open(DeclaredProfile("t"), FileStore{Path: path}, clk.now)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Readable(tPerson); !slices.Equal(got, []string{tDoc}) {
		t.Fatalf("after restart readable = %v", got)
	}
	if st := again.State(); st.AppliedEpoch == nil || *st.AppliedEpoch != 1 {
		t.Fatalf("state = %+v", st)
	}
	// epoch 1 with another digest is refused after the restart too
	_, err = again.Apply(baseSet(1, entry("rec_2", ViaAssignment)))
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != CodeEpochStale {
		t.Fatalf("err = %v, want epoch_stale", err)
	}
}

// Readable agrees with Decide for every record it lists and omits what
// Decide denies: a stale set, an ended entry, an unknown under.
func TestReadableFollowsTheFloors(t *testing.T) {
	clk := &clock{t: t0}
	p, _ := Open(DeclaredProfile("t"), nil, clk.now)
	e := entry("rec_1", ViaAssignment)
	e.ValidUntil = str(t0.Add(2 * time.Hour).Format(time.RFC3339))
	if _, err := p.Apply(baseSet(1, e)); err != nil {
		t.Fatal(err)
	}
	_ = p.Consume(docEvent(tEng))
	_ = p.Consume(Event{Subject: "bos://core.documents/documents_00000000000000000000000002", Data: EventData{Change: "upserted", Scope: tScope}}) // under unknown
	_ = p.CatchUp()
	if got := p.Readable(tPerson); !slices.Equal(got, []string{tDoc}) {
		t.Fatalf("readable = %v", got)
	}
	clk.t = t0.Add(2 * time.Hour)
	_ = p.CatchUp()
	if got := p.Readable(tPerson); got != nil {
		t.Fatalf("entry ended: readable = %v", got)
	}
	if got := p.Readable("bos://core.principals/principals_00000000000000000000000009"); got != nil {
		t.Fatalf("unknown person: readable = %v", got)
	}
	clk.t = t0.Add(25 * time.Hour)
	if got := p.Readable(tPerson); got != nil {
		t.Fatalf("stale set: readable = %v", got)
	}
}
