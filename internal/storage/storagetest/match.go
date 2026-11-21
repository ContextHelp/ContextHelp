package storagetest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MatchBySQLConformance pins ObjectStore.MatchBySQL over drv (a fresh
// database): one entry per held id with one bool per predicate in
// order, predicates compiled from RSQL by internal/search in the
// driver's dialect (tags included) and hand-written ones alike, a NULL
// predicate false, an unknown id absent, a scoped store answering only
// the rows its scope selects, and a nil viewer's store refusing.
func MatchBySQLConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	note := func(id string, tags ...string) *storage.KnowledgeObject {
		o := searchFixtureObject(id, "note", "match "+id)
		o.ContentHash = "sha256:" + id
		for _, tag := range tags {
			o.Tags = append(o.Tags, storage.Tag{Label: tag})
		}
		return o
	}
	team, shared, contact := note("mb-team", "team"), note("mb-shared", "team", "public"), note("mb-contact", "public")
	contact.Type = "contact"
	for _, o := range []*storage.KnowledgeObject{team, shared, contact} {
		if err := drv.Objects().Create(ctx, o); err != nil {
			t.Fatalf("seed object %s: %v", o.ID, err)
		}
	}
	dialect := search.DialectSQLite
	if sqlDialect(drv) == storage.SQLDialectPostgres {
		dialect = search.DialectPostgres
	}
	compiled := func(rsql string) storage.SQLPredicate {
		node, err := search.Parse(rsql)
		if err != nil {
			t.Fatalf("parse %q: %v", rsql, err)
		}
		sql, args, err := search.CompileUnnumbered(dialect, node, nil)
		if err != nil {
			t.Fatalf("compile %q: %v", rsql, err)
		}
		return storage.SQLPredicate{SQL: sql, Args: args}
	}
	preds := []storage.SQLPredicate{
		compiled("type==note;tag==team"),
		compiled("tag==public"),
		{SQL: "id = ?", Args: []any{shared.ID}},
		{SQL: "CAST(NULL AS TEXT) = ?", Args: []any{"x"}},
	}
	ids := []string{team.ID, shared.ID, contact.ID, "mb-missing"}

	got, err := drv.Objects().MatchBySQL(ctx, ids, preds)
	if err != nil {
		t.Fatalf("MatchBySQL: %v", err)
	}
	want := map[string][]bool{
		team.ID:    {true, false, false, false},
		shared.ID:  {true, true, true, false},
		contact.ID: {false, true, false, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MatchBySQL = %v, want %v", got, want)
	}

	if got, err := drv.Objects().MatchBySQL(ctx, nil, preds); err != nil || len(got) != 0 {
		t.Errorf("MatchBySQL(no ids) = %v, %v, want nothing", got, err)
	}
	if got, err := drv.Objects().MatchBySQL(ctx, ids, nil); err != nil || len(got) != 3 || len(got[team.ID]) != 0 {
		t.Errorf("MatchBySQL(no predicates) = %v, %v, want the held ids with no column", got, err)
	}

	// A scoped store answers the rows its scope selects only.
	scoped := storage.ScopedDriver(viewerOfType("note", restrictedPrincipal), drv).Objects()
	got, err = scoped.MatchBySQL(ctx, ids, preds[:2])
	if err != nil {
		t.Fatalf("scoped MatchBySQL: %v", err)
	}
	want = map[string][]bool{team.ID: {true, false}, shared.ID: {true, true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scoped MatchBySQL = %v, want %v: the contact is outside the scope", got, want)
	}

	if _, err := storage.ScopedDriver(nil, drv).Objects().MatchBySQL(ctx, ids, preds); !errors.Is(err, storage.ErrRestrictedViewer) {
		t.Errorf("nil viewer MatchBySQL err = %v, want ErrRestrictedViewer", err)
	}
}
