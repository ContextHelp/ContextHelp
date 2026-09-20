package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/search/ftsq"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	uri "hop.top/cite/scheme"
)

// errObjectNotFound is returned by scanObject when no row matches.
// Wraps storage.ErrNotFound so callers outside this driver can
// recognise a missing row without matching on message text.
var errObjectNotFound = fmt.Errorf("object %w", storage.ErrNotFound)

type ObjectStore struct {
	db     *sql.DB
	vec    *VecStore // optional ANN index; nil = brute-force fallback
	vecDim int       // configured ANN dimension; 0 means unknown/disabled
}

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

	// For text pipeline objects, populate TextContent from RawContent when empty.
	if obj.TextContent == "" && obj.RawContent != "" {
		obj.TextContent = obj.RawContent
	}

	// Derive FTS body from projection — single source of truth for indexed text.
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
		decisions, tasks, embeddings, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, projected_fts_body, source_key
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		obj.ID, obj.Type, obj.Subtype, obj.RawContent, obj.ContentType, obj.TextContent,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, nil, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.CreatedAt.Format(time.RFC3339), obj.UpdatedAt.Format(time.RFC3339),
		boolToInt(obj.FTSIndexed), boolToInt(obj.VectorIndexed), obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt, obj.ProfileID, graphJSON, projectedFTSBody, obj.SourceKey,
	)
	if err != nil {
		return fmt.Errorf("create object: %w", err)
	}
	if err := s.upsertObjectNodesTx(ctx, tx, obj.ID, obj.Graph); err != nil {
		return fmt.Errorf("create object nodes: %w", err)
	}
	if projectedFTSBody != "" {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO objects_fts(rowid, id, projected_fts_body) VALUES ((SELECT rowid FROM objects WHERE id = ?), ?, ?)`,
			obj.ID, obj.ID, projectedFTSBody)
		if err != nil {
			return fmt.Errorf("create object fts index: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create object: commit: %w", err)
	}

	// Embeddings are best-effort: stored outside the core transaction.
	if err := s.upsertEmbedding(ctx, obj.ID, obj.Embeddings); err != nil {
		return fmt.Errorf("create object embedding: %w", err)
	}
	if s.vec != nil && s.vecDim > 0 && len(obj.Embeddings) == s.vecDim {
		if err := s.vec.Upsert(ctx, obj.ID, obj.Embeddings); err != nil {
			return fmt.Errorf("create object vec index: %w", err)
		}
	}
	return nil
}

func (s *ObjectStore) Get(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key
	FROM objects WHERE id = ?`, id)
	obj, err := scanObject(row)
	if err != nil {
		return nil, err
	}
	obj.AttachmentIDs, err = s.listAttachmentIDs(ctx, id)
	return obj, err
}

// listAttachmentIDs returns the IDs of all attachments for objectID.
func (s *ObjectStore) listAttachmentIDs(ctx context.Context, objectID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM attachments WHERE object_id = ? ORDER BY created_at ASC`, objectID)
	if err != nil {
		return nil, fmt.Errorf("list attachment ids: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *ObjectStore) GetByContentHash(ctx context.Context, hash string) (*storage.KnowledgeObject, error) {
	if hash == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key
	FROM objects WHERE content_hash = ? LIMIT 1`, hash)
	obj, err := scanObject(row)
	if errors.Is(err, errObjectNotFound) {
		return nil, nil
	}
	return obj, err
}

