package http_test

import (
	"context"
	"encoding/json"
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
//	o-a  article  active     01-10       06-01       ux      acme.api     p-web     observation, go, ada, web, 03-10
//	o-b  note     active     02-10       02-11       design  acme.api     p-mail    task, rust, bob, email, 05-01
//	o-c  note     active     03-10       03-10       ux      other.x      p-web     -
//	o-i  note     inbox      04-10
//	o-d  article  discarded  05-10
//	o-r  note     raw        05-20
//
// Mention edges: o-a→acme/api, o-b→acme/api, o-b→hub/z, o-c→hub/z. So
// o-a reaches o-b at depth 1 and o-c at depth 2.
func seedObjectReadCorpus(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	objs := []*storage.KnowledgeObject{
		{
			ID: "o-a", Type: "article", Subtype: "article.long", Status: "active", Pipeline: "p-web",
			Tags:     []storage.Tag{{Label: "ux"}},
			Mentions: mentionURIs(t, "@acme.api"),
			Metadata: map[string]any{
				"type": "observation", "topics": []any{"go"}, "people": []any{"ada"},
				"source_type": "web", "dates_mentioned": []any{"2026-03-10"},
			},
			CreatedAt: day("2026-01-10T09:00:00Z"), UpdatedAt: day("2026-06-01T09:00:00Z"),
		},
		{
			ID: "o-b", Type: "note", Subtype: "note.short", Status: "active", Pipeline: "p-mail",
			Tags:     []storage.Tag{{Label: "design"}},
			Mentions: mentionURIs(t, "@acme.api"),
			Metadata: map[string]any{
				"type": "task", "topics": []any{"rust"}, "people": []any{"bob"},
				"source_type": "email", "dates_mentioned": []any{"2026-05-01"},
			},
			CreatedAt: day("2026-02-10T09:00:00Z"), UpdatedAt: day("2026-02-11T09:00:00Z"),
		},
		{
			ID: "o-c", Type: "note", Status: "active", Pipeline: "p-web",
			Tags:      []storage.Tag{{Label: "ux"}},
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
	code, body := e.get(t, "/api/v1/objects?"+rawQuery, dpkmstest.RoleReader)
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
		srv := dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
		seedObjectReadCorpus(t, driver)
		return objectReadEnv{srv: srv}
	}
	// One instance serves every read-only case.
	env := fresh(t)

	t.Run("ListFilters", func(t *testing.T) { testListFilters(t, env) })
	t.Run("ListOrderAndPaging", func(t *testing.T) { testListOrderAndPaging(t, env) })
	t.Run("ListRejectsBadParams", func(t *testing.T) { testListRejectsBadParams(t, env) })
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
		{"tag=ux", []string{"o-a", "o-c"}},
		{"mention=" + url.QueryEscape("@acme.api"), []string{"o-a", "o-b"}},
		{"mention=" + url.QueryEscape("ctxt://entity/acme/api"), []string{"o-a", "o-b"}},
		{"pipeline=p-web", []string{"o-a", "o-c"}},
		{"status=active", []string{"o-a", "o-b", "o-c"}},
		{"status=inbox", []string{"o-i"}},
		{"status=discarded", []string{"o-d"}},
		{"status=raw", []string{"o-r"}},
		{"status=all", []string{"o-a", "o-b", "o-c", "o-i", "o-d", "o-r"}},
		{"after=2026-02-01", []string{"o-b", "o-c"}},
		{"before=2026-02-01", []string{"o-a"}},
		{"after=" + url.QueryEscape("2026-02-10T09:00:01Z"), []string{"o-c"}},
		{"before=" + url.QueryEscape("2026-02-10T08:30:00-00:30"), []string{"o-a", "o-b"}},
		{"meta_type=task", []string{"o-b"}},
		{"topic=go", []string{"o-a"}},
		{"person=bob", []string{"o-b"}},
		{"source_type=web", []string{"o-a"}},
		{"since=2026-04-01", []string{"o-b"}},
		{"until=2026-04-01", []string{"o-a"}},
		{"type=note&tag=ux", []string{"o-c"}},
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
	}
	for _, tc := range cases {
		got := env.list(t, tc.query)
		assert.Equal(t, tc.want, ids(got.Data), "order for ?%s", tc.query)
		assert.Equal(t, tc.total, got.Total, "total for ?%s", tc.query)
	}
}

func testListRejectsBadParams(t *testing.T, env objectReadEnv) {
	cases := []struct{ query, param string }{
		{"before=yesterday", "before"},
		{"after=2026-13-01", "after"},
		{"since=2026-02-30", "since"},
		{"until=" + url.QueryEscape("2026-04-01T00:00:00Z"), "until"},
		{"limit=abc", "limit"},
		{"limit=-1", "limit"},
		{"offset=-1", "offset"},
		{"offset=x", "offset"},
		{"sort=recent", "sort"},
		{"dir=up", "dir"},
		{"status=bogus", "status"},
		{"foo=1", "foo"},
		{"q=" + url.QueryEscape("type==note"), "q"},
		{"tag=ux&tag=design", "tag"},
		{"type=", "type"},
	}
	for _, tc := range cases {
		code, body := env.get(t, "/api/v1/objects?"+tc.query, dpkmstest.RoleReader)
		assertInvalidParam(t, code, body, tc.param)
	}
}

func (e objectReadEnv) facets(t *testing.T, rawQuery string) map[string]int {
	t.Helper()
	code, body := e.get(t, "/api/v1/objects/facets?"+rawQuery, dpkmstest.RoleReader)
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
		{"tag=ux", map[string]int{"observation": 1, "(none)": 1}},
		{"type=note", map[string]int{"task": 1, "(none)": 1}},
		{"status=all", map[string]int{"observation": 1, "task": 1, "(none)": 4}},
		{"status=all&after=2026-04-01", map[string]int{"(none)": 3}},
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
		{"after=bad", "after"},
		{"status=nope", "status"},
		{"foo=1", "foo"},
	}
	for _, tc := range cases {
		code, body := env.get(t, "/api/v1/objects/facets?"+tc.query, dpkmstest.RoleReader)
		assertInvalidParam(t, code, body, tc.param)
	}
}

func (e objectReadEnv) related(t *testing.T, id, rawQuery string) []string {
	t.Helper()
	code, body := e.get(t, "/api/v1/objects/"+id+"/related?"+rawQuery, dpkmstest.RoleReader)
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

	code, body := env.get(t, "/api/v1/objects/o-missing/related", dpkmstest.RoleReader)
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
		code, body := env.get(t, "/api/v1/objects/o-a/related?"+tc.query, dpkmstest.RoleReader)
		assertInvalidParam(t, code, body, tc.param)
	}
}

// Every role reads; a request without a token is refused.
func testObjectReadRoles(t *testing.T, env objectReadEnv) {
	paths := []string{
		"/api/v1/objects?tag=ux",
		"/api/v1/objects/facets?tag=ux",
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
