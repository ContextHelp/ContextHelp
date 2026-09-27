package dpkmsclient_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Query embeddings replay cassettes recorded against a real Ollama serving
// snowflake-arctic-embed2 (1024 dimensions). The dpkms instance under test
// embeds the query itself, through its own provider resolver. Re-record
// with
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run TestFind ./internal/dpkmsclient/
const findCassettes = "testdata/cassettes/find"

// findQuery is the recorded query. Its own embedding is note-vec's vector,
// and note-vec shares no token with it, so a note-vec hit came from the
// vector leg; note-fts holds the query's words and no vector.
const (
	findQuery = "rotating signing keys without downtime"
	findModel = "arctic-find"
)

// findFixture seeds a driver with note-vec (vector only, under findModel,
// the default) and note-fts (full-text only).
func findFixture(t *testing.T) storage.StorageDriver {
	t.Helper()
	ctx := context.Background()
	drv := storageutil.NewTestDriver(t)

	m := registry.Model{
		ModelID: findModel, Provider: "ollama", Dimension: 1024,
		ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://127.0.0.1:11434"}`,
	}
	reg, err := registry.ForDriver(drv)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(ctx, m, true); err != nil {
		t.Fatal(err)
	}
	if err := drv.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(m)); err != nil {
		t.Fatal(err)
	}
	client, _ := providertest.OllamaClient(t, findCassettes)
	p, err := embeddings.NewProviderResolver(&embeddings.Resolver{
		LookupEnv: func(string) (string, bool) { return "", false }, HTTPClient: client,
	}).ForModel(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	vec, err := p.Embed(ctx, findQuery)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Truncate(time.Second)
	for _, o := range []*storage.KnowledgeObject{
		{ID: "note-vec", Type: "note", RawContent: "unrelated wording", CreatedAt: now, UpdatedAt: now},
		{ID: "note-fts", Type: "note", Summaries: []string{"runbook: rotating signing keys without downtime"}, CreatedAt: now, UpdatedAt: now},
	} {
		if err := drv.Objects().Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	if err := drv.Embeddings().Put(ctx, "note-vec", []storage.ObjectVector{{ModelID: findModel, Vector: vec}}); err != nil {
		t.Fatal(err)
	}
	db := drv.(interface{ DB() *sql.DB }).DB()
	if _, err := db.ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')"); err != nil {
		t.Fatal(err)
	}
	return drv
}

// startFind serves the fixture behind static tokens, with the instance's
// embedding requests going through embedClient.
func startFind(t *testing.T, embedClient *http.Client) *dpkmstest.Server {
	t.Helper()
	return dpkmstest.Start(t, findFixture(t), dpkmstest.WithStaticTokens(), dpkmstest.WithEmbeddingHTTPClient(embedClient))
}

func findIDs(res *dpkmsclient.FindResponse) []string {
	out := make([]string, len(res.Objects))
	for i, o := range res.Objects {
		out[i] = o.ID
	}
	return out
}

// Each mode against a reader token: the query is embedded on the dpkms
// side by the default model's provider, and the response reports the
// mode, the objects and the diagnostics find prints.
func TestFind_Modes(t *testing.T) {
	client, calls := providertest.OllamaClient(t, findCassettes)
	srv := startFind(t, client)
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleReader)})
	ctx := context.Background()

	for _, tc := range []struct {
		mode, want string
		ids        []string
		semantic   bool
	}{
		{"", dpkmsclient.FindModeHybrid, []string{"note-fts", "note-vec"}, true},
		{dpkmsclient.FindModeHybrid, dpkmsclient.FindModeHybrid, []string{"note-fts", "note-vec"}, true},
		{dpkmsclient.FindModeVector, dpkmsclient.FindModeVector, []string{"note-vec"}, true},
		{dpkmsclient.FindModeFTS, dpkmsclient.FindModeFTS, []string{"note-fts"}, false},
	} {
		embeds := len(calls.EmbedURLs())
		res, err := c.Find(ctx, dpkmsclient.FindRequest{Query: findQuery, Mode: tc.mode})
		if err != nil {
			t.Fatalf("mode %q: %v", tc.mode, err)
		}
		ids := findIDs(res)
		sort.Strings(ids)
		if res.Mode != tc.want || res.Query != findQuery || !slices.Equal(ids, tc.ids) || res.Total != len(ids) {
			t.Fatalf("mode %q: got mode=%q query=%q ids=%v total=%d, want mode=%q ids=%v",
				tc.mode, res.Mode, res.Query, ids, res.Total, tc.want, tc.ids)
		}
		sem := res.Diagnostics.Semantic
		if !tc.semantic {
			if sem != nil || res.Diagnostics.CandidateCount != 0 {
				t.Fatalf("fts reported a semantic leg: %+v", res.Diagnostics)
			}
			if n := len(calls.EmbedURLs()); n != embeds {
				t.Fatalf("fts embedded the query (%d embed calls)", n-embeds)
			}
			continue
		}
		if sem == nil || !sem.OK() || sem.ModelID != findModel || sem.Notice != "" {
			t.Fatalf("mode %q: semantic = %+v, want ok under %s", tc.mode, sem, findModel)
		}
		if res.Diagnostics.CandidateCount < len(ids) {
			t.Fatalf("mode %q: candidate_count %d < %d results", tc.mode, res.Diagnostics.CandidateCount, len(ids))
		}
		if urls := calls.EmbedURLs(); len(urls) != embeds+1 {
			t.Fatalf("mode %q: %d embed calls, want the query embedded once", tc.mode, len(urls)-embeds)
		}
	}
}

