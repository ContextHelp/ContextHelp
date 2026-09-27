//go:build e2e && unix

package searchgraph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
)

// GET /api/v1/search/graph, black box: real `dpkms serve` processes over
// a store seeded with the endpoint corpus, asked over HTTP the way the
// viewer, a script and a signed-in browser ask. Per storage driver:
//
//   - a private instance (no inbound auth, no gate) over a store with a
//     default model, its provider a fake Ollama that can be switched
//     off: hybrid graphs, similar edges, profile scoping, caps, the FTS
//     fallback, and parity with `ctxt find --graph` on the same store;
//   - a protected instance (static bearer tokens, inbound gate) over a
//     store with no default model: the auth matrix, the entitlement
//     gate on and off, a web UI session from `ctxt ui open`, and the
//     hosted viewer rendering through that session in headless Chrome.
//
// The SQLite run is TestDpkmsGraph_SQLite; the Postgres run lives in
// endpoint_postgres_test.go behind the integration tag.

// runEndpointSuite runs every endpoint scenario on driver typ;
// newStore returns a fresh, empty store path for it.
func runEndpointSuite(t *testing.T, typ string, newStore func(t *testing.T, name string) string) {
	e := newEnv(t)
	prov := newSwitchableProvider(t)
	hybrid := seedGraphStore(t, typ, newStore(t, "hybrid"), true)
	ftsOnly := seedGraphStore(t, typ, newStore(t, "fts"), false)

	private := e.startDpkmsWith(e.writeDpkmsConfig("dpkms-private", instanceConfig{
		store: hybrid, embedURL: prov.url,
	}), "graph-private")

	front := newSignInFront(t)
	protected := e.startDpkmsWith(e.writeDpkmsConfig("dpkms-protected", instanceConfig{
		store: ftsOnly, embedURL: closedURL(), protected: true, hosts: []string{front.host()},
	}), "graph-protected")
	front.setTarget(t, protected.url)

	t.Run("private", func(t *testing.T) {
		t.Run("NoCredentialNoGate", func(t *testing.T) {
			doc, _ := endpointDoc(t, private.url, hybrid, url.Values{"q": {"deployment"}}, nil)
			meta := metaOf(doc)
			if str(meta["mode"]) != "hybrid" || str(meta["semantic_status"]) != "ok" {
				t.Errorf("mode = %v, semantic_status = %v, want hybrid, ok", meta["mode"], meta["semantic_status"])
			}
			if !hasNode(doc, "ent:"+hiddenEntity) {
				t.Errorf("private instance hid %s: no gate applies without inbound auth", hiddenEntity)
			}
		})
		t.Run("ProfileScopesObjects", func(t *testing.T) { testProfileScope(t, private.url, hybrid) })
		t.Run("FindProfileScopesObjects", func(t *testing.T) { testFindProfileScope(t, private.url, hybrid) })
		t.Run("HybridSimilar", func(t *testing.T) { testHybridSimilar(t, private.url, hybrid) })
		t.Run("ProviderDownFallsBackToFTS", func(t *testing.T) { testProviderDown(t, private.url, hybrid, prov) })
		t.Run("Caps", func(t *testing.T) { testCaps(t, private.url, hybrid) })
		t.Run("ParityWithFind", func(t *testing.T) {
			e := e.in(t)
			testParity(t, e, private.url, e.writeCtxtConfig("ctxt-hybrid", hybrid, closedURL(), "", prov.url))
		})
	})

	t.Run("protected", func(t *testing.T) {
		t.Run("CredentialRequired", func(t *testing.T) { testCredentialRequired(t, protected.url) })
		t.Run("NoProviderIsFTSOnly", func(t *testing.T) {
			doc, _ := endpointDoc(t, protected.url, ftsOnly, url.Values{"q": {"deployment"}}, bearer(openToken))
			meta := metaOf(doc)
			if str(meta["mode"]) != "fts_only" || str(meta["semantic_status"]) != "no_default_model" {
				t.Errorf("mode = %v, semantic_status = %v, want fts_only, no_default_model", meta["mode"], meta["semantic_status"])
			}
			if _, ok := meta["vector_model"]; ok {
				t.Errorf("vector_model = %v without a default model", meta["vector_model"])
			}
			if got, want := objectAliases(doc), []string{"forecast", "glossary", "postmortem", "rollback", "runbook"}; !slices.Equal(got, want) {
				t.Errorf("objects = %v, want the full-text candidates %v", got, want)
			}
		})
		t.Run("EntitlementGate", func(t *testing.T) { testGate(t, protected.url, ftsOnly) })
		t.Run("SessionCookie", func(t *testing.T) {
			e := e.in(t)
			cfg := e.writeCtxtConfig("ctxt-signin", ftsOnly, protected.url, gatedToken, closedURL())
			testSessionCookie(t, e, protected.url, ftsOnly, cfg)
		})
		t.Run("ViewerSignedIn", func(t *testing.T) {
			e := e.in(t)
			cfg := e.writeCtxtConfig("ctxt-signin-front", ftsOnly, front.URL, gatedToken, closedURL())
			testViewerSignedIn(t, e, front, cfg)
		})
	})
}

