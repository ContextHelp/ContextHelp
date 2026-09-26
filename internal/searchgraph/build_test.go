package searchgraph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var update = flag.Bool("update", false, "rewrite golden files")

func build(t *testing.T, tr *service.SearchTrace, f *fakeStore, opts Options) *Document {
	t.Helper()
	opts.Now = fixedNow
	doc, err := Build(context.Background(), tr, f.source(), opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return doc
}

func encode(t *testing.T, doc *Document) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := doc.Encode(&buf, true); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return buf.Bytes()
}

// TestGoldens pins full documents for representative traces. Every golden
// is validated against the JGF v2.1 schema and the vocabulary contract.
// sample.jgf.json doubles as the viewer's fixture.
func TestGoldens(t *testing.T) {
	cases := []struct {
		name    string
		fixture func() (*service.SearchTrace, *fakeStore)
		opts    Options
	}{
		{"sample", sampleFixture, Options{Similar: true}},
		{"no_similar", sampleFixture, Options{}},
		{"truncated_objects", sampleFixture, Options{MaxNodes: 8}},
		{"truncated_entities_edges", sampleFixture, Options{MaxNodes: 13, MaxEdges: 20, Similar: true}},
		{"fts_fallback", fallbackFixture, Options{}},
		{"empty", func() (*service.SearchTrace, *fakeStore) {
			tr := baseTrace("nothing matches")
			tr.Mode = service.SearchModeFTSOnly
			tr.VectorPool = 0
			_, f := sampleFixture()
			return tr, f
		}, Options{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, f := tc.fixture()
			got := encode(t, build(t, tr, f, tc.opts))
			validateJGF(t, got)
			checkContract(t, got)
			if !utf8.Valid(got) {
				t.Fatal("output is not valid UTF-8")
			}
			path := filepath.Join("testdata", tc.name+".jgf.json")
			if *update {
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from golden; rerun with -update and review the diff\n got:\n%s", path, got)
			}
		})
	}
}

// TestGoldenFilesValid re-validates every committed golden from disk, so
// a hand edit (or a stale file) cannot slip past the schema.
func TestGoldenFilesValid(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.jgf.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no goldens found: %v", err)
	}
	for _, p := range paths {
		t.Run(filepath.Base(p), func(t *testing.T) {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			validateJGF(t, raw)
			checkContract(t, raw)
		})
	}
}

// TestSchemaRejectsOffContractShape proves the validator is live: an
// extra key on a node violates the JGF schema.
func TestSchemaRejectsOffContractShape(t *testing.T) {
	bad := []byte(`{"graph":{"nodes":{"query":{"label":"q","color":"red"}},"edges":[]}}`)
	inst := mustUnmarshalAny(t, bad)
	if err := jgfSchema(t).Validate(inst); err == nil {
		t.Fatal("schema accepted a node with an unknown key")
	}
}

func mustUnmarshalAny(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func edgesBy(doc *Document, rel string) []Edge {
	var out []Edge
	for _, e := range doc.Graph.Edges {
		if e.Relation == rel {
			out = append(out, e)
		}
	}
	return out
}

func hasEdge(doc *Document, src, rel, dst string) bool {
	for _, e := range doc.Graph.Edges {
		if e.Source == src && e.Relation == rel && e.Target == dst {
			return true
		}
	}
	return false
}

func TestStoredLinksCollapseToForward(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{})

	want := []struct{ src, rel, dst string }{
		{"obj:kb-rrf", "extends", "obj:kb-bm25"},
		{"obj:kb-xss", "contradicts", "obj:kb-weights"},
		{"obj:kb-rrf", "related-to", "obj:kb-weights"},
		{"obj:kb-pgvec", "related-to", "obj:kb-sqlite-vec"},
		{"obj:kb-tsrank", "derived-from", "obj:kb-bm25"},
		{"obj:kb-pgvec", "supersedes", "obj:kb-sqlite-vec"},
		{"obj:kb-pgvec", "supports", "obj:kb-embed"}, // from the inverse-only row
	}
	links := 0
	for _, e := range doc.Graph.Edges {
		switch e.Relation {
		case RelMatched, RelMentions, RelCoMention, RelSimilar:
			continue
		}
		links++
		if strings.HasSuffix(e.Relation, "-by") || e.Relation == "derived-to" {
			t.Errorf("inverse relation emitted: %+v", e)
		}
	}
	for _, w := range want {
		if !hasEdge(doc, w.src, w.rel, w.dst) {
			t.Errorf("missing %s -%s-> %s", w.src, w.rel, w.dst)
		}
	}
	if links != len(want) {
		t.Errorf("got %d link edges, want %d (each pair exactly once): %+v", links, len(want), doc.Graph.Edges)
	}
}

