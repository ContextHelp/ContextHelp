package searchgraph

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fixedNow pins generated_at in goldens.
func fixedNow() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) }

// fakeStore is an in-memory Source that counts round trips.
type fakeStore struct {
	edges      map[string][]*storage.Edge // by from object id
	entities   map[string]*storage.Entity
	embeddings map[string][]float32   // chunk 0 under fixtureModel
	chunks     map[string][][]float32 // extra chunks (1..n) under fixtureModel
	embModels  []string               // model of each EmbeddingsByID call

	reverseEdges bool // return ListFrom rows in reverse order
	getErr       error

	listCalls, getCalls, embCalls int
}

func (f *fakeStore) ListFrom(_ context.Context, fromType, fromID string) ([]*storage.Edge, error) {
	f.listCalls++
	if fromType != "object" {
		return nil, nil
	}
	out := slices.Clone(f.edges[fromID])
	if f.reverseEdges {
		slices.Reverse(out)
	}
	return out, nil
}

func (f *fakeStore) Get(_ context.Context, slug string) (*storage.Entity, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	e, ok := f.entities[slug]
	if !ok {
		return nil, fmt.Errorf("entity %w", storage.ErrNotFound)
	}
	return e, nil
}

// fixtureModel is the vector model fixture traces name; the fake store
// holds rows under it only.
const fixtureModel = "fixture-embed"

func (f *fakeStore) EmbeddingsByID(_ context.Context, modelID string, ids []string) (map[string][]storage.ObjectVector, error) {
	f.embCalls++
	f.embModels = append(f.embModels, modelID)
	out := make(map[string][]storage.ObjectVector)
	if modelID != fixtureModel {
		return out, nil
	}
	for _, id := range ids {
		v, ok := f.embeddings[id]
		if !ok {
			continue
		}
		rows := []storage.ObjectVector{{ModelID: modelID, Vector: v}}
		for i, c := range f.chunks[id] {
			rows = append(rows, storage.ObjectVector{ModelID: modelID, ChunkIdx: i + 1, Vector: c})
		}
		out[id] = rows
	}
	return out, nil
}

func (f *fakeStore) source() Source {
	return Source{Edges: f, Entities: f, Embeddings: f}
}

func (f *fakeStore) addEdge(from, toType, to, edgeType string) {
	f.edges[from] = append(f.edges[from], &storage.Edge{
		ID: fmt.Sprintf("e-%s-%s-%s", from, edgeType, to), FromType: "object", FromID: from,
		ToType: toType, ToID: to, EdgeType: edgeType, Weight: 1,
	})
}

func (f *fakeStore) link(from, to, fwd, inv string) {
	f.addEdge(from, "object", to, fwd)
	if inv != "" {
		f.addEdge(to, "object", from, inv)
	}
}

func (f *fakeStore) mention(obj string, slugs ...string) {
	for _, s := range slugs {
		f.addEdge(obj, "entity", s, "mentions")
	}
}

// XSSLabel is a hostile object title that must survive verbatim.
const xssLabel = `<img src=x onerror=alert(1)> RRF & BM25 </script>`

type candSpec struct {
	id               string
	stage            service.TraceStage
	legs             service.TraceLegs
	ftsRank, vecRank int
	ftsRaw, vecRaw   float64
	fts, vec         float64
	mention, graph   float64
	overlap          float64
	obj              *storage.KnowledgeObject
}

func (c candSpec) candidate(rank int) service.TraceCandidate {
	tc := service.TraceCandidate{
		ID: c.id, Object: c.obj, Legs: c.legs, Rank: rank, Stage: c.stage,
		FTSRank: c.ftsRank, VectorRank: c.vecRank, RRF: math.Round((c.fts+c.vec)*1e4) / 1e4,
		Breakdown: service.ScoreBreakdown{
			FTS: c.fts, Vector: c.vec, MentionBoost: c.mention,
			GraphRelevance: c.graph, WordOverlap: c.overlap,
			// Rounded so goldens carry no float summation noise.
			Total: math.Round((c.fts+c.vec+c.mention+c.graph+c.overlap)*1e4) / 1e4,
		},
	}
	if c.ftsRank > 0 {
		tc.FTSRaw = ptr(c.ftsRaw)
	}
	if c.vecRank > 0 {
		tc.VectorRaw = ptr(c.vecRaw)
	}
	return tc
}

func obj(id, typ, source string, mut func(*storage.KnowledgeObject)) *storage.KnowledgeObject {
	o := &storage.KnowledgeObject{
		ID: id, Type: typ, Source: source, Status: "active",
		CreatedAt: time.Date(2026, 9, 1, 8, 30, 0, 0, time.FixedZone("EDT", -4*3600)),
	}
	if mut != nil {
		mut(o)
	}
	return o
}

func titled(t string) func(*storage.KnowledgeObject) {
	return func(o *storage.KnowledgeObject) { o.Metadata = map[string]any{"title": t} }
}

