package http_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	uri "hop.top/cite/scheme"
)

// The object read surface: GET /api/v1/objects with the full object
// filter, GET /api/v1/objects/facets and GET /api/v1/objects/{id}/related.
// The suite runs on SQLite here and on Postgres under -tags integration.

func TestObjectReads_SQLite(t *testing.T) {
	RunObjectReadSuite(t, storageutil.NewTestDriver)
}

func day(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func mentionURIs(t *testing.T, ms ...string) []uri.URI {
	t.Helper()
	out := make([]uri.URI, 0, len(ms))
	for _, m := range ms {
		u, ok := mentions.Parse(m)
		require.True(t, ok, m)
		out = append(out, u)
	}
	return out
}

// seedObjectReadCorpus writes six objects and their mention edges.
//
//	id   type     status     created     updated     tag     mention      pipeline  metadata
//	o-a  article  active     01-10       06-01       ux,api  acme.api     p-web     observation, go, ada, web, 03-10
//	o-b  note     active     02-10       02-11       design,api acme.api     p-mail    task, rust, bob, email, 05-01
//	o-c  note     active     03-10       03-10       ux      other.x      p-web     -
//	o-i  note     inbox      04-10
//	o-d  article  discarded  05-10
//	o-r  note     raw        05-20
//
// Mention edges: o-a→acme/api, o-b→acme/api, o-b→hub/z, o-c→hub/z. So
// o-a reaches o-b at depth 1 and o-c at depth 2.
func seedObjectReadCorpus(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	ctx := ownerCtx()
	objs := []*storage.KnowledgeObject{
		{
			ID: "o-a", Type: "article", Subtype: "article.long", Status: "active", Pipeline: "p-web",
			Tags:     []storage.Tag{{Label: "ux"}, {Label: "api"}},
			Mentions: mentionURIs(t, "@acme.api"),
			Metadata: map[string]any{
				"type": "observation", "topics": []any{"go"}, "people": []any{"ada"},
				"source_type": "web", "dates_mentioned": []any{"2026-03-10"},
			},
			CreatedAt: day("2026-01-10T09:00:00Z"), UpdatedAt: day("2026-06-01T09:00:00Z"),
		},
		{
			ID: "o-b", Type: "note", Subtype: "note.short", Status: "active", Pipeline: "p-mail",
			Tags:     []storage.Tag{{Label: "design"}, {Label: "api"}},
			Mentions: mentionURIs(t, "@acme.api"),
			Metadata: map[string]any{
				"type": "task", "topics": []any{"rust"}, "people": []any{"bob"},
				"source_type": "email", "dates_mentioned": []any{"2026-05-01"},
			},
			CreatedAt: day("2026-02-10T09:00:00Z"), UpdatedAt: day("2026-02-11T09:00:00Z"),
		},
		{
			ID: "o-c", Type: "note", Status: "active", Pipeline: "p-web",
			Tags:      []storage.Tag{{Label: "ux"}, {Label: "machine learning"}},
			Mentions:  mentionURIs(t, "@other.x"),
			CreatedAt: day("2026-03-10T09:00:00Z"), UpdatedAt: day("2026-03-10T09:00:00Z"),
		},
		{ID: "o-i", Type: "note", Status: "inbox", CreatedAt: day("2026-04-10T09:00:00Z"), UpdatedAt: day("2026-04-10T09:00:00Z")},
		{ID: "o-d", Type: "article", Status: "discarded", CreatedAt: day("2026-05-10T09:00:00Z"), UpdatedAt: day("2026-05-10T09:00:00Z")},
		{ID: "o-r", Type: "note", Status: "raw", CreatedAt: day("2026-05-20T09:00:00Z"), UpdatedAt: day("2026-05-20T09:00:00Z")},
	}
	for _, o := range objs {
		require.NoError(t, driver.Objects().Create(ctx, o), o.ID)
	}
	edges := [][3]string{
		{"e-a1", "o-a", "ctxt://entity/acme/api"},
		{"e-b1", "o-b", "ctxt://entity/acme/api"},
		{"e-b2", "o-b", "ctxt://entity/hub/z"},
		{"e-c1", "o-c", "ctxt://entity/hub/z"},
	}
	for _, e := range edges {
		require.NoError(t, driver.Edges().Create(ctx, &storage.Edge{
			ID: e[0], FromType: "object", FromID: e[1], ToType: "entity", ToID: e[2],
			EdgeType: "mentions", Weight: 1, CreatedAt: day("2026-01-01T00:00:00Z"),
		}), e[0])
	}
}

type objectReadEnv struct {
	srv *dpkmstest.Server
}

func (e objectReadEnv) get(t *testing.T, path, role string) (int, []byte) {
	t.Helper()
	resp := e.srv.Request(t, http.MethodGet, path, role, nil)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

type listBody struct {
	Data  []storage.KnowledgeObject `json:"data"`
	Total int                       `json:"total"`
}

func ids(objs []storage.KnowledgeObject) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.ID)
	}
	return out
}

