package storagetest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// StewardAuditConformance pins the steward half of the read audit
// (ADR-083 §5) every driver implements: AppendSteward commits one
// entry durably and chained, Append refuses steward entries, the chain
// reads back verifiable and reports a deleted or edited row, retention
// of ordinary entries leaves steward lists alone, a caller may be kept
// from steward object lists, and concurrent appends keep one linear
// chain. open returns a fresh driver: the chain spans the whole
// instance, so each case starts from an empty one.
func StewardAuditConformance(t *testing.T, open func(t *testing.T) storage.StorageDriver) {
	t.Helper()
	for _, c := range []struct {
		name string
		fn   func(*testing.T, storage.StorageDriver)
	}{
		{"append chains and reads back", stewardConfAppend},
		{"append refusals write nothing", stewardConfRefusals},
		{"an edited entry breaks the chain", stewardConfEdited},
		{"a deleted entry breaks the chain", stewardConfDeleted},
		{"a deleted object id breaks the chain", stewardConfDeletedObject},
		{"ordinary retention keeps steward lists", stewardConfPrune},
		{"steward object lists can be hidden", stewardConfHidden},
		{"concurrent appends keep one linear chain", stewardConfConcurrent},
	} {
		t.Run(c.name, func(t *testing.T) { c.fn(t, open(t)) })
	}
}

// stewardEntry is an allowed steward read of objects under grant.
func stewardEntry(i int, grant string, objects ...string) *storage.AccessEntry {
	return &storage.AccessEntry{
		At:          sharingT0.Add(time.Duration(i)*time.Minute + 789*time.Microsecond),
		PrincipalID: "prn_steward", IdentityID: "idn_steward", Route: routeObjects,
		Decision: storage.AccessDecisionAllowed, OperationDigest: accessDigest, PolicyVersions: "steward=" + grant,
		Checks: []storage.AccessCheck{
			{Check: "authn.session", Outcome: storage.AccessOutcomePass, Reason: "authenticated"},
			{Check: "steward_grant", Outcome: storage.AccessOutcomePass, Reason: "grant_active"},
		},
		ObjectCount: len(objects), ObjectIDs: objects, StewardGrantID: grant,
	}
}

func appendSteward(t *testing.T, drv storage.StorageDriver, entries ...*storage.AccessEntry) {
	t.Helper()
	for _, e := range entries {
		if err := drv.Sharing().AccessLog().AppendSteward(context.Background(), e); err != nil {
			t.Fatalf("AppendSteward(%s): %v", e.Route, err)
		}
	}
}

// verifyChain reads the chain and verifies it with nothing pruned.
func verifyChain(t *testing.T, drv storage.StorageDriver) (storage.AccessChainHead, []*storage.AccessEntry, error) {
	t.Helper()
	head, entries, err := drv.Sharing().AccessLog().StewardChain(context.Background())
	if err != nil {
		t.Fatalf("StewardChain: %v", err)
	}
	return head, entries, storage.VerifyAccessChain(head, entries, time.Time{})
}

// rawDB is the driver's database, for the cases that tamper with rows
// the way an operator with write access could.
func rawDB(t *testing.T, drv storage.StorageDriver) *sql.DB {
	t.Helper()
	d, ok := drv.(interface{ DB() *sql.DB })
	if !ok {
		t.Skipf("%T exposes no database to tamper with", drv)
	}
	return d.DB()
}

func stewardConfAppend(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	a := stewardEntry(0, "stg_a", "obj-2", "obj-1", "obj-2")
	b := stewardEntry(1, "stg_b")
	b.Decision = storage.AccessDecisionDenied
	b.Checks = []storage.AccessCheck{
		{Check: "authn.session", Outcome: storage.AccessOutcomePass, Reason: "authenticated"},
		{Check: "steward_grant", Outcome: storage.AccessOutcomeFail, Reason: "grant_revoked"},
	}
	c := stewardEntry(2, "stg_a", "obj-3")
	appendSteward(t, drv, a, b, c)
	// An ordinary entry beside them is no part of the chain.
	if err := drv.Sharing().AccessLog().Append(ctx, accessLogEntries("steward")[:1]); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if a.Chain == nil || a.Chain.Seq != 1 || b.Chain.Seq != 2 || c.Chain.Seq != 3 || c.Chain.PrevDigest != b.Chain.EntryDigest {
		t.Fatalf("links = %+v %+v %+v, want 1, 2, 3 chained", a.Chain, b.Chain, c.Chain)
	}

	head, entries, err := verifyChain(t, drv)
	if err != nil {
		t.Fatalf("the chain of three appends does not verify: %v", err)
	}
	if head.Seq != 3 || head.Digest != c.Chain.EntryDigest || len(entries) != 3 {
		t.Fatalf("head %+v with %d entries, want the third entry's and 3", head, len(entries))
	}
	if entries[0].ID != a.ID || entries[1].ID != b.ID || entries[2].ID != c.ID {
		t.Fatalf("chain order = %s %s %s, want append order", entries[0].ID, entries[1].ID, entries[2].ID)
	}
	stewardConfReadBack(t, drv, a, c)
}