func (s *ObjectStore) GetBySourceKey(ctx context.Context, key string) (*storage.KnowledgeObject, error) {
	if key == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key
	FROM objects WHERE source_key = ? LIMIT 1`, key)
	obj, err := scanObject(row)
	if errors.Is(err, errObjectNotFound) {
		return nil, nil
	}
	return obj, err
}

func (s *ObjectStore) List(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	var conditions []string
	var args []any

	// Status filter: default to "active" to avoid breaking existing callers.
	statusVal := filter.Status
	if statusVal == "" {
		statusVal = "active"
	}
	if statusVal != "all" {
		conditions = append(conditions, "status = ?")
		args = append(args, statusVal)
	}

	if filter.Type != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, filter.Type)
	}
	if filter.Subtype != "" {
		conditions = append(conditions, "subtype = ?")
		args = append(args, filter.Subtype)
	}
	if filter.Pipeline != "" {
		conditions = append(conditions, "pipeline = ?")
		args = append(args, filter.Pipeline)
	}
	if filter.Tag != "" {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value->>'label' = ?)")
		args = append(args, filter.Tag)
	}
	if filter.Mention != "" {
		// mentions column stores a JSON array of canonical ctxt:// URI strings.
		// Accept either input form (@ns.slug or ctxt://entity/...) by parsing
		// to the canonical URI and matching against json_each.value.
		if u, ok := mentions.Parse(filter.Mention); ok {
			conditions = append(conditions, "EXISTS (SELECT 1 FROM json_each(mentions) WHERE json_each.value = ?)")
			args = append(args, u.String())
		} else {
			conditions = append(conditions, "1 = 0")
		}
	}
	if filter.ProfileID != "" {
		conditions = append(conditions, "profile_id = ?")
		args = append(args, filter.ProfileID)
	}
	if filter.After != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filter.After.Format(time.RFC3339))
	}
	if filter.Before != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, filter.Before.Format(time.RFC3339))
	}

	// Metadata facet filters (US-0407).
	mc, ma := metadataFacetConditionsSQLite(filter)
	conditions = append(conditions, mc...)
	args = append(args, ma...)

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total.
	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM objects "+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count objects: %w", err)
	}

	// Sort.
	sortCol := "created_at"
	if filter.Sort == "updated_at" {
		sortCol = "updated_at"
	}
	dir := "DESC"
	if filter.Dir == "asc" {
		dir = "ASC"
	}

	query := fmt.Sprintf(`SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key
	FROM objects %s ORDER BY %s %s`, where, sortCol, dir)

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list objects: %w", err)
	}
	defer rows.Close()

	var objects []*storage.KnowledgeObject
	for rows.Next() {
		obj, err := scanObjectFromRows(rows)
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

	// Derive FTS body from projection — single source of truth for indexed text.
	projectedFTSBody := projection.ProjectIndex(obj).FTSBody

	if projectedFTSBody != "" {
		obj.FTSIndexed = true
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update object: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Drop the old FTS entry while the content row still holds the old
	// body; the new body is indexed after the UPDATE below.
	if err := deleteObjectFTSTx(ctx, tx, obj.ID); err != nil {
		return fmt.Errorf("update object fts index: %w", err)
	}

	result, err := tx.ExecContext(ctx, `UPDATE objects SET
		type=?, subtype=?, raw_content=?, content_type=?, text_content=?,
		metadata=?, summaries=?, sections=?, tags=?, mentions=?,
		decisions=?, tasks=?, pipeline=?, source=?,
		registry_influences=?, plugins=?, content_hash=?, reinforcement_count=?, last_reinforced_at=?,
		updated_at=?, fts_indexed=?, vector_indexed=?, status=?, inbox_note=?,
		remind_at=?, reminded_at=?, profile_id=?, graph_json=?, projected_fts_body=?, source_key=?
	WHERE id=?`,
		obj.Type, obj.Subtype, obj.RawContent, obj.ContentType, obj.TextContent,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.UpdatedAt.Format(time.RFC3339),
		boolToInt(obj.FTSIndexed), boolToInt(obj.VectorIndexed), obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt, obj.ProfileID, graphJSON, projectedFTSBody, obj.SourceKey,
		obj.ID,
	)
	if err != nil {
		return fmt.Errorf("update object: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s not found", obj.ID)
	}
	if err := s.upsertObjectNodesTx(ctx, tx, obj.ID, obj.Graph); err != nil {
		return fmt.Errorf("update object nodes: %w", err)
	}
	if projectedFTSBody != "" {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO objects_fts(rowid, id, projected_fts_body) VALUES ((SELECT rowid FROM objects WHERE id = ?), ?, ?)`,
			obj.ID, obj.ID, projectedFTSBody)
		if err != nil {
			return fmt.Errorf("update object fts index: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update object: commit: %w", err)
	}

	// Embeddings are best-effort: stored outside the core transaction.
	if err := s.upsertEmbedding(ctx, obj.ID, obj.Embeddings); err != nil {
		return fmt.Errorf("update object embedding: %w", err)
	}
	if s.vec != nil && s.vecDim > 0 && len(obj.Embeddings) == s.vecDim {
		if err := s.vec.Upsert(ctx, obj.ID, obj.Embeddings); err != nil {
			return fmt.Errorf("update object vec index: %w", err)
		}
	}
	return nil
}

func (s *ObjectStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete object: begin tx: %w", err)
	}
	defer tx.Rollback()

	// FTS entry first: objects_fts has no FK cascade and FTS5 needs the
	// content row alive to compute the tokens it must drop.
	if err := deleteObjectFTSTx(ctx, tx, id); err != nil {
		return fmt.Errorf("delete object fts index: %w", err)
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM objects WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s not found", id)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete object: commit: %w", err)
	}
	return nil
}