func (e objectReadEnv) list(t *testing.T, rawQuery string) listBody {
	t.Helper()
	code, body := e.get(t, "/api/v1/objects?"+rawQuery, dpkmstest.RoleAdmin)
	require.Equal(t, http.StatusOK, code, "GET /objects?%s: %s", rawQuery, body)
	var out listBody
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

func assertInvalidParam(t *testing.T, code int, body []byte, param string) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, code, "%s", body)
	var env struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &env), "%s", body)
	assert.Equal(t, "INVALID_PARAM", env.Error.Code)
	assert.Equal(t, param, env.Error.Details["param"], "%s", body)
	assert.Contains(t, env.Error.Message, param)
}

// RunObjectReadSuite runs every object-read case against a fresh
// protected instance over the driver newDriver opens.
func RunObjectReadSuite(t *testing.T, newDriver func(*testing.T) storage.StorageDriver) {
	fresh := func(t *testing.T) objectReadEnv {
		t.Helper()
		driver := newDriver(t)
		srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())
		seedObjectReadCorpus(t, driver)
		return objectReadEnv{srv: srv}
	}
	// One instance serves every read-only case.
	env := fresh(t)

	t.Run("ListFilters", func(t *testing.T) { testListFilters(t, env) })
	t.Run("ListOrderAndPaging", func(t *testing.T) { testListOrderAndPaging(t, env) })
	t.Run("ListRejectsBadParams", func(t *testing.T) { testListRejectsBadParams(t, env) })
	t.Run("ListChangedAfter", func(t *testing.T) { testListChangedAfter(t, newDriver) })
	t.Run("Facets", func(t *testing.T) { testFacets(t, env) })
	t.Run("FacetsRejectsBadParams", func(t *testing.T) { testFacetsRejectsBadParams(t, env) })
	t.Run("Related", func(t *testing.T) { testRelated(t, env) })
	t.Run("RelatedRejectsBadParams", func(t *testing.T) { testRelatedRejectsBadParams(t, env) })
	t.Run("Roles", func(t *testing.T) { testObjectReadRoles(t, env) })
}

