package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/searchgraph"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"hop.top/kit/go/console/output"
)

// setupGraphTestDB is setupTestDB with the embedding provider pointed at a
// closed port, so the vector leg fails fast and deterministically instead
// of reaching whatever embedding server the machine happens to run.
func setupGraphTestDB(t *testing.T) *testDB {
	t.Helper()
	db := setupTestDB(t)
	f, err := os.OpenFile(db.ConfigPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open config: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString("providers:\n  embedding:\n    backend: ollama\n    endpoint: http://127.0.0.1:1\n"); err != nil {
		t.Fatalf("append config: %v", err)
	}
	return db
}

// graphTestModel is the default embedding model (dimension 4) graph tests
// store vectors under when they seed embeddings.
const graphTestModel = "graph-test-4"

// seedGraphObjects seeds objects matching "deployment" with the given
// summaries and optional 4-dimension embeddings. Embeddings register
// graphTestModel as the default model and go into its per-model index.
func seedGraphObjects(t *testing.T, db *testDB, summaries map[string]string, embeddings map[string][]float32) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	if len(embeddings) > 0 {
		lifecycleModel(t, db, graphTestModel, true)
	}
	for id, summary := range summaries {
		obj := &storage.KnowledgeObject{
			ID:         id,
			Type:       "text",
			Summaries:  []string{summary},
			RawContent: "deployment details",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := db.Driver.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		if v, ok := embeddings[id]; ok {
			if err := db.Driver.Embeddings().Put(ctx, id, []storage.ObjectVector{{ModelID: graphTestModel, Vector: v}}); err != nil {
				t.Fatalf("embed %s: %v", id, err)
			}
		}
	}
	rebuildFTSForTest(t, db)
}

// graphDoc decodes find --graph JSON output into the searchgraph document,
// failing on anything that is not exactly a JGF single-graph object.
func graphDoc(t *testing.T, out string) searchgraph.Document {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, out)
	}
	if len(top) != 1 || top["graph"] == nil {
		keys := make([]string, 0, len(top))
		for k := range top {
			keys = append(keys, k)
		}
		t.Fatalf("top-level keys = %v, want exactly [graph] (bare JGF, no envelope)", keys)
	}
	var doc searchgraph.Document
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	return doc
}

func objectNodeIDs(doc searchgraph.Document) []string {
	var ids []string
	for id, n := range doc.Graph.Nodes {
		if n.Metadata.Kind == searchgraph.KindObject {
			ids = append(ids, id)
		}
	}
	return ids
}

func edgesOf(doc searchgraph.Document, relation string) []searchgraph.Edge {
	var out []searchgraph.Edge
	for _, e := range doc.Graph.Edges {
		if e.Relation == relation {
			out = append(out, e)
		}
	}
	return out
}

// TestFindGraph_JSONIsBareJGF pins the machine contract: the document is
// the bare JGF object in either flag position, strings are not
// HTML-escaped, and a failed embedding provider for the default model
// shows up as the fts_fallback mode (plus the stderr notice) rather than an
// error.
func TestFindGraph_JSONIsBareJGF(t *testing.T) {
	for name, args := range map[string][]string{
		"flag before query": {"--format", "json", "find", "--graph", "deployment"},
		"flag after query":  {"--format", "json", "find", "deployment", "--graph"},
	} {
		t.Run(name, func(t *testing.T) {
			db := setupGraphTestDB(t)
			lifecycleModel(t, db, graphTestModel, true)
			seedGraphObjects(t, db, map[string]string{"obj_graph_a": "deployment <runbook> & notes"}, nil)

			out, stderr, err := execFind(t, db, args...)
			if err != nil {
				t.Fatalf("find --graph: %v\n%s", err, out)
			}
			doc := graphDoc(t, out)
			md := doc.Graph.Metadata
			if !strings.Contains(stderr, "notice: semantic search unavailable (provider_error)") {
				t.Errorf("stderr lacks the degraded-leg notice plain hybrid prints:\n%s", stderr)
			}
			if md.SemanticStatus != "provider_error" || md.VectorModel == "" {
				t.Errorf("semantic_status = %q vector_model = %q, want provider_error on the default model", md.SemanticStatus, md.VectorModel)
			}
			if !strings.HasPrefix(md.Vocabulary, "ctxt.search-graph/") {
				t.Errorf("metadata.vocabulary = %q, want ctxt.search-graph/ prefix", md.Vocabulary)
			}
			if md.Query != "deployment" {
				t.Errorf("metadata.query = %q, want deployment", md.Query)
			}
			if md.Mode != "fts_fallback" || md.VectorError == "" {
				t.Errorf("mode = %q vector_error = %q, want fts_fallback with the leg error", md.Mode, md.VectorError)
			}
			if _, ok := doc.Graph.Nodes[searchgraph.ObjectNodePrefix+"obj_graph_a"]; !ok {
				t.Errorf("object node missing: %v", objectNodeIDs(doc))
			}
			if !strings.Contains(out, "deployment <runbook> & notes") {
				t.Errorf("label must be written verbatim, not HTML-escaped:\n%s", out)
			}
		})
	}
}

