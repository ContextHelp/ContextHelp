//go:build e2e && unix

package searchgraph_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// stageSplitArgs is the query every golden and export test shares: five
// FTS candidates split into returned, cut_limit and cut_threshold.
var stageSplitArgs = []string{"find", "deployment", "--graph", "--limit", "2", "--min-score", "0.1"}

func args(base []string, extra ...string) []string {
	return append(slices.Clone(base), extra...)
}

func TestGolden_StageSplit(t *testing.T) {
	e := newEnv(t)
	r := e.mustRun(args(stageSplitArgs, "--format", "json")...)
	raw := decodeJGF(t, []byte(suite.corpus.alias(r.stdout)))

	meta := metaOf(raw)
	if got := str(meta["vocabulary"]); got != "ctxt.search-graph/v1" {
		t.Errorf("vocabulary = %q", got)
	}
	// No embedding server: the vector leg fails and the search falls
	// back to FTS, saying why.
	if got := str(meta["mode"]); got != "fts_fallback" {
		t.Errorf("mode = %q, want fts_fallback", got)
	}
	if ve := str(meta["vector_error"]); !strings.Contains(ve, "connection refused") {
		t.Errorf("vector_error = %q, want the refused embedding endpoint", ve)
	}
	for k, want := range map[string]int{"returned": 2, "cut_limit": 1, "cut_threshold": 2, "vector_hits": 0} {
		if got := count(raw, k); got != want {
			t.Errorf("counts.%s = %d, want %d", k, got, want)
		}
	}
	for alias, want := range map[string]string{
		"runbook": "returned", "postmortem": "returned", "rollback": "cut_limit",
		"freeze": "cut_threshold", "glossary": "cut_threshold",
	} {
		if got := nodeStage(raw, objectNode(alias)); got != want {
			t.Errorf("%s stage = %q, want %q", alias, got, want)
		}
	}
	if hasNode(raw, objectNode("budget")) {
		t.Error("budget is no FTS candidate but is a node")
	}

	assertEmissionOrder(t, raw)
	assertStoredLinks(t, raw)
	assertCoMentions(t, raw, map[[2]string]int{
		{objectNode("rollback"), objectNode("runbook")}:   1, // project/atlas
		{objectNode("postmortem"), objectNode("runbook")}: 1, // person/alice-chen
	})
	assertGolden(t, "stage_split", normalize(t, []byte(r.stdout)))
}

// assertEmissionOrder checks the documented keep order that does not
// depend on object ids: relation groups in the order matched, stored
// links, mentions, co_mention, similar, and matched edges following the
// object order (stage, then rank).
func assertEmissionOrder(t *testing.T, doc jgf) {
	t.Helper()
	group := func(rel string) int {
		switch rel {
		case "matched":
			return 0
		case "mentions":
			return 2
		case "co_mention":
			return 3
		case "similar":
			return 4
		}
		return 1 // stored link types
	}
	stage := map[string]int{"returned": 0, "cut_limit": 1, "cut_threshold": 2}
	last, lastObj := -1, [2]int{-1, -1}
	for i, e := range edgesOf(doc) {
		g := group(relationOf(e))
		if g < last {
			t.Errorf("edge %d (%s) emitted after a later relation group", i, relationOf(e))
		}
		last = g
		if g != 0 {
			continue
		}
		m := nodeMeta(nodesOf(doc)[targetOf(e)])
		key := [2]int{stage[str(m["stage"])], intOf(m["rank"])}
		if key[0] < lastObj[0] || (key[0] == lastObj[0] && key[1] < lastObj[1]) {
			t.Errorf("matched edge %d to %s out of (stage, rank) order", i, targetOf(e))
		}
		lastObj = key
	}
}

// assertStoredLinks checks inverse-pair collapse, inverse-name input and
// symmetric links, and that no link leaves the candidate set.
func assertStoredLinks(t *testing.T, doc jgf) {
	t.Helper()
	type link struct {
		src, rel, tgt string
		directed      bool
	}
	var got []link
	for _, e := range edgesOf(doc) {
		switch relationOf(e) {
		case "matched", "mentions", "co_mention", "similar":
			continue
		}
		if edgeDerivation(e) != "stored" {
			t.Errorf("link %v: derivation %q, want stored", edgeKey(e), edgeDerivation(e))
		}
		src, tgt := sourceOf(e), targetOf(e)
		if isUndirected(e) && tgt < src {
			src, tgt = tgt, src
		}
		got = append(got, link{src, relationOf(e), tgt, !isUndirected(e)})
	}
	want := []link{
		{objectNode("rollback"), "extends", objectNode("runbook"), true},
		{objectNode("freeze"), "supersedes", objectNode("glossary"), true},
		{objectNode("postmortem"), "related-to", objectNode("runbook"), false},
	}
	sortLinks := func(ls []link) {
		slices.SortFunc(ls, func(x, y link) int { return strings.Compare(x.src+x.rel+x.tgt, y.src+y.rel+y.tgt) })
	}
	sortLinks(got)
	sortLinks(want)
	if !slices.Equal(got, want) {
		t.Errorf("stored links:\n got %v\nwant %v", got, want)
	}
}