func testListFilters(t *testing.T, env objectReadEnv) {
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"o-a", "o-b", "o-c"}},
		{"type=article", []string{"o-a"}},
		{"subtype=note.short", []string{"o-b"}},
		{"tagged=ux", []string{"o-a", "o-c"}},
		{"tagged=" + url.PathEscape("ux,design"), []string{"o-a", "o-b", "o-c"}},
		{"tagged=" + url.PathEscape("api"), []string{"o-a", "o-b"}},
		{"tagged=" + url.PathEscape("+ux,+api"), []string{"o-a"}},
		{"tagged=" + url.PathEscape("+ux,+api,+design"), nil},
		{"tagged=" + url.PathEscape("+ux,api"), []string{"o-a", "o-c"}},
		{"tagged=" + url.PathEscape("design,api"), []string{"o-a", "o-b"}},
		{"tagged=nope", nil},
		// A label may hold spaces; only commas separate tags.
		{"tagged=" + url.PathEscape("machine learning"), []string{"o-c"}},
		{"tagged=" + url.PathEscape("machine"), nil},
		{"tagged=" + url.PathEscape("+ux,+machine learning"), []string{"o-c"}},
		{"tagged=" + url.PathEscape("design,machine learning"), []string{"o-b", "o-c"}},
		// A repeated parameter reads as a comma list.
		{"tagged=design&tagged=" + url.PathEscape("machine learning"), []string{"o-b", "o-c"}},
		{"tagged=+ux&tagged=%2Bapi", []string{"o-a"}},
		{"tagged=+ux&tagged=+api", []string{"o-a"}},
		{"tagged=+ux&tagged=api", []string{"o-a", "o-c"}},
		// "+" is the required marker: a raw "+" and %2B both keep it, and a
		// space in a label is %20 (a raw "+" inside a label stays a plus).
		{"tagged=+ux,api", []string{"o-a", "o-c"}},
		{"tagged=%2Bux,api", []string{"o-a", "o-c"}},
		{"tagged=%2Bux,%2Bapi", []string{"o-a"}},
		{"tagged=machine%20learning", []string{"o-c"}},
		{"tagged=%2Bmachine%20learning,ux", []string{"o-c"}},
		{"tagged=+machine%20learning,ux", []string{"o-c"}},
		{"tagged=machine+learning", nil},
		{"tagged=" + url.PathEscape("+ux,+nope"), nil},
		{"tagged=" + url.PathEscape("+ux,nope"), []string{"o-a", "o-c"}},
		{"mention=" + url.QueryEscape("@acme.api"), []string{"o-a", "o-b"}},
		{"mention=" + url.QueryEscape("ctxt://entity/acme/api"), []string{"o-a", "o-b"}},
		{"pipeline=p-web", []string{"o-a", "o-c"}},
		{"status=active", []string{"o-a", "o-b", "o-c"}},
		{"status=inbox", []string{"o-i"}},
		{"status=discarded", []string{"o-d"}},
		{"status=raw", []string{"o-r"}},
		{"status=all", []string{"o-a", "o-b", "o-c", "o-i", "o-d", "o-r"}},
		{"since=2026-02-01", []string{"o-b", "o-c"}},
		{"until=2026-02-01", []string{"o-a"}},
		{"since=" + url.QueryEscape("2026-02-10T09:00:01Z"), []string{"o-c"}},
		{"until=" + url.QueryEscape("2026-02-10T08:30:01-00:30"), []string{"o-a", "o-b"}},
		// An instant bound is exclusive; a date bound takes its whole UTC day.
		{"until=" + url.QueryEscape("2026-02-10T09:00:00Z"), []string{"o-a"}},
		{"until=2026-02-09", []string{"o-a"}},
		{"until=2026-02-10", []string{"o-a", "o-b"}},
		{"since=2026-02-10&until=2026-02-10", []string{"o-b"}},
		{"since=2026-01-15&until=2026-03-01&tagged=api", []string{"o-b"}},
		{"meta_type=task", []string{"o-b"}},
		{"topic=go", []string{"o-a"}},
		{"person=bob", []string{"o-b"}},
		{"source_type=web", []string{"o-a"}},
		{"mentioned_since=2026-04-01", []string{"o-b"}},
		{"mentioned_until=2026-04-01", []string{"o-a"}},
		{"type=note&tagged=ux", []string{"o-c"}},
		{"status=all&type=article", []string{"o-a", "o-d"}},
	}
	for _, tc := range cases {
		got := env.list(t, tc.query)
		assert.ElementsMatch(t, tc.want, ids(got.Data), "GET /objects?%s", tc.query)
		assert.Equal(t, len(tc.want), got.Total, "total for ?%s", tc.query)
	}
}

func testListOrderAndPaging(t *testing.T, env objectReadEnv) {
	cases := []struct {
		query string
		want  []string
		total int
	}{
		{"", []string{"o-c", "o-b", "o-a"}, 3},
		{"sort=created_at&dir=asc", []string{"o-a", "o-b", "o-c"}, 3},
		{"sort=updated_at&dir=desc", []string{"o-a", "o-c", "o-b"}, 3},
		{"sort=updated_at&dir=asc", []string{"o-b", "o-c", "o-a"}, 3},
		{"sort=created_at&dir=asc&limit=1", []string{"o-a"}, 3},
		{"sort=created_at&dir=asc&limit=1&offset=1", []string{"o-b"}, 3},
		{"status=all&limit=2", []string{"o-r", "o-d"}, 6},
		{"status=all&limit=0", []string{"o-r", "o-d", "o-i", "o-c", "o-b", "o-a"}, 6},
		// Results matching more of the listed tags come first (a: ux,api;
		// b: design,api; c: ux), then the sort, then id; totals ignore the
		// page and offset pages are stable.
		{"tagged=" + url.PathEscape("ux,api"), []string{"o-a", "o-c", "o-b"}, 3},
		{"tagged=" + url.PathEscape("ux,api") + "&dir=asc", []string{"o-a", "o-b", "o-c"}, 3},
		{"tagged=" + url.PathEscape("ux,api") + "&limit=1", []string{"o-a"}, 3},
		{"tagged=" + url.PathEscape("ux,api") + "&limit=1&offset=1", []string{"o-c"}, 3},
		{"tagged=" + url.PathEscape("ux,api") + "&limit=1&offset=2", []string{"o-b"}, 3},
		{"tagged=" + url.PathEscape("+ux,api"), []string{"o-a", "o-c"}, 2},
		{"tagged=" + url.PathEscape("ux,api,design"), []string{"o-b", "o-a", "o-c"}, 3},
		{"tagged=" + url.PathEscape("ux,api,design") + "&sort=updated_at&dir=asc", []string{"o-b", "o-a", "o-c"}, 3},
		{"tagged=" + url.PathEscape("ux,design"), []string{"o-c", "o-b", "o-a"}, 3},
		{"tagged=" + url.PathEscape("+ux,+api"), []string{"o-a"}, 1},
	}
	for _, tc := range cases {
		got := env.list(t, tc.query)
		assert.Equal(t, tc.want, ids(got.Data), "order for ?%s", tc.query)
		assert.Equal(t, tc.total, got.Total, "total for ?%s", tc.query)
	}
}

