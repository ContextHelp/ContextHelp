package cmd

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
	"hop.top/kit/go/console/output"
)

// Fixtures for list, show, export, classify, compose and profile schema
// evolve, which read objects over the API only.

// edt is the zone the corpus is stamped in: the daemon stamps objects in
// its local zone, and the API compares bounds as instants.
var edt = time.FixedZone("EDT", -4*3600)

// readAt is the corpus's first creation time.
var readAt = time.Date(2026, 3, 10, 9, 0, 0, 0, edt)

// seedReadCorpus stores five objects created a day apart, oldest first:
//
//	r-url     url/article, tag ux, mentions @ui.best-practice, pipeline url.article,
//	          metadata observation/auth/alice/slack, dates_mentioned 2026-04-10
//	r-note    note, tag ux, metadata task/auth, updated last
//	r-idea    note, tag design, metadata observation/billing
//	r-inbox   note, tag ux, status inbox
//	r-plain   note, no tag
func seedReadCorpus(t *testing.T, db *testDB) {
	t.Helper()
	day := func(n int) time.Time { return readAt.Add(time.Duration(n) * 24 * time.Hour) }
	objs := []*storage.KnowledgeObject{
		{
			ID: "r-url", Type: "url", Subtype: "article", Pipeline: "url.article", Status: "active",
			Summaries: []string{"Signup flow practices"},
			Tags:      []storage.Tag{{Label: "ux"}},
			Mentions:  []uri.URI{{Scheme: "ctxt", Namespace: "entity", ID: "ui/best-practice"}},
			Metadata: map[string]any{
				"type": "observation", "topics": []any{"auth"}, "people": []any{"alice-chen"},
				"source_type": "slack", "dates_mentioned": []any{"2026-04-10"},
			},
		},
		{
			ID: "r-note", Type: "note", Status: "active", Summaries: []string{"Rotate the auth keys"},
			Tags: []storage.Tag{{Label: "ux"}}, Metadata: map[string]any{"type": "task", "topics": []any{"auth"}},
		},
		{
			ID: "r-idea", Type: "note", Status: "active", Summaries: []string{"Usage-based billing"},
			Tags: []storage.Tag{{Label: "design"}}, Metadata: map[string]any{"type": "observation", "topics": []any{"billing"}},
		},
		{ID: "r-inbox", Type: "note", Status: "inbox", Tags: []storage.Tag{{Label: "ux"}}},
		{ID: "r-plain", Type: "note", Status: "active"},
	}
	for i, o := range objs {
		o.CreatedAt = day(i)
		o.UpdatedAt = o.CreatedAt
		if o.ID == "r-note" {
			o.UpdatedAt = day(10)
		}
		seedObj(t, db, o)
	}
}

// decodeFirstJSON decodes the first JSON document in out, skipping any text
// (hints, banners) before it.
func decodeFirstJSON(t *testing.T, out string, v any) {
	t.Helper()
	i := strings.Index(out, "{")
	if i < 0 {
		t.Fatalf("no JSON document in output:\n%s", out)
	}
	if err := json.NewDecoder(strings.NewReader(out[i:])).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out)
	}
}

type listDoc struct {
	Objects []storage.KnowledgeObject `json:"objects"`
	Items   []storage.KnowledgeObject `json:"items"`
	Total   int                       `json:"total"`
	Facets  map[string]int            `json:"facets"`
}

func (d listDoc) ids() []string {
	var ids []string
	for _, o := range append(d.Objects, d.Items...) {
		ids = append(ids, o.ID)
	}
	return ids
}

// listIDs runs list with --format json and returns the IDs in output
// order.
func listIDs(t *testing.T, db *testDB, args ...string) []string {
	t.Helper()
	out, err := db.exec(append([]string{"list", "--format", "json"}, args...)...)
	if err != nil {
		t.Fatalf("list %v: %v\n%s", args, err, out)
	}
	var doc listDoc
	decodeFirstJSON(t, out, &doc)
	return doc.ids()
}

