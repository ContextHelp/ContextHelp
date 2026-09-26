package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// EmbeddingStore is the SQLite storage.EmbeddingStore: canonical rows in the
// embeddings table plus one vec0 table per registered model (ADR-071,
// amendment 2026-09-26).
//
// Each model's vec0 table (vec_emb_<h>, rowid = embeddings.id) is derived
// data: per-model triggers on embeddings keep it in sync, and EnsureIndex
// rebuilds it from canonical rows whenever its ADR-070 signature drifts.
type EmbeddingStore struct {
	db *sql.DB
}

var _ storage.EmbeddingStore = (*EmbeddingStore)(nil)

// Embeddings returns the per-model embedding store.
func (d *Driver) Embeddings() storage.EmbeddingStore { return &EmbeddingStore{db: d.db} }

// vec0KMax is sqlite-vec's ceiling on k in a KNN query.
const vec0KMax = 4096

// vecIndex names the per-model schema objects EnsureIndex manages.
type vecIndex struct {
	table                     string
	insTrig, updTrig, delTrig string
}

func vecIndexFor(modelID string) vecIndex {
	t := "vec_" + storage.EmbeddingIndexName(modelID)
	return vecIndex{
		table:   t,
		insTrig: "trg_" + t + "_ins",
		updTrig: "trg_" + t + "_upd",
		delTrig: "trg_" + t + "_del",
	}
}

// Per-model DDL templates. {TABLE} and the trigger names are hash-derived
// identifiers; {MODEL} is a model_id that passed ValidateEmbeddingModelID,
// whose charset cannot close a SQL string literal.
const (
	vec0TableTmpl = `CREATE VIRTUAL TABLE {TABLE} USING vec0(embedding float[{DIM}] distance_metric=cosine)`
	vec0InsTmpl   = `CREATE TRIGGER {INS} AFTER INSERT ON embeddings WHEN new.model_id = '{MODEL}'
BEGIN INSERT INTO {TABLE}(rowid, embedding) VALUES (new.id, new.vector); END`
	vec0UpdTmpl = `CREATE TRIGGER {UPD} AFTER UPDATE OF vector ON embeddings WHEN new.model_id = '{MODEL}'
BEGIN DELETE FROM {TABLE} WHERE rowid = old.id;
      INSERT INTO {TABLE}(rowid, embedding) VALUES (new.id, new.vector); END`
	vec0DelTmpl = `CREATE TRIGGER {DEL} AFTER DELETE ON embeddings WHEN old.model_id = '{MODEL}'
BEGIN DELETE FROM {TABLE} WHERE rowid = old.id; END`
	vec0RefillTmpl = `INSERT INTO {TABLE}(rowid, embedding) SELECT id, vector FROM embeddings WHERE model_id = ?`
	vec0SearchTmpl = `SELECT e.object_id, e.chunk_idx, v.distance
  FROM {TABLE} v JOIN embeddings e ON e.id = v.rowid
 WHERE v.embedding MATCH ? AND k = ?
 ORDER BY v.distance, e.object_id, e.chunk_idx`
)

func (ix vecIndex) render(tmpl, modelID string, dim int) string {
	return strings.NewReplacer(
		"{TABLE}", ix.table,
		"{INS}", ix.insTrig,
		"{UPD}", ix.updTrig,
		"{DEL}", ix.delTrig,
		"{MODEL}", modelID,
		"{DIM}", strconv.Itoa(dim),
	).Replace(tmpl)
}

// desired returns the vec0 description and signature EnsureIndex converges
// the model's index to. Params is the table DDL, so any change to the
// index shape changes the stamp.
func (ix vecIndex) desired(spec storage.EmbeddingModelSpec) (indexsig.VectorIndexDescription, string, string) {
	desc := indexsig.VectorIndexDescription{
		Method:      "vec0",
		OpsClass:    "cosine",
		BuildParams: ix.render(vec0TableTmpl, spec.ModelID, spec.Dimension),
	}
	hash, summary := indexsig.ComputeEmbedding(
		spec.ModelID, spec.Provider, spec.Dimension, desc.Method, desc.OpsClass, desc.BuildParams,
	)
	return desc, hash, summary
}

