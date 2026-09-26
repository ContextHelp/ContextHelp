package storagetest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// EmbeddingConformanceDriver is what the embedding conformance suite needs
// from a driver: the storage surface plus raw access for registry rows and
// signature tampering.
type EmbeddingConformanceDriver interface {
	storage.StorageDriver
	DB() *sql.DB
}

// embHarness carries the shared state of one conformance run.
type embHarness struct {
	t       *testing.T
	ctx     context.Context
	drv     EmbeddingConformanceDriver
	store   storage.EmbeddingStore
	reg     *registry.Store
	dialect indexsig.Dialect
	maxDim  int
}

// EmbeddingStoreConformance runs the storage.EmbeddingStore contract
// (ADR-071, amendment 2026-09-26) against an initialized driver. Subtests
// share the database; each uses its own model IDs and object-ID prefix.
func EmbeddingStoreConformance(t *testing.T, drv EmbeddingConformanceDriver) {
	t.Helper()
	h := &embHarness{
		ctx:     context.Background(),
		drv:     drv,
		store:   drv.Embeddings(),
		dialect: indexsig.DialectSQLite,
		maxDim:  storage.SQLiteVecMaxDimension,
	}
	if d, ok := drv.(interface{ SQLDialect() string }); ok && d.SQLDialect() == "postgres" {
		h.dialect = indexsig.DialectPostgres
		h.maxDim = indexsig.PostgresHNSWMaxDimension
	}
	h.reg = registry.NewFor(drv.DB(), string(h.dialect))

	run := func(name string, fn func(h *embHarness)) {
		t.Run(name, func(t *testing.T) {
			sub := *h
			sub.t = t
			fn(&sub)
		})
	}
	run("EnsureIndexRejectsBadSpecs", testEnsureIndexRejectsBadSpecs)
	run("EnsureIndexIdempotent", testEnsureIndexIdempotent)
	run("PerModelDimensions", testPerModelDimensions)
	run("PutWrongDimensionNoPartialWrite", testPutWrongDimensionNoPartialWrite)
	run("PutReplacesPerModel", testPutReplacesPerModel)
	run("PutSkipsZeroVectors", testPutSkipsZeroVectors)
	run("IndexMissing", testIndexMissing)
	run("SearchOrderingAndChunkCollapse", testSearchOrderingAndChunkCollapse)
	run("SearchDefaultTopKAndQueryChecks", testSearchDefaultTopKAndQueryChecks)
	run("CascadeOnObjectDelete", testCascadeOnObjectDelete)
	run("SignatureDriftRebuilds", testSignatureDriftRebuilds)
	run("PurgeModel", testPurgeModel)
	run("ListMissingPaging", testListMissingPaging)
}

// --- helpers ---------------------------------------------------------------

func (h *embHarness) object(id string) {
	h.t.Helper()
	if err := h.drv.Objects().Create(h.ctx, searchFixtureObject(id, "note", "embedding fixture "+id)); err != nil {
		h.t.Fatalf("create object %s: %v", id, err)
	}
}

// model registers modelID at dim and builds its index.
func (h *embHarness) model(modelID string, dim int) storage.EmbeddingModelSpec {
	h.t.Helper()
	h.register(modelID, dim)
	spec := storage.EmbeddingModelSpec{ModelID: modelID, Provider: "conformance", Dimension: dim}
	if err := h.store.EnsureIndex(h.ctx, spec); err != nil {
		h.t.Fatalf("EnsureIndex(%s): %v", modelID, err)
	}
	return spec
}

func (h *embHarness) register(modelID string, dim int) {
	h.t.Helper()
	if err := h.reg.Register(h.ctx, registry.Model{
		ModelID: modelID, Provider: "conformance", Dimension: dim,
	}, false); err != nil {
		h.t.Fatalf("register %s: %v", modelID, err)
	}
}

func (h *embHarness) put(objectID string, vs ...storage.ObjectVector) {
	h.t.Helper()
	if err := h.store.Put(h.ctx, objectID, vs); err != nil {
		h.t.Fatalf("Put(%s): %v", objectID, err)
	}
}

func (h *embHarness) get(objectID, modelID string) []storage.ObjectVector {
	h.t.Helper()
	got, err := h.store.Get(h.ctx, objectID, modelID)
	if err != nil {
		h.t.Fatalf("Get(%s, %s): %v", objectID, modelID, err)
	}
	return got
}

