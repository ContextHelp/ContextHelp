package sharing_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const (
	recScope  = "bos://core.business_scopes/business_scopes_00000000000000000000000001"
	recPerson = "bos://core.principals/principals_00000000000000000000000001"
	recEng1   = "bos://projects.engagements/engagements_00000000000000000000000001"
	recEng2   = "bos://projects.engagements/engagements_00000000000000000000000002"
	recDoc1   = "bos://core.documents/documents_00000000000000000000000001"
	recDoc2   = "bos://core.documents/documents_00000000000000000000000002"
	recParty1 = "bos://core.parties/parties_00000000000000000000000001"
)

func rfc(t time.Time) *string { s := t.UTC().Format(time.RFC3339); return &s }

// recordFixture is a store with objects ingested from the authority, a
// person bound to its address there, and a peer holding one dated entry.
type recordFixture struct {
	*storeFixture
	now  time.Time
	peer *bosaccess.Peer
	r    *sharing.Resolver
}

func newRecordFixture(t *testing.T) *recordFixture {
	t.Helper()
	ctx := context.Background()
	f := &recordFixture{storeFixture: newStoreFixture(t), now: t0}
	for _, o := range []struct{ id, source, key string }{
		{"obj-doc1", sharing.RecordSourceName, recDoc1},
		{"obj-doc2", sharing.RecordSourceName, recDoc2},
		{"obj-party1", sharing.RecordSourceName, recParty1},
		// an object of another source is never the authority's record
		{"obj-other", "slack", "1699999999.000100"},
	} {
		obj := &storage.KnowledgeObject{
			ID: o.id, Type: "document", RawContent: o.id, Status: storage.ObjectStatusActive,
			Source: o.source, SourceKey: o.key, ContentHash: "sha256:" + o.id, CreatedAt: t0, UpdatedAt: t0,
		}
		if err := f.drv.Objects().Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}
	bind := &storage.Identity{PrincipalID: f.person.ID, Method: storage.IdentityMethodOIDC,
		Issuer: sharing.RecordIdentityIssuer, Subject: recPerson, CreatedAt: t0}
	if err := f.drv.Auth().Identities().Create(ctx, bind); err != nil {
		t.Fatal(err)
	}
	var err error
	f.peer, err = bosaccess.Open(bosaccess.DeclaredProfile("test"), &bosaccess.MemStore{}, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	f.r = sharing.NewResolver(f.st).WithClock(func() time.Time { return f.now }).
		WithRecords(sharing.AuthorityRecords{Holder: f.peer, Identities: f.drv.Auth().Identities()})
	return f
}

// apply pushes a full set with one entry for documents under eng1 from
// from to until (nil: open) and consumes the feed state of three records.
func (f *recordFixture) apply(epoch int64, from, until *string) {
	f.t.Helper()
	docs := "core.documents"
	set := bosaccess.Set{
		Contract: bosaccess.Contract, Epoch: epoch, Mode: bosaccess.ModeFull, Scope: recScope,
		GeneratedAt: t0.Format(time.RFC3339),
		Policy:      bosaccess.Policy{MaxStalenessSeconds: 86400, ReconcileIntervalSeconds: 3600, RecordBasisMaxStalenessSeconds: 3600},
		Scopes:      []bosaccess.Scope{{ID: recScope}},
		Principals:  []bosaccess.Principal{{ID: recPerson, Kind: "person", Status: "active"}},
		RecordAccess: []bosaccess.RecordEntry{{
			ID: "rec_1", Subject: recPerson, Anchor: recEng1, Scope: recScope, TypeKey: &docs,
			Via: bosaccess.ViaAssignment, Grant: "grant_1", ValidFrom: from, ValidUntil: until,
		}},
	}
	set.Normalize()
	set.Digest = bosaccess.ContentDigest(set)
	if _, err := f.peer.Apply(set); err != nil {
		f.t.Fatal(err)
	}
}

func (f *recordFixture) feed() {
	f.t.Helper()
	for subject, under := range map[string][]string{recDoc1: {recEng1}, recDoc2: {recEng2}, recParty1: {recEng1}} {
		ev := bosaccess.Event{Subject: subject, Data: bosaccess.EventData{Change: "upserted", Scope: recScope, Sensitivity: "internal", Under: under}}
		if err := f.peer.Consume(ev); err != nil {
			f.t.Fatal(err)
		}
	}
	if err := f.peer.CatchUp(); err != nil {
		f.t.Fatal(err)
	}
}

func (f *recordFixture) sees() []string {
	f.t.Helper()
	v, err := f.r.Resolve(context.Background(), f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil {
		f.t.Fatal(err)
	}
	if v.MatchesNothing() {
		return nil
	}
	return visible(f.t, f.drv, v)
}

// A person assigned to an engagement reads the records of the type the
// entry names that stand under it, from the authority's source: not a
// sibling under another engagement, not another type under the same
// anchor, not an object of another source.
func TestRecordEntriesAdmitRecordsUnderTheAnchor(t *testing.T) {
	f := newRecordFixture(t)
	if got := f.sees(); got != nil {
		t.Fatalf("before any set: sees %v, want nothing", got)
	}
	f.apply(1, nil, nil)
	f.feed()
	if got, want := f.sees(), []string{"obj-doc1"}; !slices.Equal(got, want) {
		t.Fatalf("sees %v, want %v", got, want)
	}
}

// The entry's dates are honored at the moment of each read: before its
// start nothing, inside it the record, at its end nothing.
func TestRecordEntriesHonourStartAndEnd(t *testing.T) {
	f := newRecordFixture(t)
	f.apply(1, rfc(t0.Add(time.Hour)), rfc(t0.Add(3*time.Hour)))
	f.feed()
	for _, c := range []struct {
		at   time.Duration
		want []string
	}{
		{0, nil},
		{time.Hour - time.Second, nil},
		{time.Hour, []string{"obj-doc1"}},
		{2 * time.Hour, []string{"obj-doc1"}},
		{3*time.Hour - time.Second, []string{"obj-doc1"}},
		{3 * time.Hour, nil},
	} {
		f.now = t0.Add(c.at)
		// the feed catch-up is fresh at every probe: only the dates differ
		if err := f.peer.CatchUp(); err != nil {
			t.Fatal(err)
		}
		if got := f.sees(); !slices.Equal(got, c.want) {
			t.Errorf("at +%s sees %v, want %v", c.at, got, c.want)
		}
	}
}

// A feed catch-up older than the record basis limit takes the entries
// away: the person reads nothing until the peer catches up again.
func TestRecordEntriesNeedAFreshFeedCatchUp(t *testing.T) {
	f := newRecordFixture(t)
	f.apply(1, nil, nil)
	f.feed()
	f.now = t0.Add(time.Hour + time.Second)
	if got := f.sees(); got != nil {
		t.Fatalf("stale catch-up: sees %v, want nothing", got)
	}
	if err := f.peer.CatchUp(); err != nil {
		t.Fatal(err)
	}
	if got := f.sees(); !slices.Equal(got, []string{"obj-doc1"}) {
		t.Fatalf("after catch-up: sees %v", got)
	}
}

// A record that moves to another engagement leaves the person's reach
// with the next refreshed event, and a delta that ends the entry takes
// the record away at once.
func TestRecordEntriesFollowMovesAndRemovals(t *testing.T) {
	f := newRecordFixture(t)
	f.apply(1, nil, nil)
	f.feed()
	move := bosaccess.Event{Subject: recDoc1, Data: bosaccess.EventData{Change: "refreshed", Scope: recScope, Sensitivity: "internal", Under: []string{recEng2}}}
	if err := f.peer.Consume(move); err != nil {
		t.Fatal(err)
	}
	if got := f.sees(); got != nil {
		t.Fatalf("after the move: sees %v, want nothing", got)
	}
	back := bosaccess.Event{Subject: recDoc1, Data: bosaccess.EventData{Change: "refreshed", Scope: recScope, Sensitivity: "internal", Under: []string{recEng1}}}
	if err := f.peer.Consume(back); err != nil {
		t.Fatal(err)
	}
	if got := f.sees(); !slices.Equal(got, []string{"obj-doc1"}) {
		t.Fatalf("moved back: sees %v", got)
	}
	// epoch 2 holds no entry: the next set replaces the entries
	f.apply(2, nil, nil)
	set := bosaccess.Set{
		Contract: bosaccess.Contract, Epoch: 3, Mode: bosaccess.ModeDelta, BaseEpoch: 2, Scope: recScope,
		Policy:              bosaccess.Policy{MaxStalenessSeconds: 86400, ReconcileIntervalSeconds: 3600, RecordBasisMaxStalenessSeconds: 3600},
		Scopes:              []bosaccess.Scope{{ID: recScope}},
		Principals:          []bosaccess.Principal{{ID: recPerson, Kind: "person", Status: "active"}},
		RecordAccessRemoved: []string{"rec_1"},
	}
	set.Normalize()
	set.Digest = bosaccess.ContentDigest(bosaccess.Set{Scope: recScope, Scopes: set.Scopes, Principals: set.Principals})
	if _, err := f.peer.Apply(set); err != nil {
		t.Fatal(err)
	}
	if got := f.sees(); got != nil {
		t.Fatalf("after the entry was removed: sees %v, want nothing", got)
	}
}

// A person with an ordinary grant keeps it, and the record reads add to
// it: the viewer's key names the records, so a change of them is a new
// compiled viewer.
func TestRecordEntriesAddToGrantsAndKeyNamesThem(t *testing.T) {
	f := newRecordFixture(t)
	f.join("colleagues", nil)
	f.grant("colleagues", sharing.TemplateColleagues, nil, t0)
	ctx := context.Background()
	before, err := f.r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil {
		t.Fatal(err)
	}
	granted := visible(t, f.drv, before)
	f.apply(1, nil, nil)
	f.feed()
	after, err := f.r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil {
		t.Fatal(err)
	}
	if before.PolicyKey() == after.PolicyKey() {
		t.Fatalf("policy key did not change with the records: %q", after.PolicyKey())
	}
	got := visible(t, f.drv, after)
	for _, id := range granted {
		if !slices.Contains(got, id) {
			t.Errorf("grant lost %s", id)
		}
	}
	if !slices.Contains(got, "obj-doc1") || slices.Contains(got, "obj-doc2") || slices.Contains(got, "obj-other") {
		t.Fatalf("record reads wrong: %v", got)
	}
}

// Past the bound the viewer names the first addresses only: a read is
// denied, never widened.
func TestRecordPredicateIsBounded(t *testing.T) {
	var addrs []string
	for i := 0; i < sharing.MaxRecordAddresses+10; i++ {
		addrs = append(addrs, "bos://core.documents/documents_"+time.Duration(i).String())
	}
	slices.Sort(addrs)
	v, err := sharing.Compile(sharing.Input{PrincipalID: "prn_x", Records: addrs})
	if err != nil {
		t.Fatal(err)
	}
	sql, args := v.ObjectPredicate(storage.SQLDialectSQLite)
	if sql == sharing.MatchNothing || len(args) < sharing.MaxRecordAddresses || len(args) > sharing.MaxRecordAddresses+2 {
		t.Fatalf("predicate %q binds %d args", sql, len(args))
	}
}

// The key names which records, not only that there are some: a viewer
// cached for one list is never served for another.
func TestRecordKeyNamesTheRecords(t *testing.T) {
	a := sharing.Input{PrincipalID: "prn_x", Records: []string{recDoc1}}
	b := sharing.Input{PrincipalID: "prn_x", Records: []string{recDoc2}}
	if a.Key() == b.Key() {
		t.Fatalf("one key %q for two record lists", a.Key())
	}
}
