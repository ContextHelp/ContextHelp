package storagetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// RederiveStoreConformance pins storage.RederiveStore on a driver:
// signature entries round-trip and are replaced by a second upsert; stale
// rows come back as one count per (pipeline, signature) group, leaving
// out rows skipped for a current signature; a group's rows page by
// attempts then ID with a composite cursor that stays correct while
// attempts change; a skip holds for its target only; a whole group can be
// stamped; and attempts and skips go with their object.
func RederiveStoreConformance(t *testing.T, drv storage.StorageDriver, pdb ProjectionDB) {
	t.Helper()
	rs, ok := drv.Objects().(storage.RederiveStore)
	if !ok {
		t.Fatalf("%T does not implement storage.RederiveStore", drv.Objects())
	}
	ctx := context.Background()
	rows := []struct{ id, pipeline, sig string }{
		{"rd-a", "alpha", "alpha#old"},
		{"rd-b", "alpha", "alpha#old"},
		{"rd-c", "alpha", "alpha#old"},
		{"rd-d", "beta", "beta#old"},
		{"rd-e", "alpha", "alpha#new"}, // current
		{"rd-f", "alpha", ""},          // unsigned: never stale
		{"rd-g", "alpha@v1", "alpha#old"},
	}
	for _, r := range rows {
		obj := ProjectionFixture(r.id, "rederive "+r.id)
		obj.Pipeline, obj.DerivationSig = r.pipeline, r.sig
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", r.id, err)
		}
	}
	c := &rederiveCase{ctx: ctx, drv: drv, rs: rs, pdb: pdb, current: []string{"alpha#new", "beta#new"}}

	t.Run("discarded objects are not selected, other statuses are", c.discardedNotSelected)
	t.Run("signature entries round-trip and are replaced", c.signatureEntries)
	t.Run("stale rows come back grouped by stored pipeline and signature", c.groupedStale)
	t.Run("a group pages by attempts then ID", c.pagesByTries)
	t.Run("strikes count apart from tries and clearing forgets both", c.strikesApartFromTries)
	t.Run("a skip for a current signature hides the row from groups and pages", c.skipHidesRow)
	t.Run("a skip for another target does not hide the row", c.skipOtherTarget)
	t.Run("a skip is replaced by one for the same object and target", c.skipReplaced)
	t.Run("a group is stamped whole", c.groupStampedWhole)
	t.Run("stamping a group leaves discarded rows alone", c.stampLeavesDiscarded)
	t.Run("rows for a target that is no longer current are pruned", c.pruneDeadTargets)
	t.Run("an object that is gone is ErrNotFound, not a constraint error", c.goneIsNotFound)
	t.Run("attempts and skips go with their object", c.goneWithObject)
}

// pageCBA is the order of group alpha's rows once rd-c has no tries, rd-b
// one and rd-a two.
const pageCBA = "[rd-c:0 rd-b:1 rd-a:2]"

// rederiveCase is the store under test and the helpers its subtests share.
type rederiveCase struct {
	ctx     context.Context
	drv     storage.StorageDriver
	rs      storage.RederiveStore
	pdb     ProjectionDB
	current []string
}

func (c *rederiveCase) groups(t *testing.T, cur []string) string {
	t.Helper()
	got, err := c.rs.ListStaleGroups(c.ctx, cur)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(got))
	for _, g := range got {
		out = append(out, fmt.Sprintf("%s|%s|%d", g.Pipeline, g.Sig, g.Objects))
	}
	return fmt.Sprint(out)
}

func (c *rederiveCase) page(t *testing.T, after storage.RederiveCursor, limit int) string {
	t.Helper()
	got, err := c.rs.ListRederivable(c.ctx, storage.RederiveQuery{
		Pipeline: "alpha", Sig: "alpha#old", Target: "alpha#new", After: after, Limit: limit})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(got))
	for _, o := range got {
		out = append(out, fmt.Sprintf("%s:%d", o.ID, o.Tries))
	}
	return fmt.Sprint(out)
}