// stewardConfReadBack checks the entries a and c of grant stg_a read
// back through Query as written, and an ordinary entry beside them has
// no part in the chain.
func stewardConfReadBack(t *testing.T, drv storage.StorageDriver, a, c *storage.AccessEntry) {
	t.Helper()
	got := queryAccessLog(t, drv, storage.AccessFilter{StewardGrantID: "stg_a"})
	if len(got) != 2 || got[0].ID != c.ID || got[1].ID != a.ID {
		t.Fatalf("Query(grant stg_a) = %+v, want its two entries newest first", got)
	}
	first := got[1]
	if first.StewardGrantID != "stg_a" || first.Chain == nil || *first.Chain != *a.Chain {
		t.Fatalf("read back %+v chain %+v, want the grant and the link written", first, first.Chain)
	}
	if !slices.Equal(first.ObjectIDs, []string{"obj-1", "obj-2"}) || first.ObjectCount != 3 {
		t.Fatalf("object ids %v count %d, want the list sorted without repeats and the count as given", first.ObjectIDs, first.ObjectCount)
	}
	if !first.At.Equal(sharingT0) {
		t.Fatalf("at = %s, want it kept to the millisecond (%s)", first.At, sharingT0)
	}
	ordinary := queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_log_a_steward"})
	if len(ordinary) != 1 || ordinary[0].StewardGrantID != "" || ordinary[0].Chain != nil {
		t.Fatalf("ordinary entry = %+v, want no grant and no link", ordinary)
	}
}

func stewardConfRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().AccessLog()
	appendSteward(t, drv, stewardEntry(0, "stg_r", "obj-1"))
	if err := st.Append(ctx, []*storage.AccessEntry{stewardEntry(1, "stg_r")}); err == nil {
		t.Error("Append took a steward entry: it would be outside the chain and the durable path")
	}
	noGrant := stewardEntry(2, "")
	if err := st.AppendSteward(ctx, noGrant); err == nil {
		t.Error("AppendSteward took an entry naming no grant")
	}
	bad := stewardEntry(3, "stg_r")
	bad.Decision = storage.AccessDecisionDenied // no FAIL check
	if err := st.AppendSteward(ctx, bad); err == nil {
		t.Error("AppendSteward took a denied entry without a FAIL check")
	}
	head, entries, err := verifyChain(t, drv)
	if err != nil || head.Seq != 1 || len(entries) != 1 {
		t.Fatalf("after refusals: head %+v, %d entries, %v; want the one good entry", head, len(entries), err)
	}
	if got := queryAccessLog(t, drv, storage.AccessFilter{StewardGrantID: "stg_r"}); len(got) != 1 {
		t.Fatalf("refused appends wrote rows: %+v", got)
	}
}

// tampered appends four entries, runs stmt (with %s the id of the
// third) and checks the verifier reports the break at want.
func tampered(t *testing.T, drv storage.StorageDriver, stmt string, want int64) {
	t.Helper()
	db := rawDB(t, drv)
	es := []*storage.AccessEntry{
		stewardEntry(0, "stg_t", "obj-1"), stewardEntry(1, "stg_t", "obj-1", "obj-2"),
		stewardEntry(2, "stg_t", "obj-2", "obj-3"), stewardEntry(3, "stg_t", "obj-4"),
	}
	appendSteward(t, drv, es...)
	if _, _, err := verifyChain(t, drv); err != nil {
		t.Fatalf("before tampering: %v", err)
	}
	// #nosec G201 -- test statement; the id is one this test generated.
	if _, err := db.ExecContext(context.Background(), fmt.Sprintf(stmt, es[2].ID)); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	_, _, err := verifyChain(t, drv)
	var br *storage.AccessChainBreak
	if !errors.As(err, &br) {
		t.Fatalf("after tampering: verify = %v, want a break", err)
	}
	if br.Seq != want {
		t.Fatalf("break at %d (%s), want %d", br.Seq, br.Reason, want)
	}
}

func stewardConfEdited(t *testing.T, drv storage.StorageDriver) {
	tampered(t, drv, `UPDATE access_log SET object_count = 0 WHERE id = '%s'`, 3)
}

func stewardConfDeleted(t *testing.T, drv storage.StorageDriver) {
	tampered(t, drv, `DELETE FROM access_log WHERE id = '%s'`, 3)
}