// in is e for subtest t: same home, failures reported on t.
func (e *env) in(t *testing.T) *env { return &env{t: t, home: e.home} }

func TestDpkmsGraph_SQLite(t *testing.T) {
	dir := t.TempDir()
	runEndpointSuite(t, "sqlite", func(t *testing.T, name string) string {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(dir, name, "dpkms.db")
	})
}

// --- requests ---------------------------------------------------------

// credential authenticates one request; nil sends none.
type credential func(*http.Request)

func bearer(token string) credential {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

// sessionCookie sends c with the Sec-Fetch-Site a browser sets: a
// same-origin fetch from the web UI or the viewer, or a cross-site one.
func sessionCookie(c *http.Cookie, site string) credential {
	return func(r *http.Request) {
		r.AddCookie(c)
		r.Header.Set("Sec-Fetch-Site", site)
	}
}

// endpointGET requests the search graph at base with params.
func endpointGET(t *testing.T, base string, params url.Values, cred credential) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/search/graph?"+params.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cred != nil {
		cred(req)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}

// endpointDoc requests the search graph, requires a 200 bare JGF
// document, and returns it with object ids replaced by aliases, plus
// the body as sent.
func endpointDoc(t *testing.T, base string, st *graphStore, params url.Values, cred credential) (jgf, []byte) {
	t.Helper()
	res, body := endpointGET(t, base, params, cred)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET graph %s: %d, want 200\n%s", params.Encode(), res.StatusCode, body)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	return decodeJGF(t, []byte(st.alias(string(body)))), body
}

// errorCodeOf returns the code of a standard error envelope.
func errorCodeOf(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Error.Code == "" {
		t.Fatalf("not an error envelope: %s", body)
	}
	return env.Error.Code
}

// --- document views -----------------------------------------------------

// objectAliases lists the object nodes, sorted, by alias.
func objectAliases(doc jgf) []string {
	var out []string
	for id := range nodesOf(doc) {
		if a, ok := strings.CutPrefix(id, "obj:"); ok {
			out = append(out, a)
		}
	}
	slices.Sort(out)
	return out
}

// entityNodes maps each entity node's slug to its mention_count.
func entityNodes(doc jgf) map[string]int {
	out := map[string]int{}
	for id, n := range nodesOf(doc) {
		if slug, ok := strings.CutPrefix(id, "ent:"); ok {
			out[slug] = intOf(nodeMeta(n)["mention_count"])
		}
	}
	return out
}

// coMentionWeights maps each co_mention pair, "a|b" in alias order, to
// its weight.
func coMentionWeights(doc jgf) map[string]int {
	out := map[string]int{}
	for _, e := range edgesOf(doc) {
		if relationOf(e) != "co_mention" {
			continue
		}
		pair := []string{strings.TrimPrefix(sourceOf(e), "obj:"), strings.TrimPrefix(targetOf(e), "obj:")}
		slices.Sort(pair)
		out[pair[0]+"|"+pair[1]] = intOf(edgeMeta(e)["weight"])
	}
	return out
}

func relationCount(doc jgf, rel string) int {
	n := 0
	for _, e := range edgesOf(doc) {
		if relationOf(e) == rel {
			n++
		}
	}
	return n
}

