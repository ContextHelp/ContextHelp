//go:build e2e && unix

package searchgraph_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/graph"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The endpoint corpus: what GET /api/v1/search/graph is asserted over,
// on every storage driver. It is stored through the service's raw
// analyze path (what `ctxt analyze --raw --mention` runs), so objects
// carry an owning profile, which the CLI does not set, and the same
// seeding works on SQLite and Postgres. Typed links mirror `ctxt link
// create`; the default model, vectors and the inbound grant go in
// through the driver, as no command writes them without a live
// provider or a registry.
//
// "deployment" matches every object but capacity through full text;
// capacity only through the vector leg, so it and its supports link to
// runbook exist in hybrid graphs only. The vault namespace is the one
// the gated principal is not entitled to: vault/codename is mentioned by
// runbook, rollback, postmortem (profile ops) and forecast (profile
// fin). Within ops, runbook and rollback share project/atlas and
// vault/codename (co_mention weight 2), runbook and postmortem share
// person/alice-chen and vault/codename (weight 2), and rollback and
// postmortem share vault/codename alone (weight 1).
type endpointObject struct {
	alias     string
	profile   string // owning profile; "" is global
	content   string
	mentions  []string
	embedding []float32 // stored under fixtureModel; nil stores none
}

var endpointObjects = []endpointObject{
	{
		alias: "runbook", profile: "ops",
		content:   "Deployment runbook for the atlas cluster: deployment order and health checks",
		mentions:  []string{"@project.atlas", "@person.alice-chen", "@vault.codename"},
		embedding: []float32{1, 0, 0},
	},
	{
		alias: "rollback", profile: "ops",
		content:   "Rollback checklist for a failed atlas deployment",
		mentions:  []string{"@project.atlas", "@vault.codename"},
		embedding: []float32{0.8, 0.6, 0},
	},
	{
		alias: "postmortem", profile: "ops",
		content:   "Postmortem of the billing deployment outage",
		mentions:  []string{"@person.alice-chen", "@vault.codename"},
		embedding: []float32{0.6, 0.8, 0},
	},
	{
		alias: "capacity", profile: "ops",
		content:   "Quarterly capacity plan for the atlas cluster",
		mentions:  []string{"@project.atlas"},
		embedding: []float32{0.9, 0, 0.436},
	},
	{
		alias: "forecast", profile: "fin",
		content:   "Deployment cost forecast for the finance team",
		mentions:  []string{"@team.finance", "@vault.codename"},
		embedding: []float32{0, 1, 0},
	},
	{
		alias:   "glossary",
		content: "Glossary of deployment terms",
	},
}

var endpointLinks = []fixtureLink{
	{"rollback", "extends", "runbook"},
	{"postmortem", "related-to", "runbook"},
	{"capacity", "supports", "runbook"},
}

// Principals of the protected instances. The gated one holds an inbound
// grant for every namespace but vault; the open one holds none, which
// leaves it unrestricted.
const (
	gatedPrincipal  = "e2e-graph-gated"
	gatedToken      = "e2e-graph-gated-token-7c1d0a"
	openPrincipal   = "e2e-graph-open"
	openToken       = "e2e-graph-open-token-2b9e41"
	hiddenNamespace = "vault"
	hiddenEntity    = "vault/codename"
)

var gatedNamespaces = []string{"project", "person", "team"}

// graphStore is a seeded store: storage type and path (a file or a DSN)
// as both binaries' configs name them, and the id of each object.
type graphStore struct {
	typ, path string
	ids       map[string]string // alias -> object id
}

// alias rewrites every object id in s to its alias.
func (s *graphStore) alias(v string) string {
	for a, id := range s.ids {
		v = strings.ReplaceAll(v, id, a)
	}
	return v
}

