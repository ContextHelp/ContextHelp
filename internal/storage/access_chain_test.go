package storage

import (
	"errors"
	"slices"
	"testing"
	"time"
)

var chainT0 = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

// chainEntry is a steward entry as a transport hands it over.
func chainEntry(i int) *AccessEntry {
	return &AccessEntry{
		ID: "acc_" + string(rune('a'+i)), At: chainT0.Add(time.Duration(i) * time.Minute).Add(123456 * time.Nanosecond),
		PrincipalID: "prn_steward", IdentityID: "idn_session", Route: "GET /objects/{id}",
		Decision: AccessDecisionAllowed, OperationDigest: DigestPrefix + "op",
		Checks: []AccessCheck{
			{Check: "authn.session", Outcome: AccessOutcomePass, Reason: "authenticated"},
			{Check: "steward_grant", Outcome: AccessOutcomePass, Reason: "grant_active"},
		},
		PolicyVersions: "steward=stg_1", ObjectCount: 2, EntityCount: 1,
		ObjectIDs: []string{"obj_b", "obj_a"}, StewardGrantID: "stg_1",
	}
}

// chain links n entries the way a driver does and returns them with
// the head.
func chain(n int) (AccessChainHead, []*AccessEntry) {
	var head AccessChainHead
	out := make([]*AccessEntry, n)
	for i := range out {
		e := chainEntry(i)
		head = LinkAccessEntry(head, e)
		out[i] = e
	}
	return head, out
}

// The canonical encoding is a contract: a fixed entry hashes to a
// fixed digest today and in every later version of this code. A change
// to the encoding is a new version line, never an edit of this one.
// The digests were computed outside Go from the documented encoding
// (printf ... | shasum -a 256).
func TestAccessChainEncodingIsStable(t *testing.T) {
	e := chainEntry(0)
	e.At = e.At.Truncate(time.Millisecond)
	if got, want := AccessObjectsDigest([]string{"obj_b", "obj_a", "obj_a"}), "v1:sha256:4dd520449a097c3d22a9048d385c08bb216761eaf0feb7f478ecffc38926ba37"; got != want {
		t.Errorf("objects digest = %s, want %s", got, want)
	}
	objects := AccessObjectsDigest(e.ObjectIDs)
	if got, want := AccessEntryDigest("", 1, e, objects), "v1:sha256:bb5c63ebb4139a7ca4f62054737808403aaaaa04b8cf85d8a1ca27782b984176"; got != want {
		t.Errorf("entry digest = %s, want %s", got, want)
	}
	if got, want := canonicalAccessEntry("v1:sha256:prev", 7, e, objects), `ctxt-access-chain/v1
prev "v1:sha256:prev"
seq 7
id "acc_a"
at 1792141200000
principal "prn_steward"
identity "idn_session"
route "GET /objects/{id}"
decision "allowed"
operation "v1:sha256:op"
policy "steward=stg_1"
steward_grant "stg_1"
objects 2 "`+objects+`"
entities 1
checks 2
check "authn.session" "PASS" "authenticated"
check "steward_grant" "PASS" "grant_active"
`; got != want {
		t.Errorf("canonical entry =\n%s\nwant\n%s", got, want)
	}
}

// The objects digest does not depend on the order or repeats of the
// ids, and differs for another list.
func TestAccessObjectsDigestIsOrderFree(t *testing.T) {
	a := AccessObjectsDigest([]string{"obj_a", "obj_b"})
	if b := AccessObjectsDigest([]string{"obj_b", "obj_a", "obj_b"}); a != b {
		t.Errorf("order or repeats changed the digest: %s vs %s", a, b)
	}
	if c := AccessObjectsDigest([]string{"obj_a"}); a == c {
		t.Error("a shorter list hashes like the longer one")
	}
	if AccessObjectsDigest(nil) != AccessObjectsDigest([]string{}) {
		t.Error("nil and empty lists differ")
	}
}

