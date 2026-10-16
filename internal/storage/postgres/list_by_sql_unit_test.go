package postgres

import (
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
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
		{name: "predicate scoped paged", where: "id = $1", args: []any{"a"}, filter: storage.ObjectFilter{Type: "note", Tag: "t", ProfileID: "p", Limit: 3, Offset: 1}, scope: scope},
		{name: "argless predicate scoped", where: "1 = 1", scope: scope},
	} {
		q := listBySQLQueries(c.where, c.args, c.filter, c.scope)
		assertBindsArgs(t, c.name+" count", q.count, q.args)
		assertBindsArgs(t, c.name+" page", q.page, q.pageArgs)
	}
}
