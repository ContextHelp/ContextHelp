package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/search/ftsq"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	uri "hop.top/cite/scheme"
)

// errObjectNotFound is returned by scanObjectRow when no row matches.
// Wraps storage.ErrNotFound so callers outside this driver can
// recognise a missing row without matching on message text.
var errObjectNotFound = fmt.Errorf("object %w", storage.ErrNotFound)

func (s *ObjectStore) Create(ctx context.Context, obj *storage.KnowledgeObject) error {
	f, err := marshalObjectFields(obj)
	if err != nil {
		return fmt.Errorf("create object: %w", err)
	}
	graphJSON, err := marshalGraph(obj.Graph)
	if err != nil {
		return fmt.Errorf("create object graph: %w", err)
	}
	if obj.Status == "" {
		obj.Status = "active"
	}

	// An empty TextContent defaults to the body the embedding step used
	// (projection.BodyText), so stored text, the FTS projection and the
	// embedded text agree, as on SQLite.
	obj.TextContent = projection.BodyText(obj)

	// Derive FTS body from projection — single source of truth for indexed
	// text. The generated tsvector column tracks projected_fts_body, so no
	// manual index maintenance is needed beyond writing the body.
	projectedFTSBody := projection.ProjectIndex(obj).FTSBody
	if projectedFTSBody != "" {
		obj.FTSIndexed = true
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create object: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `INSERT INTO objects (
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, status, inbox_note,
		remind_at, reminded_at, graph_json, source_key, projected_fts_body
	) VALUES (
		$1, $2, $3, $4, $5, $6,
		$7, $8, $9, $10, $11,
		$12, $13, $14, $15,
		$16, $17, $18, $19, $20,
		$21, $22, $23, $24, $25,
		$26, $27, $28, $29, $30
	)`,
		obj.ID, obj.Type, obj.Subtype, obj.RawContent, obj.ContentType, obj.TextContent,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.CreatedAt.UTC(), obj.UpdatedAt.UTC(), obj.FTSIndexed, obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt, graphJSON, obj.SourceKey, projectedFTSBody,
	)
	if err != nil {
		return fmt.Errorf("create object: %w", err)
	}
	if err := upsertObjectNodesTx(ctx, tx, obj.ID, obj.Graph); err != nil {
		return fmt.Errorf("create object nodes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create object: commit: %w", err)
	}
	return nil
}

func (s *ObjectStore) Get(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	row := s.db.QueryRowContext(ctx, objectSelectCols+` FROM objects WHERE id = $1`, id)
	return scanObjectRow(row)
}

func (s *ObjectStore) GetByContentHash(ctx context.Context, hash string) (*storage.KnowledgeObject, error) {
	if hash == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, objectSelectCols+` FROM objects WHERE content_hash = $1 LIMIT 1`, hash)
	obj, err := scanObjectRow(row)
	if errors.Is(err, errObjectNotFound) {
		return nil, nil
	}
	return obj, err
}

func (s *ObjectStore) GetBySourceKey(ctx context.Context, key string) (*storage.KnowledgeObject, error) {
	if key == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, objectSelectCols+` FROM objects WHERE source_key = $1 LIMIT 1`, key)
	obj, err := scanObjectRow(row)
	if errors.Is(err, errObjectNotFound) {
		return nil, nil
	}
	return obj, err
}