func hasEdge(doc jgf, rel, src, tgt string) bool {
	for _, e := range edgesOf(doc) {
		if relationOf(e) == rel && sourceOf(e) == src && targetOf(e) == tgt {
			return true
		}
	}
	return false
}

// withoutGeneratedAt is doc with its one per-response field blanked.
func withoutGeneratedAt(doc jgf) jgf {
	metaOf(doc)["generated_at"] = ""
	return doc
}

// --- private instance ---------------------------------------------------

// profile= scopes the objects to that profile's (global objects
// included only when unscoped), and the entities to what those objects
// mention.
func testProfileScope(t *testing.T, base string, st *graphStore) {
	for _, tc := range []struct {
		profile  string
		objects  []string
		mentions int // vault/codename's mention_count; 0 = no node
	}{
		{"ops", []string{"capacity", "postmortem", "rollback", "runbook"}, 3},
		{"fin", []string{"forecast"}, 1},
		{"", []string{"capacity", "forecast", "glossary", "postmortem", "rollback", "runbook"}, 4},
		{"nobody", nil, 0},
	} {
		t.Run("profile="+tc.profile, func(t *testing.T) {
			params := url.Values{"q": {"deployment"}}
			if tc.profile != "" {
				params.Set("profile", tc.profile)
			}
			doc, _ := endpointDoc(t, base, st, params, nil)
			if got := objectAliases(doc); !slices.Equal(got, tc.objects) {
				t.Errorf("objects = %v, want %v", got, tc.objects)
			}
			if got := entityNodes(doc)[hiddenEntity]; got != tc.mentions {
				t.Errorf("%s mention_count = %d, want %d", hiddenEntity, got, tc.mentions)
			}
			if tc.profile == "fin" && len(coMentionWeights(doc)) != 0 {
				t.Errorf("co_mention edges with one object: %v", coMentionWeights(doc))
			}
		})
	}
}

