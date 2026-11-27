package storagetest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// DerivationSig reads objects.derivation_sig straight from the table.
func (p ProjectionDB) DerivationSig(t testing.TB, id string) string {
	t.Helper()
	q := `SELECT derivation_sig FROM objects WHERE id = ?`
	if p.Dialect == indexsig.DialectPostgres {
		q = `SELECT derivation_sig FROM objects WHERE id = $1`
	}
	var sig string
	if err := p.DB.QueryRowContext(context.Background(), q, id).Scan(&sig); err != nil {
		t.Fatalf("read derivation_sig %s: %v", id, err)
	}
	return sig
}

// ChangeSeq reads objects.change_seq straight from the table.
func (p ProjectionDB) ChangeSeq(t testing.TB, id string) int64 {
	t.Helper()
	q := `SELECT change_seq FROM objects WHERE id = ?`
	if p.Dialect == indexsig.DialectPostgres {
		q = `SELECT change_seq FROM objects WHERE id = $1`
	}
	var seq int64
	if err := p.DB.QueryRowContext(context.Background(), q, id).Scan(&seq); err != nil {
		t.Fatalf("read change_seq %s: %v", id, err)
	}
	return seq
}

// contentFingerprint reads every column a stamp must leave alone: the
// timestamps, the text, the FTS body and the projection stamp.
func (p ProjectionDB) contentFingerprint(t testing.TB, id string) string {
	t.Helper()
	q := `SELECT updated_at, raw_content, text_content, projected_fts_body, projection_version, pipeline FROM objects WHERE id = ?`
	if p.Dialect == indexsig.DialectPostgres {
		q = `SELECT updated_at, raw_content, text_content, projected_fts_body, projection_version, pipeline FROM objects WHERE id = $1`
	}
	var a, b, c, d, e, f any
	if err := p.DB.QueryRowContext(context.Background(), q, id).Scan(&a, &b, &c, &d, &e, &f); err != nil {
		t.Fatalf("read fingerprint %s: %v", id, err)
	}
	return fmt.Sprint(a, "|", b, "|", c, "|", d, "|", e, "|", f)
}

// DerivationStoreConformance pins storage.DerivationStore on a driver:
// Create writes the signature and Update leaves it, the baseline stamps
// unsigned rows of a named pipeline (bare and "@v0" forms) once, an
// unsigned row is never stale, and stale rows page in ID order.
func DerivationStoreConformance(t *testing.T, drv storage.StorageDriver, pdb ProjectionDB) {
	t.Helper()
	ds, ok := drv.Objects().(storage.DerivationStore)
	if !ok {
		t.Fatalf("%T does not implement storage.DerivationStore", drv.Objects())
	}
	c := &derivationCase{
		ctx: context.Background(), drv: drv, ds: ds, pdb: pdb,
		seqs: map[string]int64{}, prints: map[string]string{},
		current: []string{"alpha#base", "beta#base"},
	}
	rows := []struct{ id, pipeline, sig string }{
		{"drv-a", "alpha", ""},
		{"drv-b", "alpha@v0", ""},
		{"drv-c", "beta", ""},
		{"drv-d", "", ""},
		{"drv-e", "alpha", "alpha#preset"},
		{"drv-f", "alpha", "alpha#old"},
	}
	for _, r := range rows {
		obj := ProjectionFixture(r.id, "derivation "+r.id)
		obj.Pipeline, obj.DerivationSig = r.pipeline, r.sig
		if err := drv.Objects().Create(c.ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", r.id, err)
		}
		c.seqs[r.id] = pdb.ChangeSeq(t, r.id)
		c.prints[r.id] = pdb.contentFingerprint(t, r.id)
	}
	for _, step := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"create writes the signature", c.createWrites},
		{"unsigned rows are never stale", c.unsignedNeverStale},
		{"baseline stamps unsigned rows of the named pipelines", c.baselineStamps},
		{"baseline is idempotent", c.baselineIdempotent},
		{"stale rows page in ID order", c.stalePaging},
		{"stamping leaves change_seq alone", c.baselineKeepsChangeSeq},
		{"stamps change nothing but the signature", c.stampsChangeOnlyTheSignature},
		{"a signature in current is not stale", c.currentIsNotStale},
		{"StampDerivation removes a row from the stale set", c.stampDerivation},
		{"Update leaves the signature", c.updateLeavesSignature},
	} {
		t.Run(step.name, step.run)
	}
}

// derivationCase is the state DerivationStoreConformance's steps share.
type derivationCase struct {
	ctx     context.Context
	drv     storage.StorageDriver
	ds      storage.DerivationStore
	pdb     ProjectionDB
	seqs    map[string]int64
	prints  map[string]string
	current []string
}

func (c *derivationCase) count(t *testing.T, cur []string, want int) {
	t.Helper()
	n, err := c.ds.CountStaleDerivations(c.ctx, cur)
	if err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("stale count against %v = %d, want %d", cur, n, want)
	}
}

func (c *derivationCase) page(t *testing.T, cur []string, after string, limit int, want string) {
	t.Helper()
	ids, err := c.ds.ListStaleDerivations(c.ctx, cur, after, limit)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(ids); got != want {
		t.Fatalf("stale page after %q limit %d = %s, want %s", after, limit, got, want)
	}
}