// deleteObjectFTSTx removes the objects_fts entry for id.
//
// objects_fts is an external-content FTS5 table (content='objects'): a
// DELETE against it makes FTS5 re-read the CURRENT objects row to learn
// which tokens to remove from the index. It must therefore run before the
// objects row is deleted or its projected_fts_body rewritten, in the same
// transaction. Rows with an empty projected_fts_body were never indexed
// (Create/Update skip the FTS insert for them), so they are skipped here
// too: deleting a never-indexed rowid decrements FTS5's row/token totals
// below what the index holds and eventually fails with SQLITE_CORRUPT.
func deleteObjectFTSTx(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx,
		`DELETE FROM objects_fts WHERE rowid = (
			SELECT rowid FROM objects WHERE id = ? AND projected_fts_body != ''
		)`, id)
	return err
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

	// Read existing within the transaction to prevent concurrent merge races.
	var obj storage.KnowledgeObject
	var (
		tagsJSON, mentionsJSON string
		lastReinforcedAt       sql.NullString
	)
	err = tx.QueryRowContext(ctx, `SELECT id, tags, mentions, last_reinforced_at
		FROM objects WHERE content_hash = ?`, hash).Scan(
		&obj.ID, &tagsJSON, &mentionsJSON, &lastReinforcedAt,
	)
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

	now := time.Now().Format(time.RFC3339)

	merged := mergeTags(obj.Tags, mergeData.Tags)
	mergedTagsJSON, _ := json.Marshal(merged)

	mergedMentionStrs := mergeStrings(mentionsToStrings(obj.Mentions), mentionsToStrings(mergeData.Mentions))
	mergedMentionsJSON, _ := json.Marshal(mergedMentionStrs)

	// Optionally update content if we now have more data (e.g. from text to url fetch)
	contentUpdate := ""
	contentArgs := []any{now, string(mergedTagsJSON), string(mergedMentionsJSON), now}
	if mergeData.RawContent != "" && mergeData.RawContent != hash {
		tc := mergeData.TextContent
		if tc == "" {
			tc = mergeData.RawContent
		}
		contentUpdate = ", raw_content = ?, text_content = ?"
		contentArgs = append(contentArgs, mergeData.RawContent, tc)
	}
	contentArgs = append(contentArgs, hash)

	// fts_indexed / vector_indexed are left untouched: Reinforce never
	// removes the objects_fts row or the embedding written at Create/Update
	// time, and no downstream re-indexer exists to flip the flags back.
	// Clearing them here misreported reinforced (deduplicated) objects as
	// unindexed even though FTS still matched them.
	query := fmt.Sprintf(`
		UPDATE objects SET
			reinforcement_count = reinforcement_count + 1,
			last_reinforced_at = ?,
			tags = ?,
			mentions = ?,
			updated_at = ?
			%s
		WHERE content_hash = ?
	`, contentUpdate)

	_, err = tx.ExecContext(ctx, query, contentArgs...)
	if err != nil {
		return "", fmt.Errorf("reinforce: update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("reinforce: commit: %w", err)
	}

	return obj.ID, nil
}

func mergeTags(existing, new []storage.Tag) []storage.Tag {
	seen := make(map[string]bool)
	result := make([]storage.Tag, 0, len(existing)+len(new))

	for _, t := range existing {
		seen[t.Label] = true
		result = append(result, t)
	}
	for _, t := range new {
		if !seen[t.Label] {
			result = append(result, t)
		}
	}
	return result
}

func mergeStrings(existing, new []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(existing)+len(new))

	for _, s := range existing {
		seen[s] = true
		result = append(result, s)
	}
	for _, s := range new {
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

func (s *ObjectStore) ListBySQL(ctx context.Context, where string, args []any, limit, offset int) ([]*storage.KnowledgeObject, int, error) {
	countQuery := "SELECT COUNT(*) FROM objects"
	if where != "" {
		countQuery += " WHERE " + where
	}

	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count by sql: %w", err)
	}

	query := `SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key
	FROM objects`
	// where is a compiled RSQL predicate (internal/search): literals are
	// already bound as ? placeholders in args, only operators and column
	// names reach the SQL text. LIMIT/OFFSET are bound below.
	// #nosec G202 -- see above; caller values travel in args, not the string.
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at DESC"
	pageArgs := append([]any(nil), args...)
	if limit > 0 {
		query += " LIMIT ?"
		pageArgs = append(pageArgs, limit)
	}
	if offset > 0 {
		// SQLite requires LIMIT before OFFSET; supply an unbounded LIMIT
		// when the caller asked only for an offset.
		if limit <= 0 {
			query += " LIMIT -1"
		}
		query += " OFFSET ?"
		pageArgs = append(pageArgs, offset)
	}

	rows, err := s.db.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list by sql: %w", err)
	}
	defer rows.Close()

	var objects []*storage.KnowledgeObject
	for rows.Next() {
		obj, err := scanObjectFromRows(rows)
		if err != nil {
			return nil, 0, err
		}
		objects = append(objects, obj)
	}
	return objects, total, rows.Err()
}