func testListRejectsBadParams(t *testing.T, env objectReadEnv) {
	cases := []struct{ query, param string }{
		{"until=yesterday", "until"},
		{"since=2026-13-01", "since"},
		{"mentioned_since=2026-02-30", "mentioned_since"},
		{"mentioned_until=" + url.QueryEscape("2026-04-01T00:00:00Z"), "mentioned_until"},
		{"before=2026-01-01", "before"},
		{"after=2026-01-01", "after"},
		{"tag=ux", "tag"},
		{"limit=abc", "limit"},
		{"limit=-1", "limit"},
		{"offset=-1", "offset"},
		{"offset=x", "offset"},
		{"sort=recent", "sort"},
		{"dir=up", "dir"},
		{"status=bogus", "status"},
		{"foo=1", "foo"},
		{"q=" + url.QueryEscape("type==note"), "q"},
		{"tagged=ux&tagged=" + url.PathEscape("a,,b"), "tagged"},
		{"tagged=" + url.PathEscape("a,,b"), "tagged"},
		{"tagged=" + url.PathEscape("a,"), "tagged"},
		{"tagged=+", "tagged"},
		{"tagged=%2B", "tagged"},
		{"tagged=" + url.PathEscape("a,+"), "tagged"},
		{"tagged=-a", "tagged"},
		{"tagged=" + url.PathEscape("a,-b"), "tagged"},
		{"tagged=" + url.PathEscape("+-a"), "tagged"},
		{"tagged=%zz", "tagged"},
		{"type=", "type"},
		{"changed_after=-1", "changed_after"},
		{"changed_after=abc", "changed_after"},
		{"changed_after=1.5", "changed_after"},
		{"changed_after=99999999999999999999", "changed_after"},
		{"changed_after=", "changed_after"},
		{"changed_after=1&changed_after=2", "changed_after"},
		{"changed_after=0&sort=created_at", "sort"},
		{"changed_after=0&dir=asc", "dir"},
		{"changed_after=0&offset=1", "offset"},
	}
	for _, tc := range cases {
		code, body := env.get(t, "/api/v1/objects?"+tc.query, dpkmstest.RoleAdmin)
		assertInvalidParam(t, code, body, tc.param)
	}
}

// changeList is a changed_after page. Objects carry no change sequence
// of their own: the page's cursor is next_changed_after.
type changeList struct {
	Data []storage.KnowledgeObject `json:"data"`
	Next *int64                    `json:"next_changed_after"`
}

func (e objectReadEnv) changes(t *testing.T, rawQuery string) (changeList, []byte) {
	t.Helper()
	code, body := e.get(t, "/api/v1/objects?"+rawQuery, dpkmstest.RoleAdmin)
	require.Equal(t, http.StatusOK, code, "GET /objects?%s: %s", rawQuery, body)
	var out changeList
	require.NoError(t, json.Unmarshal(body, &out))
	return out, body
}