func (h *embHarness) search(modelID string, q []float32, topK int) []storage.EmbeddingHit {
	h.t.Helper()
	hits, err := h.store.Search(h.ctx, storage.VectorQuery{ModelID: modelID, Vector: q, TopK: topK})
	if err != nil {
		h.t.Fatalf("Search(%s): %v", modelID, err)
	}
	return hits
}

func (h *embHarness) signature(modelID string) *indexsig.Row {
	h.t.Helper()
	row, err := indexsig.Load(h.ctx, h.drv.DB(), h.dialect, indexsig.EmbeddingSignatureID(modelID))
	if err != nil {
		h.t.Fatalf("load signature %s: %v", modelID, err)
	}
	return row
}

func ov(modelID string, chunk int, v ...float32) storage.ObjectVector {
	return storage.ObjectVector{ModelID: modelID, ChunkIdx: chunk, Vector: v, Text: fmt.Sprintf("%s#%d", modelID, chunk)}
}

// oneHot returns a dim-long unit vector along axis i.
func oneHot(dim, i int) []float32 {
	v := make([]float32, dim)
	v[i] = 1
	return v
}

func hitIDs(hits []storage.EmbeddingHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.ObjectID
	}
	return out
}

func vecEqual(a, b []float32) bool {
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

func (h *embHarness) wantVectors(objectID, modelID string, want ...[]float32) {
	h.t.Helper()
	got := h.get(objectID, modelID)
	if len(got) != len(want) {
		h.t.Fatalf("Get(%s, %s): %d chunks, want %d (%+v)", objectID, modelID, len(got), len(want), got)
	}
	for i := range want {
		if got[i].ModelID != modelID || !vecEqual(got[i].Vector, want[i]) {
			h.t.Errorf("Get(%s, %s)[%d] = %+v, want vector %v", objectID, modelID, i, got[i], want[i])
		}
	}
}

// --- subtests --------------------------------------------------------------

func testEnsureIndexRejectsBadSpecs(h *embHarness) {
	for _, dim := range []int{0, -1, h.maxDim + 1} {
		err := h.store.EnsureIndex(h.ctx, storage.EmbeddingModelSpec{ModelID: "conf-baddim@1", Provider: "p", Dimension: dim})
		if !errors.Is(err, storage.ErrEmbeddingDimension) {
			h.t.Errorf("EnsureIndex(dim=%d) = %v, want ErrEmbeddingDimension", dim, err)
		}
	}
	for _, id := range []string{"", "bad id", "x'; DROP TABLE objects; --"} {
		if err := h.store.EnsureIndex(h.ctx, storage.EmbeddingModelSpec{ModelID: id, Provider: "p", Dimension: 4}); err == nil {
			h.t.Errorf("EnsureIndex(model_id=%q) accepted an invalid model id", id)
		}
	}
}

func testEnsureIndexIdempotent(h *embHarness) {
	spec := h.model("conf-idem@1", 4)
	first := h.signature(spec.ModelID)
	if first == nil {
		h.t.Fatal("EnsureIndex stamped no signature")
	}
	for _, part := range []string{"model_id=" + spec.ModelID, "dimension=4"} {
		if !strings.Contains(first.InputsSummary, part) {
			h.t.Errorf("signature summary missing %q: %s", part, first.InputsSummary)
		}
	}
	h.object("idem-1")
	h.put("idem-1", ov(spec.ModelID, 0, 1, 0, 0, 0))
	for i := 0; i < 2; i++ {
		if err := h.store.EnsureIndex(h.ctx, spec); err != nil {
			h.t.Fatalf("EnsureIndex re-run %d: %v", i, err)
		}
	}
	if again := h.signature(spec.ModelID); again == nil || again.SignatureHash != first.SignatureHash {
		h.t.Errorf("re-run changed the signature: %+v -> %+v", first, again)
	}
	if got := hitIDs(h.search(spec.ModelID, oneHot(4, 0), 5)); len(got) != 1 || got[0] != "idem-1" {
		h.t.Errorf("search after idempotent EnsureIndex = %v, want [idem-1]", got)
	}
}

func testPerModelDimensions(h *embHarness) {
	a := h.model("conf-dim-a@1", 4)
	b := h.model("conf-dim-b@1", 8)
	h.object("dims-1")
	h.object("dims-2")
	h.put("dims-1", ov(a.ModelID, 0, oneHot(4, 0)...), ov(b.ModelID, 0, oneHot(8, 7)...))
	h.put("dims-2", ov(a.ModelID, 0, oneHot(4, 1)...), ov(b.ModelID, 0, oneHot(8, 6)...))

	h.wantVectors("dims-1", a.ModelID, oneHot(4, 0))
	h.wantVectors("dims-1", b.ModelID, oneHot(8, 7))
	if got := h.get("dims-1", a.ModelID)[0].Text; got != a.ModelID+"#0" {
		h.t.Errorf("chunk text round-trip = %q", got)
	}

	if got := hitIDs(h.search(a.ModelID, oneHot(4, 1), 1)); len(got) != 1 || got[0] != "dims-2" {
		h.t.Errorf("4-dim search = %v, want [dims-2]", got)
	}
	if got := hitIDs(h.search(b.ModelID, oneHot(8, 7), 1)); len(got) != 1 || got[0] != "dims-1" {
		h.t.Errorf("8-dim search = %v, want [dims-1]", got)
	}
}

func testPutWrongDimensionNoPartialWrite(h *embHarness) {
	a := h.model("conf-wd-a@1", 4)
	b := h.model("conf-wd-b@1", 8)
	h.object("wd-1")
	h.put("wd-1", ov(a.ModelID, 0, oneHot(4, 0)...), ov(b.ModelID, 0, oneHot(8, 0)...))

	cases := map[string][]storage.ObjectVector{
		"second model wrong": {ov(a.ModelID, 0, oneHot(4, 3)...), ov(b.ModelID, 0, oneHot(4, 1)...)},
		"first model wrong":  {ov(a.ModelID, 0, oneHot(8, 3)...), ov(b.ModelID, 0, oneHot(8, 1)...)},
		"one chunk wrong":    {ov(a.ModelID, 0, oneHot(4, 3)...), ov(a.ModelID, 1, 1, 2, 3)},
	}
	for name, vs := range cases {
		err := h.store.Put(h.ctx, "wd-1", vs)
		if !errors.Is(err, storage.ErrEmbeddingDimension) {
			h.t.Errorf("%s: Put = %v, want ErrEmbeddingDimension", name, err)
		}
		h.wantVectors("wd-1", a.ModelID, oneHot(4, 0))
		h.wantVectors("wd-1", b.ModelID, oneHot(8, 0))
	}
	// The index must still hold the original entry, not the rejected one.
	hits := h.search(a.ModelID, oneHot(4, 0), 1)
	if len(hits) != 1 || hits[0].ObjectID != "wd-1" || hits[0].Distance > 1e-5 {
		h.t.Errorf("index after rejected Put = %+v, want wd-1 at distance 0", hits)
	}
}

func testPutReplacesPerModel(h *embHarness) {
	a := h.model("conf-rep-a@1", 4)
	b := h.model("conf-rep-b@1", 4)
	h.object("rep-1")
	h.object("rep-2")
	h.put("rep-1",
		ov(a.ModelID, 0, 0, 1, 0, 0),
		ov(a.ModelID, 1, 0, 0, 1, 0),
		ov(a.ModelID, 2, 1, 0, 0, 0),
		ov(b.ModelID, 0, 0, 0, 0, 1))
	h.put("rep-2", ov(a.ModelID, 0, 1, 1, 0, 0))

	// Replace a's rows with a single chunk; b is not in the call.
	h.put("rep-1", ov(a.ModelID, 0, 0, 0, 0, 1))
	h.wantVectors("rep-1", a.ModelID, []float32{0, 0, 0, 1})
	h.wantVectors("rep-1", b.ModelID, []float32{0, 0, 0, 1})

	// Stale index entries would put rep-1 at distance 0 via the dropped
	// chunk 2; after the replace rep-2 is closer.
	q := oneHot(4, 0)
	hits := h.search(a.ModelID, q, 2)
	if got := hitIDs(hits); len(got) != 2 || got[0] != "rep-2" || got[1] != "rep-1" {
		h.t.Fatalf("search after replace = %+v, want [rep-2 rep-1]", hits)
	}
	if want := cosineDistance(q, []float32{0, 0, 0, 1}); math.Abs(hits[1].Distance-want) > 1e-5 {
		h.t.Errorf("rep-1 distance = %v, want %v (replaced chunk)", hits[1].Distance, want)
	}
	if hits[1].ChunkIdx != 0 {
		h.t.Errorf("rep-1 chunk = %d, want 0", hits[1].ChunkIdx)
	}
}

func testPutSkipsZeroVectors(h *embHarness) {
	a := h.model("conf-zero@1", 4)
	h.object("zero-1")
	h.put("zero-1", ov(a.ModelID, 0, 0, 0, 0, 0), ov(a.ModelID, 1, 0, 1, 0, 0))
	got := h.get("zero-1", a.ModelID)
	if len(got) != 1 || got[0].ChunkIdx != 1 {
		h.t.Fatalf("Get after zero-vector Put = %+v, want only chunk 1", got)
	}
	// All-zero replacement leaves the model with no rows for the object.
	h.put("zero-1", ov(a.ModelID, 0, 0, 0, 0, 0))
	if got := h.get("zero-1", a.ModelID); len(got) != 0 {
		h.t.Errorf("all-zero Put left rows: %+v", got)
	}
	if hits := h.search(a.ModelID, oneHot(4, 1), 5); len(hits) != 0 {
		h.t.Errorf("all-zero Put left index entries: %+v", hits)
	}
}

func testIndexMissing(h *embHarness) {
	h.object("miss-1")
	// Unregistered model.
	err := h.store.Put(h.ctx, "miss-1", []storage.ObjectVector{ov("conf-unregistered@1", 0, 1, 0, 0, 0)})
	if !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		h.t.Errorf("Put(unregistered) = %v, want ErrEmbeddingIndexMissing", err)
	}
	_, err = h.store.Search(h.ctx, storage.VectorQuery{ModelID: "conf-unregistered@1", Vector: oneHot(4, 0)})
	if !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		h.t.Errorf("Search(unregistered) = %v, want ErrEmbeddingIndexMissing", err)
	}
	// Registered but never indexed.
	h.register("conf-noindex@1", 4)
	err = h.store.Put(h.ctx, "miss-1", []storage.ObjectVector{ov("conf-noindex@1", 0, 1, 0, 0, 0)})
	if !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		h.t.Errorf("Put(no index) = %v, want ErrEmbeddingIndexMissing", err)
	}
	_, err = h.store.Search(h.ctx, storage.VectorQuery{ModelID: "conf-noindex@1", Vector: oneHot(4, 0)})
	if !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		h.t.Errorf("Search(no index) = %v, want ErrEmbeddingIndexMissing", err)
	}
	if got := h.get("miss-1", "conf-noindex@1"); len(got) != 0 {
		h.t.Errorf("rejected Put wrote rows: %+v", got)
	}
}