// TestFindGraph_NoDefaultModel: without a default embedding model the
// graph is FTS-only (mode fts_only, no vector model) and stderr carries the
// same no_default_model notice plain hybrid search prints.
func TestFindGraph_NoDefaultModel(t *testing.T) {
	db := setupGraphTestDB(t)
	seedGraphObjects(t, db, map[string]string{"obj_graph_a": "deployment notes"}, nil)

	out, stderr, err := execFind(t, db, "--format", "json", "find", "deployment", "--graph", "--graph-similar")
	if err != nil {
		t.Fatalf("find --graph: %v\n%s", err, stderr)
	}
	md := graphDoc(t, out).Graph.Metadata
	if md.Mode != "fts_only" || md.SemanticStatus != "no_default_model" || md.VectorModel != "" || md.VectorPool != 0 {
		t.Errorf("mode=%q semantic_status=%q vector_model=%q vector_pool=%d, want fts_only/no_default_model/\"\"/0",
			md.Mode, md.SemanticStatus, md.VectorModel, md.VectorPool)
	}
	if !strings.Contains(stderr, noDefaultNotice) {
		t.Errorf("stderr lacks the no_default_model notice:\n%s", stderr)
	}
}

// TestFindGraph_YAML asserts --format yaml renders the same document.
func TestFindGraph_YAML(t *testing.T) {
	db := setupGraphTestDB(t)
	seedGraphObjects(t, db, map[string]string{"obj_graph_a": "deployment notes"}, nil)

	out, _, err := execFind(t, db, "--format", "yaml", "find", "deployment", "--graph")
	if err != nil {
		t.Fatalf("find --graph yaml: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "graph:") || !strings.Contains(out, "vocabulary: "+searchgraph.Vocabulary) {
		t.Errorf("yaml output is not the graph document:\n%s", out)
	}
}

// TestFindGraph_HonorsFilters asserts every facet filter reaches the trace:
// the graph holds the matching candidate and never the filtered one.
func TestFindGraph_HonorsFilters(t *testing.T) {
	for _, tc := range explainFilterCases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupGraphTestDB(t)
			seedExplainFilterPair(t, db, tc.match, tc.miss)

			args := append([]string{"--format", "json", "find", "deployment", "--graph"}, tc.flags...)
			out, _, err := execFind(t, db, args...)
			if err != nil {
				t.Fatalf("find --graph %v: %v", tc.flags, err)
			}
			doc := graphDoc(t, out)
			if _, ok := doc.Graph.Nodes[searchgraph.ObjectNodePrefix+"obj_explain_match"]; !ok {
				t.Errorf("filter-matching object missing: %v", objectNodeIDs(doc))
			}
			if _, ok := doc.Graph.Nodes[searchgraph.ObjectNodePrefix+"obj_explain_miss"]; ok {
				t.Errorf("--graph ignored %v: filtered object present", tc.flags)
			}
		})
	}
}

// TestFindGraph_HonorsLimit asserts --limit reaches the trace: past the
// limit a candidate stays in the graph but is staged cut_limit.
func TestFindGraph_HonorsLimit(t *testing.T) {
	db := setupGraphTestDB(t)
	seedGraphObjects(t, db, map[string]string{
		"obj_graph_a": "deployment notes",
		"obj_graph_b": "deployment checklist",
	}, nil)

	out, _, err := execFind(t, db, "--format", "json", "find", "deployment", "--graph", "--limit", "1")
	if err != nil {
		t.Fatalf("find --graph --limit 1: %v", err)
	}
	md := graphDoc(t, out).Graph.Metadata
	if md.Limit != 1 || md.Counts.Returned != 1 || md.Counts.CutLimit != 1 {
		t.Errorf("limit=%d returned=%d cut_limit=%d, want 1/1/1", md.Limit, md.Counts.Returned, md.Counts.CutLimit)
	}
}