// changed_after lists in change order and pages by next_changed_after;
// an update of an old object lists past the cursor taken before it,
// where the created_at bound does not reach it.
func testListChangedAfter(t *testing.T, newDriver func(*testing.T) storage.StorageDriver) {
	driver := newDriver(t)
	env := objectReadEnv{srv: dpkmstest.Start(t, driver, dpkmstest.WithKeys())}
	seedObjectReadCorpus(t, driver)

	// Insertion order is change order; paging walks it to the end.
	all, body := env.changes(t, "changed_after=0&status=all")
	assert.Equal(t, []string{"o-a", "o-b", "o-c", "o-i", "o-d", "o-r"}, ids(all.Data))
	require.NotNil(t, all.Next, "%s", body)
	assert.NotContains(t, string(body), "change_seq", "objects do not expose their sequence")

	var walked []string
	var cursor int64
	for range 10 {
		page, _ := env.changes(t, fmt.Sprintf("changed_after=%d&status=all&limit=2", cursor))
		require.NotNil(t, page.Next)
		if len(page.Data) == 0 {
			assert.Equal(t, cursor, *page.Next, "an empty page keeps the cursor")
			break
		}
		assert.Greater(t, *page.Next, cursor)
		walked = append(walked, ids(page.Data)...)
		cursor = *page.Next
	}
	assert.Equal(t, ids(all.Data), walked)
	assert.Equal(t, *all.Next, cursor)

	// Status keeps its default: the active objects only.
	active, _ := env.changes(t, "changed_after=0")
	assert.Equal(t, []string{"o-a", "o-b", "o-c"}, ids(active.Data))

	// Update an old object, leaving its timestamps where they were.
	before := *all.Next
	ctx := ownerCtx()
	o, err := driver.Objects().Get(ctx, "o-a")
	require.NoError(t, err)
	o.Tags = append(o.Tags, storage.Tag{Label: "edited"})
	require.NoError(t, driver.Objects().Update(ctx, o))

	changed, _ := env.changes(t, fmt.Sprintf("changed_after=%d&status=all", before))
	assert.Equal(t, []string{"o-a"}, ids(changed.Data))
	assert.Greater(t, *changed.Next, before)
	byCreated := env.list(t, "status=all&since="+url.QueryEscape(o.CreatedAt.Add(time.Second).Format(time.RFC3339)))
	assert.NotContains(t, ids(byCreated.Data), "o-a", "created_at does not see the update")

	// Nothing since: empty page, same cursor, total 0.
	none, _ := env.changes(t, fmt.Sprintf("changed_after=%d&status=all", *changed.Next))
	assert.Empty(t, none.Data)
	assert.Equal(t, *changed.Next, *none.Next)

	// changed_after composes with a tag selection that cannot reorder
	// (one tag, or required tags only), in change order; a list that ranks
	// by tags matched (two or more tags with none required, or a plain tag
	// beside a required one) is refused.
	tagged, _ := env.changes(t, "changed_after=0&status=all&tagged=ux")
	assert.Equal(t, []string{"o-c", "o-a"}, ids(tagged.Data), "o-a was updated last")
	allTags, _ := env.changes(t, "changed_after=0&status=all&tagged="+url.PathEscape("+ux,+api"))
	assert.Equal(t, []string{"o-a"}, ids(allTags.Data))
	for _, q := range []string{"ux,design", "ux,api", "+ux,api", "ux,api,design"} {
		code, body := env.get(t, "/api/v1/objects?changed_after=0&tagged="+url.PathEscape(q), dpkmstest.RoleAdmin)
		assertInvalidParam(t, code, body, "tagged")
	}

	// Without the parameter the response has no cursor.
	_, plain := env.get(t, "/api/v1/objects", dpkmstest.RoleAdmin)
	assert.NotContains(t, string(plain), "next_changed_after")
}

