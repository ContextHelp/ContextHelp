package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// EmbeddingStore is the Postgres storage.EmbeddingStore: canonical rows in
// the embeddings table (typmod-less pgvector column) plus one partial
// expression HNSW index per registered model (ADR-071, amendment
// 2026-09-26).
//
// Each model's index casts the column to the registry dimension and is
// restricted to the model's rows; a per-model CHECK constraint backs the
// dimension guard in Put at the database level. Queries inline the model_id
// literal so the planner can match the partial index (a bound parameter
// under a generic plan falls back to a bitmap scan plus sort).
type EmbeddingStore struct {
	db   *sql.DB
	caps *pgCaps
}

var _ storage.EmbeddingStore = (*EmbeddingStore)(nil)

// Embeddings returns the per-model embedding store.
func (d *Driver) Embeddings() storage.EmbeddingStore {
	return &EmbeddingStore{db: d.db, caps: d.caps}
}

// pgEmbeddingMaxFetch bounds Search's over-fetch when chunks crowd out
// distinct objects.
const pgEmbeddingMaxFetch = 4096

// pgIndex names the per-model schema objects EnsureIndex manages.
type pgIndex struct {
	index, check string
}

func pgIndexFor(modelID string) pgIndex {
	stem := storage.EmbeddingIndexName(modelID)
	return pgIndex{index: "idx_" + stem, check: "embeddings_dim_" + stem}
}

// Per-model DDL and query templates. {INDEX} and {CHECK} are hash-derived
// identifiers; {MODEL} is a model_id that passed ValidateEmbeddingModelID,
// whose charset cannot close a SQL string literal.
const (
	pgIndexTmpl = `CREATE INDEX IF NOT EXISTS {INDEX} ON embeddings
  USING hnsw ((vector::vector({DIM})) vector_cosine_ops) WHERE model_id = '{MODEL}'`
	pgCheckTmpl  = `ALTER TABLE embeddings ADD CONSTRAINT {CHECK} CHECK (model_id <> '{MODEL}' OR vector_dims(vector) = {DIM})`
	pgSearchTmpl = `SELECT object_id, chunk_idx, vector::vector({DIM}) <=> $1::vector({DIM}) AS distance
  FROM embeddings
 WHERE model_id = '{MODEL}'
 ORDER BY vector::vector({DIM}) <=> $1::vector({DIM})
 LIMIT $2`
)

func (ix pgIndex) render(tmpl, modelID string, dim int) string {
	return strings.NewReplacer(
		"{INDEX}", ix.index,
		"{CHECK}", ix.check,
		"{MODEL}", modelID,
		"{DIM}", strconv.Itoa(dim),
	).Replace(tmpl)
}

// pgDesiredIndex is the description EnsureIndex converges each model's
// index to: HNSW cosine at pgvector's default build parameters.
var pgDesiredIndex = indexsig.VectorIndexDescription{Method: "hnsw", OpsClass: "vector_cosine_ops"}