func (s *ObjectStore) List(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	var conditions []string
	var args []any
	idx := 1

	statusVal := filter.Status
	if statusVal == "" {
		statusVal = "active"
	}
	if statusVal != "all" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", idx))
		args = append(args, statusVal)
		idx++
	}
	if filter.Type != "" {
		conditions = append(conditions, fmt.Sprintf("type = $%d", idx))
		args = append(args, filter.Type)
		idx++
	}
	if filter.Subtype != "" {
		conditions = append(conditions, fmt.Sprintf("subtype = $%d", idx))
		args = append(args, filter.Subtype)
		idx++
	}
	if filter.Pipeline != "" {
		conditions = append(conditions, fmt.Sprintf("pipeline = $%d", idx))
		args = append(args, filter.Pipeline)
		idx++
	}
	if filter.Tag != "" {
		conditions = append(conditions, fmt.Sprintf(`EXISTS (SELECT 1 FROM jsonb_array_elements(tags) AS t WHERE t->>'label' = $%d)`, idx))
		args = append(args, filter.Tag)
		idx++
	}
	if filter.Mention != "" {
		// mentions column stores a JSONB array of canonical ctxt:// URI strings.
		// Parse input to canonical URI to accept either @ns.slug or ctxt:// form.
		if u, ok := mentions.Parse(filter.Mention); ok {
			conditions = append(conditions, fmt.Sprintf(
				`EXISTS (SELECT 1 FROM jsonb_array_elements_text(mentions) AS m WHERE m = $%d)`, idx))
			args = append(args, u.String())
			idx++
		} else {
			conditions = append(conditions, "1 = 0")
		}
	}
	if filter.After != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", idx))
		args = append(args, filter.After.UTC())
		idx++
	}
	if filter.Before != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", idx))
		args = append(args, filter.Before.UTC())
		idx++
	}

	// Metadata facet filters (US-0407).
	mc, ma := metadataFacetConditionsPG(filter, &idx)
	conditions = append(conditions, mc...)
	args = append(args, ma...)

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM objects "+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count objects: %w", err)
	}

	// ORDER BY cannot take a bind parameter, so the column and direction
	// are resolved through a closed allowlist: the only reachable values
	// are the literals below. filter.Sort / filter.Dir are compared, never
	// interpolated, so caller input cannot reach the query text.
	sortCol := "created_at"
	if filter.Sort == "updated_at" {
		sortCol = "updated_at"
	}
	dir := "DESC"
	if filter.Dir == "asc" {
		dir = "ASC"
	}

	// #nosec G202 -- sortCol/dir are allowlisted literals (see above), not
	// caller-controlled strings; `where` holds only $N placeholders.
	query := objectSelectCols + ` FROM objects ` + where + ` ORDER BY ` + sortCol + ` ` + dir
	if filter.Limit > 0 {
		// #nosec G202 -- appends a generated "$N" placeholder; value in args.
		query += fmt.Sprintf(" LIMIT $%d", idx)
		args = append(args, filter.Limit)
		idx++
	}
	if filter.Offset > 0 {
		// #nosec G202 -- appends a generated "$N" placeholder; value in args.
		query += fmt.Sprintf(" OFFSET $%d", idx)
		args = append(args, filter.Offset)
		idx++
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list objects: %w", err)
	}
	defer rows.Close()

	var objects []*storage.KnowledgeObject
	for rows.Next() {
		obj, err := scanObjectRows(rows)
		if err != nil {
			return nil, 0, err
		}
		objects = append(objects, obj)
	}
	return objects, total, rows.Err()
}