func (e objectReadEnv) facets(t *testing.T, rawQuery string) map[string]int {
	t.Helper()
	code, body := e.get(t, "/api/v1/objects/facets?"+rawQuery, dpkmstest.RoleAdmin)
	require.Equal(t, http.StatusOK, code, "GET /objects/facets?%s: %s", rawQuery, body)
	var out struct {
		Data map[string]int `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.Data
}

func testFacets(t *testing.T, env objectReadEnv) {
	cases := []struct {
		query string
		want  map[string]int
	}{
		{"", map[string]int{"observation": 1, "task": 1, "(none)": 1}},
		{"tagged=ux", map[string]int{"observation": 1, "(none)": 1}},
		{"tagged=" + url.PathEscape("+ux,+api"), map[string]int{"observation": 1}},
		{"type=note", map[string]int{"task": 1, "(none)": 1}},
		{"status=all", map[string]int{"observation": 1, "task": 1, "(none)": 4}},
		{"status=all&since=2026-04-01", map[string]int{"(none)": 3}},
		{"meta_type=observation", map[string]int{"observation": 1}},
		{"status=inbox&type=article", map[string]int{}},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, env.facets(t, tc.query), "facets ?%s", tc.query)
	}
}

func testFacetsRejectsBadParams(t *testing.T, env objectReadEnv) {
	cases := []struct{ query, param string }{
		{"limit=5", "limit"},
		{"offset=1", "offset"},
		{"sort=created_at", "sort"},
		{"dir=asc", "dir"},
		{"changed_after=0", "changed_after"},
		{"since=bad", "since"},
		{"status=nope", "status"},
		{"foo=1", "foo"},
	}
	for _, tc := range cases {
		code, body := env.get(t, "/api/v1/objects/facets?"+tc.query, dpkmstest.RoleAdmin)
		assertInvalidParam(t, code, body, tc.param)
	}
}

func (e objectReadEnv) related(t *testing.T, id, rawQuery string) []string {
	t.Helper()
	code, body := e.get(t, "/api/v1/objects/"+id+"/related?"+rawQuery, dpkmstest.RoleAdmin)
	require.Equal(t, http.StatusOK, code, "GET /objects/%s/related?%s: %s", id, rawQuery, body)
	var out struct {
		Data []storage.KnowledgeObject `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.NotNil(t, out.Data, "data must be an array, not null: %s", body)
	return ids(out.Data)
}

func testRelated(t *testing.T, env objectReadEnv) {
	assert.ElementsMatch(t, []string{"o-b"}, env.related(t, "o-a", ""))
	assert.ElementsMatch(t, []string{"o-b"}, env.related(t, "o-a", "depth=1"))
	assert.ElementsMatch(t, []string{"o-b", "o-c"}, env.related(t, "o-a", "depth=2"))
	assert.ElementsMatch(t, []string{"o-b", "o-c"}, env.related(t, "o-a", "depth=3&limit=100"))
	assert.Equal(t, []string{"o-b"}, env.related(t, "o-a", "depth=2&limit=1"))
	assert.ElementsMatch(t, []string{"o-a", "o-c"}, env.related(t, "o-b", "depth=1"))
	assert.Empty(t, env.related(t, "o-i", ""))

	code, body := env.get(t, "/api/v1/objects/o-missing/related", dpkmstest.RoleAdmin)
	assert.Equal(t, http.StatusNotFound, code, "%s", body)
}

func testRelatedRejectsBadParams(t *testing.T, env objectReadEnv) {
	cases := []struct{ query, param string }{
		{"depth=0", "depth"},
		{"depth=4", "depth"},
		{"depth=x", "depth"},
		{"limit=0", "limit"},
		{"limit=101", "limit"},
		{"limit=-3", "limit"},
		{"offset=1", "offset"},
		{"depth=1&depth=2", "depth"},
	}
	for _, tc := range cases {
		code, body := env.get(t, "/api/v1/objects/o-a/related?"+tc.query, dpkmstest.RoleAdmin)
		assertInvalidParam(t, code, body, tc.param)
	}
}

// Every role reads; a request without a token is refused.
func testObjectReadRoles(t *testing.T, env objectReadEnv) {
	paths := []string{
		"/api/v1/objects?tagged=ux",
		"/api/v1/objects/facets?tagged=ux",
		"/api/v1/objects/o-a/related?depth=2",
	}
	for _, p := range paths {
		for _, role := range dpkmstest.Roles {
			code, body := env.get(t, p, role)
			assert.Equal(t, http.StatusOK, code, "%s as %s: %s", p, role, body)
		}
		code, _ := env.get(t, p, "")
		assert.Equal(t, http.StatusUnauthorized, code, "%s without a token", p)
	}
}

// The facets route must not be shadowed by GET /objects/{id}.
func TestObjectFacetsRouteIsNotAnObjectID(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver)
	resp := srv.Request(t, http.MethodGet, "/api/v1/objects/facets", "", nil)
	defer resp.Body.Close()
	var out map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	assert.Equal(t, []string{"data"}, keys)
	assert.Equal(t, "{}", string(out["data"]))
}