func testSearchOrderingAndChunkCollapse(h *embHarness) {
	a := h.model("conf-search@1", 4)
	q := []float32{1, 0, 0, 0}
	// srch-many: 30 chunks, all closer to q than any other object, so a
	// KNN that fetches only TopK rows sees nothing but srch-many.
	h.object("srch-many")
	many := make([]storage.ObjectVector, 0, 30)
	for i := 0; i < 30; i++ {
		many = append(many, ov(a.ModelID, i, 1, float32(i)*0.001, 0, 0))
	}
	h.put("srch-many", many...)
	h.object("srch-near")
	h.put("srch-near", ov(a.ModelID, 0, 0, 0, 1, 0), ov(a.ModelID, 1, 1, 0.5, 0, 0))
	h.object("srch-mid")
	h.put("srch-mid", ov(a.ModelID, 0, 1, 1, 0, 0))
	h.object("srch-far")
	h.put("srch-far", ov(a.ModelID, 0, 0, 1, 0, 0))

	hits := h.search(a.ModelID, q, 3)
	if got := hitIDs(hits); strings.Join(got, ",") != "srch-many,srch-near,srch-mid" {
		h.t.Fatalf("Search order = %v, want [srch-many srch-near srch-mid]", got)
	}
	if hits[0].ChunkIdx != 0 || hits[0].Distance > 1e-5 {
		h.t.Errorf("srch-many best chunk = %+v, want chunk 0 at distance 0", hits[0])
	}
	if hits[1].ChunkIdx != 1 {
		h.t.Errorf("srch-near best chunk = %d, want 1", hits[1].ChunkIdx)
	}
	for i, want := range []float64{0, cosineDistance(q, []float32{1, 0.5, 0, 0}), cosineDistance(q, []float32{1, 1, 0, 0})} {
		if math.Abs(hits[i].Distance-want) > 1e-5 {
			h.t.Errorf("hit %d distance = %v, want cosine distance %v", i, hits[i].Distance, want)
		}
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Distance < hits[i-1].Distance {
			h.t.Errorf("hits not ascending: %+v", hits)
		}
	}

	all := h.search(a.ModelID, q, 10)
	if got := hitIDs(all); strings.Join(got, ",") != "srch-many,srch-near,srch-mid,srch-far" {
		h.t.Errorf("Search(TopK=10) = %v, want every object once in distance order", got)
	}
}