func (c *rederiveCase) skip(t *testing.T, id, target, reason string) {
	t.Helper()
	if err := c.rs.RecordRederiveSkip(c.ctx, storage.RederiveSkip{ObjectID: id, TargetSig: target, Reason: reason, Detail: "d"}); err != nil {
		t.Fatal(err)
	}
}

func (c *rederiveCase) attempt(t *testing.T, id, target string) {
	t.Helper()
	if err := c.rs.RecordRederiveTry(c.ctx, id, target); err != nil {
		t.Fatalf("try %s: %v", id, err)
	}
}

// discarded objects are not selected, other statuses are.
func (c *rederiveCase) discardedNotSelected(t *testing.T) {
	ctx, drv, rs := c.ctx, c.drv, c.rs
	for id, status := range map[string]string{"rd-x-discarded": "discarded", "rd-x-inbox": "inbox", "rd-x-raw": "raw"} {
		obj := ProjectionFixture(id, "status "+id)
		obj.Pipeline, obj.DerivationSig, obj.Status = "statuspipe", "statuspipe#old", status
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	got, err := rs.ListStaleGroups(ctx, []string{"statuspipe#new"})
	if err != nil {
		t.Fatal(err)
	}
	var g storage.StaleGroup
	for _, c := range got {
		if c.Pipeline == "statuspipe" {
			g = c
		}
	}
	if g.Objects != 2 {
		t.Fatalf("group = %+v, want the inbox and raw objects only", g)
	}
	rows, err := rs.ListRederivable(ctx, storage.RederiveQuery{Pipeline: "statuspipe", Sig: "statuspipe#old", Target: "statuspipe#new", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(rows))
	for _, o := range rows {
		ids = append(ids, o.ID)
	}
	if fmt.Sprint(ids) != "[rd-x-inbox rd-x-raw]" {
		t.Fatalf("rows = %v", ids)
	}
	for id := range map[string]bool{"rd-x-discarded": true, "rd-x-inbox": true, "rd-x-raw": true} {
		if err := drv.Objects().Delete(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
}

// signature entries round-trip and are replaced.
func (c *rederiveCase) signatureEntries(t *testing.T) {
	ctx, rs := c.ctx, c.rs
	if _, ok, err := rs.DerivationSignatureEntries(ctx, "alpha#new"); err != nil || ok {
		t.Fatalf("unrecorded signature: ok=%v err=%v", ok, err)
	}
	if err := rs.UpsertDerivationSignature(ctx, "alpha#new", "alpha", []string{"a@1", "b@2", "llm=x/y"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := rs.DerivationSignatureEntries(ctx, "alpha#new")
	if err != nil || !ok || !slices.Equal(got, []string{"a@1", "b@2", "llm=x/y"}) {
		t.Fatalf("entries = %v ok=%v err=%v", got, ok, err)
	}
	if err := rs.UpsertDerivationSignature(ctx, "alpha#new", "alpha", []string{"a@1"}); err != nil {
		t.Fatal(err)
	}
	got, _, _ = rs.DerivationSignatureEntries(ctx, "alpha#new")
	if !slices.Equal(got, []string{"a@1"}) {
		t.Fatalf("second upsert did not replace: %v", got)
	}
}

// stale rows come back grouped by stored pipeline and signature.
func (c *rederiveCase) groupedStale(t *testing.T) {
	current, groups := c.current, c.groups
	want := "[alpha|alpha#old|3 alpha@v1|alpha#old|1 beta|beta#old|1]"
	if got := groups(t, current); got != want {
		t.Fatalf("groups = %s, want %s", got, want)
	}
}

// a group pages by attempts then ID.
func (c *rederiveCase) pagesByTries(t *testing.T) {
	page, attempt := c.page, c.attempt
	if got := page(t, storage.RederiveCursor{}, 10); got != "[rd-a:0 rd-b:0 rd-c:0]" {
		t.Fatalf("page = %s", got)
	}
	attempt(t, "rd-a", "alpha#new")
	attempt(t, "rd-a", "alpha#new")
	attempt(t, "rd-b", "alpha#new")
	if got := page(t, storage.RederiveCursor{}, 10); got != pageCBA {
		t.Fatalf("page after attempts = %s", got)
	}
	// A composite cursor resumes inside the order, and a row whose
	// attempts rose since is read at its new place.
	if got := page(t, storage.RederiveCursor{Tries: 0, ID: "rd-c"}, 10); got != "[rd-b:1 rd-a:2]" {
		t.Fatalf("page after rd-c = %s", got)
	}
	if got := page(t, storage.RederiveCursor{Tries: 1, ID: "rd-b"}, 10); got != "[rd-a:2]" {
		t.Fatalf("page after rd-b = %s", got)
	}
	if got := page(t, storage.RederiveCursor{Tries: 2, ID: "rd-a"}, 10); got != "[]" {
		t.Fatalf("page after the last = %s", got)
	}
	if got := page(t, storage.RederiveCursor{}, 2); got != "[rd-c:0 rd-b:1]" {
		t.Fatalf("limited page = %s", got)
	}
	// Attempts are per target.
	attempt(t, "rd-c", "alpha#other")
	if got := page(t, storage.RederiveCursor{}, 10); got != pageCBA {
		t.Fatalf("another target's attempts changed the order: %s", got)
	}
}

// strikes count apart from tries and clearing forgets both.
func (c *rederiveCase) strikesApartFromTries(t *testing.T) {
	ctx, rs, page, attempt := c.ctx, c.rs, c.page, c.attempt
	for want := 1; want <= 3; want++ {
		n, err := rs.RecordRederiveStrike(ctx, "rd-c", "alpha#new")
		if err != nil || n != want {
			t.Fatalf("strike = %d, %v; want %d", n, err, want)
		}
	}
	if got := page(t, storage.RederiveCursor{}, 10); got != pageCBA {
		t.Fatalf("strikes changed the order: %s", got)
	}
	if err := rs.ClearRederiveAttempts(ctx, "rd-a", "alpha#new"); err != nil {
		t.Fatal(err)
	}
	if err := rs.ClearRederiveAttempts(ctx, "rd-c", "alpha#new"); err != nil {
		t.Fatal(err)
	}
	if got := page(t, storage.RederiveCursor{}, 10); got != "[rd-a:0 rd-c:0 rd-b:1]" {
		t.Fatalf("after clearing: %s", got)
	}
	if n, _ := rs.RecordRederiveStrike(ctx, "rd-c", "alpha#new"); n != 1 {
		t.Fatalf("strikes after clearing = %d, want 1", n)
	}
	// Restore the state the next subtests expect.
	attempt(t, "rd-a", "alpha#new")
	attempt(t, "rd-a", "alpha#new")
	attempt(t, "rd-c", "alpha#new")
	_ = rs.ClearRederiveAttempts(ctx, "rd-c", "alpha#new")
}

// a skip for a current signature hides the row from groups and pages.
func (c *rederiveCase) skipHidesRow(t *testing.T) {
	current, groups, page, skip := c.current, c.groups, c.page, c.skip
	skip(t, "rd-b", "alpha#new", storage.SkipFetchFailed)
	if got := groups(t, current); got != "[alpha|alpha#old|2 alpha@v1|alpha#old|1 beta|beta#old|1]" {
		t.Fatalf("groups = %s", got)
	}
	if got := page(t, storage.RederiveCursor{}, 10); got != "[rd-c:0 rd-a:2]" {
		t.Fatalf("page = %s", got)
	}
}

// a skip for another target does not hide the row.
func (c *rederiveCase) skipOtherTarget(t *testing.T) {
	current, groups, skip := c.current, c.groups, c.skip
	skip(t, "rd-c", "alpha#older", storage.SkipRefused)
	if got := groups(t, current); got != "[alpha|alpha#old|2 alpha@v1|alpha#old|1 beta|beta#old|1]" {
		t.Fatalf("groups = %s", got)
	}
	if got := groups(t, []string{"alpha#newer", "beta#new"}); got != "[alpha|alpha#new|1 alpha|alpha#old|3 alpha@v1|alpha#old|1 beta|beta#old|1]" {
		t.Fatalf("groups against a new target = %s", got)
	}
}

// a skip is replaced by one for the same object and target.
func (c *rederiveCase) skipReplaced(t *testing.T) {
	pdb, skip := c.pdb, c.skip
	skip(t, "rd-b", "alpha#new", storage.SkipProviderFailed)
	got := pdb.RederiveSkips(t, "alpha#new")
	if len(got) != 1 || got[0].ObjectID != "rd-b" || got[0].Reason != storage.SkipProviderFailed || got[0].Detail != "d" {
		t.Fatalf("skips = %+v", got)
	}
}

// a group is stamped whole.
func (c *rederiveCase) groupStampedWhole(t *testing.T) {
	ctx, rs, current, groups := c.ctx, c.rs, c.current, c.groups
	n, err := rs.StampGroup(ctx, "beta", "beta#old", "beta#new")
	if err != nil || n != 1 {
		t.Fatalf("stamped %d, %v", n, err)
	}
	if got := groups(t, current); got != "[alpha|alpha#old|2 alpha@v1|alpha#old|1]" {
		t.Fatalf("groups after the stamp = %s", got)
	}
	// Another pipeline stamp with the same signature is another group.
	if n, _ := rs.StampGroup(ctx, "alpha@v1", "alpha#old", "alpha#new"); n != 1 {
		t.Fatalf("stamped %d", n)
	}
	if got := groups(t, current); got != "[alpha|alpha#old|2]" {
		t.Fatalf("groups after the second stamp = %s", got)
	}
}

// stamping a group leaves discarded rows alone.
func (c *rederiveCase) stampLeavesDiscarded(t *testing.T) {
	ctx, drv, rs, pdb := c.ctx, c.drv, c.rs, c.pdb
	for id, status := range map[string]string{"rd-s-active": "active", "rd-s-discarded": "discarded"} {
		obj := ProjectionFixture(id, "stamp "+id)
		obj.Pipeline, obj.DerivationSig, obj.Status = "stamppipe", "stamppipe#old", status
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	n, err := rs.StampGroup(ctx, "stamppipe", "stamppipe#old", "stamppipe#new")
	if err != nil || n != 1 {
		t.Fatalf("stamped %d, %v; want only the active row", n, err)
	}
	if got := pdb.DerivationSig(t, "rd-s-discarded"); got != "stamppipe#old" {
		t.Fatalf("discarded row signature = %q", got)
	}
	for _, id := range []string{"rd-s-active", "rd-s-discarded"} {
		_ = drv.Objects().Delete(ctx, id)
	}
}

// rows for a target that is no longer current are pruned.
func (c *rederiveCase) pruneDeadTargets(t *testing.T) {
	ctx, rs, pdb, skip, attempt := c.ctx, c.rs, c.pdb, c.skip, c.attempt
	attempt(t, "rd-g", "alpha#new")
	attempt(t, "rd-g", "alpha#gone")
	skip(t, "rd-g", "alpha#gone", storage.SkipRefused)
	skip(t, "rd-g", "alpha#new", storage.SkipRefused)
	n, err := rs.PruneRederiveRows(ctx, []string{"alpha#new", "beta#new"}, nil)
	// At least the attempt and the skip of alpha#gone (earlier subtests
	// left rows for other dead targets too).
	if err != nil || n < 2 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	if got := pdb.RederiveSkips(t, "alpha#gone"); len(got) != 0 {
		t.Fatalf("skip of a dead target kept: %+v", got)
	}
	if got := pdb.RederiveSkips(t, "alpha#new"); len(got) == 0 {
		t.Fatal("skip of a current target pruned")
	}
	if n, err := rs.PruneRederiveRows(ctx, nil, nil); err != nil || n != 0 {
		t.Fatalf("pruning against no current signature deleted %d, %v", n, err)
	}
	// The target of a definition that cannot be built at this start is
	// kept, whatever signature the registry can name.
	if err := rs.UpsertDerivationSignature(ctx, "alpha#unbuilt", "unbuilt", []string{"a@2"}); err != nil {
		t.Fatal(err)
	}
	skip(t, "rd-g", "alpha#unbuilt", storage.SkipRefused)
	skip(t, "rd-g", "alpha#orphan", storage.SkipRefused)
	if _, err := rs.PruneRederiveRows(ctx, []string{"alpha#new"}, []string{"unbuilt"}); err != nil {
		t.Fatal(err)
	}
	if len(pdb.RederiveSkips(t, "alpha#unbuilt")) != 1 {
		t.Fatal("the skip of an unbuilt definition's target was pruned")
	}
	if len(pdb.RederiveSkips(t, "alpha#orphan")) != 0 {
		t.Fatal("a target no definition produces was kept")
	}
}

// an object that is gone is ErrNotFound, not a constraint error.
func (c *rederiveCase) goneIsNotFound(t *testing.T) {
	ctx, rs := c.ctx, c.rs
	if err := rs.RecordRederiveTry(ctx, "rd-never-existed", "alpha#new"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("try = %v, want ErrNotFound", err)
	}
	if _, err := rs.RecordRederiveStrike(ctx, "rd-never-existed", "alpha#new"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("strike = %v, want ErrNotFound", err)
	}
	err := rs.RecordRederiveSkip(ctx, storage.RederiveSkip{ObjectID: "rd-never-existed", TargetSig: "alpha#new", Reason: storage.SkipRefused})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("skip = %v, want ErrNotFound", err)
	}
}

// attempts and skips go with their object.
func (c *rederiveCase) goneWithObject(t *testing.T) {
	ctx, drv, rs, pdb, skip, attempt := c.ctx, c.drv, c.rs, c.pdb, c.skip, c.attempt
	attempt(t, "rd-d", "beta#new")
	skip(t, "rd-d", "beta#new", storage.SkipRefused)
	if err := drv.Objects().Delete(ctx, "rd-d"); err != nil {
		t.Fatal(err)
	}
	if got := pdb.RederiveSkips(t, "beta#new"); len(got) != 0 {
		t.Fatalf("skips after delete = %+v", got)
	}
	// A new object under the same id starts with no attempts.
	obj := ProjectionFixture("rd-d", "again")
	obj.Pipeline, obj.DerivationSig = "beta", "beta#old"
	if err := drv.Objects().Create(ctx, obj); err != nil {
		t.Fatal(err)
	}
	triesOf := func() int {
		got, err := rs.ListRederivable(ctx, storage.RederiveQuery{Pipeline: "beta", Sig: "beta#old", Target: "beta#new", Limit: 10})
		if err != nil || len(got) != 1 || got[0].ID != "rd-d" {
			t.Fatalf("rows = %+v, %v", got, err)
		}
		return got[0].Tries
	}
	if n := triesOf(); n != 0 {
		t.Fatalf("a new object under a deleted one's id starts with %d tries", n)
	}
	attempt(t, "rd-d", "beta#new")
	if n := triesOf(); n != 1 {
		t.Fatalf("tries after one try = %d", n)
	}
}

// RederiveSkips reads the skip records for target straight from the table.
func (p ProjectionDB) RederiveSkips(t testing.TB, target string) []storage.RederiveSkip {
	t.Helper()
	q := `SELECT object_id, target_sig, reason, detail FROM rederive_skips WHERE target_sig = ? ORDER BY object_id`
	if p.Dialect == indexsig.DialectPostgres {
		q = `SELECT object_id, target_sig, reason, detail FROM rederive_skips WHERE target_sig = $1 ORDER BY object_id`
	}
	rows, err := p.DB.QueryContext(context.Background(), q, target)
	if err != nil {
		t.Fatalf("read skips: %v", err)
	}
	defer rows.Close()
	var out []storage.RederiveSkip
	for rows.Next() {
		var k storage.RederiveSkip
		if err := rows.Scan(&k.ObjectID, &k.TargetSig, &k.Reason, &k.Detail); err != nil {
			t.Fatalf("read skips: %v", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read skips: %v", err)
	}
	return out
}