// scanObject scans a single row into a KnowledgeObject.
func scanObject(row *sql.Row) (*storage.KnowledgeObject, error) {
	var obj storage.KnowledgeObject
	var (
		metadataJSON, summariesJSON, sectionsJSON, tagsJSON string
		mentionsJSON, decisionsJSON, tasksJSON              string
		influencesJSON, pluginsJSON                         string
		createdAt, updatedAt                                string
		ftsIndexed, vectorIndexed                           int
		lastReinforcedAt, remindAt, remindedAt              sql.NullString
		graphJSON                                           sql.NullString
		sourceKey                                           sql.NullString
	)

	err := row.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &obj.ProfileID, &graphJSON, &sourceKey,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errObjectNotFound
		}
		return nil, fmt.Errorf("scan object: %w", err)
	}

	if err := unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
		remindAt, remindedAt); err != nil {
		return nil, fmt.Errorf("scan object: %w", err)
	}
	if graphJSON.Valid {
		g, err := unmarshalGraph(graphJSON.String)
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

func scanObjectFromRows(rows *sql.Rows) (*storage.KnowledgeObject, error) {
	var obj storage.KnowledgeObject
	var (
		metadataJSON, summariesJSON, sectionsJSON, tagsJSON string
		mentionsJSON, decisionsJSON, tasksJSON              string
		influencesJSON, pluginsJSON                         string
		createdAt, updatedAt                                string
		ftsIndexed, vectorIndexed                           int
		lastReinforcedAt, remindAt, remindedAt              sql.NullString
		graphJSON                                           sql.NullString
		sourceKey                                           sql.NullString
	)

	err := rows.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &obj.ProfileID, &graphJSON, &sourceKey,
	)
	if err != nil {
		return nil, fmt.Errorf("scan object row: %w", err)
	}

	if err := unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
		remindAt, remindedAt); err != nil {
		return nil, fmt.Errorf("scan object row: %w", err)
	}
	if graphJSON.Valid {
		g, err := unmarshalGraph(graphJSON.String)
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

func unmarshalObjectJSON(obj *storage.KnowledgeObject,
	metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
	mentionsJSON, decisionsJSON, tasksJSON,
	influencesJSON, pluginsJSON,
	createdAt, updatedAt string,
	ftsIndexed, vectorIndexed int,
	lastReinforcedAt, remindAt, remindedAt sql.NullString,
) error {
	for _, col := range []struct {
		raw  string
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
	obj.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	obj.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	obj.FTSIndexed = ftsIndexed != 0
	obj.VectorIndexed = vectorIndexed != 0
	if lastReinforcedAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastReinforcedAt.String)
		obj.LastReinforcedAt = &t
	}
	if remindAt.Valid {
		t, _ := time.Parse(time.RFC3339, remindAt.String)
		obj.RemindAt = &t
	}
	if remindedAt.Valid {
		t, _ := time.Parse(time.RFC3339, remindedAt.String)
		obj.RemindedAt = &t
	}
	return nil
}

type objectFields struct {
	metadata, summaries, sections, tags    string
	mentions, decisions, tasks             string
	influences, plugins                    string
	lastReinforcedAt, remindAt, remindedAt sql.NullString
}

func marshalObjectFields(obj *storage.KnowledgeObject) (objectFields, error) {
	var f objectFields
	var err error
	marshal := func(name string, v any) string {
		if err != nil {
			return ""
		}
		b, e := json.Marshal(v)
		if e != nil {
			err = fmt.Errorf("marshal %s: %w", name, e)
			return ""
		}
		return string(b)
	}

	f.metadata = marshal("metadata", obj.Metadata)
	f.summaries = marshal("summaries", obj.Summaries)
	f.sections = marshal("sections", obj.Sections)
	f.tags = marshal("tags", obj.Tags)
	f.mentions = marshal("mentions", mentionsToStrings(obj.Mentions))
	f.decisions = marshal("decisions", obj.Decisions)
	f.tasks = marshal("tasks", obj.Tasks)
	f.influences = marshal("influences", obj.RegistryInfluences)
	f.plugins = marshal("plugins", obj.Plugins)

	if obj.LastReinforcedAt != nil {
		f.lastReinforcedAt = sql.NullString{String: obj.LastReinforcedAt.Format(time.RFC3339), Valid: true}
	}
	if obj.RemindAt != nil {
		f.remindAt = sql.NullString{String: obj.RemindAt.Format(time.RFC3339), Valid: true}
	}
	if obj.RemindedAt != nil {
		f.remindedAt = sql.NullString{String: obj.RemindedAt.Format(time.RFC3339), Valid: true}
	}

	return f, err
}

func marshalGraph(g *storage.ObjectGraph) (sql.NullString, error) {
	if g == nil {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(g)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("marshal graph: %w", err)
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

// decodeJSONColumn decodes a JSON column read back out of our own database.
//
// An empty string is a legitimately absent column: the JSON columns carry a
// DEFAULT but no NOT NULL, so a NULL (scanned into a string as "") or a row
// written before the column existed reads as empty. Those keep the field's
// zero value, exactly as before.
//
// Anything else that fails to decode is corrupt stored data. That is returned
// so the caller can surface it instead of silently yielding an empty field.
func decodeJSONColumn(raw, column string, dst any) error {
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return fmt.Errorf("unmarshal %s: %w", column, err)
	}
	return nil
}

func unmarshalGraph(raw string) (*storage.ObjectGraph, error) {
	if raw == "" {
		return nil, nil
	}
	var g storage.ObjectGraph
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, fmt.Errorf("unmarshal graph: %w", err)
	}
	return &g, nil
}