func baseTrace(query string) *service.SearchTrace {
	return &service.SearchTrace{
		Query: query, FTSQuery: query, Mode: service.SearchModeHybrid,
		VectorModel: fixtureModel, SemanticStatus: retrieval.SemanticOK,
		FTSPool: 50, VectorPool: 50, RRFK: 60, FTSWeight: 1, VectorWeight: 1,
		Weights: service.TraceRerankWeights{
			MentionBoost: 0.05, MaxMentionBoost: 0.3, DirectBacklink: 0.1,
			HopBacklink: 0.05, WordOverlap: 0.2,
		},
		Threshold: 0.3, Limit: 4,
	}
}

func fillCounts(tr *service.SearchTrace, specs []candSpec) {
	c := &tr.Counts
	for i, s := range specs {
		tr.Candidates = append(tr.Candidates, s.candidate(i+1))
		c.Candidates++
		switch s.legs {
		case service.TraceLegsBoth:
			c.Both++
			c.FTSHits++
			c.VectorHits++
		case service.TraceLegsFTS:
			c.FTSOnly++
			c.FTSHits++
		case service.TraceLegsVector:
			c.VectorOnly++
			c.VectorHits++
		}
		switch s.stage {
		case service.TraceStageReturned:
			c.Returned++
		case service.TraceStageCutLimit:
			c.CutLimit++
		case service.TraceStageCutThreshold:
			c.CutThreshold++
		}
	}
}