func sorted(ids []string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

// TestListFiltersOverAPI: every filter flag reaches GET /api/v1/objects
// and selects what it names.
func TestListFiltersOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedReadCorpus(t, db)
	cases := []struct {
		args []string
		want []string
	}{
		{nil, []string{"r-idea", "r-note", "r-plain", "r-url"}},
		{[]string{"--type", "url"}, []string{"r-url"}},
		{[]string{"--subtype", "article"}, []string{"r-url"}},
		{[]string{"--tagged", "ux"}, []string{"r-note", "r-url"}},
		{[]string{"--mention", "@ui.best-practice"}, []string{"r-url"}},
		{[]string{"--pipeline", "url.article"}, []string{"r-url"}},
		{[]string{"--status", "inbox"}, []string{"r-inbox"}},
		{[]string{"--status", "all", "--tagged", "ux"}, []string{"r-inbox", "r-note", "r-url"}},
		{[]string{"--after", "2026-03-11"}, []string{"r-idea", "r-note", "r-plain"}},
		{[]string{"--before", "2026-03-12"}, []string{"r-note", "r-url"}},
		{[]string{"--meta-type", "observation"}, []string{"r-idea", "r-url"}},
		{[]string{"--topic", "auth"}, []string{"r-note", "r-url"}},
		{[]string{"--person", "alice-chen"}, []string{"r-url"}},
		{[]string{"--source-type", "slack"}, []string{"r-url"}},
		{[]string{"--since", "2026-04-01", "--until", "2026-04-30"}, []string{"r-url"}},
		{[]string{"--since", "2026-05-01"}, nil},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if got := sorted(listIDs(t, db, tc.args...)); !slices.Equal(got, tc.want) {
				t.Errorf("list %v = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestListSortOverAPI: --sort takes created (and its synonym recent) or
// updated; --dir orders; paging follows the order.
func TestListSortOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedReadCorpus(t, db)
	byCreated := []string{"r-plain", "r-idea", "r-note", "r-url"}
	cases := []struct {
		args []string
		want []string
	}{
		{nil, byCreated},
		{[]string{"--sort", "created"}, byCreated},
		{[]string{"--sort", "recent"}, byCreated},
		{[]string{"--sort", "created", "--dir", "asc"}, []string{"r-url", "r-note", "r-idea", "r-plain"}},
		{[]string{"--sort", "updated"}, []string{"r-note", "r-plain", "r-idea", "r-url"}},
		{[]string{"--limit", "2", "--start", "1"}, []string{"r-idea", "r-note"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if got := listIDs(t, db, tc.args...); !slices.Equal(got, tc.want) {
				t.Errorf("list %v = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestListRejectsWhatStorageCannotDo: sort keys and multi-tag filters
// storage has no support for are usage errors, not silent no-ops.
func TestListRejectsWhatStorageCannotDo(t *testing.T) {
	db := setupTestDB(t)
	for _, args := range [][]string{
		{"--sort", "view"}, {"--sort", "match"}, {"--sort", "weight"}, {"--sort", "score"},
		{"--sort", "created_at"}, {"--dir", "up"},
		{"--tagged", "ux,design"},
		{"--after", "yesterday"}, {"--before", "03/01/2026"}, {"--since", "2026-4-1"}, {"--until", "now"},
		{"--status", "archived"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := db.exec(append([]string{"list"}, args...)...)
			if got := exitOf(err); got != output.ExitUsage {
				t.Fatalf("exit %d (%v); want 2\n%s", got, err, out)
			}
		})
	}
	for _, flag := range []string{"--hint", "--orig-lang", "--no-track"} {
		if _, err := db.exec("list", flag, "x"); err == nil {
			t.Errorf("list %s was accepted; the flag is removed", flag)
		}
	}
}

// TestListFacetsOverAPI: --facets counts through GET /objects/facets
// with the same filter.
func TestListFacetsOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedReadCorpus(t, db)

	out, err := db.exec("list", "--facets", "--tagged", "ux", "--format", "json")
	if err != nil {
		t.Fatalf("list --facets: %v\n%s", err, out)
	}
	var doc listDoc
	decodeFirstJSON(t, out, &doc)
	if want := map[string]int{"observation": 1, "task": 1}; !mapsEqual(doc.Facets, want) {
		t.Errorf("facets = %v, want %v", doc.Facets, want)
	}
	if got := sorted(doc.ids()); !slices.Equal(got, []string{"r-note", "r-url"}) || doc.Total != 2 {
		t.Errorf("objects = %v (total %d)", got, doc.Total)
	}

	out, err = db.exec("list", "--facets")
	if err != nil {
		t.Fatalf("list --facets (table): %v\n%s", err, out)
	}
	for _, want := range []string{"Facets (metadata type):", "observation", "(none)", "r-plain"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output lacks %q:\n%s", want, out)
		}
	}
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestListCursorOverAPI: the cursor stays a client file; each listing
// returns only objects created after the last one it advanced past, even
// though the objects carry a non-UTC offset.
func TestListCursorOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedReadCorpus(t, db)

	out, err := db.exec("list", "--cursor", "feed", "--advance", "--format", "json")
	if err != nil {
		t.Fatalf("first cursor list: %v\n%s", err, out)
	}
	var doc listDoc
	decodeFirstJSON(t, out, &doc)
	if want := []string{"r-url", "r-note", "r-idea", "r-plain"}; !slices.Equal(doc.ids(), want) {
		t.Fatalf("first cursor list = %v, want %v (created, ascending)", doc.ids(), want)
	}

	if got := listIDs(t, db, "--cursor", "feed", "--advance"); len(got) != 0 {
		t.Fatalf("second cursor list = %v, want nothing new", got)
	}

	seedObj(t, db, &storage.KnowledgeObject{
		ID: "r-new", Type: "note", Status: "active",
		CreatedAt: readAt.Add(4*24*time.Hour + 5*time.Second), UpdatedAt: readAt.Add(4*24*time.Hour + 5*time.Second),
	})
	if got := listIDs(t, db, "--cursor", "feed", "--advance"); !slices.Equal(got, []string{"r-new"}) {
		t.Fatalf("after one new object: %v, want [r-new]", got)
	}
}

// TestObjectReadsNeverOpenLocalStore: every command answers from the
// instance with the config's storage path pointing somewhere new, and
// none of them creates a database there.
func TestObjectReadsNeverOpenLocalStore(t *testing.T) {
	db := setupTestDB(t)
	seedReadCorpus(t, db)
	seedObj(t, db, &storage.KnowledgeObject{ID: "o-cls", Summaries: []string{"The server crashed"}})
	withClassifier(t)
	local := pointStorageElsewhere(t, db)
	if _, err := db.exec("profile", "create", "ops"); err != nil {
		t.Fatalf("profile create: %v", err)
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"list"}, "r-url"},
		{[]string{"list", "--facets"}, "observation"},
		{[]string{"list", "--cursor", "c1", "--advance"}, "r-url"},
		{[]string{"show", "r-url"}, "Knowledge Object: r-url"},
		{[]string{"export", "r-url"}, `"id": "r-url"`},
		{[]string{"classify", "o-cls"}, "The server crashed"},
		{[]string{"compose", "brief", "--tagged", "ux"}, "## r-url"},
		{[]string{"profile", "schema", "evolve", "ops"}, "ops"},
	} {
		out, err := db.exec(tc.args...)
		if err != nil {
			t.Errorf("%v: %v\n%s", tc.args, err, out)
			continue
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("%v output lacks %q:\n%s", tc.args, tc.want, out)
		}
	}
	assertNoLocalStore(t, local)
}

// TestObjectReadsExitCodes: unreachable 70, refused token 5, unknown
// object 3, for every command.
func TestObjectReadsExitCodes(t *testing.T) {
	withClassifier(t)
	commands := [][]string{
		{"list"}, {"list", "--facets"}, {"show", "o-x"}, {"export", "o-x"}, {"classify", "o-x"},
		{"compose", "brief"}, {"profile", "schema", "evolve", "ops"},
	}
	profile := func(t *testing.T, db *testDB) {
		t.Helper()
		if _, err := db.exec("profile", "create", "ops"); err != nil {
			t.Fatalf("profile create: %v", err)
		}
	}

	down := setupTestDB(t, dpkmstest.Unreachable())
	profile(t, down)
	for _, args := range commands {
		if out, err := down.exec(args...); exitOf(err) != output.ExitPrerequisite {
			t.Errorf("unreachable %v: exit %d (%v); want 70\n%s", args, exitOf(err), err, out)
		}
	}

	locked := setupTestDB(t, dpkmstest.WithKeys())
	profile(t, locked)
	locked.useRole(t, "none")
	for _, args := range commands {
		if out, err := locked.exec(args...); exitOf(err) != output.ExitUnauthorized {
			t.Errorf("no token %v: exit %d (%v); want 5\n%s", args, exitOf(err), err, out)
		}
	}

	open := setupTestDB(t, dpkmstest.WithKeys())
	open.useRole(t, dpkmstest.RoleReader)
	for _, args := range [][]string{{"show", "o-x"}, {"export", "o-x"}, {"classify", "o-x"}} {
		if out, err := open.exec(args...); exitOf(err) != output.ExitNotFound {
			t.Errorf("reader %v: exit %d (%v); want 3\n%s", args, exitOf(err), err, out)
		}
	}
}

// TestShowOverAPI: show prints the object and its related objects from
// GET /objects/{id}/related.
func TestShowOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedReadCorpus(t, db)
	ctx := context.Background()
	for _, e := range [][2]string{{"r-url", "ctxt://entity/acme/api"}, {"r-note", "ctxt://entity/acme/api"}} {
		if err := db.Driver.Edges().Create(ctx, &storage.Edge{
			ID: "e-" + e[0], FromType: "object", FromID: e[0], ToType: "entity", ToID: e[1],
			EdgeType: "mentions", Weight: 1, CreatedAt: readAt,
		}); err != nil {
			t.Fatal(err)
		}
	}

	out, err := db.exec("show", "r-url")
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	for _, want := range []string{"Knowledge Object: r-url", "Subtype:   article", "Tags:      ux", "See Also:", "r-note  Rotate the auth keys"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output lacks %q:\n%s", want, out)
		}
	}

	for _, args := range [][]string{{"--raw"}, {"--format", "json"}} {
		out, err := db.exec(append([]string{"show", "r-url"}, args...)...)
		if err != nil {
			t.Fatalf("show %v: %v\n%s", args, err, out)
		}
		var obj storage.KnowledgeObject
		decodeFirstJSON(t, out, &obj)
		if obj.ID != "r-url" || obj.Subtype != "article" {
			t.Errorf("show %v = %+v", args, obj)
		}
	}
}

// TestExportOverAPI: export prints the object as JSON; --dest is gone.
func TestExportOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedReadCorpus(t, db)
	out, err := db.exec("export", "r-url")
	if err != nil {
		t.Fatalf("export: %v\n%s", err, out)
	}
	var obj storage.KnowledgeObject
	decodeFirstJSON(t, out, &obj)
	if obj.ID != "r-url" || len(obj.Summaries) != 1 {
		t.Errorf("export = %+v", obj)
	}
	if _, err := db.exec("export", "r-url", "--dest", "/tmp/vault"); err == nil {
		t.Error("export --dest was accepted; the flag is removed")
	}
}

// withClassifier replaces the classifier with one that echoes its input,
// so classify runs without cgo.
func withClassifier(t *testing.T) *string {
	t.Helper()
	var seen string
	prev := classifyFunc
	classifyFunc = func(text string) (*classifyResult, error) {
		seen = text
		return &classifyResult{Results: []classifySignal{{Name: "echo", Type: "label", Confidence: 1, Labels: []string{text}}}}, nil
	}
	t.Cleanup(func() { classifyFunc = prev })
	return &seen
}

// TestClassifyObjectOverAPI: an o- argument classifies the object's
// first summary, read from the instance.
func TestClassifyObjectOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedObj(t, db, &storage.KnowledgeObject{ID: "o-cls", Summaries: []string{"The server crashed"}})
	seen := withClassifier(t)

	if out, err := db.exec("classify", "o-cls"); err != nil {
		t.Fatalf("classify: %v\n%s", err, out)
	}
	if *seen != "The server crashed" {
		t.Errorf("classified %q, want the object's summary", *seen)
	}
	if out, err := db.exec("classify", "plain text"); err != nil || *seen != "plain text" {
		t.Errorf("raw text: %q %v\n%s", *seen, err, out)
	}
}

