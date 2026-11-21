package postgres

import (
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
)

// restrictedTestScope is the scope storage.ViewerScope builds for a
// viewer of notes: ? placeholders, renumbered by the driver.
func restrictedTestScope(t *testing.T) *storage.Scope {
	t.Helper()
	sc, ok := storage.ViewerScope(noteViewer{}, storage.SQLDialectPostgres)
	if !ok {
		t.Fatal("ViewerScope: no scope for a restricted viewer")
	}
	return &sc
}

type noteViewer struct{}

func (noteViewer) IsUnrestricted() bool { return false }
func (noteViewer) PrincipalID() string  { return "prn_test" }
func (noteViewer) ObjectPredicate(string) (string, []any) {
	return "type = ?", []any{"note"}
}
func (noteViewer) Allows(string) bool { return true }

var pgPlaceholder = regexp.MustCompile(`\$(\d+)`)

// assertBindsArgs fails unless sql's placeholders are exactly $1..$n
// for its n args, and no ? is left for Postgres to read as an operator.
func assertBindsArgs(t *testing.T, name, sql string, args []any) {
	t.Helper()
	if strings.Contains(sql, "?") {
		t.Errorf("%s: unbound ? in %q", name, sql)
	}
	var got []int
	for _, m := range pgPlaceholder.FindAllStringSubmatch(sql, -1) {
		n, _ := strconv.Atoi(m[1])
		if !slices.Contains(got, n) {
			got = append(got, n)
		}
	}
	slices.Sort(got)
	want := make([]int, len(args))
	for i := range want {
		want[i] = i + 1
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s: placeholders %v for %d args in %q", name, got, len(args), sql)
	}
}

// TestListBySQLQueries_RestrictedViewer pins the count and page SQL a
// restricted viewer's ListBySQL runs: the caller's $1 first, List's
// status predicate next, the viewer's scope renumbered after it, the
// page's LIMIT and OFFSET last.
func TestListBySQLQueries_RestrictedViewer(t *testing.T) {
	q := listBySQLQueries("id LIKE $1", []any{"rv-%"}, storage.ObjectFilter{Limit: 10, Offset: 5}, restrictedTestScope(t))

	const wantCount = "SELECT COUNT(*) FROM objects WHERE (id LIKE $1) AND status = $2 AND (status = $3 AND (type = $4))"
	if q.count != wantCount {
		t.Errorf("count =\n  %s\nwant\n  %s", q.count, wantCount)
	}
	wantArgs := []any{"rv-%", storage.ObjectStatusActive, storage.ObjectStatusActive, "note"}
	if !reflect.DeepEqual(q.args, wantArgs) {
		t.Errorf("args = %v, want %v", q.args, wantArgs)
	}
	if !strings.HasSuffix(q.page, " FROM objects WHERE (id LIKE $1) AND status = $2 AND (status = $3 AND (type = $4)) ORDER BY created_at DESC LIMIT $5 OFFSET $6") {
		t.Errorf("page = %s", q.page)
	}
	if want := append(append([]any(nil), wantArgs...), 10, 5); !reflect.DeepEqual(q.pageArgs, want) {
		t.Errorf("pageArgs = %v, want %v", q.pageArgs, want)
	}
}

// TestListBySQLQueries_BindsEveryArg checks every shape ListBySQL
// builds binds exactly its args, $1 to $n, with or without a caller
// predicate, a scope, filters and paging.
func TestListBySQLQueries_BindsEveryArg(t *testing.T) {
	scope := restrictedTestScope(t)
	for _, c := range []struct {
		name   string
		where  string
		args   []any
		filter storage.ObjectFilter
		scope  *storage.Scope
	}{
		{name: "no predicate"},
		{name: "no predicate scoped", scope: scope},
		{name: "all statuses scoped", filter: storage.ObjectFilter{Status: storage.ObjectStatusAll}, scope: scope},
		{name: "predicate", where: "id = $1 OR type = $2", args: []any{"a", "b"}},
		{name: "predicate scoped paged", where: "id = $1", args: []any{"a"}, filter: storage.ObjectFilter{Type: "note", Tags: tagfilter.One("t"), ProfileID: "p", Limit: 3, Offset: 1}, scope: scope},
		{name: "argless predicate scoped", where: "1 = 1", scope: scope},
		{name: "tags any", where: "id = $1", args: []any{"a"}, filter: storage.ObjectFilter{Tags: mustTags(t, "a,b,c")}},
		{name: "tags required plain tier paged scoped", where: "id = $1", args: []any{"a"}, filter: storage.ObjectFilter{Tags: mustTags(t, "+a,b,+c,d"), Limit: 5, Offset: 2}, scope: scope},
		{name: "tags created bounds", filter: storage.ObjectFilter{Tags: mustTags(t, "a,b"), After: &time.Time{}, Limit: 1}},
	} {
		q := listBySQLQueries(c.where, c.args, c.filter, c.scope)
		assertBindsArgs(t, c.name+" count", q.count, q.args)
		assertBindsArgs(t, c.name+" page", q.page, q.pageArgs)
	}
}

func mustTags(t *testing.T, s string) tagfilter.Filter {
	t.Helper()
	f, err := tagfilter.Parse([]string{s})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestTagConditionsPG pins the tag predicates and the match-count order:
// any mode is one IN test, all mode one test per required tag, the count
// of selected tags carried orders; every label is a bound placeholder.
func TestTagConditionsPG(t *testing.T) {
	idx := 3
	conds, args := tagConditionsPG(mustTags(t, "+a,b,+c"), &idx)
	if len(conds) != 2 || len(args) != 2 || idx != 5 {
		t.Fatalf("all mode: conds %q args %v idx %d; want 2 tests, 2 args, idx 5", conds, args, idx)
	}
	if !strings.Contains(conds[0], "IN ($3)") || !strings.Contains(conds[1], "IN ($4)") {
		t.Errorf("all mode placeholders: %q", conds)
	}
	idx = 1
	conds, args = tagConditionsPG(mustTags(t, "a,b"), &idx)
	if len(conds) != 1 || len(args) != 2 || !strings.Contains(conds[0], "IN ($1, $2)") {
		t.Errorf("any mode: conds %q args %v", conds, args)
	}
	idx = 3
	match, margs := tagMatchPG(mustTags(t, "a,b"), &idx)
	if len(margs) != 2 || !strings.Contains(match, "IN ($3, $4)") || idx != 5 {
		t.Errorf("match count counts every selected tag: %q %v idx %d", match, margs, idx)
	}
	match, margs = tagMatchPG(mustTags(t, "a,b"), &idx)
	if len(margs) != 2 || match == "" {
		t.Errorf("any mode with two tags ranks: %q %v", match, margs)
	}
	for _, spec := range []string{"a", "+a,+b"} {
		if m, a := tagMatchPG(mustTags(t, spec), &idx); m != "" || a != nil {
			t.Errorf("%q cannot reorder, so it must not rank: %q %v", spec, m, a)
		}
	}
}

// TestCreatedConditionsHalfOpen: created bounds are since <= created <
// until, so a whole-day until (the next day's start) loses no microsecond.
func TestCreatedConditionsHalfOpen(t *testing.T) {
	a := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := a.AddDate(0, 0, 1)
	idx := 1
	conds, _ := createdConditionsPG(storage.ObjectFilter{After: &a, Before: &b}, &idx)
	if got := strings.Join(conds, " AND "); got != "created_at >= $1 AND created_at < $2" {
		t.Errorf("conditions = %q", got)
	}
}