func (s *ObjectStore) upsertObjectNodesTx(ctx context.Context, tx *sql.Tx,
	objectID string, g *storage.ObjectGraph,
) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM object_nodes WHERE object_id = ?`, objectID); err != nil {
		return fmt.Errorf("delete object_nodes: %w", err)
	}
	if g == nil {
		return nil
	}
	for _, n := range g.Nodes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO object_nodes (id, object_id, node_type, ordinal, content, created_at)
			 VALUES (?, ?, ?, ?, ?, datetime('now'))`,
			n.ID, objectID, n.NodeType, n.Order, n.Content,
		); err != nil {
			return fmt.Errorf("insert object_node %s: %w", n.ID, err)
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// upsertEmbedding stores a float32 slice as a BLOB in object_embeddings.
// It is a no-op when the slice is empty.
func (s *ObjectStore) upsertEmbedding(ctx context.Context, id string, vec []float32) error {
	if len(vec) == 0 {
		return nil
	}
	blob := float32SliceToBlob(vec)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO object_embeddings(id, embedding, dimensions) VALUES(?,?,?)
		 ON CONFLICT(id) DO UPDATE SET embedding=excluded.embedding, dimensions=excluded.dimensions`,
		id, blob, len(vec))
	return err
}

// ListWithoutEmbeddings returns all active objects that have no stored embedding blob.
func (s *ObjectStore) ListWithoutEmbeddings(ctx context.Context) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.type, o.subtype, o.raw_content, o.content_type, o.text_content,
		       o.metadata, o.summaries, o.sections, o.tags, o.mentions,
		       o.decisions, o.tasks, o.pipeline, o.source,
		       o.registry_influences, o.plugins, o.content_hash, o.reinforcement_count, o.last_reinforced_at,
		       o.created_at, o.updated_at, o.fts_indexed, o.vector_indexed, o.status, o.inbox_note,
		       o.remind_at, o.reminded_at
		FROM objects o
		LEFT JOIN object_embeddings oe ON o.id = oe.id
		WHERE oe.id IS NULL AND o.status = 'active'
	`)
	if err != nil {
		return nil, fmt.Errorf("list without embeddings: %w", err)
	}
	defer rows.Close()

	var objects []*storage.KnowledgeObject
	for rows.Next() {
		var obj storage.KnowledgeObject
		var (
			metadataJSON, summariesJSON, sectionsJSON, tagsJSON string
			mentionsJSON, decisionsJSON, tasksJSON              string
			influencesJSON, pluginsJSON                         string
			createdAt, updatedAt                                string
			ftsIndexed, vectorIndexed                           int
			lastReinforcedAt                                    sql.NullString
			remindAt, remindedAt                                sql.NullString
		)
		if err := rows.Scan(
			&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
			&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
			&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
			&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
			&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed, &obj.Status, &obj.InboxNote,
			&remindAt, &remindedAt,
		); err != nil {
			return nil, fmt.Errorf("list without embeddings scan: %w", err)
		}
		if err := unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
			mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
			createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
			remindAt, remindedAt); err != nil {
			return nil, fmt.Errorf("list without embeddings scan: %w", err)
		}
		objects = append(objects, &obj)
	}
	return objects, rows.Err()
}