func (s *ObjectStore) Update(ctx context.Context, obj *storage.KnowledgeObject) error {
	f, err := marshalObjectFields(obj)
	if err != nil {
		return fmt.Errorf("update object: %w", err)
	}
	graphJSON, err := marshalGraph(obj.Graph)
	if err != nil {
		return fmt.Errorf("update object graph: %w", err)
	}

	// Derive FTS body from projection — single source of truth for indexed
	// text; the generated tsvector column tracks the rewritten body.
	projectedFTSBody := projection.ProjectIndex(obj).FTSBody
	if projectedFTSBody != "" {
		obj.FTSIndexed = true
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update object: begin tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE objects SET
		type=$1, subtype=$2, raw_content=$3, content_type=$4, text_content=$5,
		metadata=$6, summaries=$7, sections=$8, tags=$9, mentions=$10,
		decisions=$11, tasks=$12, pipeline=$13, source=$14,
		registry_influences=$15, plugins=$16, content_hash=$17,
		reinforcement_count=$18, last_reinforced_at=$19,
		updated_at=$20, fts_indexed=$21, status=$22, inbox_note=$23,
		remind_at=$24, reminded_at=$25, graph_json=$26, projected_fts_body=$27
	WHERE id=$28`,
		obj.Type, obj.Subtype, obj.RawContent, obj.ContentType, obj.TextContent,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash,
		obj.ReinforcementCount, f.lastReinforcedAt,
		obj.UpdatedAt.UTC(), obj.FTSIndexed, obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt, graphJSON, projectedFTSBody,
		obj.ID,
	)
	if err != nil {
		return fmt.Errorf("update object: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s not found", obj.ID)
	}
	if err := upsertObjectNodesTx(ctx, tx, obj.ID, obj.Graph); err != nil {
		return fmt.Errorf("update object nodes: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update object: commit: %w", err)
	}
	return nil
}

func (s *ObjectStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM objects WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s not found", id)
	}
	return nil
}

func (s *ObjectStore) Reinforce(ctx context.Context, hash string, mergeData *storage.KnowledgeObject) (string, error) {
	if hash == "" {
		return "", fmt.Errorf("reinforce: empty content hash")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("reinforce: begin tx: %w", err)
	}
	defer tx.Rollback()

	var obj storage.KnowledgeObject
	var tagsJSON, mentionsJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT id, tags, mentions FROM objects WHERE content_hash = $1`, hash).
		Scan(&obj.ID, &tagsJSON, &mentionsJSON)
	if err != nil {
		return "", fmt.Errorf("reinforce: lookup: %w", err)
	}

	// Decode failures must not be swallowed here: the merged result is
	// written straight back below, so treating corrupt stored tags or
	// mentions as empty would silently overwrite them.
	if err := decodeJSONColumn(tagsJSON, "tags", &obj.Tags); err != nil {
		return "", fmt.Errorf("reinforce: %w", err)
	}
	var mentionStrs []string
	if err := decodeJSONColumn(mentionsJSON, "mentions", &mentionStrs); err != nil {
		return "", fmt.Errorf("reinforce: %w", err)
	}
	obj.Mentions = mentions.ParseSlice(mentionStrs)

	now := time.Now().UTC()
	merged := mergeTags(obj.Tags, mergeData.Tags)
	mergedTagsJSON, _ := json.Marshal(merged)
	mergedMentionStrs := mergeStrings(mentionsToStrings(obj.Mentions), mentionsToStrings(mergeData.Mentions))
	mergedMentionsJSON, _ := json.Marshal(mergedMentionStrs)

	// fts_indexed is left untouched: Reinforce never rewrites
	// projected_fts_body (the generated tsvector stays valid), and no
	// downstream re-indexer exists to flip the flag back. Clearing it here
	// misreported reinforced (deduplicated) objects as unindexed even
	// though FTS still matched.
	//
	// New content rewrites text_content with the same default Create
	// applies (projection.BodyText), keeping it in step with raw_content.
	if mergeData.RawContent != "" && mergeData.RawContent != hash {
		_, err = tx.ExecContext(ctx, `UPDATE objects SET
			reinforcement_count = reinforcement_count + 1,
			last_reinforced_at = $1,
			tags = $2,
			mentions = $3,
			updated_at = $4,
			raw_content = $5,
			text_content = $6
		WHERE content_hash = $7`,
			now, mergedTagsJSON, mergedMentionsJSON, now,
			mergeData.RawContent, projection.BodyText(mergeData), hash,
		)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE objects SET
			reinforcement_count = reinforcement_count + 1,
			last_reinforced_at = $1,
			tags = $2,
			mentions = $3,
			updated_at = $4
		WHERE content_hash = $5`,
			now, mergedTagsJSON, mergedMentionsJSON, now, hash,
		)
	}
	if err != nil {
		return "", fmt.Errorf("reinforce: update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("reinforce: commit: %w", err)
	}
	return obj.ID, nil
}

func (s *ObjectStore) ListBySQL(ctx context.Context, where string, args []any, limit, offset int) ([]*storage.KnowledgeObject, int, error) {
	countQuery := "SELECT COUNT(*) FROM objects"
	if where != "" {
		countQuery += " WHERE " + where
	}
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count by sql: %w", err)
	}

	// where is a compiled RSQL predicate (internal/search): literals are
	// already bound as $N placeholders in args, only operators and column
	// names reach the SQL text. LIMIT/OFFSET are bound below rather than
	// formatted in.
	// #nosec G202 -- see above; caller values travel in args, not the string.
	query := objectSelectCols + ` FROM objects`
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at DESC"
	pageArgs := append([]any(nil), args...)
	idx := len(pageArgs) + 1
	if limit > 0 {
		// #nosec G202 -- appends a generated "$N" placeholder; value in args.
		query += fmt.Sprintf(" LIMIT $%d", idx)
		pageArgs = append(pageArgs, limit)
		idx++
	}
	if offset > 0 {
		// #nosec G202 -- appends a generated "$N" placeholder; value in args.
		query += fmt.Sprintf(" OFFSET $%d", idx)
		pageArgs = append(pageArgs, offset)
	}

	rows, err := s.db.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list by sql: %w", err)
	}
	defer rows.Close()

	var objects []*storage.KnowledgeObject
	for rows.Next() {
		obj, err := scanObjectRows(rows)
		if err != nil {
			return nil, 0, err
		}
		objects = append(objects, obj)
	}
	return objects, total, rows.Err()
}

// VectorSearch ranks objects through q.ModelID's per-model partial HNSW
// index, joining the object filters in SQL. The model_id predicate and the
// vector(<dim>) cast are literals so the planner matches the partial
// expression index (a bound model_id falls back to a bitmap scan under a
// generic plan); model_id is safe to inline because the registry restricts
// its charset (storage.ValidateEmbeddingModelID). Rows are chunks, so the
// query over-fetches and keeps each object's closest chunk.
// Metadata["score"] is 1 - cosine distance.
func (s *ObjectStore) VectorSearch(ctx context.Context, q storage.VectorQuery, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	if len(q.Vector) == 0 {
		return nil, fmt.Errorf("vector search: empty query vector")
	}
	// Cosine distance to a zero-magnitude query is undefined for every
	// row: nothing can rank, so nothing is returned.
	if isZeroVector(q.Vector) {
		return nil, nil
	}
	if err := storage.ValidateEmbeddingModelID(q.ModelID); err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	dim, err := s.indexedDimension(ctx, q.ModelID)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	if len(q.Vector) != dim {
		return nil, fmt.Errorf("vector search: model %s: query has %d dimensions, index has %d: %w",
			q.ModelID, len(q.Vector), dim, storage.ErrEmbeddingDimension)
	}

	// $1 is the query vector (referenced by the score and the ORDER BY);
	// filter conditions number themselves from $2.
	conditions := []string{fmt.Sprintf("e.model_id = '%s'", q.ModelID)}
	args := []any{encodePgVector(q.Vector)}
	idx := 2

	if filter.Type != "" {
		conditions = append(conditions, fmt.Sprintf("type = $%d", idx))
		args = append(args, filter.Type)
		idx++
	}
	if filter.Subtype != "" {
		conditions = append(conditions, fmt.Sprintf("subtype = $%d", idx))
		args = append(args, filter.Subtype)
		idx++
	}
	if filter.Pipeline != "" {
		conditions = append(conditions, fmt.Sprintf("pipeline = $%d", idx))
		args = append(args, filter.Pipeline)
		idx++
	}

	// Metadata facet filters (US-0407).
	mc, ma := metadataFacetConditionsPG(filter, &idx)
	conditions = append(conditions, mc...)
	args = append(args, ma...)

	limit := filter.Limit
	if limit <= 0 {
		limit = q.TopK
	}
	if limit <= 0 {
		limit = 20
	}

	// The embeddings subquery exposes only object_id, model_id and vector,
	// so the bare object column names in objectSelectCols and the facet
	// conditions stay unambiguous; the planner flattens it into a join.
	// Interpolated: the validated model_id, the integer dimension, the
	// condition list (literals and generated $N markers) and $N indexes.
	dist := fmt.Sprintf("e.vector::vector(%d) <=> $1::vector(%d)", dim, dim)
	// #nosec G201 G202 -- no caller string reaches the query text; values are in args.
	query := objectSelectCols + fmt.Sprintf(
		`, 1 - (%s) AS score
		  FROM objects
		  JOIN (SELECT object_id, model_id, vector FROM embeddings) e ON e.object_id = objects.id
		 WHERE %s
		 ORDER BY %s
		 LIMIT $%d`,
		dist, strings.Join(conditions, " AND "), dist, idx)
	args = append(args, limit*pgChunkOverfetch)

	seen := map[string]bool{}
	var objects []*storage.KnowledgeObject
	err = queryVectorRows(ctx, s.db, s.caps, func(rows *sql.Rows) error {
		for rows.Next() {
			obj, score, err := scanObjectRowWithScore(rows)
			if err != nil {
				return err
			}
			if seen[obj.ID] || len(objects) >= limit {
				continue
			}
			seen[obj.ID] = true
			if obj.Metadata == nil {
				obj.Metadata = make(map[string]any)
			}
			obj.Metadata["score"] = score
			objects = append(objects, obj)
		}
		return nil
	}, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	return objects, nil
}

// pgChunkOverfetch bounds how many chunk rows VectorSearch reads per result
// slot so that collapsing chunks to one hit per object can still fill the
// limit.
const pgChunkOverfetch = 4

// indexedDimension returns the registry dimension of modelID when its
// per-model index exists, and ErrEmbeddingIndexMissing otherwise.
func (s *ObjectStore) indexedDimension(ctx context.Context, modelID string) (int, error) {
	var (
		dim     int
		indexed bool
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT dimension, to_regclass($2) IS NOT NULL
		  FROM embedding_models
		 WHERE model_id = $1`,
		modelID, "idx_"+storage.EmbeddingIndexName(modelID),
	).Scan(&dim, &indexed)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (!indexed || dim <= 0)) {
		return 0, fmt.Errorf("model %s: %w", modelID, storage.ErrEmbeddingIndexMissing)
	}
	if err != nil {
		return 0, fmt.Errorf("model %s index lookup: %w", modelID, err)
	}
	return dim, nil
}