func stewardConfDeletedObject(t *testing.T, drv storage.StorageDriver) {
	tampered(t, drv, `DELETE FROM access_log_objects WHERE access_id = '%s' AND object_id = 'obj-3'`, 3)
}

func stewardConfPrune(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().AccessLog()
	appendAccessLog(t, drv, "prune-steward")
	appendSteward(t, drv, stewardEntry(0, "stg_p", "obj-1", "obj-2"), stewardEntry(1, "stg_p", "obj-3"))
	future := sharingT0.Add(24 * time.Hour)
	n, err := st.Prune(ctx, future)
	if err != nil || n == 0 {
		t.Fatalf("Prune = %d, %v; want the ordinary lists pruned", n, err)
	}
	for _, e := range queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_log_a_prune-steward"}) {
		if len(e.ObjectIDs) != 0 {
			t.Fatalf("an ordinary list survived Prune: %+v", e)
		}
	}
	kept := queryAccessLog(t, drv, storage.AccessFilter{StewardGrantID: "stg_p"})
	if len(kept) != 2 || len(kept[0].ObjectIDs) != 1 || len(kept[1].ObjectIDs) != 2 {
		t.Fatalf("steward entries after Prune = %+v, want both lists whole", kept)
	}
	if _, _, err := verifyChain(t, drv); err != nil {
		t.Fatalf("chain after Prune: %v", err)
	}

	n, err = st.PruneSteward(ctx, sharingT0.Add(30*time.Second))
	if err != nil || n != 2 {
		t.Fatalf("PruneSteward(before the second) = %d, %v; want the first entry's two ids", n, err)
	}
	head, entries, err := st.StewardChain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.VerifyAccessChain(head, entries, sharingT0.Add(30*time.Second)); err != nil {
		t.Fatalf("chain after steward retention, verified with its cut: %v", err)
	}
	if err := storage.VerifyAccessChain(head, entries, time.Time{}); err == nil {
		t.Fatal("a pruned list verified as whole with no retention cut")
	}
	if entries[0].ObjectCount != 2 || len(entries[0].ObjectIDs) != 0 || len(entries[1].ObjectIDs) != 1 {
		t.Fatalf("after PruneSteward = %+v / %+v, want the first list gone, its count and the second list kept", entries[0], entries[1])
	}
}

func stewardConfHidden(t *testing.T, drv storage.StorageDriver) {
	appendAccessLog(t, drv, "hidden")
	appendSteward(t, drv, stewardEntry(0, "stg_h", "obj-1-hidden"))
	open := queryAccessLog(t, drv, storage.AccessFilter{ObjectID: "obj-1-hidden"})
	if len(open) != 3 {
		t.Fatalf("Query(object) = %d entries, want the two ordinary ones and the steward one", len(open))
	}
	hidden := queryAccessLog(t, drv, storage.AccessFilter{ObjectID: "obj-1-hidden", HideStewardObjects: true})
	if len(hidden) != 2 || slices.ContainsFunc(hidden, func(e *storage.AccessEntry) bool { return e.StewardGrantID != "" }) {
		t.Fatalf("Query(object, hidden) = %+v, want the ordinary entries only", hidden)
	}
	listed := queryAccessLog(t, drv, storage.AccessFilter{StewardGrantID: "stg_h", HideStewardObjects: true})
	if len(listed) != 1 || len(listed[0].ObjectIDs) != 0 || listed[0].ObjectCount != 1 || listed[0].Route != routeObjects {
		t.Fatalf("Query(grant, hidden) = %+v, want the entry with its route and count and no ids", listed)
	}
	for _, e := range queryAccessLog(t, drv, storage.AccessFilter{PrincipalID: "prn_log_a_hidden", HideStewardObjects: true}) {
		if len(e.ObjectIDs) == 0 {
			t.Fatalf("an ordinary entry lost its ids to HideStewardObjects: %+v", e)
		}
	}
}

// stewardConcurrency is how many appends race in the concurrency case.
const stewardConcurrency = 32

func stewardConfConcurrent(t *testing.T, drv storage.StorageDriver) {
	var wg sync.WaitGroup
	errs := make(chan error, stewardConcurrency)
	for i := range stewardConcurrency {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := stewardEntry(i, fmt.Sprintf("stg_c%02d", i), fmt.Sprintf("obj-%02d", i))
			errs <- drv.Sharing().AccessLog().AppendSteward(context.Background(), e)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a concurrent append failed: %v", err)
		}
	}
	head, entries, err := verifyChain(t, drv)
	if err != nil {
		t.Fatalf("chain after %d concurrent appends: %v", stewardConcurrency, err)
	}
	if head.Seq != stewardConcurrency || len(entries) != stewardConcurrency {
		t.Fatalf("head %+v with %d entries, want %d of each: no append lost", head, len(entries), stewardConcurrency)
	}
}
