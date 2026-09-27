// Package servicetest holds cross-driver fixtures for service-level search
// tests: a service plus the semantic source dpkms serve builds (the
// driver's model registry, reaching a fake embedding provider on a
// loopback test port) and scenarios every storage driver must pass.
package servicetest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// ModelID is the default embedding model a HybridFixture registers.
const ModelID = "hybrid-fixture-4"

// QueryVector is the vector the fake provider returns for every input, so
// every query embeds to it. Objects indexed at QueryVector are the vector
// leg's best hits.
var QueryVector = []float32{1, 0, 0, 0}

// NearVector is close to QueryVector (cosine ~0.99): a vector-leg hit that
// ranks below exact matches.
var NearVector = []float32{0.99, 0.14, 0, 0}

// FakeOllama serves Ollama's /api/show and /api/embed on a loopback test
// port, embedding every input as QueryVector. It counts embed calls.
func FakeOllama(t testing.TB) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var embeds atomic.Int32
	vec := make([]float64, len(QueryVector))
	for i, v := range QueryVector {
		vec[i] = float64(v)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			_ = json.NewEncoder(w).Encode(map[string]any{"model_info": map[string]any{
				"general.architecture": "fixture", "fixture.context_length": 512,
			}})
		case "/api/embed":
			embeds.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float64{vec}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &embeds
}

// HybridFixture is a service over a driver with its semantic leg wired.
type HybridFixture struct {
	Svc *service.Service
	Drv storage.StorageDriver
	// Sem is the semantic leg search calls take, as dpkms serve hands
	// POST /find its RouterConfig.Semantic.
	Sem retrieval.SemanticSource
	// Embeds counts query embeddings served by the fake provider; nil
	// without a model.
	Embeds *atomic.Int32
}

// NewHybridFixture builds a service over drv and the semantic leg its
// search calls take. withModel registers ModelID as the default model, served
// by FakeOllama, and builds its index; without it the registry has no
// default model, so the semantic leg reports no_default_model.
func NewHybridFixture(t testing.TB, drv storage.StorageDriver, withModel bool) *HybridFixture {
	t.Helper()
	ctx := context.Background()
	models, err := registry.ForDriver(drv)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	r := &embeddings.Resolver{LookupEnv: func(string) (string, bool) { return "", false }, Registry: models}
	sem := retrieval.SemanticSource{Models: models, Resolver: embeddings.NewProviderResolver(r)}

	f := &HybridFixture{Drv: drv, Sem: sem}
	if withModel {
		srv, embeds := FakeOllama(t)
		f.Embeds = embeds
		m := registry.Model{
			ModelID: ModelID, Provider: "ollama", Dimension: len(QueryVector),
			ConfigJSON: `{"backend":"ollama","model":"fixture-embed","endpoint":"` + srv.URL + `"}`,
		}
		if err := models.Register(ctx, m, true); err != nil {
			t.Fatalf("register model: %v", err)
		}
		if err := drv.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(m)); err != nil {
			t.Fatalf("ensure index: %v", err)
		}
	}

	f.Svc = service.New(drv, jobs.NewQueue(drv.Jobs()), pipeline.DefaultRegistry(),
		search.NewEngine(drv), "", nil, config.Config{Search: config.DefaultSearchConfig()})
	return f
}

// Seed stores a graph-canonical object owned by profileID ("" = global)
// whose text is content; a non-nil vec is indexed under ModelID when the
// fixture registered it.
func (f *HybridFixture) Seed(t testing.TB, id, typ, profileID, content string, vec []float32) *storage.KnowledgeObject {
	t.Helper()
	ctx := context.Background()
	obj := storageutil.BuildGraphKO(id, typ, content)
	obj.ProfileID = profileID
	if err := f.Drv.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	if vec != nil && f.Embeds != nil {
		if err := f.Drv.Embeddings().Put(ctx, id, []storage.ObjectVector{{ModelID: ModelID, Vector: vec}}); err != nil {
			t.Fatalf("index %s: %v", id, err)
		}
	}
	return obj
}

// ProfileQuery shares tokens only with the "<profile>-text" objects
// SeedProfileCorpus stores; the "<profile>-vec" objects match through the
// vector leg alone.
const ProfileQuery = "orbital zephyr"

// ProfileCorpusProfiles are the owners SeedProfileCorpus stores objects
// for; "" is the global scope (IDs "global-text", "global-vec").
var ProfileCorpusProfiles = []string{"alpha", "beta", ""}

// SeedProfileCorpus stores, per ProfileCorpusProfiles entry, an FTS hit
// ("<p>-text", indexed at QueryVector) and a vector-only hit ("<p>-vec",
// indexed at NearVector).
func (f *HybridFixture) SeedProfileCorpus(t testing.TB) {
	t.Helper()
	for _, p := range ProfileCorpusProfiles {
		f.Seed(t, ProfileCorpusID(p, "text"), "note", p, "Orbital zephyr mission notes for the launch window.", QueryVector)
		f.Seed(t, ProfileCorpusID(p, "vec"), "note", p, "Unrelated wording about quarterly budgets.", NearVector)
	}
}

