package storagetest

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// ProjectionDB is a driver's database handle for the projection tests:
// it rewinds rows to an older projection and reads stored bodies back.
type ProjectionDB struct {
	DB      *sql.DB
	Dialect indexsig.Dialect
}

// Stale rewinds one stored object to what an older projection left
// behind: body as its projected_fts_body (and FTS index entry) and version
// as its projection_version stamp. SQLite's objects_fts is an
// external-content table, so the old entry is dropped while the row still
// holds the old body and the new one indexed after the rewrite; Postgres
// derives its tsvector from the column.
func (p ProjectionDB) Stale(t testing.TB, id, body, version string) {
	t.Helper()
	ctx := context.Background()
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("stale %s: begin: %v", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	exec := func(q string, args ...any) {
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("stale %s: %v", id, err)
		}
	}
	if p.Dialect == indexsig.DialectPostgres {
		exec(`UPDATE objects SET projected_fts_body = $1, projection_version = $2 WHERE id = $3`, body, version, id)
	} else {
		exec(`DELETE FROM objects_fts WHERE rowid = (SELECT rowid FROM objects WHERE id = ? AND projected_fts_body != '')`, id)
		exec(`UPDATE objects SET projected_fts_body = ?, projection_version = ? WHERE id = ?`, body, version, id)
		if body != "" {
			exec(`INSERT INTO objects_fts(rowid, id, projected_fts_body) VALUES ((SELECT rowid FROM objects WHERE id = ?), ?, ?)`, id, id, body)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("stale %s: commit: %v", id, err)
	}
}

// Body reads projected_fts_body straight from the objects table.
func (p ProjectionDB) Body(t testing.TB, id string) string {
	t.Helper()
	q := `SELECT projected_fts_body FROM objects WHERE id = ?`
	if p.Dialect == indexsig.DialectPostgres {
		q = `SELECT projected_fts_body FROM objects WHERE id = $1`
	}
	var body string
	if err := p.DB.QueryRowContext(context.Background(), q, id).Scan(&body); err != nil {
		t.Fatalf("read body %s: %v", id, err)
	}
	return body
}

// ProjectionFixture returns an object whose summary repeats its body: the
// current projection indexes the text once, an older one indexed it twice.
// Every object carries the token "quokka"; LegacyToken appears only in the
// stale bodies the conformance writes.
func ProjectionFixture(id, text string) *storage.KnowledgeObject {
	body := "quokka " + text
	return &storage.KnowledgeObject{
		ID:          id,
		Type:        "note",
		RawContent:  body,
		TextContent: body,
		Summaries:   []string{body},
		CreatedAt:   fixtureTime(),
		UpdatedAt:   fixtureTime(),
	}
}

// LegacyToken marks text only a stale (pre-re-projection) body carries.
const LegacyToken = "zebralegacy"

// StaleBody is the body an older projection stored for obj: its text
// twice, plus LegacyToken.
func StaleBody(obj *storage.KnowledgeObject) string {
	return LegacyToken + " " + obj.TextContent + " " + obj.TextContent
}

// ProjectionStoreConformance pins storage.ProjectionStore on a driver:
// Create and Update stamp the current version, stale rows page in ID
// order, and Reproject rewrites the body, the FTS index and the stamp from
// the stored fields without touching updated_at.
func ProjectionStoreConformance(t *testing.T, drv storage.StorageDriver, pdb ProjectionDB) {
	t.Helper()
	ps, ok := drv.Objects().(storage.ProjectionStore)
	if !ok {
		t.Fatalf("%T does not implement storage.ProjectionStore", drv.Objects())
	}
	c := &projectionCase{ctx: context.Background(), drv: drv, ps: ps, pdb: pdb, objs: map[string]*storage.KnowledgeObject{}}
	for i, id := range []string{"proj-a", "proj-b", "proj-c", "proj-d", "proj-e"} {
		obj := ProjectionFixture(id, fmt.Sprintf("object number %d", i))
		if err := drv.Objects().Create(c.ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
		c.objs[id] = obj
	}

	t.Run("create stamps the current version", func(t *testing.T) {
		c.wantStale(t, 0)
	})

	// proj-a stays current; the rest carry an older projection. proj-e
	// predates the stamp column (empty stamp).
	for _, id := range []string{"proj-b", "proj-c", "proj-d"} {
		pdb.Stale(t, id, StaleBody(c.objs[id]), "v1")
	}
	pdb.Stale(t, "proj-e", StaleBody(c.objs["proj-e"]), "")

	t.Run("stale rows page in ID order", func(t *testing.T) {
		c.wantStale(t, 4)
		c.wantPage(t, "", 2, "[proj-b proj-c]")
		c.wantPage(t, "proj-c", 10, "[proj-d proj-e]")
	})
	t.Run("stale body is searchable before reprojection", func(t *testing.T) {
		assertFTS(c.ctx, t, drv, LegacyToken, "[proj-b proj-c proj-d proj-e]")
	})
	t.Run("reproject rewrites body, index and stamp", c.reprojectRewrites)
	t.Run("reproject is idempotent", func(t *testing.T) {
		c.wantReproject(t, "proj-b", false)
		c.wantReproject(t, "proj-a", false)
		c.wantReproject(t, "proj-missing", false)
	})
	t.Run("update stamps the current version", c.updateStamps)
	t.Run("every object is findable by its current text", func(t *testing.T) {
		c.wantReproject(t, "proj-d", true)
		c.wantReproject(t, "proj-e", true)
		assertFTS(c.ctx, t, drv, LegacyToken, "[]")
		assertFTS(c.ctx, t, drv, "quokka", "[proj-a proj-b proj-c proj-d proj-e]")
		c.wantStale(t, 0)
	})
}

// projectionCase is the state ProjectionStoreConformance's steps share.
type projectionCase struct {
	ctx  context.Context
	drv  storage.StorageDriver
	ps   storage.ProjectionStore
	pdb  ProjectionDB
	objs map[string]*storage.KnowledgeObject
}

func (c *projectionCase) wantStale(t *testing.T, want int) {
	t.Helper()
	n, err := c.ps.CountStaleProjections(c.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("stale count = %d, want %d", n, want)
	}
}

func (c *projectionCase) wantPage(t *testing.T, after string, limit int, want string) {
	t.Helper()
	page, err := c.ps.ListStaleProjections(c.ctx, after, limit)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(page); got != want {
		t.Fatalf("stale page after %q = %s, want %s", after, got, want)
	}
}

func (c *projectionCase) wantReproject(t *testing.T, id string, want bool) {
	t.Helper()
	changed, err := c.ps.Reproject(c.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if changed != want {
		t.Errorf("Reproject(%s) = %v, want %v", id, changed, want)
	}
}

func (c *projectionCase) reprojectRewrites(t *testing.T) {
	before, err := c.drv.Objects().Get(c.ctx, "proj-b")
	if err != nil {
		t.Fatal(err)
	}
	c.wantReproject(t, "proj-b", true)
	want := projection.ProjectIndex(before).FTSBody
	if want != c.objs["proj-b"].TextContent {
		t.Fatalf("fixture drift: projection %q, want the text once", want)
	}
	if got := c.pdb.Body(t, "proj-b"); got != want {
		t.Errorf("stored body = %q, want %q", got, want)
	}
	after, err := c.drv.Objects().Get(c.ctx, "proj-b")
	if err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at moved: %s -> %s", before.UpdatedAt, after.UpdatedAt)
	}
	if !after.FTSIndexed {
		t.Error("fts_indexed false after reprojection")
	}
	assertFTS(c.ctx, t, c.drv, LegacyToken, "[proj-c proj-d proj-e]")
	c.wantStale(t, 3)
}

func (c *projectionCase) updateStamps(t *testing.T) {
	obj, err := c.drv.Objects().Get(c.ctx, "proj-c")
	if err != nil {
		t.Fatal(err)
	}
	obj.UpdatedAt = obj.UpdatedAt.Add(time.Minute)
	if err := c.drv.Objects().Update(c.ctx, obj); err != nil {
		t.Fatal(err)
	}
	c.wantPage(t, "", 10, "[proj-d proj-e]")
	assertFTS(c.ctx, t, c.drv, LegacyToken, "[proj-d proj-e]")
}

// assertFTS checks the IDs an FTS query returns, sorted.
func assertFTS(ctx context.Context, t testing.TB, drv storage.StorageDriver, query, want string) {
	t.Helper()
	res, err := drv.Objects().FTSSearch(ctx, query, storage.ObjectFilter{Limit: 1000})
	if err != nil {
		t.Fatalf("FTSSearch(%q): %v", query, err)
	}
	if got := fmt.Sprint(sortedIDs(res)); got != want {
		t.Errorf("FTSSearch(%q) = %s, want %s", query, got, want)
	}
}

// AssertFTS is assertFTS for other packages' tests.
func AssertFTS(t testing.TB, drv storage.StorageDriver, query, want string) {
	t.Helper()
	assertFTS(context.Background(), t, drv, query, want)
}

// sortedIDs returns the objects' IDs in order.
func sortedIDs(objs []*storage.KnowledgeObject) []string {
	ids := resultIDs(objs)
	sort.Strings(ids)
	return ids
}