// assertCoMentions checks the co_mention edges and their shared-entity
// weights; keys are alias-ordered endpoint pairs.
func assertCoMentions(t *testing.T, doc jgf, want map[[2]string]int) {
	t.Helper()
	got := map[[2]string]int{}
	for _, e := range edgesOf(doc) {
		if relationOf(e) != "co_mention" {
			continue
		}
		if !isUndirected(e) || edgeDerivation(e) != "derived" {
			t.Errorf("co_mention %v: want undirected and derived", edgeKey(e))
		}
		p := [2]string{sourceOf(e), targetOf(e)}
		if p[1] < p[0] {
			p[0], p[1] = p[1], p[0]
		}
		got[p] = intOf(edgeMeta(e)["weight"])
	}
	if len(got) != len(want) {
		t.Errorf("co_mention edges = %v, want %v", got, want)
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("co_mention %v weight = %d, want %d", p, got[p], w)
		}
	}
}

func TestGolden_Truncated(t *testing.T) {
	e := newEnv(t)
	// Four nodes keep the query and the three top-ranked objects and no
	// entity; three edges keep only matched edges, whose order follows
	// rank alone.
	r := e.mustRun(args(stageSplitArgs, "--graph-max-nodes", "4", "--graph-max-edges", "3", "--format", "json")...)
	doc := normalize(t, []byte(r.stdout))
	meta := metaOf(doc)
	if meta["truncated"] != true {
		t.Error("truncated = false, want true")
	}
	caps := meta["caps"].(map[string]any)
	if intOf(caps["max_nodes"]) != 4 || intOf(caps["max_edges"]) != 3 {
		t.Errorf("caps = %v, want max_nodes 4, max_edges 3", caps)
	}
	if n, m, ent := count(doc, "nodes"), count(doc, "edges"), count(doc, "entities"); n != 4 || m != 3 || ent != 0 {
		t.Errorf("counts nodes/edges/entities = %d/%d/%d, want 4/3/0", n, m, ent)
	}
	// Trace tallies stay pre-truncation.
	if got := count(doc, "candidates"); got != 5 {
		t.Errorf("counts.candidates = %d, want 5 (pre-truncation)", got)
	}
	assertGolden(t, "truncated", doc)
}

func TestGolden_EmptyResult(t *testing.T) {
	e := newEnv(t)
	// A query nothing matches is not an error: exit 0 and a document
	// holding only the query node.
	r := e.mustRun("find", "zzqqxx", "--graph", "--format", "json")
	doc := normalize(t, []byte(r.stdout))
	if len(nodesOf(doc)) != 1 || !hasNode(doc, "query") || len(edgesOf(doc)) != 0 {
		t.Errorf("empty result: want only the query node and no edges\n%s", r.stdout)
	}
	assertGolden(t, "empty", doc)
}

// fakeOllama answers /api/show (the model's context length) and
// /api/embed for the fixture model: queries mentioning deployment embed
// along the axis the runbook sits on, anything else orthogonal.
func fakeOllama(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.Method != http.MethodPost || req.Model != "fixture-embed" {
			http.NotFound(w, r)
			return
		}
		var body any
		switch r.URL.Path {
		case "/api/show":
			body = map[string]any{"model_info": map[string]any{
				"general.architecture": "fixture", "fixture.context_length": 2048,
			}}
		case "/api/embed":
			vec := []float64{0, 0, 1}
			if strings.Contains(strings.ToLower(req.Input), "deployment") {
				vec = []float64{1, 0, 0}
			}
			body = map[string]any{"embeddings": [][]float64{vec}}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestGolden_HybridSimilar(t *testing.T) {
	e := newEnv(t, withEmbedding(fakeOllama(t)))
	r := e.mustRun("find", "deployment", "--graph", "--graph-similar", "--graph-similar-threshold", "0.75", "--format", "json")
	doc := normalize(t, []byte(r.stdout))
	meta := metaOf(doc)
	if got := str(meta["mode"]); got != "hybrid" {
		t.Fatalf("mode = %q, want hybrid\n%s", got, r.stdout)
	}
	if _, ok := meta["vector_error"]; ok {
		t.Errorf("vector_error set in hybrid mode: %v", meta["vector_error"])
	}
	legs := map[string]string{}
	for id, n := range nodesOf(doc) {
		if m := nodeMeta(n); m["kind"] == "object" {
			legs[id] = str(m["legs"])
		}
	}
	for alias, want := range map[string]string{"runbook": "both", "glossary": "fts", "budget": "vector"} {
		if got := legs[objectNode(alias)]; got != want {
			t.Errorf("%s legs = %q, want %q", alias, got, want)
		}
	}
	// budget is a candidate now, so the postmortem -> budget link shows.
	var supports, similar int
	for _, e := range edgesOf(doc) {
		switch relationOf(e) {
		case "supports":
			supports++
		case "similar":
			similar++
			if w, _ := edgeMeta(e)["weight"].(json.Number).Float64(); w < 0.75 {
				t.Errorf("similar edge %v below threshold: %g", edgeKey(e), w)
			}
		}
	}
	if supports != 1 || similar == 0 {
		t.Errorf("supports edges = %d (want 1), similar edges = %d (want > 0)", supports, similar)
	}
	assertGolden(t, "hybrid_similar", doc)
}

// Both flag positions produce the same document.
func TestFlagPositions(t *testing.T) {
	e := newEnv(t)
	a := e.mustRun("find", "--graph", "--format", "json", "deployment")
	b := e.mustRun("find", "deployment", "--graph", "--format", "json")
	if !sameDoc(normalize(t, []byte(a.stdout)), normalize(t, []byte(b.stdout))) {
		t.Errorf("find --graph q and find q --graph differ\n%s\n---\n%s", a.stdout, b.stdout)
	}
}