// FTSSearch runs full-text search over the generated tsvector column.
// websearch_to_tsquery neutralizes hostile query syntax by design; ts_rank_cd
// orders best-first (DESC), matching the rank semantics of the SQLite bm25
// leg (which orders ascending because bm25 is smaller-is-better). RRF
// upstream consumes rank order only — score parity with bm25 is explicitly
// not the contract. The score lands in Metadata["fts_score"], same key as
// SQLite.
func (s *ObjectStore) FTSSearch(ctx context.Context, query string, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	if query == "" {
		return nil, fmt.Errorf("fts search: empty query")
	}

	// The driver owns its dialect's quoting: raw user text arrives here and
	// is reduced to bare AND'd terms for websearch_to_tsquery (raw
	// hyphenated input would parse as a strict <-> phrase — semantic drift
	// from the SQLite leg). Input with no usable tokens matches nothing.
	query = ftsq.ForPostgres(query)
	if query == "" {
		return nil, nil
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	// $1 is the raw query text; the tsquery is computed once in the FROM
	// clause and shared by the match predicate and the rank expression.
	// Filter placeholders number themselves from $2, mirroring the SQLite
	// filter shape (type + metadata facets).
	conditions := []string{"fts @@ q"}
	args := []any{query}
	idx := 2

	if filter.Type != "" {
		conditions = append(conditions, fmt.Sprintf("type = $%d", idx))
		args = append(args, filter.Type)
		idx++
	}

	// Metadata facet filters (US-0407).
	mc, ma := metadataFacetConditionsPG(filter, &idx)
	conditions = append(conditions, mc...)
	args = append(args, ma...)

	// #nosec G202 -- no user data reaches the SQL text. Every caller
	// value travels as a $N placeholder; the three interpolated items
	// are the compile-time const ftsRegconfig, a condition list built
	// only from literals and generated $N markers, and an int limit.
	q := objectSelectCols + fmt.Sprintf(`, ts_rank_cd(fts, q) AS score
		FROM objects, websearch_to_tsquery('%s', $1) AS q
		WHERE %s
		ORDER BY score DESC LIMIT %d`,
		ftsRegconfig, strings.Join(conditions, " AND "), limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close()

	var results []*storage.KnowledgeObject
	for rows.Next() {
		obj, score, err := scanObjectRowWithScore(rows)
		if err != nil {
			return nil, fmt.Errorf("fts search scan: %w", err)
		}
		if obj.Metadata == nil {
			obj.Metadata = make(map[string]any)
		}
		obj.Metadata["fts_score"] = score
		results = append(results, obj)
	}
	return results, rows.Err()
}

// nodeTypeObjectIDs returns object IDs that contain at least one node of each
// requested type. When nodeTypes is empty, nil is returned (no pre-filter).
func (s *ObjectStore) nodeTypeObjectIDs(ctx context.Context, nodeTypes []string) (map[string]struct{}, error) {
	if len(nodeTypes) == 0 {
		return nil, nil
	}
	// Only objects that have ALL requested node types are returned.
	placeholders := make([]string, len(nodeTypes))
	args := make([]any, len(nodeTypes))
	for i, t := range nodeTypes {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = t
	}
	// #nosec G201 -- the interpolated values are generated $N markers
	// and a count, never caller input: nodeTypes travels in args.
	q := fmt.Sprintf(
		`SELECT object_id FROM object_nodes WHERE node_type IN (%s)
		 GROUP BY object_id HAVING COUNT(DISTINCT node_type) = $%d`,
		strings.Join(placeholders, ", "), len(nodeTypes)+1,
	)
	args = append(args, len(nodeTypes))

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("node type object IDs: %w", err)
	}
	defer rows.Close()

	ids := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("node type object IDs scan: %w", err)
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}

// FTSSearchNodeAware runs FTS search with an optional NodeAwareFilter.
// When naf.NodeTypes is set, only objects containing ALL those node types are
// returned. Results include a populated DocumentView when naf.ReturnNodeHits
// is true. Node-type pre-filtering happens in-process after the query so
// relevance ordering is preserved, mirroring the SQLite shape.
func (s *ObjectStore) FTSSearchNodeAware(
	ctx context.Context,
	query string,
	filter storage.ObjectFilter,
	naf pluginapi.NodeAwareFilter,
) ([]*pluginapi.NodeAwareResult, error) {
	objects, err := s.FTSSearch(ctx, query, filter)
	if err != nil {
		return nil, err
	}

	allowedIDs, err := s.nodeTypeObjectIDs(ctx, naf.NodeTypes)
	if err != nil {
		return nil, err
	}

	var out []*pluginapi.NodeAwareResult
	for _, obj := range objects {
		if allowedIDs != nil {
			if _, ok := allowedIDs[obj.ID]; !ok {
				continue
			}
		}
		r := &pluginapi.NodeAwareResult{Object: obj}
		if naf.ReturnNodeHits {
			dv := projection.ProjectDocument(obj)
			r.DocumentView = &dv
		}
		out = append(out, r)
	}
	return out, nil
}

// VectorSearchNodeAware runs vector search with an optional NodeAwareFilter.
// When naf.NodeTypes is set, only objects containing ALL those node types are
// returned. Results include a populated DocumentView when naf.ReturnNodeHits
// is true. Node-type pre-filtering happens in-process after the query so
// distance ordering is preserved, mirroring the SQLite shape.
func (s *ObjectStore) VectorSearchNodeAware(
	ctx context.Context,
	q storage.VectorQuery,
	filter storage.ObjectFilter,
	naf pluginapi.NodeAwareFilter,
) ([]*pluginapi.NodeAwareResult, error) {
	objects, err := s.VectorSearch(ctx, q, filter)
	if err != nil {
		return nil, err
	}

	allowedIDs, err := s.nodeTypeObjectIDs(ctx, naf.NodeTypes)
	if err != nil {
		return nil, err
	}

	var out []*pluginapi.NodeAwareResult
	for _, obj := range objects {
		if allowedIDs != nil {
			if _, ok := allowedIDs[obj.ID]; !ok {
				continue
			}
		}
		r := &pluginapi.NodeAwareResult{Object: obj}
		if naf.ReturnNodeHits {
			dv := projection.ProjectDocument(obj)
			r.DocumentView = &dv
		}
		out = append(out, r)
	}
	return out, nil
}

// queryVectorRows executes a KNN query and hands the rows to scan. When the
// extension supports iterative index scans (pgvector >= 0.8.0), the query
// runs in a transaction with hnsw.iterative_scan = strict_order: pgvector
// applies WHERE after HNSW traversal, so without iterative scans a filtered
// KNN can under-return below LIMIT even when qualifying neighbors exist.
// strict_order keeps exact distance ordering, preserving the cross-driver
// rank contract.
func queryVectorRows(ctx context.Context, db *sql.DB, caps *pgCaps,
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
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SET LOCAL hnsw.iterative_scan = strict_order`); err != nil {
		return fmt.Errorf("enable iterative scan: %w", err)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if err := scan(rows); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	return tx.Commit()
}

// objectSelectCols is the SELECT column list (no trailing FROM).
const objectSelectCols = `SELECT
	id, type, subtype, raw_content, content_type, text_content,
	metadata, summaries, sections, tags, mentions,
	decisions, tasks, pipeline, source,
	registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
	created_at, updated_at, fts_indexed, status, inbox_note,
	remind_at, reminded_at, graph_json, source_key`

func scanObjectRow(row *sql.Row) (*storage.KnowledgeObject, error) {
	var obj storage.KnowledgeObject
	var (
		metadataJSON, summariesJSON, sectionsJSON, tagsJSON []byte
		mentionsJSON, decisionsJSON, tasksJSON              []byte
		influencesJSON, pluginsJSON                         []byte
		lastReinforcedAt, remindAt, remindedAt              sql.NullTime
		graphJSON                                           []byte
		sourceKey                                           sql.NullString
	)
	err := row.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&obj.CreatedAt, &obj.UpdatedAt, &obj.FTSIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &graphJSON, &sourceKey,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errObjectNotFound
		}
		return nil, fmt.Errorf("scan object: %w", err)
	}
	if err := unmarshalObjectFields(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		lastReinforcedAt, remindAt, remindedAt); err != nil {
		return nil, fmt.Errorf("scan object: %w", err)
	}
	if len(graphJSON) > 0 {
		g, err := unmarshalGraph(string(graphJSON))
		if err != nil {
			return nil, fmt.Errorf("unmarshal graph: %w", err)
		}
		obj.Graph = g
	}
	if sourceKey.Valid {
		obj.SourceKey = sourceKey.String
	}
	return &obj, nil
}

func scanObjectRows(rows *sql.Rows) (*storage.KnowledgeObject, error) {
	var obj storage.KnowledgeObject
	var (
		metadataJSON, summariesJSON, sectionsJSON, tagsJSON []byte
		mentionsJSON, decisionsJSON, tasksJSON              []byte
		influencesJSON, pluginsJSON                         []byte
		lastReinforcedAt, remindAt, remindedAt              sql.NullTime
		graphJSON                                           []byte
		sourceKey                                           sql.NullString
	)
	err := rows.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&obj.CreatedAt, &obj.UpdatedAt, &obj.FTSIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &graphJSON, &sourceKey,
	)
	if err != nil {
		return nil, fmt.Errorf("scan object row: %w", err)
	}
	if err := unmarshalObjectFields(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		lastReinforcedAt, remindAt, remindedAt); err != nil {
		return nil, fmt.Errorf("scan object row: %w", err)
	}
	if len(graphJSON) > 0 {
		g, err := unmarshalGraph(string(graphJSON))
		if err != nil {
			return nil, fmt.Errorf("unmarshal graph: %w", err)
		}
		obj.Graph = g
	}
	if sourceKey.Valid {
		obj.SourceKey = sourceKey.String
	}
	return &obj, nil
}

// scanObjectRowWithScore scans base cols + a float64 score appended at the end.
func scanObjectRowWithScore(rows *sql.Rows) (*storage.KnowledgeObject, float64, error) {
	var obj storage.KnowledgeObject
	var (
		metadataJSON, summariesJSON, sectionsJSON, tagsJSON []byte
		mentionsJSON, decisionsJSON, tasksJSON              []byte
		influencesJSON, pluginsJSON                         []byte
		lastReinforcedAt, remindAt, remindedAt              sql.NullTime
		graphJSON                                           []byte
		sourceKey                                           sql.NullString
		score                                               float64
	)
	err := rows.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&obj.CreatedAt, &obj.UpdatedAt, &obj.FTSIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &graphJSON, &sourceKey,
		&score,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("scan object+score: %w", err)
	}
	if err := unmarshalObjectFields(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		lastReinforcedAt, remindAt, remindedAt); err != nil {
		return nil, 0, fmt.Errorf("scan object+score: %w", err)
	}
	if len(graphJSON) > 0 {
		g, err := unmarshalGraph(string(graphJSON))
		if err != nil {
			return nil, 0, fmt.Errorf("unmarshal graph: %w", err)
		}
		obj.Graph = g
	}
	if sourceKey.Valid {
		obj.SourceKey = sourceKey.String
	}
	return &obj, score, nil
}

func unmarshalObjectFields(obj *storage.KnowledgeObject,
	metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
	mentionsJSON, decisionsJSON, tasksJSON,
	influencesJSON, pluginsJSON []byte,
	lastReinforcedAt, remindAt, remindedAt sql.NullTime,
) error {
	for _, col := range []struct {
		raw  []byte
		name string
		dst  any
	}{
		{metadataJSON, "metadata", &obj.Metadata},
		{summariesJSON, "summaries", &obj.Summaries},
		{sectionsJSON, "sections", &obj.Sections},
		{tagsJSON, "tags", &obj.Tags},
		{decisionsJSON, "decisions", &obj.Decisions},
		{tasksJSON, "tasks", &obj.Tasks},
		{influencesJSON, "registry_influences", &obj.RegistryInfluences},
		{pluginsJSON, "plugins", &obj.Plugins},
	} {
		if err := decodeJSONColumn(col.raw, col.name, col.dst); err != nil {
			return err
		}
	}
	var mentionStrs []string
	if err := decodeJSONColumn(mentionsJSON, "mentions", &mentionStrs); err != nil {
		return err
	}
	obj.Mentions = mentions.ParseSlice(mentionStrs)
	if lastReinforcedAt.Valid {
		t := lastReinforcedAt.Time
		obj.LastReinforcedAt = &t
	}
	if remindAt.Valid {
		t := remindAt.Time
		obj.RemindAt = &t
	}
	if remindedAt.Valid {
		t := remindedAt.Time
		obj.RemindedAt = &t
	}
	return nil
}

type objectFields struct {
	metadata, summaries, sections, tags    string
	mentions, decisions, tasks             string
	influences, plugins                    string
	lastReinforcedAt, remindAt, remindedAt sql.NullTime
}

func marshalObjectFields(obj *storage.KnowledgeObject) (objectFields, error) {
	var f objectFields
	var firstErr error

	// Normalize nil slices/maps to empty JSON arrays/objects to avoid JSONB null.
	metadata := obj.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	plugins := obj.Plugins
	if plugins == nil {
		plugins = map[string]any{}
	}
	summaries := obj.Summaries
	if summaries == nil {
		summaries = []string{}
	}
	sections := obj.Sections
	if sections == nil {
		sections = []storage.Section{}
	}
	tags := obj.Tags
	if tags == nil {
		tags = []storage.Tag{}
	}
	mentions := mentionsToStrings(obj.Mentions)
	if mentions == nil {
		mentions = []string{}
	}
	decisions := obj.Decisions
	if decisions == nil {
		decisions = []storage.Decision{}
	}
	tasks := obj.Tasks
	if tasks == nil {
		tasks = []storage.Task{}
	}
	influences := obj.RegistryInfluences
	if influences == nil {
		influences = []string{}
	}

	marshal := func(name string, v any) string {
		if firstErr != nil {
			return ""
		}
		b, e := json.Marshal(v)
		if e != nil {
			firstErr = fmt.Errorf("marshal %s: %w", name, e)
			return ""
		}
		return string(b)
	}
	f.metadata = marshal("metadata", metadata)
	f.summaries = marshal("summaries", summaries)
	f.sections = marshal("sections", sections)
	f.tags = marshal("tags", tags)
	f.mentions = marshal("mentions", mentions)
	f.decisions = marshal("decisions", decisions)
	f.tasks = marshal("tasks", tasks)
	f.influences = marshal("influences", influences)
	f.plugins = marshal("plugins", plugins)
	if obj.LastReinforcedAt != nil {
		f.lastReinforcedAt = sql.NullTime{Time: *obj.LastReinforcedAt, Valid: true}
	}
	if obj.RemindAt != nil {
		f.remindAt = sql.NullTime{Time: *obj.RemindAt, Valid: true}
	}
	if obj.RemindedAt != nil {
		f.remindedAt = sql.NullTime{Time: *obj.RemindedAt, Valid: true}
	}
	return f, firstErr
}

// encodePgVector encodes a float32 slice into pgvector literal format "[v1,v2,...]".
func encodePgVector(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", f)
	}
	b.WriteByte(']')
	return b.String()
}

func mergeTags(existing, newTags []storage.Tag) []storage.Tag {
	seen := make(map[string]bool)
	result := make([]storage.Tag, 0, len(existing)+len(newTags))
	for _, t := range existing {
		seen[t.Label] = true
		result = append(result, t)
	}
	for _, t := range newTags {
		if !seen[t.Label] {
			result = append(result, t)
		}
	}
	return result
}

func mergeStrings(existing, newSlice []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(existing)+len(newSlice))
	for _, s := range existing {
		seen[s] = true
		result = append(result, s)
	}
	for _, s := range newSlice {
		if !seen[s] {
			result = append(result, s)
		}
	}
	return result
}

func mentionsToStrings(uris []uri.URI) []string {
	s := make([]string, len(uris))
	for i, u := range uris {
		s[i] = u.String()
	}
	return s
}

// marshalGraph serialises a graph to JSON for storage. Returns nil for nil graphs.
func marshalGraph(g *storage.ObjectGraph) ([]byte, error) {
	if g == nil {
		return nil, nil
	}
	b, err := json.Marshal(g)
	if err != nil {
		return nil, fmt.Errorf("marshal graph: %w", err)
	}
	return b, nil
}

// unmarshalGraph deserialises a JSON string into an ObjectGraph.
// decodeJSONColumn decodes a JSONB column read back out of our own database.
//
// An empty payload is a legitimately absent column: the JSONB columns carry a
// DEFAULT but no NOT NULL, so a NULL (scanned into []byte as nil) or a row
// written before the column existed reads as empty. Those keep the field's
// zero value, exactly as before.
//
// Anything else that fails to decode is corrupt stored data. That is returned
// so the caller can surface it instead of silently yielding an empty field.
func decodeJSONColumn(raw []byte, column string, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("unmarshal %s: %w", column, err)
	}
	return nil
}

func unmarshalGraph(raw string) (*storage.ObjectGraph, error) {
	if raw == "" || raw == "{}" {
		return &storage.ObjectGraph{}, nil
	}
	var g storage.ObjectGraph
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, fmt.Errorf("unmarshal graph: %w", err)
	}
	return &g, nil
}

// upsertObjectNodesTx replaces object_nodes rows for the given object within tx.
func upsertObjectNodesTx(ctx context.Context, tx *sql.Tx,
	objectID string, g *storage.ObjectGraph,
) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM object_nodes WHERE object_id = $1`, objectID); err != nil {
		return fmt.Errorf("delete object_nodes: %w", err)
	}
	if g == nil {
		return nil
	}
	for _, n := range g.Nodes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO object_nodes (id, object_id, node_type, ordinal, content, created_at)
			 VALUES ($1, $2, $3, $4, $5, NOW())`,
			n.ID, objectID, n.NodeType, n.Order, n.Content,
		); err != nil {
			return fmt.Errorf("insert object_node %s: %w", n.ID, err)
		}
	}
	return nil
}

// metadataFacetConditionsPG builds WHERE clauses for metadata facet filters
// using Postgres JSONB operators. idx is the current positional param counter.
func metadataFacetConditionsPG(f storage.ObjectFilter, idx *int) ([]string, []any) {
	var conds []string
	var args []any

	if f.MetadataType != "" {
		conds = append(conds, fmt.Sprintf("metadata->>'type' = $%d", *idx))
		args = append(args, f.MetadataType)
		*idx++
	}
	if f.SourceType != "" {
		conds = append(conds, fmt.Sprintf("metadata->>'source_type' = $%d", *idx))
		args = append(args, f.SourceType)
		*idx++
	}
	if f.MetadataTopic != "" {
		conds = append(conds, fmt.Sprintf("metadata->'topics' ? $%d", *idx))
		args = append(args, f.MetadataTopic)
		*idx++
	}
	if f.MetadataPerson != "" {
		conds = append(conds, fmt.Sprintf("metadata->'people' ? $%d", *idx))
		args = append(args, f.MetadataPerson)
		*idx++
	}
	if f.MetadataSince != nil {
		conds = append(conds, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM jsonb_array_elements_text(metadata->'dates_mentioned') d WHERE d.value >= $%d)", *idx))
		args = append(args, f.MetadataSince.Format("2006-01-02"))
		*idx++
	}
	if f.MetadataUntil != nil {
		conds = append(conds, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM jsonb_array_elements_text(metadata->'dates_mentioned') d WHERE d.value <= $%d)", *idx))
		args = append(args, f.MetadataUntil.Format("2006-01-02"))
		*idx++
	}
	return conds, args
}

// isZeroVector reports whether every component is zero.
func isZeroVector(v []float32) bool {
	for _, f := range v {
		if f != 0 {
			return false
		}
	}
	return true
}