func TestDepthOneNeverPullsNonCandidates(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{Similar: true})
	for id := range doc.Graph.Nodes {
		if strings.Contains(id, "kb-outside") {
			t.Fatalf("non-candidate node %s pulled in", id)
		}
	}
	for _, e := range doc.Graph.Edges {
		if _, ok := doc.Graph.Nodes[e.Source]; !ok {
			t.Errorf("edge source %s not a node", e.Source)
		}
		if _, ok := doc.Graph.Nodes[e.Target]; !ok {
			t.Errorf("edge target %s not a node", e.Target)
		}
	}
	if f.listCalls != len(tr.Candidates) {
		t.Errorf("ListFrom calls = %d, want one per candidate (%d)", f.listCalls, len(tr.Candidates))
	}
}

func TestMentionsAndEntities(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{})

	m := edgesBy(doc, RelMentions)
	if len(m) != 18 { // 19 stored mention rows on candidates minus one duplicate
		t.Errorf("mentions edges = %d, want 18", len(m))
	}
	cases := map[string]struct {
		label string
		count int
	}{
		"ent:ml/embeddings": {"Embeddings", 4},
		"ent:search/rrf":    {"Reciprocal Rank Fusion", 3},
		"ent:db/sqlite":     {"db/sqlite", 2},   // no stored entity
		"ent:db/pgvector":   {"db/pgvector", 2}, // blank title
		"ent:people/ada":    {"Ada Lovelace & <co>", 1},
	}
	for id, w := range cases {
		n, ok := doc.Graph.Nodes[id]
		if !ok {
			t.Errorf("missing entity node %s", id)
			continue
		}
		if n.Label != w.label || n.Metadata.MentionCount != w.count {
			t.Errorf("%s = %q/%d, want %q/%d", id, n.Label, n.Metadata.MentionCount, w.label, w.count)
		}
	}
	if doc.Graph.Metadata.Counts.Entities != 7 {
		t.Errorf("entities = %d, want 7", doc.Graph.Metadata.Counts.Entities)
	}
}

func TestCoMentionOncePerPair(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{})
	got := map[string]float64{}
	for _, e := range edgesBy(doc, RelCoMention) {
		key := e.Source + " " + e.Target
		if _, dup := got[key]; dup {
			t.Errorf("duplicate co_mention %s", key)
		}
		got[key] = *e.Metadata.Weight
	}
	// kb-rrf and kb-bm25 share only search/bm25; kb-pgvec and kb-embed
	// share ml/embeddings and db/pgvector.
	if got["obj:kb-bm25 obj:kb-rrf"] != 1 {
		t.Errorf("kb-bm25/kb-rrf weight = %v, want 1", got["obj:kb-bm25 obj:kb-rrf"])
	}
	if got["obj:kb-embed obj:kb-pgvec"] != 2 {
		t.Errorf("kb-embed/kb-pgvec weight = %v, want 2", got["obj:kb-embed obj:kb-pgvec"])
	}
	for k := range got {
		if strings.Contains(k, "kb-orphan") {
			t.Errorf("kb-orphan mentions nothing but has co_mention %s", k)
		}
	}
}

func TestTruncationKeepsObjectsByStageThenRank(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{MaxNodes: 8})
	md := doc.Graph.Metadata
	if !md.Truncated || md.Caps.MaxNodes != 8 || len(doc.Graph.Nodes) != 8 {
		t.Fatalf("truncated=%v caps=%+v nodes=%d", md.Truncated, md.Caps, len(doc.Graph.Nodes))
	}
	// Query + 4 returned + 2 cut_limit + best-ranked cut_threshold.
	for _, id := range []string{"query", "obj:kb-rrf", "obj:kb-bm25", "obj:kb-xss", "obj:kb-pgvec",
		"obj:kb-sqlite-vec", "obj:kb-weights", "obj:kb-tsrank"} {
		if _, ok := doc.Graph.Nodes[id]; !ok {
			t.Errorf("missing %s", id)
		}
	}
	if f.getCalls != 0 {
		t.Errorf("entity lookups = %d with no entity budget, want 0", f.getCalls)
	}
	if f.listCalls != 7 {
		t.Errorf("ListFrom calls = %d, want 7 (kept objects only)", f.listCalls)
	}
}