// TestFindGraph_Caps asserts the cap flags reach the builder, and that
// the unset flags carry the builder's own defaults.
func TestFindGraph_Caps(t *testing.T) {
	cases := []struct {
		name          string
		flags         []string
		nodes, edges  int
		wantTruncated bool
	}{
		{"defaults", nil, searchgraph.DefaultMaxNodes, searchgraph.DefaultMaxEdges, false},
		{"node cap", []string{"--graph-max-nodes", "1"}, 1, searchgraph.DefaultMaxEdges, true},
		{"edge cap", []string{"--graph-max-edges", "1"}, searchgraph.DefaultMaxNodes, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupGraphTestDB(t)
			seedGraphObjects(t, db, map[string]string{
				"obj_graph_a": "deployment notes",
				"obj_graph_b": "deployment checklist",
			}, nil)

			args := append([]string{"--format", "json", "find", "deployment", "--graph"}, tc.flags...)
			out, _, err := execFind(t, db, args...)
			if err != nil {
				t.Fatalf("find --graph %v: %v", tc.flags, err)
			}
			md := graphDoc(t, out).Graph.Metadata
			if md.Caps.MaxNodes != tc.nodes || md.Caps.MaxEdges != tc.edges {
				t.Errorf("caps = %+v, want nodes=%d edges=%d", md.Caps, tc.nodes, tc.edges)
			}
			if md.Truncated != tc.wantTruncated {
				t.Errorf("truncated = %v, want %v", md.Truncated, tc.wantTruncated)
			}
			if md.Counts.Nodes > tc.nodes || md.Counts.Edges > tc.edges {
				t.Errorf("counts %+v exceed caps", md.Counts)
			}
		})
	}
}