// ListWithEmbeddings returns all objects that have a stored embedding blob.
func (s *ObjectStore) ListWithEmbeddings(ctx context.Context) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, o.type, o.subtype, o.raw_content, o.content_type, o.text_content,
		       o.metadata, o.summaries, o.sections, o.tags, o.mentions,
		       o.decisions, o.tasks, o.pipeline, o.source,
		       o.registry_influences, o.plugins, o.content_hash, o.reinforcement_count, o.last_reinforced_at,
		       o.created_at, o.updated_at, o.fts_indexed, o.vector_indexed, o.status, o.inbox_note,
		       o.remind_at, o.reminded_at, o.profile_id,
		       oe.embedding
		FROM objects o
		INNER JOIN object_embeddings oe ON o.id = oe.id
	`)
	if err != nil {
		return nil, fmt.Errorf("list with embeddings: %w", err)
	}
	defer rows.Close()

	var objects []*storage.KnowledgeObject
	for rows.Next() {
		var obj storage.KnowledgeObject
		var (
			metadataJSON, summariesJSON, sectionsJSON, tagsJSON string
			mentionsJSON, decisionsJSON, tasksJSON              string
			influencesJSON, pluginsJSON                         string
			createdAt, updatedAt                                string
			ftsIndexed, vectorIndexed                           int
			lastReinforcedAt                                    sql.NullString
			remindAt, remindedAt                                sql.NullString
			embeddingBlob                                       []byte
		)
		if err := rows.Scan(
			&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
			&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
			&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
			&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
			&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed, &obj.Status, &obj.InboxNote,
			&remindAt, &remindedAt, &obj.ProfileID,
			&embeddingBlob,
		); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		if err := unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
			mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
			createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
			remindAt, remindedAt); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		obj.Embeddings = blobToFloat32Slice(embeddingBlob)
		objects = append(objects, &obj)
	}
	return objects, rows.Err()
}

// VectorSearch returns the top-K objects ranked by vector similarity.
//
// When an ANN index (vec field) is available, it delegates to the sqlite-vec
// KNN query (O(log n)) and hydrates the small result set individually.
// When no ANN index is wired (e.g. unit tests using small vectors), it falls
// back to a full scan of object_embeddings with in-process cosine similarity.
func (s *ObjectStore) VectorSearch(ctx context.Context, vector []float32, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	if len(vector) == 0 {
		return nil, fmt.Errorf("vector search: empty query vector")
	}
	// Cosine similarity is undefined against a zero-magnitude query, so no
	// row can rank: return nothing, matching the ANN leg (vec0 reports
	// unrankable NULL distances) and the Postgres driver (NaN distance
	// fails its range predicate). Without this guard the brute-force
	// scorer below hands every candidate back at score 0.
	if isZeroVector(vector) {
		return nil, nil
	}

	if s.vec != nil && s.vecDim > 0 && len(vector) == s.vecDim {
		return s.vectorSearchANN(ctx, vector, filter)
	}
	return s.vectorSearchBruteForce(ctx, vector, filter)
}

// vectorSearchANN uses the sqlite-vec KNN index for O(log n) approximate search.
// It over-fetches by a factor of annOverfetch to allow post-filtering by type/subtype/pipeline.
func (s *ObjectStore) vectorSearchANN(ctx context.Context, vector []float32, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	const annOverfetch = 10

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	topK := limit * annOverfetch

	hits, err := s.vec.Search(ctx, vector, topK)
	if err != nil {
		return nil, fmt.Errorf("vector search ann: %w", err)
	}

	var out []*storage.KnowledgeObject
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		obj, err := s.Get(ctx, h.ID)
		if err != nil {
			// Object may have been deleted between index and fetch; skip.
			continue
		}
		if filter.Type != "" && obj.Type != filter.Type {
			continue
		}
		if filter.Subtype != "" && obj.Subtype != filter.Subtype {
			continue
		}
		if filter.Pipeline != "" && obj.Pipeline != filter.Pipeline {
			continue
		}
		if !matchesMetadataFacets(obj, filter) {
			continue
		}
		if obj.Metadata == nil {
			obj.Metadata = make(map[string]any)
		}
		// vec0 reports cosine distance (migration 034 pins the metric);
		// score = 1 - cosine_distance is the cross-driver mapping shared
		// with the Postgres driver and the brute-force path below.
		obj.Metadata["score"] = 1.0 - float64(h.Score)
		out = append(out, obj)
	}
	return out, nil
}

// vectorSearchBruteForce is the legacy O(n) fallback used when no ANN index is
// wired (e.g. unit tests with non-standard embedding dimensions).
func (s *ObjectStore) vectorSearchBruteForce(ctx context.Context, vector []float32, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	candidates, err := s.ListWithEmbeddings(ctx)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	type scored struct {
		obj   *storage.KnowledgeObject
		score float64
	}

	var results []scored
	for _, obj := range candidates {
		if filter.Type != "" && obj.Type != filter.Type {
			continue
		}
		if filter.Subtype != "" && obj.Subtype != filter.Subtype {
			continue
		}
		if filter.Pipeline != "" && obj.Pipeline != filter.Pipeline {
			continue
		}
		if !matchesMetadataFacets(obj, filter) {
			continue
		}
		if len(obj.Embeddings) == 0 {
			continue
		}
		score := cosineSimilarity(vector, obj.Embeddings)
		results = append(results, scored{obj: obj, score: score})
	}

	// Sort descending by similarity (insertion sort; results slice is typically small).
	for i := 1; i < len(results); i++ {
		key := results[i]
		j := i - 1
		for j >= 0 && results[j].score < key.score {
			results[j+1] = results[j]
			j--
		}
		results[j+1] = key
	}

	limit := filter.Limit
	if limit <= 0 || limit > len(results) {
		limit = len(results)
	}

	out := make([]*storage.KnowledgeObject, limit)
	for i := 0; i < limit; i++ {
		out[i] = results[i].obj
		if out[i].Metadata == nil {
			out[i].Metadata = make(map[string]any)
		}
		out[i].Metadata["score"] = results[i].score
	}
	return out, nil
}

// FTSSearch queries the objects_fts FTS5 virtual table and returns matching objects
// ranked by bm25 relevance score. bm25() returns negative values in SQLite FTS5;
// ORDER BY score (ascending) gives best matches first.
func (s *ObjectStore) FTSSearch(ctx context.Context, query string, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	if query == "" {
		return nil, fmt.Errorf("fts search: empty query")
	}

	// The driver owns its dialect's quoting: raw user text arrives here and
	// FTS5 phrase-quoting is applied at the boundary, never by callers.
	// Input with no usable tokens matches nothing rather than erroring.
	query = ftsq.ForSQLite(query)
	if query == "" {
		return nil, nil
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	q := `
		SELECT o.id, o.type, o.subtype, o.raw_content, o.content_type, o.text_content,
		       o.metadata, o.summaries, o.sections, o.tags, o.mentions,
		       o.decisions, o.tasks, o.pipeline, o.source,
		       o.registry_influences, o.plugins, o.content_hash, o.reinforcement_count, o.last_reinforced_at,
		       o.created_at, o.updated_at, o.fts_indexed, o.vector_indexed, o.status, o.inbox_note,
		       o.remind_at, o.reminded_at, o.profile_id, o.graph_json,
		       bm25(objects_fts) AS score
		FROM objects_fts
		JOIN objects o ON objects_fts.id = o.id
		WHERE objects_fts MATCH ?`
	args := []any{query}

	if filter.Type != "" {
		q += " AND o.type = ?"
		args = append(args, filter.Type)
	}

	// Metadata facet filters (US-0407). metadataFacetConditionsSQLite
	// returns constant SQL fragments whose values are all `?` bound into
	// ma — no caller string is ever concatenated into q here.
	// #nosec G202 -- fragments are compile-time constants; values in args.
	mc, ma := metadataFacetConditionsSQLite(filter)
	for _, c := range mc {
		// Prefix bare column refs with table alias for the JOIN query.
		aliased := strings.ReplaceAll(c, "metadata", "o.metadata")
		// #nosec G202 -- c is a compile-time constant fragment from
		// metadataFacetConditionsSQLite; its values are `?` bound into ma.
		q += " AND " + aliased
	}
	args = append(args, ma...)

	q += " ORDER BY score LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close()

	var results []*storage.KnowledgeObject
	for rows.Next() {
		var obj storage.KnowledgeObject
		var (
			metadataJSON, summariesJSON, sectionsJSON, tagsJSON string
			mentionsJSON, decisionsJSON, tasksJSON              string
			influencesJSON, pluginsJSON                         string
			createdAt, updatedAt                                string
			ftsIndexed, vectorIndexed                           int
			lastReinforcedAt, remindAt, remindedAt              sql.NullString
			graphJSON                                           sql.NullString
			score                                               float64
		)
		err := rows.Scan(
			&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
			&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
			&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
			&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
			&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed, &obj.Status, &obj.InboxNote,
			&remindAt, &remindedAt, &obj.ProfileID, &graphJSON,
			&score,
		)
		if err != nil {
			return nil, fmt.Errorf("fts search scan: %w", err)
		}
		if err := unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
			mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
			createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
			remindAt, remindedAt); err != nil {
			return nil, fmt.Errorf("fts search scan: %w", err)
		}
		if graphJSON.Valid {
			g, err := unmarshalGraph(graphJSON.String)
			if err != nil {
				return nil, fmt.Errorf("fts search unmarshal graph: %w", err)
			}
			obj.Graph = g
		}
		if obj.Metadata == nil {
			obj.Metadata = make(map[string]any)
		}
		obj.Metadata["fts_score"] = score
		results = append(results, &obj)
	}
	return results, rows.Err()
}

// cosineSimilarity returns the cosine similarity between two vectors.
// Returns 0 when either vector has zero magnitude.
func cosineSimilarity(a, b []float32) float64 {
	n := len(a)
	if n > len(b) {
		n = len(b)
	}
	var dot, normA, normB float64
	for i := 0; i < n; i++ {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// float32SliceToBlob encodes []float32 as little-endian bytes.
func float32SliceToBlob(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// blobToFloat32Slice decodes little-endian bytes to []float32.
func blobToFloat32Slice(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}

// nodeTypeObjectIDs returns object IDs that contain at least one node of each
// requested type. When nodeTypes is empty, nil is returned (no pre-filter).
func (s *ObjectStore) nodeTypeObjectIDs(ctx context.Context, nodeTypes []string) (map[string]struct{}, error) {
	if len(nodeTypes) == 0 {
		return nil, nil
	}
	// Build: SELECT object_id FROM object_nodes WHERE node_type IN (?, ?, ...)
	// GROUP BY object_id HAVING COUNT(DISTINCT node_type) = N
	// so only objects that have ALL requested node types are returned.
	placeholders := make([]string, len(nodeTypes))
	args := make([]any, len(nodeTypes))
	for i, t := range nodeTypes {
		placeholders[i] = "?"
		args[i] = t
	}
	// The only text interpolated is the "?, ?, ..." placeholder run whose
	// length is len(nodeTypes); the node type strings themselves are bound
	// into args. Variable-length IN lists cannot be expressed with a single
	// bind parameter in SQLite, so this expansion is unavoidable.
	// #nosec G201 -- format arg is a generated placeholder list, not data.
	q := fmt.Sprintf(
		`SELECT object_id FROM object_nodes WHERE node_type IN (%s)
		 GROUP BY object_id HAVING COUNT(DISTINCT node_type) = ?`,
		strings.Join(placeholders, ", "),
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
// returned. Results include a populated DocumentView when naf.ReturnNodeHits is true.
func (s *ObjectStore) FTSSearchNodeAware(
	ctx context.Context,
	query string,
	filter storage.ObjectFilter,
	naf pluginapi.NodeAwareFilter,
) ([]*pluginapi.NodeAwareResult, error) {
	// Run FTS first; node-type pre-filter is applied in-process after the query
	// so that bm25 ranking is preserved. (object_nodes has an index on node_type;
	// the pre-filter set is small relative to the full index.)
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
// returned. Results include a populated DocumentView when naf.ReturnNodeHits is true.
func (s *ObjectStore) VectorSearchNodeAware(
	ctx context.Context,
	vector []float32,
	filter storage.ObjectFilter,
	naf pluginapi.NodeAwareFilter,
) ([]*pluginapi.NodeAwareResult, error) {
	objects, err := s.VectorSearch(ctx, vector, filter)
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

// metadataFacetConditionsSQLite builds WHERE clauses for metadata facet filters
// using SQLite json_extract / json_each.
func metadataFacetConditionsSQLite(f storage.ObjectFilter) ([]string, []any) {
	var conds []string
	var args []any

	if f.MetadataType != "" {
		conds = append(conds, "json_extract(metadata, '$.type') = ?")
		args = append(args, f.MetadataType)
	}
	if f.SourceType != "" {
		conds = append(conds, "json_extract(metadata, '$.source_type') = ?")
		args = append(args, f.SourceType)
	}
	if f.MetadataTopic != "" {
		conds = append(conds,
			"EXISTS (SELECT 1 FROM json_each(json_extract(metadata, '$.topics')) WHERE json_each.value = ?)")
		args = append(args, f.MetadataTopic)
	}
	if f.MetadataPerson != "" {
		conds = append(conds,
			"EXISTS (SELECT 1 FROM json_each(json_extract(metadata, '$.people')) WHERE json_each.value = ?)")
		args = append(args, f.MetadataPerson)
	}
	if f.MetadataSince != nil {
		conds = append(conds,
			"EXISTS (SELECT 1 FROM json_each(json_extract(metadata, '$.dates_mentioned')) WHERE json_each.value >= ?)")
		args = append(args, f.MetadataSince.Format("2006-01-02"))
	}
	if f.MetadataUntil != nil {
		conds = append(conds,
			"EXISTS (SELECT 1 FROM json_each(json_extract(metadata, '$.dates_mentioned')) WHERE json_each.value <= ?)")
		args = append(args, f.MetadataUntil.Format("2006-01-02"))
	}
	return conds, args
}

// matchesMetadataFacets checks whether an object passes the metadata facet
// filters in-memory. Used for post-filtering in vector search where
// SQL-level JSON filtering is not possible.
func matchesMetadataFacets(obj *storage.KnowledgeObject, f storage.ObjectFilter) bool {
	if obj.Metadata == nil {
		return f.MetadataType == "" && f.SourceType == "" &&
			f.MetadataTopic == "" && f.MetadataPerson == "" &&
			f.MetadataSince == nil && f.MetadataUntil == nil
	}
	if f.MetadataType != "" {
		if v, _ := obj.Metadata["type"].(string); v != f.MetadataType {
			return false
		}
	}
	if f.SourceType != "" {
		if v, _ := obj.Metadata["source_type"].(string); v != f.SourceType {
			return false
		}
	}
	if f.MetadataTopic != "" && !metadataSliceContains(obj.Metadata, "topics", f.MetadataTopic) {
		return false
	}
	if f.MetadataPerson != "" && !metadataSliceContains(obj.Metadata, "people", f.MetadataPerson) {
		return false
	}
	if f.MetadataSince != nil && !metadataHasDateGE(obj.Metadata, "dates_mentioned", f.MetadataSince) {
		return false
	}
	if f.MetadataUntil != nil && !metadataHasDateLE(obj.Metadata, "dates_mentioned", f.MetadataUntil) {
		return false
	}
	return true
}

func metadataSliceContains(m map[string]any, key, needle string) bool {
	arr, ok := m[key].([]any)
	if !ok {
		return false
	}
	for _, v := range arr {
		if s, _ := v.(string); s == needle {
			return true
		}
	}
	return false
}

func metadataHasDateGE(m map[string]any, key string, since *time.Time) bool {
	arr, ok := m[key].([]any)
	if !ok {
		return false
	}
	threshold := since.Format("2006-01-02")
	for _, v := range arr {
		if s, _ := v.(string); s >= threshold {
			return true
		}
	}
	return false
}

func metadataHasDateLE(m map[string]any, key string, until *time.Time) bool {
	arr, ok := m[key].([]any)
	if !ok {
		return false
	}
	threshold := until.Format("2006-01-02")
	for _, v := range arr {
		if s, _ := v.(string); s <= threshold {
			return true
		}
	}
	return false
}
