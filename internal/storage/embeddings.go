package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
)

// Embedding variants (ADR-080 §7). The raw variant embeds the owner's
// text; the masked variant embeds the masked projection (the searchable
// minimum of a structured type, every classed part removed) and exists
// only for objects whose masked text differs from the raw one. Restricted
// viewers search the masked variant and fall back to the raw one where
// no masked row exists.
const (
	EmbeddingVariantRaw    = "raw"
	EmbeddingVariantMasked = "masked"
)

// EmbeddingVariants lists every variant an embeddings row may carry.
var EmbeddingVariants = []string{EmbeddingVariantRaw, EmbeddingVariantMasked}

// ObjectVector is one embedding a registered model produced for one chunk
// of an object: a row of the embeddings table keyed (object, model, chunk,
// variant) minus the object ID, which is known only once the object is
// persisted.
type ObjectVector struct {
	ModelID  string    `json:"model_id"`
	ChunkIdx int       `json:"chunk_idx"`
	Vector   []float32 `json:"vector"`
	// Text is the chunk text that was embedded (eval and debugging).
	Text string `json:"text,omitempty"`
	// Variant is EmbeddingVariantRaw or EmbeddingVariantMasked; empty
	// means raw.
	Variant string `json:"variant,omitempty"`
}

// EffectiveVariant returns v.Variant, raw when empty.
func (v ObjectVector) EffectiveVariant() string {
	if v.Variant == "" {
		return EmbeddingVariantRaw
	}
	return v.Variant
}

// EmbeddingModelSpec is the registry data a per-model vector index is built
// from. Dimension is the registry's measured dimension, never a driver
// default.
type EmbeddingModelSpec struct {
	ModelID   string
	Provider  string
	Dimension int
}

// VectorQuery is a nearest-neighbour query against one model's index.
type VectorQuery struct {
	ModelID string
	Vector  []float32
	// TopK bounds the number of object-level hits; <= 0 means 10.
	TopK int
	// Variant selects the rows ranked: EmbeddingVariantRaw (the default
	// when empty) ranks raw rows only; EmbeddingVariantMasked ranks each
	// object's masked row where one exists and its raw row otherwise.
	Variant string
}

// EffectiveVariant returns q.Variant, raw when empty.
func (q VectorQuery) EffectiveVariant() string {
	if q.Variant == "" {
		return EmbeddingVariantRaw
	}
	return q.Variant
}

// EmbeddingHit is one object-level search hit: the object's closest chunk
// under the queried model. Distance is cosine distance (the pinned
// cross-driver metric); smaller is closer and score = 1 - Distance.
type EmbeddingHit struct {
	ObjectID string
	ChunkIdx int
	Distance float64
}

// EmbeddingStore is the single write and read path for embeddings (ADR-071,
// amendment 2026-09-26): canonical rows in the embeddings table keyed
// (object, model, chunk), plus one ANN index per registered model at the
// registry's dimension.
type EmbeddingStore interface {
	// EnsureIndex creates the model's ANN index when absent, rebuilds it
	// from canonical rows when its ADR-070 signature (embeddings_<model_id>)
	// no longer matches, and stamps the signature. Idempotent. Errors with
	// ErrEmbeddingDimension when spec.Dimension is outside the backend's
	// indexable range.
	EnsureIndex(ctx context.Context, spec EmbeddingModelSpec) error

	// Put replaces objectID's rows for every model present in vectors (all
	// chunks of that object under that model) and leaves other models'
	// rows untouched. Canonical rows and index entries change atomically.
	// A vector whose length differs from the model's registry dimension
	// fails with ErrEmbeddingDimension; zero-magnitude vectors are skipped
	// (cosine distance is undefined for them).
	Put(ctx context.Context, objectID string, vectors []ObjectVector) error

	// Get returns objectID's stored chunks under modelID, ordered by
	// chunk index. No rows is an empty result, not an error.
	Get(ctx context.Context, objectID, modelID string) ([]ObjectVector, error)

	// Search returns object-level hits from the model's index, closest
	// first, collapsing chunks to the best one per object. Fails with
	// ErrEmbeddingIndexMissing when the model has no index.
	Search(ctx context.Context, q VectorQuery) ([]EmbeddingHit, error)

	// ListMissing pages object IDs that have no row under modelID, in
	// ascending ID order, strictly after afterObjectID ("" starts at the
	// beginning). The migration job's backfill cursor.
	ListMissing(ctx context.Context, modelID, afterObjectID string, limit int) ([]string, error)

	// PurgeModel deletes every row and the index for modelID, plus its
	// index signature. The registry row is the caller's to remove.
	PurgeModel(ctx context.Context, modelID string) error
}

// EmbeddingReader is an optional EmbeddingStore capability: read one
// model's stored chunks for a known set of objects in one round trip (Get
// is one object per call). Callers type-assert an EmbeddingStore to it;
// stores that cannot serve it simply don't implement it.
type EmbeddingReader interface {
	// EmbeddingsByID returns, for each id with rows under modelID, its
	// chunks ordered by chunk index: the same canonical rows modelID's
	// index is built from. Ids without a row under modelID (or unknown
	// ids) are absent from the map; that is not an error. Duplicate ids
	// are tolerated.
	EmbeddingsByID(ctx context.Context, modelID string, ids []string) (map[string][]ObjectVector, error)
}

// ErrEmbeddingIndexMissing is returned when a model has no ANN index
// (unregistered, unprobed dimension, or not yet built).
var ErrEmbeddingIndexMissing = errors.New("embedding index missing")

// ErrEmbeddingDimension is returned when a vector or a registry dimension
// does not fit the model's index.
var ErrEmbeddingDimension = errors.New("embedding dimension mismatch")

// SQLiteVecMaxDimension is sqlite-vec's vec0 vector-column ceiling
// (v0.1.6 rejects float[8193] at CREATE time).
const SQLiteVecMaxDimension = 8192

// embeddingModelIDPattern bounds model IDs to characters that are safe to
// embed as SQL string literals in per-model DDL (sqlite triggers, Postgres
// partial-index predicates): no quotes, whitespace, or control characters.
var embeddingModelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/+-]{0,199}$`)

// ValidateEmbeddingModelID reports whether id may be registered and used in
// per-model index DDL.
func ValidateEmbeddingModelID(id string) error {
	if !embeddingModelIDPattern.MatchString(id) {
		return fmt.Errorf("invalid embedding model_id %q: want 1-200 chars of [A-Za-z0-9._:@/+-], starting alphanumeric", id)
	}
	return nil
}

// EmbeddingIndexName returns the backend-neutral identifier stem for a
// model's ANN index: "emb_" plus the first 16 hex digits of sha256(model_id).
// Drivers prefix it ("vec_" for the sqlite vec0 table, "idx_" for the
// Postgres partial index). Hashing keeps identifiers valid and bounded for
// any model_id; the mapping is deterministic, so no registry column is needed.
func EmbeddingIndexName(modelID string) string {
	sum := sha256.Sum256([]byte(modelID))
	return "emb_" + hex.EncodeToString(sum[:8])
}