// Explain, facets, the filter and the knobs travel in the request.
func TestFind_ExplainFacetsKnobs(t *testing.T) {
	client, _ := providertest.OllamaClient(t, findCassettes)
	srv := startFind(t, client)
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleReader)})
	ctx := context.Background()

	res, err := c.Find(ctx, dpkmsclient.FindRequest{Query: findQuery, Explain: true, Facets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Explain) != len(res.Objects) || len(res.Objects) != 2 {
		t.Fatalf("explain %d entries for %d objects", len(res.Explain), len(res.Objects))
	}
	for i, e := range res.Explain {
		if e.ID != res.Objects[i].ID || e.Breakdown.Total <= 0 {
			t.Fatalf("explain[%d] = %+v for object %s", i, e, res.Objects[i].ID)
		}
		if e.ID == "note-vec" && (e.Breakdown.Vector <= 0 || e.Breakdown.FTS != 0) {
			t.Fatalf("note-vec breakdown %+v, want the vector leg only", e.Breakdown)
		}
	}
	if res.Facets["(none)"] != 2 {
		t.Fatalf("facets = %v, want both objects under (none)", res.Facets)
	}

	minScore := 5.0
	res, err = c.Find(ctx, dpkmsclient.FindRequest{Query: findQuery, Search: dpkmsclient.FindSearch{MinScore: &minScore}})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Diagnostics
	if len(res.Objects) != 0 || res.Objects == nil || d.Threshold != minScore || d.CandidateCount != 2 || d.BelowThresholdCount != 2 || d.TopBelowThresholdScore <= 0 {
		t.Fatalf("min_score 5: objects=%v diagnostics=%+v", res.Objects, d)
	}

	res, err = c.Find(ctx, dpkmsclient.FindRequest{Query: findQuery, Mode: dpkmsclient.FindModeFTS, Filter: dpkmsclient.FindFilter{MetaType: "task"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 0 {
		t.Fatalf("meta_type filter ignored: %v", findIDs(res))
	}
}

// With the provider down (its endpoint refuses connections) vector and
// hybrid degrade to full-text, as the local path does, and say why in
// diagnostics.semantic. With fallback_to_fts off the search is refused
// with 422 naming the model.
func TestFind_ProviderDown(t *testing.T) {
	srv := startFind(t, &http.Client{Timeout: 5 * time.Second})
	c := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleReader)})
	ctx := context.Background()

	for _, mode := range []string{dpkmsclient.FindModeHybrid, dpkmsclient.FindModeVector} {
		res, err := c.Find(ctx, dpkmsclient.FindRequest{Query: findQuery, Mode: mode})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if ids := findIDs(res); !slices.Equal(ids, []string{"note-fts"}) {
			t.Fatalf("%s: ids %v, want the full-text hit only", mode, ids)
		}
		sem := res.Diagnostics.Semantic
		if sem == nil || sem.Status != "provider_error" || sem.ModelID != findModel ||
			!strings.HasPrefix(sem.Notice, "semantic search unavailable (provider_error): model "+findModel) {
			t.Fatalf("%s: semantic = %+v, want provider_error for %s", mode, sem, findModel)
		}

		off := false
		_, err = c.Find(ctx, dpkmsclient.FindRequest{Query: findQuery, Mode: mode, Search: dpkmsclient.FindSearch{FallbackToFTS: &off}})
		e := envelope(t, err)
		var re *dpkmsclient.RemoteError
		if e.ExitCode != 2 || !errors.As(err, &re) || re.StatusCode != http.StatusUnprocessableEntity ||
			re.Code != "SEMANTIC_UNAVAILABLE" || !strings.Contains(re.Message, "model "+findModel) {
			t.Fatalf("%s fallback off: exit=%d err=%v", mode, e.ExitCode, err)
		}
	}
}