// TestComposeOverAPI: compose selects through GET /objects and renders
// on the client.
func TestComposeOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedReadCorpus(t, db)

	out, err := db.exec("compose", "brief", "--tagged", "ux", "--since", "2026-03-11")
	if err != nil {
		t.Fatalf("compose: %v\n%s", err, out)
	}
	if !strings.Contains(out, "# brief") || !strings.Contains(out, "## r-note") || strings.Contains(out, "## r-url") ||
		!strings.Contains(out, "Rotate the auth keys [ref:r-note]") {
		t.Errorf("compose output:\n%s", out)
	}

	out, err = db.exec("compose", "summary", "--mention", "@ui.best-practice", "--export", "json")
	if err != nil {
		t.Fatalf("compose json: %v\n%s", err, out)
	}
	var res struct {
		SourceIDs []string `json:"source_ids"`
	}
	decodeFirstJSON(t, out, &res)
	if !slices.Equal(res.SourceIDs, []string{"r-url"}) {
		t.Errorf("source_ids = %v", res.SourceIDs)
	}

	for _, args := range [][]string{{"--since", "yesterday"}, {"--until", "now"}, {"--tagged", "ux,design"}} {
		if _, err := db.exec(append([]string{"compose", "brief"}, args...)...); exitOf(err) != output.ExitUsage {
			t.Errorf("compose %v: exit %d (%v); want 2", args, exitOf(err), err)
		}
	}
	if _, err := db.exec("compose", "brief", "--session", "sess_1"); err == nil {
		t.Error("compose --session was accepted; the flag is removed")
	}
}