func (c *derivationCase) sigIs(t *testing.T, id, want string) {
	t.Helper()
	if got := c.pdb.DerivationSig(t, id); got != want {
		t.Fatalf("%s derivation_sig = %q, want %q", id, got, want)
	}
}

func (c *derivationCase) createWrites(t *testing.T) {
	c.sigIs(t, "drv-a", "")
	c.sigIs(t, "drv-e", "alpha#preset")
}

func (c *derivationCase) unsignedNeverStale(t *testing.T) {
	c.count(t, c.current, 2)
	c.page(t, c.current, "", 10, "[drv-e drv-f]")
}

func (c *derivationCase) baselineStamps(t *testing.T) {
	n, err := c.ds.StampDerivationBaseline(c.ctx, map[string]string{"alpha": "alpha#base", "beta": "beta#base"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("stamped %d rows, want 3 (bare, @v0 and beta)", n)
	}
	c.sigIs(t, "drv-a", "alpha#base")
	c.sigIs(t, "drv-b", "alpha#base")
	c.sigIs(t, "drv-c", "beta#base")
	c.sigIs(t, "drv-d", "")
	c.sigIs(t, "drv-e", "alpha#preset")
	c.sigIs(t, "drv-f", "alpha#old")
}

func (c *derivationCase) baselineIdempotent(t *testing.T) {
	n, err := c.ds.StampDerivationBaseline(c.ctx, map[string]string{"alpha": "alpha#other"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("second baseline stamped %d rows, want 0", n)
	}
	c.sigIs(t, "drv-a", "alpha#base")
}

func (c *derivationCase) stalePaging(t *testing.T) {
	c.count(t, c.current, 2)
	c.page(t, c.current, "", 1, "[drv-e]")
	c.page(t, c.current, "drv-e", 1, "[drv-f]")
	c.page(t, c.current, "drv-f", 1, "[]")
	c.page(t, c.current, "", 10, "[drv-e drv-f]")
}

// The signature never goes on the wire: stamping must not re-push the
// object to federation targets.
func (c *derivationCase) baselineKeepsChangeSeq(t *testing.T) {
	for _, id := range []string{"drv-a", "drv-b", "drv-c"} {
		if got := c.pdb.ChangeSeq(t, id); got != c.seqs[id] {
			t.Errorf("%s change_seq %d after the baseline stamp, want %d", id, got, c.seqs[id])
		}
	}
}

// The change_seq triggers skip the bump for a signature change, so the
// signature must only ever be written alone.
func (c *derivationCase) stampsChangeOnlyTheSignature(t *testing.T) {
	for _, id := range []string{"drv-a", "drv-b", "drv-c", "drv-f"} {
		if got := c.pdb.contentFingerprint(t, id); got != c.prints[id] {
			t.Errorf("%s changed beyond its signature:\n got %s\nwant %s", id, got, c.prints[id])
		}
	}
}

func (c *derivationCase) currentIsNotStale(t *testing.T) {
	c.count(t, append([]string{"alpha#preset", "alpha#old"}, c.current...), 0)
	c.count(t, nil, 5) // every signed row: drv-a, -b, -c, -e, -f
	c.page(t, nil, "drv-c", 10, "[drv-e drv-f]")
}

func (c *derivationCase) stampDerivation(t *testing.T) {
	if err := c.ds.StampDerivation(c.ctx, "drv-f", "alpha#base"); err != nil {
		t.Fatal(err)
	}
	c.sigIs(t, "drv-f", "alpha#base")
	if got := c.pdb.ChangeSeq(t, "drv-f"); got != c.seqs["drv-f"] {
		t.Errorf("drv-f change_seq %d after StampDerivation, want %d", got, c.seqs["drv-f"])
	}
	if got := c.pdb.contentFingerprint(t, "drv-f"); got != c.prints["drv-f"] {
		t.Errorf("StampDerivation changed more than the signature:\n got %s\nwant %s", got, c.prints["drv-f"])
	}
	c.count(t, c.current, 1)
	c.page(t, c.current, "", 10, "[drv-e]")
	if err := c.ds.StampDerivation(c.ctx, "drv-missing", "alpha#base"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stamp of a missing object = %v, want ErrNotFound", err)
	}
}

func (c *derivationCase) updateLeavesSignature(t *testing.T) {
	obj, err := c.drv.Objects().Get(c.ctx, "drv-e")
	if err != nil {
		t.Fatal(err)
	}
	obj.InboxNote = "edited"
	before := c.pdb.ChangeSeq(t, "drv-e")
	obj.DerivationSig = "" // a read-modify-write copy carries none
	if err := c.drv.Objects().Update(c.ctx, obj); err != nil {
		t.Fatal(err)
	}
	c.sigIs(t, "drv-e", "alpha#preset")
	if got := c.pdb.ChangeSeq(t, "drv-e"); got <= before {
		t.Errorf("an ordinary Update left change_seq at %d (was %d): it must still bump", got, before)
	}
	obj.DerivationSig = "alpha#hijack"
	if err := c.drv.Objects().Update(c.ctx, obj); err != nil {
		t.Fatal(err)
	}
	c.sigIs(t, "drv-e", "alpha#preset")
}