func TestTruncationKeepsEntitiesByMentionCount(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{MaxNodes: 13})
	var ents []string
	for id, n := range doc.Graph.Nodes {
		if n.Metadata.Kind == KindEntity {
			ents = append(ents, id)
		}
	}
	// Budget 3: embeddings (4), then the count-3 ties by slug.
	for _, id := range []string{"ent:ml/embeddings", "ent:db/postgres", "ent:search/bm25"} {
		if _, ok := doc.Graph.Nodes[id]; !ok {
			t.Errorf("missing top entity %s (have %v)", id, ents)
		}
	}
	if len(ents) != 3 || f.getCalls != 3 || !doc.Graph.Metadata.Truncated {
		t.Errorf("entities = %v, lookups = %d, truncated = %v; want 3, 3, true",
			ents, f.getCalls, doc.Graph.Metadata.Truncated)
	}
}

func TestTruncationKeepsEdgesByRelation(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{MaxEdges: 20, Similar: true})
	if len(doc.Graph.Edges) != 20 || !doc.Graph.Metadata.Truncated {
		t.Fatalf("edges = %d truncated = %v, want 20/true", len(doc.Graph.Edges), doc.Graph.Metadata.Truncated)
	}
	// 9 matched, then the 7 stored links, then mentions.
	var got []string
	for _, e := range doc.Graph.Edges {
		kind := e.Relation
		if kind != RelMatched && kind != RelMentions {
			kind = "link"
		}
		if len(got) == 0 || got[len(got)-1] != kind {
			got = append(got, kind)
		}
	}
	if strings.Join(got, ",") != "matched,link,mentions" {
		t.Errorf("relation runs = %v, want matched,link,mentions", got)
	}
	if n := len(edgesBy(doc, RelMatched)); n != 9 {
		t.Errorf("matched = %d, want 9", n)
	}
}

func TestNotTruncatedWhenEverythingFits(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{Similar: true})
	md := doc.Graph.Metadata
	if md.Truncated {
		t.Error("truncated set although nothing was dropped")
	}
	if md.Caps != (Caps{MaxNodes: DefaultMaxNodes, MaxEdges: DefaultMaxEdges}) {
		t.Errorf("caps = %+v, want defaults", md.Caps)
	}
	if f.embCalls != 1 {
		t.Errorf("EmbeddingsByID calls = %d, want 1", f.embCalls)
	}
}

func TestSimilarEdges(t *testing.T) {
	tr, f := sampleFixture()
	doc := build(t, tr, f, Options{Similar: true})
	sim := edgesBy(doc, RelSimilar)
	if len(sim) == 0 {
		t.Fatal("no similar edges")
	}
	for _, e := range sim {
		w := *e.Metadata.Weight
		if w < DefaultSimilarThreshold || w > 1 {
			t.Errorf("similar weight %v outside [threshold, 1]", w)
		}
		if strings.Contains(e.Source+e.Target, "kb-orphan") || strings.Contains(e.Source+e.Target, "kb-tsrank") {
			t.Errorf("similar edge on zero/missing embedding: %+v", e)
		}
	}
	if !hasEdge(doc, "obj:kb-bm25", RelSimilar, "obj:kb-rrf") {
		t.Error("missing similar kb-bm25/kb-rrf")
	}
	if hasEdge(doc, "obj:kb-rrf", RelSimilar, "obj:kb-xss") || hasEdge(doc, "obj:kb-xss", RelSimilar, "obj:kb-rrf") {
		t.Error("orthogonal pair got a similar edge")
	}
	if n := len(edgesBy(build(t, tr, f, Options{}), RelSimilar)); n != 0 {
		t.Errorf("similar edges without opt-in = %d", n)
	}
}