// EnsureIndex converges the model's vec0 table, its sync triggers and its
// signature to the desired shape. A no-op when the stored signature, the
// live table DDL and all three triggers already match; otherwise the index
// is dropped, recreated and refilled from canonical rows in one
// transaction, and re-stamped in the same transaction.
func (s *EmbeddingStore) EnsureIndex(ctx context.Context, spec storage.EmbeddingModelSpec) error {
	if err := storage.ValidateEmbeddingModelID(spec.ModelID); err != nil {
		return err
	}
	if spec.Dimension <= 0 || spec.Dimension > storage.SQLiteVecMaxDimension {
		return fmt.Errorf("sqlite embeddings ensure index %s: dimension %d outside 1..%d: %w",
			spec.ModelID, spec.Dimension, storage.SQLiteVecMaxDimension, storage.ErrEmbeddingDimension)
	}
	ix := vecIndexFor(spec.ModelID)
	want, hash, summary := ix.desired(spec)
	sigID := indexsig.EmbeddingSignatureID(spec.ModelID)

	current, err := s.indexCurrent(ctx, ix, want, sigID, hash)
	if err != nil {
		return err
	}
	if current {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite embeddings ensure index %s: begin: %w", spec.ModelID, err)
	}
	defer func() { _ = tx.Rollback() }()

	// The first statement writes, so the transaction holds the write lock
	// before it reads anything (no deferred-lock upgrade failures when two
	// processes open the same database).
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM index_signatures WHERE signature_id = ?`, sigID); err != nil {
		return fmt.Errorf("sqlite embeddings ensure index %s: clear signature: %w", spec.ModelID, err)
	}
	var mismatched int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embeddings WHERE model_id = ? AND length(vector) <> ?`,
		spec.ModelID, 4*spec.Dimension).Scan(&mismatched); err != nil {
		return fmt.Errorf("sqlite embeddings ensure index %s: check stored dimensions: %w", spec.ModelID, err)
	}
	if mismatched > 0 {
		return fmt.Errorf("sqlite embeddings ensure index %s: %d stored rows are not %d-dimensional: %w",
			spec.ModelID, mismatched, spec.Dimension, storage.ErrEmbeddingDimension)
	}
	if err := dropVecIndex(ctx, tx, ix); err != nil {
		return fmt.Errorf("sqlite embeddings ensure index %s: %w", spec.ModelID, err)
	}
	for _, stmt := range []string{
		want.BuildParams,
		ix.render(vec0InsTmpl, spec.ModelID, spec.Dimension),
		ix.render(vec0UpdTmpl, spec.ModelID, spec.Dimension),
		ix.render(vec0DelTmpl, spec.ModelID, spec.Dimension),
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqlite embeddings ensure index %s: create: %w", spec.ModelID, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		ix.render(vec0RefillTmpl, spec.ModelID, spec.Dimension), spec.ModelID); err != nil {
		return fmt.Errorf("sqlite embeddings ensure index %s: refill: %w", spec.ModelID, err)
	}
	if err := indexsig.Upsert(ctx, tx, indexsig.DialectSQLite, sigID, hash, summary); err != nil {
		return fmt.Errorf("sqlite embeddings ensure index %s: %w", spec.ModelID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite embeddings ensure index %s: commit: %w", spec.ModelID, err)
	}
	return nil
}

