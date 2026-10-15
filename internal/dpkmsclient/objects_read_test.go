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
	Type: "url", Subtype: "article", Tag: "ux", Mention: "@ui.best-practice",
	Pipeline: "url.article", Status: "all",
	After: "2026-01-01", Before: "2026-02-01T10:00:00-04:00",
	MetaType: "observation", Topic: "auth", Person: "alice-chen", SourceType: "slack",
	Since: "2026-04-01", Until: "2026-04-23",
	Limit: 7, Offset: 3, Sort: "updated_at", Dir: "asc",
}

// TestObjectFilterQuery pins the wire name and value of every filter
// field on GET /objects and GET /objects/facets.
func TestObjectFilterQuery(t *testing.T) {
	filterParams := url.Values{
		"type": {"url"}, "subtype": {"article"}, "tag": {"ux"}, "mention": {"@ui.best-practice"},
		"pipeline": {"url.article"}, "status": {"all"},
		"after": {"2026-01-01"}, "before": {"2026-02-01T10:00:00-04:00"},
		"meta_type": {"observation"}, "topic": {"auth"}, "person": {"alice-chen"}, "source_type": {"slack"},
		"since": {"2026-04-01"}, "until": {"2026-04-23"},
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
		"meta_type": {dpkmsclient.ObjectFilter{MetaType: "observation"}, []string{"f-1", "f-2"}},
		"topic":     {dpkmsclient.ObjectFilter{Topic: "auth"}, []string{"f-1", "f-3"}},
		"after":     {dpkmsclient.ObjectFilter{After: "2026-03-02"}, []string{"f-2", "f-3"}},
		"before":    {dpkmsclient.ObjectFilter{Before: "2026-03-02T12:00:00Z"}, []string{"f-1", "f-2"}},
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