func testSearchDefaultTopKAndQueryChecks(h *embHarness) {
	a := h.model("conf-topk@1", 4)
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("topk-%02d", i)
		h.object(id)
		h.put(id, ov(a.ModelID, 0, 1, float32(i), 0, 0))
	}
	if got := h.search(a.ModelID, oneHot(4, 0), 0); len(got) != 10 {
		h.t.Errorf("TopK=0 returned %d hits, want the default 10", len(got))
	}
	if got := h.search(a.ModelID, []float32{0, 0, 0, 0}, 5); len(got) != 0 {
		h.t.Errorf("zero-magnitude query returned %+v, want none", got)
	}
	_, err := h.store.Search(h.ctx, storage.VectorQuery{ModelID: a.ModelID, Vector: oneHot(8, 0)})
	if !errors.Is(err, storage.ErrEmbeddingDimension) {
		h.t.Errorf("wrong-dimension query = %v, want ErrEmbeddingDimension", err)
	}
}

func testCascadeOnObjectDelete(h *embHarness) {
	a := h.model("conf-cascade@1", 4)
	h.object("casc-1")
	h.object("casc-2")
	h.put("casc-1", ov(a.ModelID, 0, 1, 0, 0, 0), ov(a.ModelID, 1, 1, 0.1, 0, 0))
	h.put("casc-2", ov(a.ModelID, 0, 1, 1, 0, 0))
	if err := h.drv.Objects().Delete(h.ctx, "casc-1"); err != nil {
		h.t.Fatalf("delete object: %v", err)
	}
	if got := h.get("casc-1", a.ModelID); len(got) != 0 {
		h.t.Errorf("rows survived object delete: %+v", got)
	}
	if got := hitIDs(h.search(a.ModelID, oneHot(4, 0), 5)); len(got) != 1 || got[0] != "casc-2" {
		h.t.Errorf("search after object delete = %v, want [casc-2]", got)
	}
}

