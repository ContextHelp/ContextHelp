package upgrade

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// fixtureDB spins up an in-memory SQLite DB with a minimal `objects` table
// matching production's columns the selector touches: id, pipeline,
// graph_json, created_at. Other columns are omitted — the selector only
// needs columns it queries by.
func fixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE objects (
			id TEXT PRIMARY KEY,
			pipeline TEXT DEFAULT '',
			graph_json TEXT,
			created_at TEXT NOT NULL DEFAULT '2026-05-07T00:00:00Z'
		)
	`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	rows := []struct {
		id, pipeline, graph, createdAt string
	}{
		{"obj-a", "text.short@v0", "", "2026-05-01T00:00:00Z"},
		{"obj-b", "text.short@v0", "{}", "2026-05-02T00:00:00Z"},
		{"obj-c", "text.short@v1", "{}", "2026-05-03T00:00:00Z"},
		{"obj-d", "text.long@v0", "{}", "2026-05-04T00:00:00Z"},
		{"obj-e", "doc.pdf@v0", "", "2026-05-05T00:00:00Z"},
	}
	for _, r := range rows {
		_, err := db.Exec(
			"INSERT INTO objects (id, pipeline, graph_json, created_at) VALUES (?, ?, ?, ?)",
			r.id, r.pipeline, r.graph, r.createdAt,
		)
		if err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
	}
	return db
}

func TestParseSelector_PipelineFilter(t *testing.T) {
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sel.sql != "pipeline = ?" {
		t.Errorf("sql = %q, want pipeline = ?", sel.sql)
	}
	if len(sel.args) != 1 || sel.args[0] != "text.short@v0" {
		t.Errorf("args = %v, want [text.short@v0]", sel.args)
	}
	if sel.Raw() != "pipeline=text.short@v0" {
		t.Errorf("Raw = %q", sel.Raw())
	}
}

func TestParseSelector_RejectsBarePipelineFilter(t *testing.T) {
	// Bare `pipeline=text.short` (no @vN) is the footgun ADR-070 §1
	// calls out — would re-ingest every version of the family.
	_, err := ParseSelector("pipeline=text.short")
	if err == nil {
		t.Fatal("expected error for bare pipeline filter")
	}
	if !strings.Contains(err.Error(), "@vN") {
		t.Errorf("error should mention @vN suffix: %v", err)
	}
}

func TestParseSelector_RejectsEmpty(t *testing.T) {
	_, err := ParseSelector("")
	if err == nil {
		t.Fatal("expected empty error")
	}
	_, err = ParseSelector("   ")
	if err == nil {
		t.Fatal("expected empty error for whitespace")
	}
}

func TestParseSelector_RejectsUnknownPrefix(t *testing.T) {
	_, err := ParseSelector("foo=bar")
	if err == nil {
		t.Fatal("expected error for unknown prefix")
	}
}

func TestParseSelector_WhereForm(t *testing.T) {
	sel, err := ParseSelector("where:graph_json IS NULL")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sel.sql != "graph_json IS NULL" {
		t.Errorf("sql = %q, want graph_json IS NULL", sel.sql)
	}
}

// TestParseSelector_RejectsInjection verifies the where-form rejects every
// dangerous fragment / keyword the parser scans for. One assertion per
// vector keeps the failure message helpful.
func TestParseSelector_RejectsInjection(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"semicolon", "where:1=1; DROP TABLE objects"},
		{"sql_comment", "where:1=1 -- DROP"},
		{"block_comment_open", "where:1=1 /* DROP"},
		{"block_comment_close", "where:1=1 */"},
		{"drop_keyword", "where:DROP TABLE objects"},
		{"insert_keyword", "where:1=1 OR INSERT INTO foo"},
		{"update_keyword", "where:1=1 OR UPDATE foo SET x=1"},
		{"delete_keyword", "where:1=1 OR DELETE FROM foo"},
		{"alter_keyword", "where:1=1 OR ALTER TABLE foo"},
		{"create_keyword", "where:1=1 OR CREATE INDEX bad"},
		{"pragma_keyword", "where:PRAGMA writable_schema=1"},
		{"attach_keyword", "where:ATTACH DATABASE 'x' AS y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSelector(tc.input)
			if err == nil {
				t.Fatalf("expected rejection for %q", tc.input)
			}
		})
	}
}

// TestParseSelector_AllowlistRejectsUnknownIdentifiers covers the vectors the
// keyword denylist alone does not catch: data exfiltration via subquery,
// reads of other tables, and function calls. None of these contain a DDL/DML
// keyword or a chaining marker, so the allowlist is the control that stops
// them.
func TestParseSelector_AllowlistRejectsUnknownIdentifiers(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"subquery_select", "where:id IN (SELECT id FROM secrets)"},
		{"union_exfil", "where:1=1 UNION SELECT password FROM users"},
		{"other_table", "where:profile_id = users.id"},
		{"function_call", "where:randomblob(1000000000) IS NOT NULL"},
		{"load_extension", "where:load_extension('evil.so') IS NULL"},
		{"unknown_column", "where:secret_column = 'x'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseSelector(tc.input); err == nil {
				t.Fatalf("expected rejection for %q", tc.input)
			}
		})
	}
}

// TestParseSelector_AllowlistAcceptsLegitimatePredicates guards against the
// allowlist being so tight it breaks real selectors.
func TestParseSelector_AllowlistAcceptsLegitimatePredicates(t *testing.T) {
	cases := []string{
		"where:graph_json IS NULL",
		"where:graph_json IS NULL AND pipeline LIKE 'text.short%'",
		"where:type = 'note' OR type = 'article'",
		"where:reinforcement_count > 3 AND status IS NOT NULL",
		"where:created_at BETWEEN '2026-01-01' AND '2026-06-01'",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := ParseSelector(in); err != nil {
				t.Fatalf("legitimate predicate %q rejected: %v", in, err)
			}
		})
	}
}

// TestParseSelector_LiteralsAreNotIdentifiers confirms string literal bodies
// are treated as data — a value containing a word like "select" must not trip
// the allowlist.
func TestParseSelector_LiteralsAreNotIdentifiers(t *testing.T) {
	if _, err := ParseSelector("where:source = 'select-all-notes'"); err != nil {
		t.Fatalf("literal containing a keyword should be allowed: %v", err)
	}
}

func TestSelector_CountMatching_PipelineFilter(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := sel.CountMatching(context.Background(), db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got != 2 {
		t.Errorf("count = %d, want 2", got)
	}
}

func TestSelector_CountMatching_WhereForm(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelectorWithDB("where:graph_json IS NULL OR graph_json = ''", db)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := sel.CountMatching(context.Background(), db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got != 2 { // obj-a + obj-e
		t.Errorf("count = %d, want 2", got)
	}
}

func TestSelector_IterateMatching_DeterministicOrder(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ch, iterErrs, err := sel.IterateMatching(context.Background(), db)
	if err != nil {
		t.Fatalf("iter: %v", err)
	}
	got := []string{}
	for id := range ch {
		got = append(got, id)
	}
	want := []string{"obj-a", "obj-b"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i, id := range got {
		if id != want[i] {
			t.Errorf("got[%d] = %s, want %s", i, id, want[i])
		}
	}
	if iterErr := <-iterErrs; iterErr != nil {
		t.Fatalf("iterate err: %v", iterErr)
	}
}

func TestSelector_IterateMatching_ContextCancelClosesChan(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, _, err := sel.IterateMatching(ctx, db)
	if err != nil {
		t.Fatalf("iter: %v", err)
	}
	// Drain one then cancel; the iterator goroutine must release.
	<-ch
	cancel()
	// Drain remaining (channel will close).
	for range ch {
	}
}

// TestParseSelectorWithDB_PreWarmsRejectsBadColumn confirms the
// EXPLAIN QUERY PLAN pre-warm rejects predicates referring to columns
// that don't exist on the schema.
func TestParseSelectorWithDB_PreWarmsRejectsBadColumn(t *testing.T) {
	db := fixtureDB(t)
	_, err := ParseSelectorWithDB("where:no_such_column = 'x'", db)
	if err == nil {
		t.Fatal("expected rejection for bogus column")
	}
}
