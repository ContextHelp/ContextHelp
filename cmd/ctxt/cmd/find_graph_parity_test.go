package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// seedParityCorpus stores objects matching "deployment" with vectors
// under a default model served by a fake provider, entities (one without
// a stored record), mentions, a stored link and metadata facets, so every
// relation of the document is exercised.
func seedParityCorpus(t *testing.T, db *testDB) {
	t.Helper()
	ctx := context.Background()
	srv, _ := servicetest.FakeOllama(t)
	// TestMain points the CLI's embedding endpoint at a closed port; the
	// model's own endpoint is the fake provider, and so is the CLI's here.
	t.Setenv("CTXT_EMBEDDING_ENDPOINT", srv.URL)
	m := registry.Model{
		ModelID: "parity-4", Provider: "ollama", Dimension: len(servicetest.QueryVector),
		ConfigJSON: `{"backend":"ollama","model":"fixture-embed","endpoint":"` + srv.URL + `"}`,
	}
	if err := registryOf(t, db).Register(ctx, m, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Driver.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(m)); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	objs := []struct {
		id, text, topic string
		vec             []float32
	}{
		{"par-a", "deployment runbook for the payments service", "ops", servicetest.QueryVector},
		{"par-b", "deployment checklist and rollback steps", "ops", servicetest.NearVector},
		{"par-c", "deployment incident review notes", "retro", []float32{0.7, 0.7, 0, 0}},
		{"par-d", "unrelated wording about quarterly budgets", "finance", servicetest.NearVector},
	}
	for _, o := range objs {
		if err := db.Driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: o.id, Type: "note", Summaries: []string{o.text}, RawContent: o.text,
			Metadata:  map[string]any{"topics": []any{o.topic}},
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", o.id, err)
		}
		if err := db.Driver.Embeddings().Put(ctx, o.id, []storage.ObjectVector{{ModelID: m.ModelID, Vector: o.vec}}); err != nil {
			t.Fatalf("embed %s: %v", o.id, err)
		}
	}
	for slug, title := range map[string]string{"svc/payments": "Payments", "ops/rollback": "Rollback"} {
		if err := db.Driver.Entities().Upsert(ctx, &storage.Entity{
			Slug: slug, Title: title, Namespace: "ops", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	edge := func(from, toType, to, typ string) {
		n++
		if err := db.Driver.Edges().Create(ctx, &storage.Edge{
			ID: "par-edge-" + string(rune('a'+n)), FromType: "object", FromID: from,
			ToType: toType, ToID: to, EdgeType: typ, Weight: 1, CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	edge("par-a", "entity", "svc/payments", "mentions")
	edge("par-a", "entity", "ops/rollback", "mentions")
	edge("par-b", "entity", "ops/rollback", "mentions")
	edge("par-c", "entity", "svc/payments", "mentions")
	edge("par-c", "entity", "team/sre", "mentions") // no stored record
	edge("par-b", "object", "par-a", "extends")
	rebuildFTSForTest(t, db)
}

// parityServer serves the dpkms router over the CLI's store, wired the way
// dpkms serve wires it: the router carries the semantic leg (the model
// registry and a provider resolver). The endpoint runs the built-in
// search defaults, which the CLI's loaded config also carries.
func parityServer(t *testing.T, db *testDB) *httptest.Server {
	t.Helper()
	models, err := registry.ForDriver(db.Driver)
	if err != nil {
		t.Fatal(err)
	}
	r := &embeddings.Resolver{LookupEnv: func(string) (string, bool) { return "", false }, Registry: models}
	sem := retrieval.SemanticSource{Models: models, Resolver: embeddings.NewProviderResolver(r)}
	svc := service.New(db.Driver, jobs.NewQueue(db.Driver.Jobs()), pipeline.DefaultRegistry(),
		search.NewEngine(db.Driver), "", nil, *cfg)
	ts := httptest.NewServer(httpserver.NewRouterWithConfig(svc, httpserver.RouterConfig{Semantic: sem}))
	t.Cleanup(ts.Close)
	return ts
}

// comparableGraph decodes a document and drops generated_at, the one
// field that legitimately differs between two builds.
func comparableGraph(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	g, _ := doc["graph"].(map[string]any)
	md, _ := g["metadata"].(map[string]any)
	if md == nil || md["generated_at"] == nil {
		t.Fatalf("document has no graph.metadata.generated_at:\n%s", raw)
	}
	delete(md, "generated_at")
	return doc
}

// TestFindGraph_EndpointParity proves the CLI and dpkms share one
// pipeline: for the same query, filters and caps, GET /api/v1/search/graph
// answers the document `ctxt find --graph --format json` prints, modulo
// generated_at.
func TestFindGraph_EndpointParity(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		query url.Values
		// relations the document must carry, so equality is not vacuous
		relations []string
	}{
		{"defaults", nil, url.Values{}, []string{"matched", "extends", "mentions", "co_mention"}},
		{
			"filters and caps",
			[]string{"--limit", "2", "--min-score", "0", "--topic", "ops",
				"--graph-max-nodes", "4", "--graph-max-edges", "6", "--graph-similar", "--graph-similar-threshold", "0.9"},
			url.Values{"limit": {"2"}, "min_score": {"0"}, "topic": {"ops"},
				"max_nodes": {"4"}, "max_edges": {"6"}, "similar": {"true"}, "similar_threshold": {"0.9"}},
			[]string{"matched", "extends"},
		},
		{
			"similar, every candidate",
			[]string{"--limit", "1", "--graph-similar"},
			url.Values{"limit": {"1"}, "similar": {"true"}},
			[]string{"matched", "extends", "mentions", "co_mention", "similar"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			seedParityCorpus(t, db)

			args := append([]string{"--format", "json", "find", "--graph", "deployment"}, tc.flags...)
			out, stderr, err := execFind(t, db, args...)
			if err != nil {
				t.Fatalf("find --graph: %v\n%s", err, stderr)
			}
			cli := comparableGraph(t, []byte(out))

			q := tc.query
			q.Set("q", "deployment")
			resp, err := http.Get(parityServer(t, db).URL + "/api/v1/search/graph?" + q.Encode())
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("endpoint status %d: %s", resp.StatusCode, body)
			}
			api := comparableGraph(t, body)

			md := cli["graph"].(map[string]any)["metadata"].(map[string]any)
			if md["mode"] != "hybrid" || md["semantic_status"] != "ok" {
				t.Fatalf("CLI graph did not run the vector leg (mode %v, status %v, error %v): parity would be vacuous", md["mode"], md["semantic_status"], md["vector_error"])
			}
			rels := map[string]bool{}
			for _, e := range cli["graph"].(map[string]any)["edges"].([]any) {
				rels[e.(map[string]any)["relation"].(string)] = true
			}
			for _, r := range tc.relations {
				if !rels[r] {
					t.Errorf("CLI document has no %s edge; the corpus should produce one", r)
				}
			}
			if !reflect.DeepEqual(cli, api) {
				cj, _ := json.MarshalIndent(cli, "", "  ")
				aj, _ := json.MarshalIndent(api, "", "  ")
				t.Errorf("endpoint document differs from the CLI's\nCLI:\n%s\nendpoint:\n%s", cj, aj)
			}
		})
	}
}