// POST /api/v1/find with a profile answers that profile's objects only,
// in every mode, from the same store and semantic leg as the graph.
func testFindProfileScope(t *testing.T, base string, st *graphStore) {
	find := func(t *testing.T, body string) []string {
		t.Helper()
		res, err := http.Post(base+"/api/v1/find", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK {
			t.Fatalf("POST /find %s: %d\n%s", body, res.StatusCode, raw)
		}
		var out struct {
			Objects []struct {
				ID string `json:"id"`
			} `json:"objects"`
			Diagnostics struct {
				Semantic *struct {
					Status string `json:"status"`
				} `json:"semantic"`
			} `json:"diagnostics"`
		}
		if err := json.Unmarshal([]byte(st.alias(string(raw))), &out); err != nil {
			t.Fatal(err)
		}
		if s := out.Diagnostics.Semantic; s != nil && s.Status != "ok" {
			t.Fatalf("POST /find %s: semantic leg %s", body, s.Status)
		}
		var ids []string
		for _, o := range out.Objects {
			ids = append(ids, o.ID)
		}
		slices.Sort(ids)
		return ids
	}
	ops := []string{"capacity", "postmortem", "rollback", "runbook"}
	for _, mode := range []string{"hybrid", "vector", "fts"} {
		t.Run(mode, func(t *testing.T) {
			got := find(t, `{"query":"deployment","mode":"`+mode+`","limit":50,"profile":"ops"}`)
			if len(got) == 0 {
				t.Fatal("profile ops: no results")
			}
			for _, id := range got {
				if !slices.Contains(ops, id) {
					t.Errorf("profile ops: %s is not an ops object (got %v)", id, got)
				}
			}
			if got := find(t, `{"query":"deployment","mode":"`+mode+`","limit":50,"profile":"nobody"}`); len(got) != 0 {
				t.Errorf("profile nobody: got %v", got)
			}
		})
	}
	if got := find(t, `{"query":"deployment","limit":50}`); !slices.Contains(got, "forecast") || !slices.Contains(got, "runbook") {
		t.Errorf("unscoped: got %v, want every profile's objects", got)
	}
}

// The default is the hybrid trace without similar edges; similar=true
// adds them above the threshold. capacity is a vector-only candidate,
// so its supports link to runbook shows.
func testHybridSimilar(t *testing.T, base string, st *graphStore) {
	plain, _ := endpointDoc(t, base, st, url.Values{"q": {"deployment"}}, nil)
	if n := relationCount(plain, "similar"); n != 0 {
		t.Errorf("similar edges without similar=true: %d", n)
	}
	doc, _ := endpointDoc(t, base, st, url.Values{
		"q": {"deployment"}, "similar": {"true"}, "similar_threshold": {"0.75"},
	}, nil)
	if got := str(metaOf(doc)["mode"]); got != "hybrid" {
		t.Fatalf("mode = %q, want hybrid", got)
	}
	for alias, want := range map[string]string{"runbook": "both", "glossary": "fts", "capacity": "vector"} {
		if got := str(nodeMeta(nodesOf(doc)[objectNode(alias)])["legs"]); got != want {
			t.Errorf("%s legs = %q, want %q", alias, got, want)
		}
	}
	if !hasEdge(doc, "supports", objectNode("capacity"), objectNode("runbook")) {
		t.Error("no capacity -supports-> runbook edge in the hybrid graph")
	}
	similar := 0
	for _, e := range edgesOf(doc) {
		if relationOf(e) != "similar" {
			continue
		}
		similar++
		if w, _ := edgeMeta(e)["weight"].(json.Number).Float64(); w < 0.75 {
			t.Errorf("similar edge %v below the threshold: %g", edgeKey(e), w)
		}
	}
	if similar == 0 {
		t.Error("similar=true: no similar edges")
	}
}

// With the provider failing, the graph is the full-text fallback: 200,
// the reason in metadata, no vector-only candidate.
func testProviderDown(t *testing.T, base string, st *graphStore, prov *switchableProvider) {
	prov.down.Store(true)
	t.Cleanup(func() { prov.down.Store(false) })
	doc, _ := endpointDoc(t, base, st, url.Values{"q": {"deployment"}}, nil)
	meta := metaOf(doc)
	if got := str(meta["mode"]); got != "fts_fallback" {
		t.Fatalf("mode = %q, want fts_fallback", got)
	}
	if str(meta["vector_error"]) == "" {
		t.Error("fts_fallback without a vector_error")
	}
	if hasNode(doc, objectNode("capacity")) || relationCount(doc, "supports") != 0 {
		t.Error("vector-only capacity, or its supports link, in a full-text graph")
	}
	if got := count(doc, "vector_hits"); got != 0 {
		t.Errorf("counts.vector_hits = %d, want 0", got)
	}
}

// Tiny caps truncate; caps out of [1, max] are refused, never clamped;
// the maxima themselves are accepted.
func testCaps(t *testing.T, base string, st *graphStore) {
	doc, _ := endpointDoc(t, base, st, url.Values{
		"q": {"deployment"}, "max_nodes": {"3"}, "max_edges": {"2"},
	}, nil)
	meta := metaOf(doc)
	if b, _ := meta["truncated"].(bool); !b {
		t.Error("truncated = false under max_nodes=3, max_edges=2")
	}
	caps := meta["caps"].(map[string]any)
	if intOf(caps["max_nodes"]) != 3 || intOf(caps["max_edges"]) != 2 {
		t.Errorf("caps = %v, want max_nodes 3, max_edges 2", caps)
	}
	if n, m := len(nodesOf(doc)), len(edgesOf(doc)); n > 3 || m > 2 {
		t.Errorf("%d nodes, %d edges: over the caps", n, m)
	}

	for name, params := range map[string]url.Values{
		"max_nodes over max": {"q": {"deployment"}, "max_nodes": {"1001"}},
		"max_edges over max": {"q": {"deployment"}, "max_edges": {"10001"}},
		"max_nodes zero":     {"q": {"deployment"}, "max_nodes": {"0"}},
		"max_edges not int":  {"q": {"deployment"}, "max_edges": {"1e9"}},
		"mode":               {"q": {"deployment"}, "mode": {"fts"}},
		"missing q":          {"max_nodes": {"3"}},
	} {
		res, body := endpointGET(t, base, params, nil)
		if res.StatusCode != http.StatusBadRequest || errorCodeOf(t, body) != "INVALID_REQUEST" {
			t.Errorf("%s: %d %s, want 400 INVALID_REQUEST", name, res.StatusCode, body)
		}
	}
	endpointDoc(t, base, st, url.Values{"q": {"deployment"}, "max_nodes": {"1000"}, "max_edges": {"10000"}}, nil)
}

// The endpoint's document is `ctxt find --graph --format json`'s on the
// same store, field for field, but for generated_at: the flags and the
// query parameters name the same options.
func testParity(t *testing.T, e *env, base, cfg string) {
	for _, tc := range []struct {
		name   string
		flags  []string
		params url.Values
	}{
		{"defaults", nil, url.Values{}},
		{"limit threshold caps",
			[]string{"--limit", "2", "--min-score", "0.1", "--graph-max-nodes", "6", "--graph-max-edges", "5"},
			url.Values{"limit": {"2"}, "min_score": {"0.1"}, "max_nodes": {"6"}, "max_edges": {"5"}}},
		{"similar",
			[]string{"--graph-similar", "--graph-similar-threshold", "0.75"},
			url.Values{"similar": {"true"}, "similar_threshold": {"0.75"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := e.mustRun(append([]string{"--config", cfg, "find", "deployment", "--graph", "--format", "json"}, tc.flags...)...)
			tc.params.Set("q", "deployment")
			res, body := endpointGET(t, base, tc.params, nil)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("GET graph: %d\n%s", res.StatusCode, body)
			}
			cli := withoutGeneratedAt(decodeJGF(t, []byte(r.stdout)))
			api := withoutGeneratedAt(decodeJGF(t, body))
			if !sameDoc(cli, api) {
				t.Errorf("endpoint and ctxt find --graph differ\n--- ctxt\n%s\n--- endpoint\n%s", canonical(cli), canonical(api))
			}
		})
	}
}

// --- protected instance -------------------------------------------------

// The route answers a valid bearer token or session cookie only.
func testCredentialRequired(t *testing.T, base string) {
	q := url.Values{"q": {"deployment"}}
	for name, cred := range map[string]credential{
		"no credential": nil,
		"unknown token": bearer("e2e-graph-not-a-token"),
		"bogus cookie":  sessionCookie(&http.Cookie{Name: "__Host-dpkms_session", Value: "bogus"}, "same-origin"),
	} {
		if res, body := endpointGET(t, base, q, cred); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401\n%s", name, res.StatusCode, body)
		}
	}
	for _, tok := range []string{gatedToken, openToken} {
		if res, body := endpointGET(t, base, q, bearer(tok)); res.StatusCode != http.StatusOK {
			t.Errorf("valid token: %d, want 200\n%s", res.StatusCode, body)
		}
	}
}