// sampleFixture covers every stage, both legs and each single leg, every
// link type (forward/inverse pairs, a symmetric link stored twice, an
// inverse-only row, a link to a non-candidate), ignored non-link edges,
// duplicate mentions, an entity without a stored record, hostile labels
// and embeddings (including zero-magnitude and missing).
func sampleFixture() (*service.SearchTrace, *fakeStore) {
	const (
		returned = service.TraceStageReturned
		cutLimit = service.TraceStageCutLimit
		cutThr   = service.TraceStageCutThreshold
		both     = service.TraceLegsBoth
		fts      = service.TraceLegsFTS
		vec      = service.TraceLegsVector
	)
	specs := []candSpec{
		{id: "kb-rrf", stage: returned, legs: both, ftsRank: 1, vecRank: 2, ftsRaw: -7.25, vecRaw: 0.83,
			fts: 0.016, vec: 0.016, mention: 0.15, graph: 0.3, overlap: 0.2,
			obj: obj("kb-rrf", "note", "cli", titled("Reciprocal rank fusion notes"))},
		{id: "kb-bm25", stage: returned, legs: fts, ftsRank: 2, ftsRaw: -6.5,
			fts: 0.016, mention: 0.15, graph: 0.2, overlap: 0.2,
			obj: obj("kb-bm25", "article", "web", func(o *storage.KnowledgeObject) {
				o.Summaries = []string{"BM25 scoring in SQLite FTS5\nsecond line ignored"}
			})},
		{id: "kb-xss", stage: returned, legs: vec, vecRank: 1, vecRaw: 0.88,
			vec: 0.016, mention: 0.1, graph: 0.1, overlap: 0.3,
			obj: obj("kb-xss", "note", "", titled(xssLabel))},
		{id: "kb-pgvec", stage: returned, legs: both, ftsRank: 4, vecRank: 3, ftsRaw: -3.1, vecRaw: 0.79,
			fts: 0.015, vec: 0.016, mention: 0.15, graph: 0.15, overlap: 0.1,
			obj: obj("kb-pgvec", "note", "cli", titled("pgvector HNSW index tuning"))},
		{id: "kb-sqlite-vec", stage: cutLimit, legs: both, ftsRank: 5, vecRank: 4, ftsRaw: -2.4, vecRaw: 0.74,
			fts: 0.015, vec: 0.016, mention: 0.1, graph: 0.1, overlap: 0.15,
			obj: obj("kb-sqlite-vec", "note", "cli", titled("sqlite-vec brute force fallback"))},
		{id: "kb-weights", stage: cutLimit, legs: vec, vecRank: 5, vecRaw: 0.7,
			vec: 0.015, mention: 0.05, graph: 0.2, overlap: 0.1,
			obj: obj("kb-weights", "decision", "", titled("Leg weights: 1.0 / 1.0"))},
		{id: "kb-tsrank", stage: cutThr, legs: fts, ftsRank: 3, ftsRaw: -4.8,
			fts: 0.016, mention: 0.1, graph: 0.1, overlap: 0.05,
			obj: obj("kb-tsrank", "note", "", func(o *storage.KnowledgeObject) {
				o.RawContent = "\n\n# Postgres ts_rank_cd\nbody text"
			})},
		{id: "kb-orphan", stage: cutThr, legs: fts, ftsRank: 6, ftsRaw: -1.2,
			fts: 0.015, overlap: 0.1},
		{id: "kb-embed", stage: cutThr, legs: vec, vecRank: 6, vecRaw: 0.61,
			vec: 0.015, mention: 0.1,
			obj: obj("kb-embed", "note", "import", func(o *storage.KnowledgeObject) {
				o.TextContent = "Embedding model migration checklist"
			})},
	}
	tr := baseTrace("hybrid search tuning")
	tr.FTSQuery = `"hybrid" OR "search" OR "tuning"`
	fillCounts(tr, specs)

	f := &fakeStore{
		edges: map[string][]*storage.Edge{},
		entities: map[string]*storage.Entity{
			"search/rrf":    {Slug: "search/rrf", Title: "Reciprocal Rank Fusion"},
			"search/bm25":   {Slug: "search/bm25", Title: "BM25"},
			"db/postgres":   {Slug: "db/postgres", Title: "PostgreSQL"},
			"ml/embeddings": {Slug: "ml/embeddings", Title: "Embeddings"},
			"people/ada":    {Slug: "people/ada", Title: "Ada Lovelace & <co>"},
			"db/pgvector":   {Slug: "db/pgvector", Title: "  "}, // blank title: slug fallback
			// db/sqlite has no stored record: slug fallback.
		},
		embeddings: map[string][]float32{
			"kb-rrf":        {1, 0.2, 0, 0},
			"kb-bm25":       {0.9, 0.3, 0.1, 0},
			"kb-xss":        {0, 0, 1, 0},
			"kb-pgvec":      {0, 1, 0, 0.2},
			"kb-sqlite-vec": {0, 0.95, 0.1, 0.25},
			"kb-weights":    {0.8, 0.1, 0, 0.1},
			"kb-orphan":     {0, 0, 0, 0},
			"kb-embed":      {0, 0.7, 0, 0.7},
			"kb-outside":    {1, 0.2, 0, 0},
		},
	}
	f.link("kb-rrf", "kb-bm25", "extends", "extended-by")
	f.link("kb-xss", "kb-weights", "contradicts", "contradicted-by")
	f.link("kb-rrf", "kb-weights", "related-to", "")
	f.link("kb-pgvec", "kb-sqlite-vec", "related-to", "")
	f.link("kb-sqlite-vec", "kb-pgvec", "related-to", "")
	f.link("kb-tsrank", "kb-bm25", "derived-from", "derived-to")
	f.link("kb-pgvec", "kb-sqlite-vec", "supersedes", "superseded-by")
	f.addEdge("kb-embed", "object", "kb-pgvec", "supported-by") // inverse-only row
	f.link("kb-rrf", "kb-outside", "supports", "supported-by")  // non-candidate target
	f.addEdge("kb-rrf", "object", "kb-bm25", "contains")
	f.addEdge("kb-pgvec", "object", "kb-embed", "alternative")
	f.addEdge("kb-bm25", "object", "kb-rrf", "batch_contains")

	f.mention("kb-rrf", "search/rrf", "search/bm25", "ml/embeddings", "search/rrf")
	f.mention("kb-bm25", "search/bm25", "db/sqlite", "db/postgres")
	f.mention("kb-xss", "search/rrf", "people/ada")
	f.mention("kb-pgvec", "db/postgres", "db/pgvector", "ml/embeddings")
	f.mention("kb-sqlite-vec", "db/sqlite", "ml/embeddings")
	f.mention("kb-weights", "search/rrf")
	f.mention("kb-tsrank", "db/postgres", "search/bm25")
	f.mention("kb-embed", "ml/embeddings", "db/pgvector")
	f.mention("kb-outside", "search/rrf", "db/postgres")
	f.addEdge("kb-rrf", "entity", "search/rrf", "references") // not a mention
	return tr, f
}

// fallbackFixture is an FTS-only trace after a vector-leg failure.
func fallbackFixture() (*service.SearchTrace, *fakeStore) {
	tr := baseTrace("ts_rank_cd")
	tr.Mode = service.SearchModeFTSFallback
	tr.SemanticStatus = retrieval.SemanticProviderError
	tr.VectorError = "model fixture-embed: embed query: dial tcp 127.0.0.1:11434: connection refused"
	tr.VectorPool = 50
	fillCounts(tr, []candSpec{
		{id: "kb-tsrank", stage: service.TraceStageReturned, legs: service.TraceLegsFTS,
			ftsRank: 1, ftsRaw: 0.42, fts: 0.016, mention: 0.1, overlap: 0.3,
			obj: obj("kb-tsrank", "note", "", titled("Postgres ts_rank_cd"))},
		{id: "kb-bm25", stage: service.TraceStageCutThreshold, legs: service.TraceLegsFTS,
			ftsRank: 2, ftsRaw: 0.11, fts: 0.016, overlap: 0.05,
			obj: obj("kb-bm25", "article", "web", nil)},
	})
	_, f := sampleFixture()
	return tr, f
}