// Every field of the entry is covered: changing any one changes the
// digest.
func TestAccessEntryDigestCoversEveryField(t *testing.T) {
	base := chainEntry(0)
	objects := AccessObjectsDigest(base.ObjectIDs)
	want := AccessEntryDigest("", 1, base, objects)
	for name, mutate := range map[string]func(e *AccessEntry){
		"id":        func(e *AccessEntry) { e.ID = "acc_z" },
		"at":        func(e *AccessEntry) { e.At = e.At.Add(time.Millisecond) },
		"principal": func(e *AccessEntry) { e.PrincipalID = "prn_other" },
		"identity":  func(e *AccessEntry) { e.IdentityID = "" },
		"route":     func(e *AccessEntry) { e.Route = "GET /objects" },
		"decision":  func(e *AccessEntry) { e.Decision = AccessDecisionDenied },
		"operation": func(e *AccessEntry) { e.OperationDigest = DigestPrefix + "other" },
		"policy":    func(e *AccessEntry) { e.PolicyVersions = "" },
		"grant":     func(e *AccessEntry) { e.StewardGrantID = "stg_2" },
		"count":     func(e *AccessEntry) { e.ObjectCount = 3 },
		"entities":  func(e *AccessEntry) { e.EntityCount = 0 },
		"check":     func(e *AccessEntry) { e.Checks[1].Reason = "grant_revoked" },
		"checks":    func(e *AccessEntry) { e.Checks = e.Checks[:1] },
	} {
		e := chainEntry(0)
		e.Checks = slices.Clone(e.Checks)
		mutate(e)
		if AccessEntryDigest("", 1, e, objects) == want {
			t.Errorf("changing %s left the digest unchanged", name)
		}
	}
	if AccessEntryDigest("v1:sha256:x", 1, base, objects) == want {
		t.Error("the previous digest is not covered")
	}
	if AccessEntryDigest("", 2, base, objects) == want {
		t.Error("the position is not covered")
	}
	if AccessEntryDigest("", 1, base, AccessObjectsDigest(nil)) == want {
		t.Error("the object list is not covered")
	}
}

// LinkAccessEntry numbers from 1, chains each entry to the previous
// digest and keeps At to the millisecond.
func TestLinkAccessEntry(t *testing.T) {
	head, entries := chain(3)
	if head.Seq != 3 || head.Digest != entries[2].Chain.EntryDigest {
		t.Fatalf("head = %+v, want the third entry's", head)
	}
	for i, e := range entries {
		if e.Chain.Seq != int64(i+1) {
			t.Errorf("entry %d seq = %d", i, e.Chain.Seq)
		}
		if e.At.Nanosecond()%int(time.Millisecond) != 0 {
			t.Errorf("entry %d at %s keeps sub-millisecond time", i, e.At)
		}
	}
	if entries[0].Chain.PrevDigest != "" || entries[1].Chain.PrevDigest != entries[0].Chain.EntryDigest {
		t.Errorf("links = %+v, %+v", entries[0].Chain, entries[1].Chain)
	}
}

