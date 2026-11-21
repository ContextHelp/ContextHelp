package dpkmsclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
)

// captureQuery answers every request with an empty list (or empty
// counts) and records the last request's query.
func captureQuery(t *testing.T) (*dpkmsclient.Client, *url.Values) {
	t.Helper()
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/facets") {
			_, _ = w.Write([]byte(`{"data":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	}))
	t.Cleanup(srv.Close)
	return newClient(t, dpkmsclient.Endpoint{URL: srv.URL}), &got
}

// fullFilter sets every ObjectFilter field to a distinct value.
var fullFilter = dpkmsclient.ObjectFilter{
	Type: "url", Subtype: "article", Tagged: tagfilter.One("ux"), Mention: "@ui.best-practice",
	Pipeline: "url.article", Status: "all",
	Since: "2026-01-01", Until: "2026-02-01T10:00:00-04:00",
	MetaType: "observation", Topic: "auth", Person: "alice-chen", SourceType: "slack",
	MentionedSince: "2026-04-01", MentionedUntil: "2026-04-23",
	Limit: 7, Offset: 3, Sort: "updated_at", Dir: "asc",
}

// TestObjectFilterQuery pins the wire name and value of every filter
// field on GET /objects and GET /objects/facets.
func TestObjectFilterQuery(t *testing.T) {
	filterParams := url.Values{
		"type": {"url"}, "subtype": {"article"}, "tagged": {"ux"}, "mention": {"@ui.best-practice"},
		"pipeline": {"url.article"}, "status": {"all"},
		"since": {"2026-01-01"}, "until": {"2026-02-01T10:00:00-04:00"},
		"meta_type": {"observation"}, "topic": {"auth"}, "person": {"alice-chen"}, "source_type": {"slack"},
		"mentioned_since": {"2026-04-01"}, "mentioned_until": {"2026-04-23"},
	}
	c, got := captureQuery(t)
	ctx := context.Background()

	if _, _, err := c.ListObjects(ctx, fullFilter); err != nil {
		t.Fatal(err)
	}
	want := url.Values{"limit": {"7"}, "offset": {"3"}, "sort": {"updated_at"}, "dir": {"asc"}}
	for k, v := range filterParams {
		want[k] = v
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("GET /objects query\n got %v\nwant %v", *got, want)
	}

	if _, err := c.ObjectFacets(ctx, fullFilter); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got, filterParams) {
		t.Errorf("GET /objects/facets query (filter only, no paging)\n got %v\nwant %v", *got, filterParams)
	}

	// An empty filter sends only the limit (0: every match).
	if _, _, err := c.ListObjects(ctx, dpkmsclient.ObjectFilter{}); err != nil {
		t.Fatal(err)
	}
	if want := (url.Values{"limit": {"0"}}); !reflect.DeepEqual(*got, want) {
		t.Errorf("empty filter query = %v, want %v", *got, want)
	}
}

// ChangedAfter is a list-only parameter: ListObjects sends it, the
// facets request does not.
func TestObjectChangedAfterQuery(t *testing.T) {
	c, got := captureQuery(t)
	ctx := context.Background()
	zero := int64(0)
	if _, err := c.ListObjectChanges(ctx, dpkmsclient.ObjectFilter{Limit: 5}); err != nil {
		t.Fatal(err)
	}
	if want := (url.Values{"limit": {"5"}, "changed_after": {"0"}}); !reflect.DeepEqual(*got, want) {
		t.Errorf("default query = %v, want %v", *got, want)
	}
	seq := int64(42)
	if _, err := c.ListObjectChanges(ctx, dpkmsclient.ObjectFilter{ChangedAfter: &seq, Status: "all"}); err != nil {
		t.Fatal(err)
	}
	if want := (url.Values{"limit": {"0"}, "status": {"all"}, "changed_after": {"42"}}); !reflect.DeepEqual(*got, want) {
		t.Errorf("query = %v, want %v", *got, want)
	}
	if _, err := c.ObjectFacets(ctx, dpkmsclient.ObjectFilter{ChangedAfter: &zero}); err != nil {
		t.Fatal(err)
	}
	if want := (url.Values{}); !reflect.DeepEqual(*got, want) {
		t.Errorf("facets query = %v, want %v", *got, want)
	}
}

// ListObjectChanges follows the cursor dpkms answers with, and an
// update of an old object comes back past it.
func TestObjects_ListObjectChanges(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())
	c := roleClient(t, srv, dpkmstest.RoleReader)
	ctx := context.Background()
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"c-1", "c-2", "c-3"} {
		o := &storage.KnowledgeObject{ID: id, Type: "note", Status: "active", RawContent: id, CreatedAt: at, UpdatedAt: at}
		if err := driver.Objects().Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(p dpkmsclient.ObjectChanges) []string {
		var out []string
		for _, o := range p.Objects {
			out = append(out, o.ID)
		}
		return out
	}

	p1, err := c.ListObjectChanges(ctx, dpkmsclient.ObjectFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(p1), []string{"c-1", "c-2"}) || p1.Total != 3 {
		t.Fatalf("page 1 = %v total %d", ids(p1), p1.Total)
	}
	p2, err := c.ListObjectChanges(ctx, dpkmsclient.ObjectFilter{Limit: 2, ChangedAfter: &p1.NextChangedAfter})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(p2), []string{"c-3"}) || p2.NextChangedAfter <= p1.NextChangedAfter {
		t.Fatalf("page 2 = %v next %d", ids(p2), p2.NextChangedAfter)
	}

	o, err := driver.Objects().Get(ctx, "c-1")
	if err != nil {
		t.Fatal(err)
	}
	o.Tags = []storage.Tag{{Label: "edited"}}
	if err := driver.Objects().Update(ctx, o); err != nil {
		t.Fatal(err)
	}
	p3, err := c.ListObjectChanges(ctx, dpkmsclient.ObjectFilter{ChangedAfter: &p2.NextChangedAfter})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(p3), []string{"c-1"}) {
		t.Fatalf("after the update = %v, want [c-1]", ids(p3))
	}
	// An empty page keeps the cursor.
	p4, err := c.ListObjectChanges(ctx, dpkmsclient.ObjectFilter{ChangedAfter: &p3.NextChangedAfter})
	if err != nil {
		t.Fatal(err)
	}
	if len(p4.Objects) != 0 || p4.NextChangedAfter != p3.NextChangedAfter {
		t.Fatalf("empty page = %v next %d, want cursor %d", ids(p4), p4.NextChangedAfter, p3.NextChangedAfter)
	}
}

// TestObjectFilterAcceptedByDpkms: every parameter the client sends is
// one dpkms accepts (it answers an unknown one with 400).
func TestObjectFilterAcceptedByDpkms(t *testing.T) {
	c := roleClient(t, startObjects(t), dpkmstest.RoleReader)
	ctx := context.Background()
	if _, _, err := c.ListObjects(ctx, fullFilter); err != nil {
		t.Fatalf("GET /objects with every field: %v", err)
	}
	if _, err := c.ObjectFacets(ctx, fullFilter); err != nil {
		t.Fatalf("GET /objects/facets with every field: %v", err)
	}
	_, _, err := c.ListObjects(ctx, dpkmsclient.ObjectFilter{Since: "yesterday"})
	if e := envelope(t, err); e.ExitCode != 2 {
		t.Fatalf("malformed since: exit %d, want 2", e.ExitCode)
	}
}

// startFacetCorpus serves three objects: two observations about auth,
// one task, all created on different days.
func startFacetCorpus(t *testing.T) *dpkmstest.Server {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())
	ctx := context.Background()
	day := func(d int) time.Time { return time.Date(2026, 3, d, 12, 0, 0, 0, time.UTC) }
	for _, o := range []*storage.KnowledgeObject{
		{ID: "f-1", Metadata: map[string]any{"type": "observation", "topics": []any{"auth"}}, CreatedAt: day(1)},
		{ID: "f-2", Metadata: map[string]any{"type": "observation", "topics": []any{"billing"}}, CreatedAt: day(2)},
		{ID: "f-3", Metadata: map[string]any{"type": "task", "topics": []any{"auth"}}, CreatedAt: day(3)},
	} {
		o.Type, o.Status, o.RawContent, o.UpdatedAt = "note", "active", o.ID, o.CreatedAt
		if err := driver.Objects().Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	return srv
}

func TestObjects_FilterAndFacets(t *testing.T) {
	c := roleClient(t, startFacetCorpus(t), dpkmstest.RoleReader)
	ctx := context.Background()
	ids := func(f dpkmsclient.ObjectFilter) []string {
		t.Helper()
		objs, _, err := c.ListObjects(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, o := range objs {
			out = append(out, o.ID)
		}
		slices.Sort(out)
		return out
	}
	for name, tc := range map[string]struct {
		f    dpkmsclient.ObjectFilter
		want []string
	}{
		"meta_type":          {dpkmsclient.ObjectFilter{MetaType: "observation"}, []string{"f-1", "f-2"}},
		"topic":              {dpkmsclient.ObjectFilter{Topic: "auth"}, []string{"f-1", "f-3"}},
		"since":              {dpkmsclient.ObjectFilter{Since: "2026-03-02"}, []string{"f-2", "f-3"}},
		"until":              {dpkmsclient.ObjectFilter{Until: "2026-03-02T12:00:01Z"}, []string{"f-1", "f-2"}},
		"until is exclusive": {dpkmsclient.ObjectFilter{Until: "2026-03-02T12:00:00Z"}, []string{"f-1"}},
	} {
		if got := ids(tc.f); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}

	counts, err := c.ObjectFacets(ctx, dpkmsclient.ObjectFilter{Topic: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{"observation": 1, "task": 1}; !reflect.DeepEqual(counts, want) {
		t.Errorf("facets topic=auth = %v, want %v", counts, want)
	}
}

func TestObjects_RelatedObjects(t *testing.T) {
	srv := startObjects(t)
	ctx := context.Background()
	for i, e := range [][2]string{{"o-1", "ctxt://entity/acme/api"}, {"o-2", "ctxt://entity/acme/api"}} {
		if err := srv.Driver.Edges().Create(ctx, &storage.Edge{
			ID: "e-" + e[0], FromType: "object", FromID: e[0], ToType: "entity", ToID: e[1],
			EdgeType: "mentions", Weight: 1, CreatedAt: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
		}); err != nil {
			t.Fatal(err)
		}
	}
	c := roleClient(t, srv, dpkmstest.RoleReader)

	rel, err := c.RelatedObjects(ctx, "o-1", 1, 5)
	if err != nil || len(rel) != 1 || rel[0].ID != "o-2" {
		t.Fatalf("related o-1: %+v %v", rel, err)
	}
	rel, err = c.RelatedObjects(ctx, "o-3", 0, 0)
	if err != nil || len(rel) != 0 {
		t.Fatalf("related o-3 (defaults): %+v %v", rel, err)
	}
	_, err = c.RelatedObjects(ctx, "o-missing", 1, 5)
	if e := envelope(t, err); e.ExitCode != 3 {
		t.Fatalf("missing: exit %d, want 3", e.ExitCode)
	}
	_, err = c.RelatedObjects(ctx, "o-1", 9, 5)
	if e := envelope(t, err); e.ExitCode != 2 {
		t.Fatalf("depth 9: exit %d, want 2", e.ExitCode)
	}
}

// TestTaggedWireRoundTrip: the server reads `tagged` raw, where a "+" is the
// required marker and a space is %20, so the client must send "+" as %2B and
// spaces as %20 and the value must decode, PathUnescape-style, to exactly
// what was sent.
func TestTaggedWireRoundTrip(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	}))
	t.Cleanup(srv.Close)
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	for _, spec := range []string{"+a,b,+c", "machine learning,+c++", "+a b,c"} {
		f, err := tagfilter.Parse([]string{spec})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.ListObjects(context.Background(), dpkmsclient.ObjectFilter{Tagged: f}); err != nil {
			t.Fatal(err)
		}
		var val string
		for _, pair := range strings.Split(raw, "&") {
			if k, v, _ := strings.Cut(pair, "="); k == "tagged" {
				val = v
			}
		}
		if strings.ContainsAny(val, "+ ") && strings.Contains(strings.ReplaceAll(val, "%2B", ""), "+") {
			t.Errorf("%q: raw tagged %q carries a bare +", spec, val)
		}
		got, err := url.PathUnescape(val)
		if err != nil || got != spec {
			t.Errorf("%q: raw tagged %q decodes to %q (%v)", spec, val, got, err)
		}
	}
}