// TestFindGraph_Similar asserts --graph-similar and its threshold reach
// the builder: identical-direction embeddings yield a similar edge, a
// threshold above their cosine suppresses it, and the default adds none.
func TestFindGraph_Similar(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		want  int
	}{
		{"off by default", nil, 0},
		{"on", []string{"--graph-similar"}, 1},
		{"threshold above cosine", []string{"--graph-similar", "--graph-similar-threshold", "0.999"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupGraphTestDB(t)
			seedGraphObjects(t, db,
				map[string]string{"obj_graph_a": "deployment notes", "obj_graph_b": "deployment checklist"},
				map[string][]float32{"obj_graph_a": {1, 0, 0, 0}, "obj_graph_b": {0.95, 0.1, 0, 0}})

			args := append([]string{"--format", "json", "find", "deployment", "--graph"}, tc.flags...)
			out, _, err := execFind(t, db, args...)
			if err != nil {
				t.Fatalf("find --graph %v: %v", tc.flags, err)
			}
			if got := len(edgesOf(graphDoc(t, out), searchgraph.RelSimilar)); got != tc.want {
				t.Errorf("similar edges = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestFindGraph_UsageErrors asserts every invalid combination is refused
// as a usage error (exit 2) naming the offending flag, before any search.
func TestFindGraph_UsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"fts", []string{"--format", "json", "find", "q", "--graph", "--fts"}, []string{"--graph", "--fts"}},
		{"semantic", []string{"--format", "json", "find", "q", "--graph", "--semantic"}, []string{"--graph", "--semantic"}},
		{"explain", []string{"--format", "json", "find", "--explain", "q", "--graph"}, []string{"--graph", "--explain"}},
		{"facets", []string{"--format", "json", "find", "--graph", "--facets", "q"}, []string{"--graph", "--facets"}},
		{"several", []string{"--format", "json", "find", "q", "--graph", "--fts", "--facets"}, []string{"--fts", "--facets"}},
		{"max-nodes without graph", []string{"--format", "json", "find", "q", "--graph-max-nodes", "5"}, []string{"--graph-max-nodes", "--graph"}},
		{"max-edges without graph", []string{"find", "q", "--graph-max-edges", "5"}, []string{"--graph-max-edges", "--graph"}},
		{"similar without graph", []string{"find", "q", "--graph-similar"}, []string{"--graph-similar", "--graph"}},
		{"threshold without similar", []string{"--format", "json", "find", "q", "--graph", "--graph-similar-threshold", "0.5"}, []string{"--graph-similar-threshold", "--graph-similar"}},
		{"zero node cap", []string{"--format", "json", "find", "q", "--graph", "--graph-max-nodes", "0"}, []string{"--graph-max-nodes"}},
		{"negative edge cap", []string{"--format", "json", "find", "q", "--graph", "--graph-max-edges", "-1"}, []string{"--graph-max-edges"}},
		{"threshold out of range", []string{"--format", "json", "find", "q", "--graph", "--graph-similar", "--graph-similar-threshold", "1.5"}, []string{"--graph-similar-threshold"}},
		{"csv output", []string{"--format", "csv", "find", "--graph", "q"}, []string{"--graph", "csv", "--format json", ".gexf", "viewer"}},
		{"text output", []string{"--format", "text", "find", "q", "--graph"}, []string{"--graph", "text"}},
		{"no-browser without graph", []string{"find", "q", "--no-browser"}, []string{"--no-browser", "--graph"}},
		{"idle timeout without graph", []string{"find", "q", "--graph-idle-timeout", "1m"}, []string{"--graph-idle-timeout", "--graph"}},
		{"no-browser with json", []string{"--format", "json", "find", "q", "--graph", "--no-browser"}, []string{"--no-browser", "viewer"}},
		{"idle timeout with yaml", []string{"--format", "yaml", "find", "q", "--graph", "--graph-idle-timeout", "1m"}, []string{"--graph-idle-timeout", "viewer"}},
		{"no-browser with file", []string{"find", "q", "--graph", "-o", "g.html", "--no-browser"}, []string{"--no-browser", "viewer"}},
		{"zero idle timeout", []string{"find", "q", "--graph", "--graph-idle-timeout", "0s"}, []string{"--graph-idle-timeout", "positive"}},
		{"negative idle timeout", []string{"find", "q", "--graph", "--graph-idle-timeout", "-1m"}, []string{"--graph-idle-timeout", "positive"}},
		{"unknown extension", []string{"find", "q", "--graph", "-o", "g.txt"}, []string{"-o g.txt", `".txt"`, ".gexf", ".graphml", ".html", ".json", ".yaml"}},
		{"no extension", []string{"find", "q", "--graph", "-o", "graph"}, []string{"no file extension", ".gexf"}},
		{"json format, gexf file", []string{"--format", "json", "find", "q", "--graph", "-o", "g.gexf"}, []string{"--format json", "g.gexf", "GEXF"}},
		{"yaml format, json file", []string{"--format", "yaml", "find", "q", "--graph", "-o", "g.json"}, []string{"--format yaml", "g.json", "JSON Graph Format"}},
		{"json format, html file", []string{"--format", "json", "find", "q", "--graph", "-o", "g.html"}, []string{"--format json", "g.html"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupGraphTestDB(t)
			out, err := db.exec(tc.args...)
			if err == nil {
				t.Fatalf("%v: want usage error, got success:\n%s", tc.args, out)
			}
			if got := ExitCodeFor(err); got != output.ExitUsage {
				t.Errorf("exit code = %d, want %d (usage): %v", got, output.ExitUsage, err)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not mention %q", err, w)
				}
			}
		})
	}
}

// TestFindGraphBuildError_SimilarUnsupported pins the mapping for a store
// that cannot read embeddings: a usage error that keeps the sentinel.
// sqlite and postgres both implement the reader, so no command-level run
// can reach this path.
func TestFindGraphBuildError_SimilarUnsupported(t *testing.T) {
	err := findGraphBuildError(fmt.Errorf("wrapped: %w", searchgraph.ErrSimilarUnsupported))
	if got := ExitCodeFor(err); got != output.ExitUsage {
		t.Errorf("exit code = %d, want %d", got, output.ExitUsage)
	}
	if !errors.Is(err, searchgraph.ErrSimilarUnsupported) {
		t.Error("mapped error must still match searchgraph.ErrSimilarUnsupported")
	}
	if !strings.Contains(err.Error(), "--graph-similar") {
		t.Errorf("error %q should name --graph-similar", err)
	}

	other := findGraphBuildError(errors.New("boom"))
	if got := ExitCodeFor(other); got != output.ExitGeneric {
		t.Errorf("unrelated build error exit = %d, want %d", got, output.ExitGeneric)
	}
}