// A chain of N entries verifies; every way of damaging it is reported
// at the first entry it touches.
func TestVerifyAccessChain(t *testing.T) {
	head, entries := chain(5)
	if err := VerifyAccessChain(head, entries, time.Time{}); err != nil {
		t.Fatalf("intact chain: %v", err)
	}
	shuffled := []*AccessEntry{entries[3], entries[0], entries[4], entries[2], entries[1]}
	if err := VerifyAccessChain(head, shuffled, time.Time{}); err != nil {
		t.Fatalf("intact chain read in another order: %v", err)
	}
	if err := VerifyAccessChain(AccessChainHead{}, nil, time.Time{}); err != nil {
		t.Fatalf("empty chain: %v", err)
	}

	copyChain := func() []*AccessEntry {
		out := make([]*AccessEntry, len(entries))
		for i, e := range entries {
			c := *e
			link := *e.Chain
			c.Chain = &link
			c.Checks = slices.Clone(e.Checks)
			c.ObjectIDs = slices.Clone(e.ObjectIDs)
			out[i] = &c
		}
		return out
	}
	for name, c := range map[string]struct {
		damage func([]*AccessEntry) ([]*AccessEntry, AccessChainHead)
		seq    int64
	}{
		"an edited field": {func(es []*AccessEntry) ([]*AccessEntry, AccessChainHead) {
			es[2].ObjectCount = 0
			return es, head
		}, 3},
		"an edited field and its digest": {func(es []*AccessEntry) ([]*AccessEntry, AccessChainHead) {
			es[2].Route = "GET /objects"
			es[2].Chain.EntryDigest = AccessEntryDigest(es[2].Chain.PrevDigest, 3, es[2], es[2].Chain.ObjectsDigest)
			return es, head
		}, 4},
		"a deleted entry": {func(es []*AccessEntry) ([]*AccessEntry, AccessChainHead) {
			return slices.Delete(es, 1, 2), head
		}, 2},
		"a deleted object id": {func(es []*AccessEntry) ([]*AccessEntry, AccessChainHead) {
			es[3].ObjectIDs = es[3].ObjectIDs[:1]
			return es, head
		}, 4},
		"a deleted object list": {func(es []*AccessEntry) ([]*AccessEntry, AccessChainHead) {
			es[3].ObjectIDs = nil
			return es, head
		}, 4},
		"a deleted last entry": {func(es []*AccessEntry) ([]*AccessEntry, AccessChainHead) {
			return es[:4], head
		}, 5},
		"an entry outside the chain": {func(es []*AccessEntry) ([]*AccessEntry, AccessChainHead) {
			es[1].Chain = nil
			return es, head
		}, 0},
		"a repeated position": {func(es []*AccessEntry) ([]*AccessEntry, AccessChainHead) {
			es[4].Chain.Seq = 4
			return es, head
		}, 4},
		"a head that moved on": {func(es []*AccessEntry) ([]*AccessEntry, AccessChainHead) {
			return es, AccessChainHead{Seq: 6, Digest: head.Digest}
		}, 6},
	} {
		es, h := c.damage(copyChain())
		err := VerifyAccessChain(h, es, time.Time{})
		var br *AccessChainBreak
		if !errors.As(err, &br) {
			t.Errorf("%s: verify = %v, want a break", name, err)
			continue
		}
		if br.Seq != c.seq {
			t.Errorf("%s: break at %d (%s), want %d", name, br.Seq, br.Reason, c.seq)
		}
	}
}

// Retention may empty the object lists older than its cut; the chain
// still verifies, and a list newer than the cut, or a list cut only in
// part, is still a break.
func TestVerifyAccessChainAfterRetention(t *testing.T) {
	head, entries := chain(4)
	cut := entries[2].At
	entries[0].ObjectIDs, entries[1].ObjectIDs = nil, nil
	if err := VerifyAccessChain(head, entries, cut); err != nil {
		t.Fatalf("lists pruned before the cut: %v", err)
	}
	if err := VerifyAccessChain(head, entries, time.Time{}); err == nil {
		t.Error("with nothing pruned by retention, an empty list is a break")
	}
	entries[2].ObjectIDs = nil
	if err := VerifyAccessChain(head, entries, cut); err == nil {
		t.Error("a list at the cut was emptied: a break")
	}
	entries[2].ObjectIDs = []string{"obj_a", "obj_b"}
	entries[1].ObjectIDs = []string{"obj_a"}
	if err := VerifyAccessChain(head, entries, cut); err == nil {
		t.Error("a list cut in part before the cut: a break")
	}
}

// A steward entry must name its grant; an ordinary entry may not go
// through the steward path, nor a steward one through Append's.
func TestPrepareStewardEntry(t *testing.T) {
	e := chainEntry(0)
	e.ObjectIDs = []string{"obj_b", "obj_a", "obj_b"}
	if err := PrepareStewardEntry(e, chainT0); err != nil {
		t.Fatalf("PrepareStewardEntry: %v", err)
	}
	if !slices.Equal(e.ObjectIDs, []string{"obj_a", "obj_b"}) {
		t.Errorf("object ids = %v, want sorted without repeats", e.ObjectIDs)
	}
	e.StewardGrantID = ""
	if err := PrepareStewardEntry(e, chainT0); err == nil {
		t.Error("a steward entry without a grant was accepted")
	}
	if err := PrepareAccessEntry(chainEntry(1), chainT0); err == nil {
		t.Error("Append's preparation accepted a steward entry")
	}
}