// A principal without a grant sees every entity; the gated principal's
// document has no trace of the vault entity: no node, no mentions edge,
// its share of every co_mention weight gone (and the edge with it when
// nothing else is shared), one entity fewer in the counts, the other
// entities' mention counts untouched, and its slug, namespace and title
// nowhere in the body. Objects are profile-scoped, never entity-gated.
func testGate(t *testing.T, base string, st *graphStore) {
	params := url.Values{"q": {"deployment"}, "profile": {"ops"}}
	open, _ := endpointDoc(t, base, st, params, bearer(openToken))
	gated, raw := endpointDoc(t, base, st, params, bearer(gatedToken))

	// Guard: ungated, the vault entity is there and adds to every pair.
	openEnts := entityNodes(open)
	if openEnts[hiddenEntity] != 3 {
		t.Fatalf("ungated %s mention_count = %d, want 3 (the gate-off baseline)", hiddenEntity, openEnts[hiddenEntity])
	}
	if got, want := coMentionWeights(open), map[string]int{"rollback|runbook": 2, "postmortem|runbook": 2, "postmortem|rollback": 1}; !maps.Equal(got, want) {
		t.Errorf("ungated co_mention weights = %v, want %v", got, want)
	}

	if hasNode(gated, "ent:"+hiddenEntity) {
		t.Errorf("gated document has the %s node", hiddenEntity)
	}
	if got, want := coMentionWeights(gated), map[string]int{"rollback|runbook": 1, "postmortem|runbook": 1}; !maps.Equal(got, want) {
		t.Errorf("gated co_mention weights = %v, want %v", got, want)
	}
	for _, e := range edgesOf(gated) {
		if targetOf(e) == "ent:"+hiddenEntity || sourceOf(e) == "ent:"+hiddenEntity {
			t.Errorf("gated edge touches the hidden entity: %v", edgeKey(e))
		}
	}
	if got, want := relationCount(gated, "mentions"), relationCount(open, "mentions")-3; got != want {
		t.Errorf("gated mentions edges = %d, want %d", got, want)
	}
	if got, want := count(gated, "entities"), count(open, "entities")-1; got != want {
		t.Errorf("gated counts.entities = %d, want %d", got, want)
	}
	delete(openEnts, hiddenEntity)
	if got := entityNodes(gated); !maps.Equal(got, openEnts) {
		t.Errorf("gated entities = %v, want the ungated ones less %s: %v", got, hiddenEntity, openEnts)
	}
	if !slices.Equal(objectAliases(gated), objectAliases(open)) {
		t.Errorf("gated objects = %v, ungated %v: objects are not entity-gated", objectAliases(gated), objectAliases(open))
	}
	for _, secret := range []string{hiddenEntity, hiddenNamespace, "codename"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("gated body contains %q", secret)
		}
	}
}