func TestSimilarUnsupported(t *testing.T) {
	tr, f := sampleFixture()
	src := Source{Edges: f, Entities: f}
	_, err := Build(context.Background(), tr, src, Options{Similar: true})
	if !errors.Is(err, ErrSimilarUnsupported) {
		t.Fatalf("err = %v, want ErrSimilarUnsupported", err)
	}
	if f.listCalls != 0 {
		t.Errorf("store touched before rejecting: %d calls", f.listCalls)
	}
}

// Similar edges read the trace's vector model, the default model the
// search read; without one (no default model) there is nothing to compare.
func TestSimilarReadsTraceVectorModel(t *testing.T) {
	tr, f := sampleFixture()
	build(t, tr, f, Options{Similar: true})
	if !slices.Equal(f.embModels, []string{fixtureModel}) {
		t.Errorf("EmbeddingsByID models = %v, want [%s]", f.embModels, fixtureModel)
	}

	tr, f = sampleFixture()
	tr.VectorModel = "other-model"
	if n := len(edgesBy(build(t, tr, f, Options{Similar: true}), RelSimilar)); n != 0 {
		t.Errorf("similar edges from another model's rows = %d, want 0", n)
	}

	tr, f = sampleFixture()
	tr.Mode, tr.VectorModel, tr.SemanticStatus = service.SearchModeFTSOnly, "", retrieval.SemanticNoDefaultModel
	doc := build(t, tr, f, Options{Similar: true})
	if n := len(edgesBy(doc, RelSimilar)); n != 0 {
		t.Errorf("similar edges without a vector model = %d, want 0", n)
	}
	if f.embCalls != 0 {
		t.Errorf("EmbeddingsByID calls without a vector model = %d, want 0", f.embCalls)
	}
	if md := doc.Graph.Metadata; md.VectorModel != "" || md.SemanticStatus != "no_default_model" {
		t.Errorf("metadata vector_model/semantic_status = %q/%q", md.VectorModel, md.SemanticStatus)
	}
}

// A pair's similarity is that of its closest chunk pair.
func TestSimilarClosestChunkPair(t *testing.T) {
	tr, f := sampleFixture()
	// kb-xss (chunk 0 {0,0,1,0}) is orthogonal to kb-rrf; a second chunk
	// parallel to kb-rrf's makes the pair similar at cosine 1.
	f.chunks = map[string][][]float32{"kb-xss": {{1, 0.2, 0, 0}}}
	doc := build(t, tr, f, Options{Similar: true})
	var w float64
	for _, e := range edgesBy(doc, RelSimilar) {
		if e.Source == "obj:kb-rrf" && e.Target == "obj:kb-xss" {
			w = *e.Metadata.Weight
		}
	}
	if math.Abs(w-1) > 1e-9 {
		t.Errorf("kb-rrf/kb-xss similar weight = %v, want 1 (closest chunk pair)", w)
	}
	if md := doc.Graph.Metadata; md.VectorModel != fixtureModel || md.SemanticStatus != "ok" {
		t.Errorf("metadata vector_model/semantic_status = %q/%q", md.VectorModel, md.SemanticStatus)
	}
}

func TestBuildErrors(t *testing.T) {
	_, f := sampleFixture()
	if _, err := Build(context.Background(), nil, f.source(), Options{}); err == nil {
		t.Error("nil trace accepted")
	}
	tr, _ := sampleFixture()
	if _, err := Build(context.Background(), tr, Source{}, Options{}); err == nil {
		t.Error("empty source accepted")
	}
	f.getErr = errors.New("disk on fire")
	if _, err := Build(context.Background(), tr, f.source(), Options{}); err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("entity store failure not surfaced: %v", err)
	}
}

func TestDeterministicOutput(t *testing.T) {
	tr, f := sampleFixture()
	first := encode(t, build(t, tr, f, Options{Similar: true}))
	tr, f = sampleFixture()
	f.reverseEdges = true
	second := encode(t, build(t, tr, f, Options{Similar: true}))
	if !bytes.Equal(first, second) {
		t.Error("output depends on store row order")
	}
}

