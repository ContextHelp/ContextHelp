package searchgraph_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/searchgraph"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// TestBuildAgainstSQLite runs the builder over a real SQLite store: link
// pairs written the way `ctxt link` writes them, mention edges, a stored
// and a missing entity, per-model embeddings read through SourceFrom (only
// the trace's vector model counts), and a non-candidate neighbour that
// must stay out of the graph.
func TestBuildAgainstSQLite(t *testing.T) {
	ctx := context.Background()
	drv := graphSQLite(t)

	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	objs := map[string][]float32{
		"sg-a": {1, 0, 0, 0},
		"sg-b": {0.95, 0.1, 0, 0},
		"sg-c": {0, 1, 0, 0},
		"sg-d": {1, 0, 0, 0}, // not a candidate
	}
	stored := map[string]*storage.KnowledgeObject{}
	for _, id := range []string{"sg-a", "sg-b", "sg-c", "sg-d"} {
		o := &storage.KnowledgeObject{
			ID: id, Type: "note", Status: "active", RawContent: "object " + id,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := drv.Objects().Create(ctx, o); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if err := drv.Embeddings().Put(ctx, id, []storage.ObjectVector{{ModelID: "sg-model", Vector: objs[id]}}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
		stored[id] = o
	}
	// Under another model sg-a and sg-c are identical; that model is not
	// the one the search read, so it must not produce a similar edge.
	for _, id := range []string{"sg-a", "sg-c"} {
		if err := drv.Embeddings().Put(ctx, id, []storage.ObjectVector{{ModelID: "sg-other", Vector: []float32{0, 0, 1, 0}}}); err != nil {
			t.Fatalf("put %s (sg-other): %v", id, err)
		}
	}
	n := 0
	edge := func(fromType, from, toType, to, typ string) {
		n++
		e := &storage.Edge{
			ID: "sg-edge-" + string(rune('a'+n)), FromType: fromType, FromID: from,
			ToType: toType, ToID: to, EdgeType: typ, Weight: 1, CreatedAt: now,
		}
		if err := drv.Edges().Create(ctx, e); err != nil {
			t.Fatalf("edge %s %s %s: %v", from, typ, to, err)
		}
	}
	edge("object", "sg-a", "object", "sg-b", "extends")
	edge("object", "sg-b", "object", "sg-a", "extended-by")
	edge("object", "sg-c", "object", "sg-a", "related-to")
	edge("object", "sg-a", "object", "sg-d", "supports")
	edge("object", "sg-d", "object", "sg-a", "supported-by")
	edge("object", "sg-a", "entity", "topic/x", "mentions")
	edge("entity", "topic/x", "object", "sg-a", "mentioned_in")
	edge("object", "sg-b", "entity", "topic/x", "mentions")
	edge("object", "sg-c", "entity", "topic/y", "mentions")
	edge("object", "sg-d", "entity", "topic/x", "mentions")
	if err := drv.Entities().Upsert(ctx, &storage.Entity{
		Slug: "topic/x", Title: "Topic X", Namespace: "topic", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	tr := &service.SearchTrace{
		Query: "topic", FTSQuery: "topic", Mode: service.SearchModeHybrid, Limit: 2,
		VectorModel: "sg-model", SemanticStatus: retrieval.SemanticOK,
	}
	for i, id := range []string{"sg-a", "sg-b", "sg-c"} {
		stage := service.TraceStageReturned
		if i == 2 {
			stage = service.TraceStageCutLimit
		}
		tr.Candidates = append(tr.Candidates, service.TraceCandidate{
			ID: id, Object: stored[id], Legs: service.TraceLegsBoth, Rank: i + 1, Stage: stage,
		})
	}

	src := searchgraph.SourceFrom(drv)
	if src.Embeddings == nil {
		t.Fatal("SourceFrom: sqlite embedding store should provide embeddings by id")
	}
	doc, err := searchgraph.Build(ctx, tr, src, searchgraph.Options{Similar: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var got []string
	for _, e := range doc.Graph.Edges {
		got = append(got, e.Source+" "+e.Relation+" "+e.Target)
	}
	want := []string{
		"query matched obj:sg-a",
		"query matched obj:sg-b",
		"query matched obj:sg-c",
		"obj:sg-a extends obj:sg-b",
		"obj:sg-a related-to obj:sg-c",
		"obj:sg-a mentions ent:topic/x",
		"obj:sg-b mentions ent:topic/x",
		"obj:sg-c mentions ent:topic/y",
		"obj:sg-a co_mention obj:sg-b",
		"obj:sg-a similar obj:sg-b",
	}
	if !slices.Equal(got, want) {
		t.Errorf("edges:\n got %q\nwant %q", got, want)
	}
	if _, ok := doc.Graph.Nodes["obj:sg-d"]; ok {
		t.Error("non-candidate sg-d pulled into the graph")
	}
	if l := doc.Graph.Nodes["ent:topic/x"].Label; l != "Topic X" {
		t.Errorf("stored entity label = %q", l)
	}
	if l := doc.Graph.Nodes["ent:topic/y"].Label; l != "topic/y" {
		t.Errorf("missing entity label = %q, want slug", l)
	}
	if c := doc.Graph.Nodes["ent:topic/x"].Metadata.MentionCount; c != 2 {
		t.Errorf("topic/x mention_count = %d, want 2 (sg-d is not in the graph)", c)
	}
}

// graphSQLite opens a fresh SQLite store with two 4-dimension models
// indexed: sg-model (the default) and sg-other.
func graphSQLite(t *testing.T) *sqlite.Driver {
	t.Helper()
	ctx := context.Background()
	drv, err := sqlite.New(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { drv.Close(ctx) })
	reg, err := registry.ForDriver(drv)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []registry.Model{
		{ModelID: "sg-model", Provider: "fixture", Dimension: 4, ConfigJSON: "{}"},
		{ModelID: "sg-other", Provider: "fixture", Dimension: 4, ConfigJSON: "{}"},
	} {
		if err := reg.Register(ctx, m, m.ModelID == "sg-model"); err != nil {
			t.Fatal(err)
		}
		if err := drv.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(m)); err != nil {
			t.Fatal(err)
		}
	}
	return drv
}