// indexCurrent reports whether the model's index already matches the
// desired shape: stored signature, live vec0 DDL and all sync triggers.
func (s *EmbeddingStore) indexCurrent(ctx context.Context, ix vecIndex,
	want indexsig.VectorIndexDescription, sigID, hash string,
) (bool, error) {
	stored, err := indexsig.Load(ctx, s.db, indexsig.DialectSQLite, sigID)
	if err != nil {
		return false, err
	}
	if stored == nil || stored.SignatureHash != hash {
		return false, nil
	}
	live, err := indexsig.SQLiteVectorIndexFor(ctx, s.db, ix.table)
	if err != nil {
		return false, err
	}
	if live != want {
		return false, nil
	}
	return vecIndexComplete(ctx, s.db, ix)
}

// vecIndexComplete reports whether the vec0 table and all three sync
// triggers exist. Writing without the triggers would leave canonical rows
// the index never sees.
func vecIndexComplete(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, ix vecIndex,
) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		 WHERE (type = 'table'   AND name = ?)
		    OR (type = 'trigger' AND tbl_name = 'embeddings' AND name IN (?, ?, ?))`,
		ix.table, ix.insTrig, ix.updTrig, ix.delTrig).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", ix.table, err)
	}
	return n == 4, nil
}

// dropVecIndex removes the triggers first, then the vec0 table (which takes
// its shadow tables with it).
func dropVecIndex(ctx context.Context, tx *sql.Tx, ix vecIndex) error {
	for _, stmt := range []string{
		ix.render(`DROP TRIGGER IF EXISTS {INS}`, "", 0),
		ix.render(`DROP TRIGGER IF EXISTS {UPD}`, "", 0),
		ix.render(`DROP TRIGGER IF EXISTS {DEL}`, "", 0),
		ix.render(`DROP TABLE IF EXISTS {TABLE}`, "", 0),
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("drop %s: %w", ix.table, err)
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

// checkFinite rejects NaN and infinite components, which cosine distance
// cannot rank.
func checkFinite(v []float32) error {
	for i, f := range v {
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			return fmt.Errorf("component %d is not finite", i)
		}
	}
	return nil
}

// Put replaces objectID's rows for every model present in vectors. All
// checks (registry dimension, index presence, finite components) run before
// the transaction commits, so a rejected call leaves every model's rows as
// they were. Zero-magnitude vectors are skipped; a model whose vectors are
// all zero therefore ends with no rows.
func (s *EmbeddingStore) Put(ctx context.Context, objectID string, vectors []storage.ObjectVector) error {
	if objectID == "" {
		return errors.New("sqlite embeddings put: empty object id")
	}
	groups := groupByModel(vectors)
	if len(groups) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite embeddings put %s: begin: %w", objectID, err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, g := range groups {
		// Delete first: the first statement takes the write lock, and the
		// delete trigger clears the index entries of the replaced rows.
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM embeddings WHERE object_id = ? AND model_id = ?`,
			objectID, g.modelID); err != nil {
			return fmt.Errorf("sqlite embeddings put %s/%s: clear: %w", objectID, g.modelID, err)
		}
		dim, err := s.writableDimension(ctx, tx, g.modelID)
		if err != nil {
			return fmt.Errorf("sqlite embeddings put %s: %w", objectID, err)
		}
		for _, v := range g.vectors {
			if len(v.Vector) != dim {
				return fmt.Errorf("sqlite embeddings put %s/%s chunk %d: got %d dimensions, registry says %d: %w",
					objectID, g.modelID, v.ChunkIdx, len(v.Vector), dim, storage.ErrEmbeddingDimension)
			}
			if err := checkFinite(v.Vector); err != nil {
				return fmt.Errorf("sqlite embeddings put %s/%s chunk %d: %w", objectID, g.modelID, v.ChunkIdx, err)
			}
			if isZeroMagnitude(v.Vector) {
				continue
			}
			blob, err := sqlite_vec.SerializeFloat32(v.Vector)
			if err != nil {
				return fmt.Errorf("sqlite embeddings put %s/%s: encode: %w", objectID, g.modelID, err)
			}
			var text any
			if v.Text != "" {
				text = v.Text
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, text, created_at)
				VALUES (?, ?, ?, ?, ?, ?)`,
				objectID, g.modelID, v.ChunkIdx, blob, text, now); err != nil {
				return fmt.Errorf("sqlite embeddings put %s/%s chunk %d: %w", objectID, g.modelID, v.ChunkIdx, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite embeddings put %s: commit: %w", objectID, err)
	}
	return nil
}

// writableDimension returns the model's registry dimension after checking
// that its index (vec0 table plus sync triggers) exists.
func (s *EmbeddingStore) writableDimension(ctx context.Context, tx *sql.Tx, modelID string) (int, error) {
	dim, err := registryDimension(ctx, tx, modelID)
	if err != nil {
		return 0, err
	}
	ok, err := vecIndexComplete(ctx, tx, vecIndexFor(modelID))
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("model %s: %w", modelID, storage.ErrEmbeddingIndexMissing)
	}
	return dim, nil
}

// registryDimension reads embedding_models.dimension. An unregistered model
// has no index by definition.
func registryDimension(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, modelID string,
) (int, error) {
	var dim int
	err := q.QueryRowContext(ctx,
		`SELECT dimension FROM embedding_models WHERE model_id = ?`, modelID).Scan(&dim)
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
		SELECT chunk_idx, vector, text FROM embeddings
		 WHERE object_id = ? AND model_id = ?
		 ORDER BY chunk_idx`, objectID, modelID)
	if err != nil {
		return nil, fmt.Errorf("sqlite embeddings get %s/%s: %w", objectID, modelID, err)
	}
	defer rows.Close()
	out := make([]storage.ObjectVector, 0, 1) // single-chunk today
	for rows.Next() {
		var (
			v    storage.ObjectVector
			blob []byte
			text sql.NullString
		)
		if err := rows.Scan(&v.ChunkIdx, &blob, &text); err != nil {
			return nil, fmt.Errorf("sqlite embeddings get %s/%s: %w", objectID, modelID, err)
		}
		if len(blob)%4 != 0 {
			return nil, fmt.Errorf("sqlite embeddings get %s/%s chunk %d: vector blob of %d bytes is not float32",
				objectID, modelID, v.ChunkIdx, len(blob))
		}
		v.ModelID = modelID
		v.Vector = decodeFloat32(blob)
		v.Text = text.String
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite embeddings get %s/%s: %w", objectID, modelID, err)
	}
	return out, nil
}