// TestSchemaEvolveOverAPI: evolve reads recent objects from the instance
// and suggests what the profile's schema lacks.
func TestSchemaEvolveOverAPI(t *testing.T) {
	db := setupTestDB(t)
	for i, m := range []map[string]any{
		{"type": "incident", "topics": []any{"k8s"}},
		{"type": "incident", "topics": []any{"k8s", "auth"}},
		{"type": "task", "topics": []any{"auth"}},
	} {
		at := readAt.Add(time.Duration(i) * time.Hour)
		seedObj(t, db, &storage.KnowledgeObject{
			ID: "se-" + string(rune('a'+i)), Status: "active", CreatedAt: at, UpdatedAt: at,
			Metadata: map[string]any{"enrichment.structured_metadata": m},
		})
	}
	if _, err := db.exec("profile", "create", "ops"); err != nil {
		t.Fatal(err)
	}

	out, err := db.exec("profile", "schema", "evolve", "ops", "--format", "json")
	if err != nil {
		t.Fatalf("evolve: %v\n%s", err, out)
	}
	var res struct {
		NewTypes  []struct{ Value string } `json:"new_types"`
		NewTopics []struct{ Value string } `json:"new_topics"`
	}
	decodeFirstJSON(t, out, &res)
	if len(res.NewTypes) != 1 || res.NewTypes[0].Value != "incident" ||
		len(res.NewTopics) != 2 || res.NewTopics[0].Value != "auth" || res.NewTopics[1].Value != "k8s" {
		t.Errorf("evolve = %+v", res)
	}
}