// ProfileCorpusID names a SeedProfileCorpus object.
func ProfileCorpusID(profile, kind string) string {
	if profile == "" {
		profile = "global"
	}
	return profile + "-" + kind
}

// RunHybridProfileScope asserts that every text search mode scopes to
// ObjectFilter.ProfileID on drv (a fresh database): hybrid (both legs),
// semantic and FTS return only the named profile's objects, as does Find
// with FindRequest.Profile in each mode (facets too), and an empty
// ProfileID does not scope — the same contract List and the query-language
// engine honor.
func RunHybridProfileScope(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	f := NewHybridFixture(t, drv, true)
	f.SeedProfileCorpus(t)
	cfg := config.DefaultSearchConfig()

	hybridIDs := func(t *testing.T, profileID string) []string {
		t.Helper()
		env, err := f.Svc.HybridSearchExplainFilteredWithDiagnostics(ctx, ProfileQuery,
			storage.ObjectFilter{ProfileID: profileID, Limit: 20}, f.Sem, cfg)
		if err != nil {
			t.Fatalf("hybrid: %v", err)
		}
		if env.Diagnostics.Semantic == nil || !env.Diagnostics.Semantic.OK() {
			t.Fatalf("hybrid: vector leg did not run: %+v", env.Diagnostics.Semantic)
		}
		ids := make([]string, len(env.Results))
		for i, r := range env.Results {
			ids[i] = r.Object.ID
		}
		return sorted(ids)
	}

	t.Run("hybrid", func(t *testing.T) {
		for _, p := range []string{"alpha", "beta"} {
			want := []string{ProfileCorpusID(p, "text"), ProfileCorpusID(p, "vec")}
			if got := hybridIDs(t, p); !equal(got, want) {
				t.Errorf("profile %q: got %v want %v (another profile leaked through a leg)", p, got, want)
			}
		}
		if got := hybridIDs(t, ""); len(got) != 2*len(ProfileCorpusProfiles) {
			t.Errorf("unscoped: got %v want all %d objects", got, 2*len(ProfileCorpusProfiles))
		}
	})

	t.Run("semantic", func(t *testing.T) {
		objs, diag, err := f.Svc.SemanticSearchFiltered(ctx, ProfileQuery,
			storage.ObjectFilter{ProfileID: "alpha", Limit: 20}, f.Sem, cfg)
		if err != nil {
			t.Fatalf("semantic: %v", err)
		}
		if diag.Semantic == nil || !diag.Semantic.OK() {
			t.Fatalf("semantic: vector leg did not run: %+v", diag.Semantic)
		}
		want := []string{ProfileCorpusID("alpha", "text"), ProfileCorpusID("alpha", "vec")}
		if got := sorted(ids(objs)); !equal(got, want) {
			t.Errorf("got %v want %v", got, want)
		}
	})

	t.Run("find", func(t *testing.T) { runFindProfileScope(t, f) })

	t.Run("fts", func(t *testing.T) {
		objs, err := f.Svc.FindByTextFiltered(ctx, ProfileQuery, storage.ObjectFilter{ProfileID: "alpha", Limit: 20})
		if err != nil {
			t.Fatalf("fts: %v", err)
		}
		want := []string{ProfileCorpusID("alpha", "text")}
		if got := ids(objs); !equal(got, want) {
			t.Errorf("got %v want %v", got, want)
		}
	})
}

// runFindProfileScope asserts Find with FindRequest.Profile answers only
// that profile's objects in every mode, facet counts included, and an
// unscoped Find answers every profile's.
func runFindProfileScope(t *testing.T, f *HybridFixture) {
	t.Helper()
	ctx := context.Background()
	for _, mode := range []string{service.FindModeHybrid, service.FindModeVector, service.FindModeFTS} {
		res, err := f.Svc.Find(ctx, service.FindRequest{
			Query: ProfileQuery, Mode: mode, Profile: "alpha", Limit: 20, Facets: true,
		}, f.Sem)
		if err != nil {
			t.Fatalf("find %s: %v", mode, err)
		}
		want := []string{ProfileCorpusID("alpha", "text"), ProfileCorpusID("alpha", "vec")}
		if mode == service.FindModeFTS {
			want = want[:1]
		}
		if got := ids(res.Objects); !equal(got, want) {
			t.Errorf("find %s, profile alpha: got %v want %v", mode, got, want)
		}
		counted := 0
		for _, n := range res.Facets {
			counted += n
		}
		if counted != 2 {
			t.Errorf("find %s, profile alpha: facets %v count %d objects, want the profile's 2", mode, res.Facets, counted)
		}
	}
	res, err := f.Svc.Find(ctx, service.FindRequest{Query: ProfileQuery, Limit: 20}, f.Sem)
	if err != nil {
		t.Fatalf("find unscoped: %v", err)
	}
	if len(res.Objects) != 2*len(ProfileCorpusProfiles) {
		t.Errorf("find unscoped: got %v want all %d objects", ids(res.Objects), 2*len(ProfileCorpusProfiles))
	}
}

func ids(objs []*storage.KnowledgeObject) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.ID
	}
	return out
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	a, b = sorted(a), sorted(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