// A browser signed in with a `ctxt ui open` link acts as the principal
// whose token minted it: its cookie reads the same gated document the
// token does. A cross-site read with the cookie is refused.
func testSessionCookie(t *testing.T, e *env, base string, st *graphStore, cfg string) {
	cookie := exchangeCode(t, base, codeOf(t, e.signInLink(t, cfg, base)))
	params := url.Values{"q": {"deployment"}, "profile": {"ops"}}
	viaCookie, _ := endpointDoc(t, base, st, params, sessionCookie(cookie, "same-origin"))
	viaToken, _ := endpointDoc(t, base, st, params, bearer(gatedToken))
	if hasNode(viaCookie, "ent:"+hiddenEntity) {
		t.Errorf("session of %s sees %s", gatedPrincipal, hiddenEntity)
	}
	if !sameDoc(withoutGeneratedAt(viaCookie), withoutGeneratedAt(viaToken)) {
		t.Errorf("cookie and token documents differ\n--- cookie\n%s\n--- token\n%s", canonical(viaCookie), canonical(viaToken))
	}
	if res, body := endpointGET(t, base, params, sessionCookie(cookie, "cross-site")); res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site read with the cookie: %d, want 403\n%s", res.StatusCode, body)
	}
}

var signInLinkRE = regexp.MustCompile(`http://127\.0\.0\.1:\d+/ui/auth#code=[A-Za-z0-9_-]{43}`)

// signInLink runs `ctxt ui open --no-browser` with cfg and returns the
// link it prints, which must be on origin.
func (e *env) signInLink(t *testing.T, cfg, origin string) string {
	t.Helper()
	r := e.mustRun("--config", cfg, "ui", "open", "--no-browser")
	link := signInLinkRE.FindString(r.stdout)
	if link == "" || !strings.HasPrefix(link, origin+"/ui/auth#code=") {
		t.Fatalf("no sign-in link on %s in the output\n%s", origin, r)
	}
	return link
}

func codeOf(t *testing.T, link string) string {
	t.Helper()
	_, code, ok := strings.Cut(link, "#code=")
	if !ok {
		t.Fatalf("link %q has no code", link)
	}
	return code
}