func TestLabelsVerbatimAndValidUTF8(t *testing.T) {
	tr, f := sampleFixture()
	tr.Query = "q <script>alert('x')</script> \xff"
	tr.Candidates[1].Object.Summaries = []string{"bad \xfe\xff bytes"}
	doc := build(t, tr, f, Options{})
	raw := encode(t, doc)

	if !utf8.Valid(raw) {
		t.Fatal("output is not valid UTF-8")
	}
	// Hostile markup is written literally, not <-escaped.
	for _, s := range []string{xssLabel, "<script>alert('x')</script>", "Ada Lovelace & <co>"} {
		if !bytes.Contains(raw, []byte(s)) {
			t.Errorf("output does not contain %q verbatim", s)
		}
	}
	var back Document
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if got := back.Graph.Nodes["obj:kb-xss"].Label; got != xssLabel {
		t.Errorf("round-tripped label = %q, want %q", got, xssLabel)
	}
	if got := back.Graph.Nodes["obj:kb-bm25"].Label; got != "bad �� bytes" && got != "bad � bytes" {
		t.Errorf("invalid UTF-8 label = %q", got)
	}
	validateJGF(t, raw)
}

func TestObjectLabelFallbacks(t *testing.T) {
	long := strings.Repeat("é", maxLabelRunes+10)
	cases := []struct {
		name string
		obj  *storage.KnowledgeObject
		want string
	}{
		{"nil object", nil, "id-1"},
		{"metadata title", &storage.KnowledgeObject{Metadata: map[string]any{"title": " T "}, Summaries: []string{"S"}}, "T"},
		{"summary", &storage.KnowledgeObject{Summaries: []string{"\nS1\nS2"}}, "S1"},
		{"text first line", &storage.KnowledgeObject{TextContent: "\n  ## Heading\nbody", RawContent: "raw"}, "Heading"},
		{"raw first line", &storage.KnowledgeObject{RawContent: "raw line\nmore"}, "raw line"},
		{"empty", &storage.KnowledgeObject{}, "id-1"},
		{"truncated", &storage.KnowledgeObject{Summaries: []string{long}}, strings.Repeat("é", maxLabelRunes-1) + "…"},
	}
	for _, c := range cases {
		if got := objectLabel("id-1", c.obj); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestTruncatedFlagPerCap pins each cap's boundary independently: exactly
// at the cap nothing is dropped; one below, the cap alone sets truncated.
func TestTruncatedFlagPerCap(t *testing.T) {
	tr, f := sampleFixture()
	total := len(build(t, tr, f, Options{Similar: true}).Graph.Edges)

	doc := build(t, tr, f, Options{Similar: true, MaxEdges: total})
	if doc.Graph.Metadata.Truncated || len(doc.Graph.Edges) != total {
		t.Errorf("MaxEdges=%d: truncated=%v edges=%d", total, doc.Graph.Metadata.Truncated, len(doc.Graph.Edges))
	}
	doc = build(t, tr, f, Options{Similar: true, MaxEdges: total - 1})
	if !doc.Graph.Metadata.Truncated || len(doc.Graph.Edges) != total-1 {
		t.Errorf("MaxEdges=%d: truncated=%v edges=%d", total-1, doc.Graph.Metadata.Truncated, len(doc.Graph.Edges))
	}

	// Objects only: candidates without mentions, one over the node cap.
	bare := baseTrace("bare")
	fillCounts(bare, []candSpec{
		{id: "x1", stage: service.TraceStageReturned, legs: service.TraceLegsFTS, ftsRank: 1},
		{id: "x2", stage: service.TraceStageReturned, legs: service.TraceLegsFTS, ftsRank: 2},
		{id: "x3", stage: service.TraceStageCutLimit, legs: service.TraceLegsFTS, ftsRank: 3},
	})
	doc = build(t, bare, f, Options{MaxNodes: 4})
	if doc.Graph.Metadata.Truncated || len(doc.Graph.Nodes) != 4 {
		t.Errorf("MaxNodes=4: truncated=%v nodes=%d", doc.Graph.Metadata.Truncated, len(doc.Graph.Nodes))
	}
	doc = build(t, bare, f, Options{MaxNodes: 3})
	if !doc.Graph.Metadata.Truncated || len(doc.Graph.Nodes) != 3 {
		t.Errorf("MaxNodes=3: truncated=%v nodes=%d", doc.Graph.Metadata.Truncated, len(doc.Graph.Nodes))
	}
	if _, ok := doc.Graph.Nodes["obj:x3"]; ok {
		t.Error("lowest-priority object kept over the cap")
	}
}