func testSignatureDriftRebuilds(h *embHarness) {
	spec := h.model("conf-drift@1", 4)
	h.object("drift-1")
	h.put("drift-1", ov(spec.ModelID, 0, 1, 0, 0, 0))
	good := h.signature(spec.ModelID)

	if err := indexsig.Upsert(h.ctx, h.drv.DB(), h.dialect,
		indexsig.EmbeddingSignatureID(spec.ModelID), "stale", "stale"); err != nil {
		h.t.Fatalf("tamper signature: %v", err)
	}
	if err := h.store.EnsureIndex(h.ctx, spec); err != nil {
		h.t.Fatalf("EnsureIndex after drift: %v", err)
	}
	if got := h.signature(spec.ModelID); got == nil || got.SignatureHash != good.SignatureHash {
		h.t.Errorf("drifted signature not re-stamped: %+v, want hash %s", got, good.SignatureHash)
	}

	// A changed input (provider) is drift too: new stamp, index rebuilt
	// from canonical rows.
	spec.Provider = "conformance-v2"
	if err := h.store.EnsureIndex(h.ctx, spec); err != nil {
		h.t.Fatalf("EnsureIndex after provider change: %v", err)
	}
	moved := h.signature(spec.ModelID)
	if moved == nil || moved.SignatureHash == good.SignatureHash ||
		!strings.Contains(moved.InputsSummary, "provider=conformance-v2") {
		h.t.Errorf("provider change not re-stamped: %+v", moved)
	}
	if got := hitIDs(h.search(spec.ModelID, oneHot(4, 0), 5)); len(got) != 1 || got[0] != "drift-1" {
		h.t.Errorf("rebuilt index lost rows: search = %v, want [drift-1]", got)
	}
	// Writes after a rebuild still reach the index.
	h.object("drift-2")
	h.put("drift-2", ov(spec.ModelID, 0, 0, 1, 0, 0))
	if got := hitIDs(h.search(spec.ModelID, oneHot(4, 1), 1)); len(got) != 1 || got[0] != "drift-2" {
		h.t.Errorf("write after rebuild not indexed: search = %v", got)
	}
}