// seedGraphStore stores the endpoint corpus and the gated principal's
// grant in a fresh store; withModel also registers fixtureModel as the
// default and stores the corpus vectors under it.
func seedGraphStore(t *testing.T, typ, path string, withModel bool) *graphStore {
	t.Helper()
	ctx := context.Background()
	drv, err := storageutil.NewDriver(typ, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("init %s store: %v", typ, err)
	}
	defer drv.Close(ctx)

	st := &graphStore{typ: typ, path: path, ids: map[string]string{}}
	svc := service.NewWithOptions(drv, jobs.NewQueue(drv.Jobs()), pipeline.DefaultRegistry(), search.NewEngine(drv), "", nil, nil)
	for _, o := range endpointObjects {
		id, err := svc.Analyze(ctx, service.AnalyzeRequest{
			Content: o.content, Type: "text", Source: "cli", Raw: true, NoFanout: true,
			Mentions: o.mentions, Profile: o.profile,
		})
		if err != nil {
			t.Fatalf("store %s: %v", o.alias, err)
		}
		st.ids[o.alias] = id
	}

	now := time.Now().Truncate(time.Second)
	link := func(from, typ, to string) {
		if err := drv.Edges().Create(ctx, &storage.Edge{
			ID: uuid.NewString(), FromType: "object", FromID: from, ToType: "object", ToID: to,
			EdgeType: typ, Weight: 1, CreatedAt: now,
		}); err != nil {
			t.Fatalf("link %s -[%s]-> %s: %v", from, typ, to, err)
		}
	}
	for _, l := range endpointLinks {
		lt := graph.LinkType(l.typ)
		inv, err := graph.InverseLinkType(lt)
		if err != nil {
			t.Fatal(err)
		}
		link(st.ids[l.from], l.typ, st.ids[l.to])
		if !graph.IsSymmetric(lt) {
			link(st.ids[l.to], string(inv), st.ids[l.from])
		}
	}

	if err := drv.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
		RegistryName: gatedPrincipal, Plan: "inbound", Namespaces: gatedNamespaces, FetchedAt: now,
	}); err != nil {
		t.Fatalf("grant %s: %v", gatedPrincipal, err)
	}

	if !withModel {
		return st
	}
	reg, err := registry.ForDriver(drv)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(ctx, fixtureModel, true); err != nil {
		t.Fatalf("register %s: %v", fixtureModel.ModelID, err)
	}
	if err := drv.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(fixtureModel)); err != nil {
		t.Fatalf("index %s: %v", fixtureModel.ModelID, err)
	}
	for _, o := range endpointObjects {
		if o.embedding == nil {
			continue
		}
		v := []storage.ObjectVector{{ModelID: fixtureModel.ModelID, Vector: o.embedding}}
		if err := drv.Embeddings().Put(ctx, st.ids[o.alias], v); err != nil {
			t.Fatalf("store embedding for %s: %v", o.alias, err)
		}
	}
	return st
}

// switchableProvider is fakeOllama behind a switch: while down, every
// request fails with 503, so a search's vector leg fails.
type switchableProvider struct {
	url  string
	down atomic.Bool
}

func newSwitchableProvider(t *testing.T) *switchableProvider {
	t.Helper()
	target, err := url.Parse(fakeOllama(t))
	if err != nil {
		t.Fatal(err)
	}
	p := &switchableProvider{}
	fwd := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.down.Load() {
			http.Error(w, "provider down", http.StatusServiceUnavailable)
			return
		}
		fwd.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	p.url = srv.URL
	return p
}

// instanceConfig is one dpkms instance's config.
type instanceConfig struct {
	store     *graphStore
	embedURL  string
	protected bool     // static bearer auth for both principals; private otherwise
	hosts     []string // server.allowed_hosts beyond loopback
}

// writeDpkmsConfig writes c as e.dir/<name>.yaml and returns its path.
func (e *env) writeDpkmsConfig(name string, c instanceConfig) string {
	e.t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "storage:\n  type: %s\n  path: %q\n", c.store.typ, c.store.path)
	fmt.Fprintf(&b, "providers:\n  embedding:\n    backend: ollama\n    endpoint: %q\n", c.embedURL)
	b.WriteString("browser:\n  enabled: false\n")
	if c.protected {
		b.WriteString("server:\n  access: protected\n")
		if len(c.hosts) > 0 {
			b.WriteString("  allowed_hosts:\n")
			for _, h := range c.hosts {
				fmt.Fprintf(&b, "    - %q\n", h)
			}
		}
		fmt.Fprintf(&b, `  auth:
    provider: static
    static:
      tokens:
        - token: %q
          principal: %q
          roles: [reader]
        - token: %q
          principal: %q
          roles: [reader]
`, gatedToken, gatedPrincipal, openToken, openPrincipal)
	}
	return e.writeConfig(name, b.String())
}

// writeCtxtConfig writes a ctxt config reading st directly, with the
// dpkms client at serverURL (token may be empty) and the embedding
// provider at embedURL.
func (e *env) writeCtxtConfig(name string, st *graphStore, serverURL, token, embedURL string) string {
	e.t.Helper()
	return e.writeConfig(name, fmt.Sprintf(`storage:
  type: %s
  path: %q
server:
  url: %q
  token: %q
providers:
  embedding:
    backend: ollama
    endpoint: %q
`, st.typ, st.path, serverURL, token, embedURL))
}

func (e *env) writeConfig(name, body string) string {
	e.t.Helper()
	path := filepath.Join(e.dir, name+".yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		e.t.Fatal(err)
	}
	return path
}