// decodeFloat32 reverses sqlite_vec.SerializeFloat32 (little-endian float32).
func decodeFloat32(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

// Search runs a KNN query against the model's vec0 table and collapses
// chunks to the best one per object. k starts at TopK and grows until TopK
// distinct objects are found, the index is exhausted, or vec0's k ceiling
// is reached.
func (s *EmbeddingStore) Search(ctx context.Context, q storage.VectorQuery) ([]storage.EmbeddingHit, error) {
	topK := q.TopK
	if topK <= 0 {
		topK = 10
	}
	dim, err := registryDimension(ctx, s.db, q.ModelID)
	if err != nil {
		return nil, fmt.Errorf("sqlite embeddings search: %w", err)
	}
	ix := vecIndexFor(q.ModelID)
	live, err := indexsig.SQLiteVectorIndexFor(ctx, s.db, ix.table)
	if err != nil {
		return nil, fmt.Errorf("sqlite embeddings search: %w", err)
	}
	if !live.Exists() {
		return nil, fmt.Errorf("sqlite embeddings search: model %s: %w", q.ModelID, storage.ErrEmbeddingIndexMissing)
	}
	if len(q.Vector) != dim {
		return nil, fmt.Errorf("sqlite embeddings search %s: query has %d dimensions, registry says %d: %w",
			q.ModelID, len(q.Vector), dim, storage.ErrEmbeddingDimension)
	}
	// Cosine distance to a zero-magnitude query is undefined for every row.
	if isZeroMagnitude(q.Vector) {
		return nil, nil
	}
	blob, err := sqlite_vec.SerializeFloat32(q.Vector)
	if err != nil {
		return nil, fmt.Errorf("sqlite embeddings search: encode: %w", err)
	}
	query := ix.render(vec0SearchTmpl, q.ModelID, dim)

	for k := topK; ; k *= 4 {
		if k > vec0KMax {
			k = vec0KMax
		}
		hits, fetched, err := s.knn(ctx, query, blob, k)
		if err != nil {
			return nil, fmt.Errorf("sqlite embeddings search %s: %w", q.ModelID, err)
		}
		collapsed := collapseChunks(hits, topK)
		if len(collapsed) >= topK || fetched < k || k == vec0KMax {
			return collapsed, nil
		}
	}
}

func (s *EmbeddingStore) knn(ctx context.Context, query string, blob []byte, k int) ([]storage.EmbeddingHit, int, error) {
	rows, err := s.db.QueryContext(ctx, query, blob, k)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	hits := make([]storage.EmbeddingHit, 0, k)
	fetched := 0
	for rows.Next() {
		var (
			h    storage.EmbeddingHit
			dist sql.NullFloat64
		)
		if err := rows.Scan(&h.ObjectID, &h.ChunkIdx, &dist); err != nil {
			return nil, 0, err
		}
		fetched++
		// NULL distance: a zero-magnitude row written outside Put.
		if !dist.Valid {
			continue
		}
		h.Distance = dist.Float64
		hits = append(hits, h)
	}
	return hits, fetched, rows.Err()
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
	if limit <= 0 {
		limit = -1 // SQLite: a negative LIMIT means no limit.
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id FROM objects o
		 WHERE o.id > ?
		   AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.object_id = o.id AND e.model_id = ?)
		 ORDER BY o.id
		 LIMIT ?`, afterObjectID, modelID, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite embeddings list missing %s: %w", modelID, err)
	}
	defer rows.Close()
	ids := make([]string, 0, min(max(limit, 0), 1024))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("sqlite embeddings list missing %s: %w", modelID, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite embeddings list missing %s: %w", modelID, err)
	}
	return ids, nil
}

// PurgeModel drops the model's triggers and vec0 table, then deletes its
// rows and signature, in one transaction. The model ID is not validated:
// the index identifiers are hash-derived and the deletes are parameterized,
// so a model skipped by EnsureIndex for an invalid ID can still be purged.
func (s *EmbeddingStore) PurgeModel(ctx context.Context, modelID string) error {
	if modelID == "" {
		return errors.New("sqlite embeddings purge: empty model id")
	}
	ix := vecIndexFor(modelID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite embeddings purge %s: begin: %w", modelID, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := dropVecIndex(ctx, tx, ix); err != nil {
		return fmt.Errorf("sqlite embeddings purge %s: %w", modelID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM embeddings WHERE model_id = ?`, modelID); err != nil {
		return fmt.Errorf("sqlite embeddings purge %s: delete rows: %w", modelID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM index_signatures WHERE signature_id = ?`,
		indexsig.EmbeddingSignatureID(modelID)); err != nil {
		return fmt.Errorf("sqlite embeddings purge %s: delete signature: %w", modelID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite embeddings purge %s: commit: %w", modelID, err)
	}
	return nil
}

// ensureEmbeddingIndexes runs EnsureIndex for every registered model: the
// ADR-070 verify-and-rebuild pass that follows every Migrate. Models whose
// ID or dimension cannot be indexed, or whose stored rows disagree with
// the registry dimension, are skipped with a warning: opening the database
// must not fail over one model's index.
func ensureEmbeddingIndexes(ctx context.Context, d *Driver) error {
	specs, err := registeredEmbeddingSpecs(ctx, d.db)
	if err != nil {
		return err
	}

	store := &EmbeddingStore{db: d.db}
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