func testPurgeModel(h *embHarness) {
	a := h.model("conf-purge-a@1", 4)
	b := h.model("conf-purge-b@1", 4)
	h.object("purge-1")
	h.put("purge-1", ov(a.ModelID, 0, 1, 0, 0, 0), ov(b.ModelID, 0, 0, 1, 0, 0))

	if err := h.store.PurgeModel(h.ctx, a.ModelID); err != nil {
		h.t.Fatalf("PurgeModel: %v", err)
	}
	if got := h.get("purge-1", a.ModelID); len(got) != 0 {
		h.t.Errorf("purged model kept rows: %+v", got)
	}
	if _, err := h.store.Search(h.ctx, storage.VectorQuery{ModelID: a.ModelID, Vector: oneHot(4, 0)}); !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		h.t.Errorf("Search(purged) = %v, want ErrEmbeddingIndexMissing", err)
	}
	if sig := h.signature(a.ModelID); sig != nil {
		h.t.Errorf("purged model kept its signature: %+v", sig)
	}
	if _, err := h.reg.Get(h.ctx, a.ModelID); err != nil {
		h.t.Errorf("PurgeModel touched the registry row: %v", err)
	}
	h.wantVectors("purge-1", b.ModelID, []float32{0, 1, 0, 0})
	if got := hitIDs(h.search(b.ModelID, oneHot(4, 1), 5)); len(got) != 1 || got[0] != "purge-1" {
		h.t.Errorf("other model's index damaged by purge: %v", got)
	}
	// Re-indexing a purged model starts empty.
	if err := h.store.EnsureIndex(h.ctx, a); err != nil {
		h.t.Fatalf("EnsureIndex after purge: %v", err)
	}
	if hits := h.search(a.ModelID, oneHot(4, 0), 5); len(hits) != 0 {
		h.t.Errorf("re-indexed purged model returned %+v", hits)
	}
}

func testListMissingPaging(h *embHarness) {
	m := h.model("conf-missing@1", 4)
	for i := 0; i < 8; i++ {
		h.object(fmt.Sprintf("lm-%02d", i))
	}
	h.put("lm-01", ov(m.ModelID, 0, 1, 0, 0, 0))
	h.put("lm-04", ov(m.ModelID, 0, 1, 0, 0, 0))
	want := []string{"lm-00", "lm-02", "lm-03", "lm-05", "lm-06", "lm-07"}

	// Walk pages of 4 from just before the prefix; stop at the first ID
	// outside it (other subtests' objects share the table).
	var got []string
	cursor := "lm-"
	for page := 0; page < 10; page++ {
		ids, err := h.store.ListMissing(h.ctx, m.ModelID, cursor, 4)
		if err != nil {
			h.t.Fatalf("ListMissing: %v", err)
		}
		if len(ids) > 4 {
			h.t.Fatalf("page of %d exceeds limit 4: %v", len(ids), ids)
		}
		done := len(ids) < 4
		for _, id := range ids {
			if id <= cursor {
				h.t.Fatalf("page not strictly after cursor %q: %v", cursor, ids)
			}
			if !strings.HasPrefix(id, "lm-") {
				done = true
				break
			}
			got = append(got, id)
		}
		if done {
			break
		}
		cursor = ids[len(ids)-1]
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		h.t.Errorf("ListMissing walk = %v, want %v", got, want)
	}

	// Unbounded page from the start of the prefix.
	all, err := h.store.ListMissing(h.ctx, m.ModelID, "lm-", 0)
	if err != nil {
		h.t.Fatalf("ListMissing(limit 0): %v", err)
	}
	var prefixed []string
	for _, id := range all {
		if strings.HasPrefix(id, "lm-") {
			prefixed = append(prefixed, id)
		}
	}
	if strings.Join(prefixed, ",") != strings.Join(want, ",") {
		h.t.Errorf("ListMissing(limit 0) = %v, want %v", prefixed, want)
	}
}