// POST /find needs read:objects: a reader token passes, no token is 401,
// and a malformed request is a USAGE error.
func TestFind_Auth(t *testing.T) {
	client, _ := providertest.OllamaClient(t, findCassettes)
	srv := startFind(t, client)
	ctx := context.Background()
	req := dpkmsclient.FindRequest{Query: findQuery, Mode: dpkmsclient.FindModeFTS}

	anon := newClient(t, dpkmsclient.Endpoint{URL: srv.URL})
	_, err := anon.Find(ctx, req)
	var re *dpkmsclient.RemoteError
	if e := envelope(t, err); e.ExitCode != 5 || !errors.As(err, &re) || re.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: exit=%d err=%v", e.ExitCode, err)
	}

	reader := newClient(t, dpkmsclient.Endpoint{URL: srv.URL, Token: srv.Token(dpkmstest.RoleReader)})
	if _, err := reader.Find(ctx, req); err != nil {
		t.Fatalf("reader: %v", err)
	}

	for _, bad := range []dpkmsclient.FindRequest{{}, {Query: "q", Mode: "semantic"}, {Query: "q", Filter: dpkmsclient.FindFilter{Since: "yesterday"}}} {
		_, err := reader.Find(ctx, bad)
		if e := envelope(t, err); e.ExitCode != 2 || !errors.As(err, &re) || re.StatusCode != http.StatusBadRequest || re.Code != "INVALID_REQUEST" {
			t.Fatalf("%+v: exit=%d err=%v", bad, e.ExitCode, err)
		}
	}

	// Unknown fields are refused, so a misspelled knob never silently
	// falls back to its default.
	resp := srv.Request(t, http.MethodPost, "/api/v1/find", dpkmstest.RoleReader, strings.NewReader(`{"query":"q","search":{"rrfk":5}}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown knob: status %d, want 400", resp.StatusCode)
	}
}

// TestFindWireParity keeps the client's wire types in step with the
// request the dpkms handler decodes and the response it encodes.
func TestFindWireParity(t *testing.T) {
	for _, pair := range []struct{ client, server reflect.Type }{
		{reflect.TypeFor[dpkmsclient.FindRequest](), reflect.TypeFor[service.FindRequest]()},
		{reflect.TypeFor[dpkmsclient.FindResponse](), reflect.TypeFor[service.FindResult]()},
	} {
		client, server := wireShape(pair.client), wireShape(pair.server)
		if !reflect.DeepEqual(client, server) {
			t.Errorf("%s vs %s: wire shapes differ\nclient: %v\nserver: %v", pair.client, pair.server, client, server)
		}
	}
}

// wireShape lists every JSON path of typ with its tag options and kind,
// following structs, pointers, slices and maps declared in this module's
// wire types. Types shared by both sides (storage objects, plugin
// projections, time) are leaves named by their full type.
func wireShape(typ reflect.Type) []string {
	var out []string
	var walk func(prefix string, t reflect.Type)
	walk = func(prefix string, t reflect.Type) {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		switch t.Kind() {
		case reflect.Slice:
			walk(prefix+"[]", t.Elem())
			return
		case reflect.Map:
			walk(prefix+"{}", t.Elem())
			return
		case reflect.Struct:
			if pkg := t.PkgPath(); !strings.HasSuffix(pkg, "/dpkmsclient") && !strings.HasSuffix(pkg, "/service") && !strings.HasSuffix(pkg, "/retrieval") {
				out = append(out, prefix+" "+t.String())
				return
			}
			for f := range t.Fields() {
				tag := f.Tag.Get("json")
				name, opts, _ := strings.Cut(tag, ",")
				walk(prefix+"."+name+"("+opts+")", f.Type)
			}
			return
		}
		kind := t.Kind().String()
		if t.Kind() == reflect.String {
			kind = "string" // retrieval.SemanticStatus encodes as a string
		}
		out = append(out, prefix+" "+kind)
	}
	walk("", typ)
	sort.Strings(out)
	return out
}