// EnsureIndex converges the model's partial HNSW index, its dimension CHECK
// and its signature. A no-op when the stored signature matches and the live
// index and constraint are in place; otherwise both are dropped and
// recreated (the index builds from canonical rows) and the signature is
// re-stamped, in one transaction.
func (s *EmbeddingStore) EnsureIndex(ctx context.Context, spec storage.EmbeddingModelSpec) error {
	if err := storage.ValidateEmbeddingModelID(spec.ModelID); err != nil {
		return err
	}
	if spec.Dimension <= 0 || spec.Dimension > hnswMaxDimension {
		return fmt.Errorf("postgres embeddings ensure index %s: dimension %d outside 1..%d: %w",
			spec.ModelID, spec.Dimension, hnswMaxDimension, storage.ErrEmbeddingDimension)
	}
	ix := pgIndexFor(spec.ModelID)
	sigID := indexsig.EmbeddingSignatureID(spec.ModelID)
	hash, summary := indexsig.ComputeEmbedding(spec.ModelID, spec.Provider, spec.Dimension,
		pgDesiredIndex.Method, pgDesiredIndex.OpsClass, pgDesiredIndex.BuildParams)

	current, err := s.indexCurrent(ctx, s.db, ix, sigID, hash)
	if err != nil {
		return fmt.Errorf("postgres embeddings ensure index %s: %w", spec.ModelID, err)
	}
	if current {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres embeddings ensure index %s: begin: %w", spec.ModelID, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize concurrent rebuilds of the same model (register racing a
	// daemon open), then re-check under the lock.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, sigID); err != nil {
		return fmt.Errorf("postgres embeddings ensure index %s: lock: %w", spec.ModelID, err)
	}
	if current, err := s.indexCurrent(ctx, tx, ix, sigID, hash); err != nil {
		return fmt.Errorf("postgres embeddings ensure index %s: %w", spec.ModelID, err)
	} else if current {
		return tx.Commit()
	}

	var mismatched int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE model_id = $1 AND vector_dims(vector) <> $2`,
		spec.ModelID, spec.Dimension).Scan(&mismatched); err != nil {
		return fmt.Errorf("postgres embeddings ensure index %s: check stored dimensions: %w", spec.ModelID, err)
	}
	if mismatched > 0 {
		return fmt.Errorf("postgres embeddings ensure index %s: %d stored rows are not %d-dimensional: %w",
			spec.ModelID, mismatched, spec.Dimension, storage.ErrEmbeddingDimension)
	}
	if err := dropPgIndex(ctx, tx, ix); err != nil {
		return fmt.Errorf("postgres embeddings ensure index %s: %w", spec.ModelID, err)
	}
	for _, stmt := range []string{
		ix.render(pgCheckTmpl, spec.ModelID, spec.Dimension),
		ix.render(pgIndexTmpl, spec.ModelID, spec.Dimension),
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("postgres embeddings ensure index %s: create: %w", spec.ModelID, err)
		}
	}
	if err := indexsig.Upsert(ctx, tx, indexsig.DialectPostgres, sigID, hash, summary); err != nil {
		return fmt.Errorf("postgres embeddings ensure index %s: %w", spec.ModelID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("postgres embeddings ensure index %s: commit: %w", spec.ModelID, err)
	}
	return nil
}

// indexCurrent reports whether the stored signature matches hash and the
// live index and CHECK constraint have the desired shape.
func (s *EmbeddingStore) indexCurrent(ctx context.Context, q indexsig.DBTX, ix pgIndex, sigID, hash string) (bool, error) {
	stored, err := indexsig.Load(ctx, q, indexsig.DialectPostgres, sigID)
	if err != nil {
		return false, err
	}
	if stored == nil || stored.SignatureHash != hash {
		return false, nil
	}
	return pgIndexComplete(ctx, q, ix)
}

// pgIndexComplete reports whether the model's partial index has the
// desired description and its dimension CHECK exists.
func pgIndexComplete(ctx context.Context, q indexsig.DBTX, ix pgIndex) (bool, error) {
	live, err := indexsig.PostgresVectorIndexFor(ctx, q, ix.index)
	if err != nil {
		return false, err
	}
	if live != pgDesiredIndex {
		return false, nil
	}
	var hasCheck bool
	if err := q.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_constraint
		                WHERE conname = $1 AND conrelid = 'embeddings'::regclass)`,
		ix.check).Scan(&hasCheck); err != nil {
		return false, fmt.Errorf("inspect %s: %w", ix.check, err)
	}
	return hasCheck, nil
}

// dropPgIndex removes the model's index and dimension CHECK.
func dropPgIndex(ctx context.Context, tx *sql.Tx, ix pgIndex) error {
	for _, stmt := range []string{
		ix.render(`DROP INDEX IF EXISTS {INDEX}`, "", 0),
		ix.render(`ALTER TABLE embeddings DROP CONSTRAINT IF EXISTS {CHECK}`, "", 0),
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("drop %s: %w", ix.index, err)
		}
	}
	return nil
}

// modelVectors is one model's slice of a Put call.
type modelVectors struct {
	modelID string
	vectors []storage.ObjectVector
}

// groupByModel splits vectors per model, in first-appearance order.
func groupByModel(vectors []storage.ObjectVector) []modelVectors {
	var out []modelVectors
	idx := map[string]int{}
	for _, v := range vectors {
		i, ok := idx[v.ModelID]
		if !ok {
			i = len(out)
			idx[v.ModelID] = i
			out = append(out, modelVectors{modelID: v.ModelID})
		}
		out[i].vectors = append(out[i].vectors, v)
	}
	return out
}

// isZeroMagnitude reports whether every component is zero: cosine distance
// is undefined for such a vector.
func isZeroMagnitude(v []float32) bool {
	for _, f := range v {
		if f != 0 {
			return false
		}
	}
	return true
}

// checkFinite rejects NaN and infinite components, which pgvector refuses
// and cosine distance cannot rank.
func checkFinite(v []float32) error {
	for i, f := range v {
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return fmt.Errorf("component %d is not finite", i)
		}
	}
	return nil
}