// exchangeCode trades a login code for the session cookie the way the
// web UI's sign-in page does.
func exchangeCode(t *testing.T, base, code string) *http.Cookie {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/ui/auth/session", strings.NewReader(`{"code":"`+code+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Ctxt-CSRF", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	for _, c := range res.Cookies() {
		if strings.HasPrefix(c.Name, "__Host-dpkms_") {
			return c
		}
	}
	t.Fatalf("exchange: %d, no session cookie\n%s", res.StatusCode, body)
	return nil
}

// --- the viewer, signed in ----------------------------------------------

const signInProbePath = "/__e2e/signin-probe.js"

// signInProbeJS runs on the sign-in page ahead of the web UI. The web UI
// opens its event stream once it holds the session cookie; the probe
// then closes the stream (headless Chrome's virtual time cannot run out
// while it is open) and loads the viewer on the same origin, so the
// viewer's graph fetch carries nothing but the browser's cookie.
const signInProbeJS = `(() => {
  const Native = window.EventSource;
  window.EventSource = class extends Native {
    constructor(url, init) {
      super(url, init);
      this.addEventListener('open', () => {
        this.close();
        location.replace(%q);
      });
    }
  };
})();`

// signInFront is a pass-through proxy in front of a protected dpkms,
// keeping the Host header (the instance allows it), that adds the probe
// to the sign-in page and records what reached the server.
type signInFront struct {
	*httptest.Server
	target atomic.Pointer[url.URL]
	mu     sync.Mutex
	seen   []frontRequest
}

type frontRequest struct {
	method, uri   string
	cookie, token bool
	status        int
}

const viewerTarget = "/ui/searchgraph/?q=deployment"

func newSignInFront(t *testing.T) *signInFront {
	t.Helper()
	f := &signInFront{}
	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(f.target.Load())
			r.Out.Host = r.In.Host
		},
		FlushInterval: -1,
		ModifyResponse: func(res *http.Response) error {
			req := res.Request
			f.mu.Lock()
			f.seen = append(f.seen, frontRequest{
				method: req.Method, uri: req.URL.RequestURI(),
				cookie: strings.Contains(req.Header.Get("Cookie"), "__Host-dpkms_"),
				token:  req.Header.Get("Authorization") != "",
				status: res.StatusCode,
			})
			f.mu.Unlock()
			if req.Method != http.MethodGet || req.URL.Path != "/ui/auth" ||
				!strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
				return nil
			}
			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if err != nil {
				return err
			}
			if !bytes.Contains(body, []byte("<head>")) {
				return errors.New("sign-in page: no <head>")
			}
			body = bytes.Replace(body, []byte("<head>"), []byte(`<head><script src="`+signInProbePath+`"></script>`), 1)
			res.Body = io.NopCloser(bytes.NewReader(body))
			res.ContentLength = int64(len(body))
			res.Header.Set("Content-Length", strconv.Itoa(len(body)))
			return nil
		},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == signInProbePath {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = fmt.Fprintf(w, signInProbeJS, viewerTarget)
			return
		}
		if f.target.Load() == nil {
			http.Error(w, "no target yet", http.StatusBadGateway)
			return
		}
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *signInFront) host() string { return strings.TrimPrefix(f.URL, "http://") }

func (f *signInFront) setTarget(t *testing.T, target string) {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	f.target.Store(u)
}

func (f *signInFront) requests() []frontRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

// Headless Chrome follows a `ctxt ui open` link to the protected
// instance's sign-in page, is signed in by the web UI, then loads the
// viewer, which renders the graph fetched with the session cookie alone.
func testViewerSignedIn(t *testing.T, e *env, front *signInFront, cfg string) {
	chrome := chromePath(t)
	link := e.signInLink(t, cfg, front.URL)
	dom, err := launch.DumpDOM(context.Background(), link, launch.Options{
		Chrome:            chrome,
		VirtualTimeBudget: 15 * time.Second,
		Timeout:           60 * time.Second,
		Attempts:          1, // the code is single-use
		Env:               e.environ(),
		Dir:               e.dir,
		TempDir:           t.TempDir(),
	})
	if err != nil {
		t.Fatalf("headless chrome: %v", err)
	}
	assertRendered(t, dom, "deployment")

	var graphFetch *frontRequest
	for _, r := range front.requests() {
		if r.uri == "/api/v1/ui/login-codes" {
			continue // ctxt ui open minting the code with its token
		}
		if r.token {
			t.Errorf("%s %s carried an Authorization header", r.method, r.uri)
		}
		if r.method == http.MethodGet && strings.HasPrefix(r.uri, "/api/v1/search/graph?") {
			graphFetch = &r
		}
	}
	switch {
	case graphFetch == nil:
		t.Errorf("the viewer never fetched the graph; requests: %+v", front.requests())
	case graphFetch.uri != "/api/v1/search/graph?q=deployment" || !graphFetch.cookie || graphFetch.status != http.StatusOK:
		t.Errorf("graph fetch = %+v, want GET /api/v1/search/graph?q=deployment with the cookie, 200", *graphFetch)
	}
}