// pgVectorLiteral encodes v in pgvector's text form with float32
// round-trip precision.
func pgVectorLiteral(v []float32) string {
	b := make([]byte, 0, 2+len(v)*10)
	b = append(b, '[')
	for i, f := range v {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendFloat(b, float64(f), 'g', -1, 32)
	}
	return string(append(b, ']'))
}

// parsePgVectorLiteral decodes pgvector's text form.
func parsePgVectorLiteral(s string) ([]float32, error) {
	s = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(s), "["), "]")
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("parse vector component %d: %w", i, err)
		}
		out[i] = float32(f)
	}
	return out, nil
}

// Put replaces objectID's rows for every model present in vectors. All
// checks (registry dimension, index presence, finite components) run before
// the transaction commits, so a rejected call leaves every model's rows as
// they were. Zero-magnitude vectors are skipped; a model whose vectors are
// all zero therefore ends with no rows.
func (s *EmbeddingStore) Put(ctx context.Context, objectID string, vectors []storage.ObjectVector) error {
	if objectID == "" {
		return errors.New("postgres embeddings put: empty object id")
	}
	groups := groupByModel(vectors)
	if len(groups) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres embeddings put %s: begin: %w", objectID, err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, g := range groups {
		dim, err := writableDimension(ctx, tx, g.modelID)
		if err != nil {
			return fmt.Errorf("postgres embeddings put %s: %w", objectID, err)
		}
		for _, v := range g.vectors {
			if len(v.Vector) != dim {
				return fmt.Errorf("postgres embeddings put %s/%s chunk %d: got %d dimensions, registry says %d: %w",
					objectID, g.modelID, v.ChunkIdx, len(v.Vector), dim, storage.ErrEmbeddingDimension)
			}
			if err := checkFinite(v.Vector); err != nil {
				return fmt.Errorf("postgres embeddings put %s/%s chunk %d: %w", objectID, g.modelID, v.ChunkIdx, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM embeddings WHERE object_id = $1 AND model_id = $2`, objectID, g.modelID); err != nil {
			return fmt.Errorf("postgres embeddings put %s/%s: clear: %w", objectID, g.modelID, err)
		}
		for _, v := range g.vectors {
			if isZeroMagnitude(v.Vector) {
				continue
			}
			var text any
			if v.Text != "" {
				text = v.Text
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, text, created_at)
				VALUES ($1, $2, $3, $4::vector, $5, NOW())`,
				objectID, g.modelID, v.ChunkIdx, pgVectorLiteral(v.Vector), text); err != nil {
				return fmt.Errorf("postgres embeddings put %s/%s chunk %d: %w", objectID, g.modelID, v.ChunkIdx, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("postgres embeddings put %s: commit: %w", objectID, err)
	}
	return nil
}

// writableDimension returns the model's registry dimension after checking
// that its index exists.
func writableDimension(ctx context.Context, q indexsig.DBTX, modelID string) (int, error) {
	dim, err := registryDimension(ctx, q, modelID)
	if err != nil {
		return 0, err
	}
	if err := requireIndex(ctx, q, modelID); err != nil {
		return 0, err
	}
	return dim, nil
}

// requireIndex fails with ErrEmbeddingIndexMissing unless the model's
// partial index exists.
func requireIndex(ctx context.Context, q indexsig.DBTX, modelID string) error {
	var exists bool
	if err := q.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`,
		pgIndexFor(modelID).index).Scan(&exists); err != nil {
		return fmt.Errorf("inspect index of %s: %w", modelID, err)
	}
	if !exists {
		return fmt.Errorf("model %s: %w", modelID, storage.ErrEmbeddingIndexMissing)
	}
	return nil
}

// registryDimension reads embedding_models.dimension. An unregistered model
// has no index by definition.
func registryDimension(ctx context.Context, q indexsig.DBTX, modelID string) (int, error) {
	var dim int
	err := q.QueryRowContext(ctx,
		`SELECT dimension FROM embedding_models WHERE model_id = $1`, modelID).Scan(&dim)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("model %s is not registered: %w", modelID, storage.ErrEmbeddingIndexMissing)
	}
	if err != nil {
		return 0, fmt.Errorf("read dimension of %s: %w", modelID, err)
	}
	return dim, nil
}

// Get returns objectID's chunks under modelID, ordered by chunk index.
func (s *EmbeddingStore) Get(ctx context.Context, objectID, modelID string) ([]storage.ObjectVector, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_idx, vector::text, text FROM embeddings
		 WHERE object_id = $1 AND model_id = $2
		 ORDER BY chunk_idx`, objectID, modelID)
	if err != nil {
		return nil, fmt.Errorf("postgres embeddings get %s/%s: %w", objectID, modelID, err)
	}
	defer rows.Close()
	out := make([]storage.ObjectVector, 0, 1) // single-chunk today
	for rows.Next() {
		var (
			v    storage.ObjectVector
			lit  string
			text sql.NullString
		)
		if err := rows.Scan(&v.ChunkIdx, &lit, &text); err != nil {
			return nil, fmt.Errorf("postgres embeddings get %s/%s: %w", objectID, modelID, err)
		}
		if v.Vector, err = parsePgVectorLiteral(lit); err != nil {
			return nil, fmt.Errorf("postgres embeddings get %s/%s chunk %d: %w", objectID, modelID, v.ChunkIdx, err)
		}
		v.ModelID = modelID
		v.Text = text.String
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres embeddings get %s/%s: %w", objectID, modelID, err)
	}
	return out, nil
}

// Search runs a KNN query over the model's partial index and collapses
// chunks to the best one per object. The LIMIT starts at TopK and grows
// until TopK distinct objects are found, the rows run out, or the fetch
// cap is reached.
func (s *EmbeddingStore) Search(ctx context.Context, q storage.VectorQuery) ([]storage.EmbeddingHit, error) {
	topK := q.TopK
	if topK <= 0 {
		topK = 10
	}
	// The model_id is inlined into the query below; refuse anything the
	// charset rule does not admit before it gets there.
	if err := storage.ValidateEmbeddingModelID(q.ModelID); err != nil {
		return nil, fmt.Errorf("postgres embeddings search: %w", err)
	}
	dim, err := writableDimension(ctx, s.db, q.ModelID)
	if err != nil {
		return nil, fmt.Errorf("postgres embeddings search: %w", err)
	}
	if len(q.Vector) != dim {
		return nil, fmt.Errorf("postgres embeddings search %s: query has %d dimensions, registry says %d: %w",
			q.ModelID, len(q.Vector), dim, storage.ErrEmbeddingDimension)
	}
	if err := checkFinite(q.Vector); err != nil {
		return nil, fmt.Errorf("postgres embeddings search %s: %w", q.ModelID, err)
	}
	// Cosine distance to a zero-magnitude query is undefined for every row.
	if isZeroMagnitude(q.Vector) {
		return nil, nil
	}
	query := pgIndexFor(q.ModelID).render(pgSearchTmpl, q.ModelID, dim)
	lit := pgVectorLiteral(q.Vector)

	for k := topK; ; k *= 4 {
		if k > pgEmbeddingMaxFetch {
			k = pgEmbeddingMaxFetch
		}
		var (
			hits    []storage.EmbeddingHit
			fetched int
		)
		err := queryVectorRowsTx(ctx, s.db, s.caps, func(rows *sql.Rows) error {
			for rows.Next() {
				var (
					h    storage.EmbeddingHit
					dist sql.NullFloat64
				)
				if err := rows.Scan(&h.ObjectID, &h.ChunkIdx, &dist); err != nil {
					return err
				}
				fetched++
				if !dist.Valid || math.IsNaN(dist.Float64) {
					continue
				}
				h.Distance = dist.Float64
				hits = append(hits, h)
			}
			return nil
		}, query, lit, k)
		if err != nil {
			return nil, fmt.Errorf("postgres embeddings search %s: %w", q.ModelID, err)
		}
		collapsed := collapseChunks(hits, topK)
		if len(collapsed) >= topK || fetched < k || k == pgEmbeddingMaxFetch {
			return collapsed, nil
		}
	}
}

// queryVectorRowsTx runs a KNN query, inside a transaction with
// hnsw.iterative_scan = strict_order when the extension supports it, so an
// index scan keeps producing rows past ef_search in exact distance order.
func queryVectorRowsTx(ctx context.Context, db *sql.DB, caps *pgCaps,
	scan func(*sql.Rows) error, query string, args ...any,
) error {
	if caps == nil || !caps.iterativeScan {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		if err := scan(rows); err != nil {
			return err
		}
		return rows.Err()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SET LOCAL hnsw.iterative_scan = strict_order`); err != nil {
		return fmt.Errorf("enable iterative scan: %w", err)
	}
	if err := scanTxRows(ctx, tx, scan, query, args...); err != nil {
		return err
	}
	return tx.Commit()
}

// scanTxRows runs query on tx and hands the rows to scan, closing them
// before the caller commits.
func scanTxRows(ctx context.Context, tx *sql.Tx, scan func(*sql.Rows) error, query string, args ...any) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if err := scan(rows); err != nil {
		return err
	}
	return rows.Err()
}

// collapseChunks keeps each object's first (closest) hit from a
// distance-ordered list, up to limit objects.
func collapseChunks(hits []storage.EmbeddingHit, limit int) []storage.EmbeddingHit {
	seen := make(map[string]bool, len(hits))
	out := make([]storage.EmbeddingHit, 0, min(limit, len(hits)))
	for _, h := range hits {
		if seen[h.ObjectID] {
			continue
		}
		seen[h.ObjectID] = true
		out = append(out, h)
		if len(out) == limit {
			break
		}
	}
	return out
}

// ListMissing pages object IDs with no row under modelID, ascending, after
// the cursor. limit <= 0 returns every remaining ID.
func (s *EmbeddingStore) ListMissing(ctx context.Context, modelID, afterObjectID string, limit int) ([]string, error) {
	var lim any // NULL: LIMIT NULL means no limit.
	if limit > 0 {
		lim = limit
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id FROM objects o
		 WHERE o.id > $1
		   AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.object_id = o.id AND e.model_id = $2)
		 ORDER BY o.id
		 LIMIT $3`, afterObjectID, modelID, lim)
	if err != nil {
		return nil, fmt.Errorf("postgres embeddings list missing %s: %w", modelID, err)
	}
	defer rows.Close()
	ids := make([]string, 0, min(max(limit, 0), 1024))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("postgres embeddings list missing %s: %w", modelID, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres embeddings list missing %s: %w", modelID, err)
	}
	return ids, nil
}

// PurgeModel drops the model's index and dimension CHECK, then deletes its
// rows and signature, in one transaction. The model ID is not validated:
// the identifiers are hash-derived and the deletes are parameterized, so a
// model skipped by EnsureIndex for an invalid ID can still be purged.
func (s *EmbeddingStore) PurgeModel(ctx context.Context, modelID string) error {
	if modelID == "" {
		return errors.New("postgres embeddings purge: empty model id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres embeddings purge %s: begin: %w", modelID, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := dropPgIndex(ctx, tx, pgIndexFor(modelID)); err != nil {
		return fmt.Errorf("postgres embeddings purge %s: %w", modelID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM embeddings WHERE model_id = $1`, modelID); err != nil {
		return fmt.Errorf("postgres embeddings purge %s: delete rows: %w", modelID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM index_signatures WHERE signature_id = $1`,
		indexsig.EmbeddingSignatureID(modelID)); err != nil {
		return fmt.Errorf("postgres embeddings purge %s: delete signature: %w", modelID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("postgres embeddings purge %s: commit: %w", modelID, err)
	}
	return nil
}

// ensureEmbeddingIndexes runs EnsureIndex for every registered model: the
// ADR-070 verify-and-rebuild pass that follows every Migrate. Models whose
// ID or dimension cannot be indexed, or whose stored rows disagree with the
// registry dimension, are skipped with a warning: opening the database must
// not fail over one model's index.
func ensureEmbeddingIndexes(ctx context.Context, d *Driver) error {
	specs, err := registeredEmbeddingSpecs(ctx, d.db)
	if err != nil {
		return err
	}

	store := &EmbeddingStore{db: d.db, caps: d.caps}
	for _, sp := range specs {
		if err := storage.ValidateEmbeddingModelID(sp.ModelID); err != nil {
			slog.Warn("embedding index skipped: invalid model id", "model_id", sp.ModelID, "error", err)
			continue
		}
		if err := store.EnsureIndex(ctx, sp); err != nil {
			if errors.Is(err, storage.ErrEmbeddingDimension) {
				slog.Warn("embedding index skipped: dimension", "model_id", sp.ModelID,
					"dimension", sp.Dimension, "error", err)
				continue
			}
			return fmt.Errorf("ensure embedding index %s: %w", sp.ModelID, err)
		}
	}
	return nil
}

// registeredEmbeddingSpecs reads every embedding_models row as an index spec.
func registeredEmbeddingSpecs(ctx context.Context, db *sql.DB) ([]storage.EmbeddingModelSpec, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT model_id, provider, dimension FROM embedding_models ORDER BY model_id`)
	if err != nil {
		return nil, fmt.Errorf("list embedding models: %w", err)
	}
	defer rows.Close()
	specs := make([]storage.EmbeddingModelSpec, 0, 4)
	for rows.Next() {
		var sp storage.EmbeddingModelSpec
		if err := rows.Scan(&sp.ModelID, &sp.Provider, &sp.Dimension); err != nil {
			return nil, fmt.Errorf("scan embedding model: %w", err)
		}
		specs = append(specs, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list embedding models: %w", err)
	}
	return specs, nil
}
