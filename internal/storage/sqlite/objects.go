package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"hop.top/uri"
)

type ObjectStore struct {
	db *sql.DB
}

func (s *ObjectStore) Create(ctx context.Context, obj *storage.KnowledgeObject) error {
	f, err := marshalObjectFields(obj)
	if err != nil {
		return fmt.Errorf("create object: %w", err)
	}

	if obj.Status == "" {
		obj.Status = "active"
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO objects (
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, embeddings, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
		remind_at, reminded_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		obj.ID, obj.Type, obj.Subtype, obj.RawContent, obj.ContentType, obj.TextContent,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, nil, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.CreatedAt.Format(time.RFC3339), obj.UpdatedAt.Format(time.RFC3339),
		boolToInt(obj.FTSIndexed), boolToInt(obj.VectorIndexed), obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt,
	)
	if err != nil {
		return fmt.Errorf("create object: %w", err)
	}
	if err := s.upsertEmbedding(ctx, obj.ID, obj.Embeddings); err != nil {
		return fmt.Errorf("create object embedding: %w", err)
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
		remind_at, reminded_at
	FROM objects WHERE id = ?`, id)
	return scanObject(row)
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
		remind_at, reminded_at
	FROM objects WHERE content_hash = ? LIMIT 1`, hash)
	return scanObject(row)
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
	if filter.After != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filter.After.Format(time.RFC3339))
	}
	if filter.Before != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, filter.Before.Format(time.RFC3339))
	}

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
		remind_at, reminded_at
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

	result, err := s.db.ExecContext(ctx, `UPDATE objects SET
		type=?, subtype=?, raw_content=?, content_type=?, text_content=?,
		metadata=?, summaries=?, sections=?, tags=?, mentions=?,
		decisions=?, tasks=?, pipeline=?, source=?,
		registry_influences=?, plugins=?, content_hash=?, reinforcement_count=?, last_reinforced_at=?,
		updated_at=?, fts_indexed=?, vector_indexed=?, status=?, inbox_note=?,
		remind_at=?, reminded_at=?
	WHERE id=?`,
		obj.Type, obj.Subtype, obj.RawContent, obj.ContentType, obj.TextContent,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.UpdatedAt.Format(time.RFC3339),
		boolToInt(obj.FTSIndexed), boolToInt(obj.VectorIndexed), obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt,
		obj.ID,
	)
	if err != nil {
		return fmt.Errorf("update object: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s not found", obj.ID)
	}
	if err := s.upsertEmbedding(ctx, obj.ID, obj.Embeddings); err != nil {
		return fmt.Errorf("update object embedding: %w", err)
	}
	return nil
}

func (s *ObjectStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM objects WHERE id = ?", id)
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

	json.Unmarshal([]byte(tagsJSON), &obj.Tags)
	var mentionStrs []string
	json.Unmarshal([]byte(mentionsJSON), &mentionStrs)
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
		contentUpdate = ", raw_content = ?, text_content = ?"
		contentArgs = append(contentArgs, mergeData.RawContent, mergeData.TextContent)
	}
	contentArgs = append(contentArgs, hash)

	query := fmt.Sprintf(`
		UPDATE objects SET
			reinforcement_count = reinforcement_count + 1,
			last_reinforced_at = ?,
			tags = ?,
			mentions = ?,
			updated_at = ?,
			fts_indexed = 0,
			vector_indexed = 0
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
		remind_at, reminded_at
	FROM objects`
	if where != "" {
		query += " WHERE " + where
	}
	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
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
	)

	err := row.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("object not found")
		}
		return nil, fmt.Errorf("scan object: %w", err)
	}

	unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
		remindAt, remindedAt)
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
	)

	err := rows.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan object row: %w", err)
	}

	unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
		remindAt, remindedAt)
	return &obj, nil
}

func unmarshalObjectJSON(obj *storage.KnowledgeObject,
	metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
	mentionsJSON, decisionsJSON, tasksJSON,
	influencesJSON, pluginsJSON,
	createdAt, updatedAt string,
	ftsIndexed, vectorIndexed int,
	lastReinforcedAt, remindAt, remindedAt sql.NullString,
) {
	json.Unmarshal([]byte(metadataJSON), &obj.Metadata)
	json.Unmarshal([]byte(summariesJSON), &obj.Summaries)
	json.Unmarshal([]byte(sectionsJSON), &obj.Sections)
	json.Unmarshal([]byte(tagsJSON), &obj.Tags)
	var mentionStrs []string
	json.Unmarshal([]byte(mentionsJSON), &mentionStrs)
	obj.Mentions = mentions.ParseSlice(mentionStrs)
	json.Unmarshal([]byte(decisionsJSON), &obj.Decisions)
	json.Unmarshal([]byte(tasksJSON), &obj.Tasks)
	json.Unmarshal([]byte(influencesJSON), &obj.RegistryInfluences)
	json.Unmarshal([]byte(pluginsJSON), &obj.Plugins)
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
}

type objectFields struct {
	metadata, summaries, sections, tags       string
	mentions, decisions, tasks                string
	influences, plugins                       string
	lastReinforcedAt, remindAt, remindedAt    sql.NullString
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
		unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
			mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
			createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
			remindAt, remindedAt)
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
		       o.remind_at, o.reminded_at,
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
			&remindAt, &remindedAt,
			&embeddingBlob,
		); err != nil {
			return nil, fmt.Errorf("scan embedding row: %w", err)
		}
		unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
			mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
			createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
			remindAt, remindedAt)
		obj.Embeddings = blobToFloat32Slice(embeddingBlob)
		objects = append(objects, &obj)
	}
	return objects, rows.Err()
}

// VectorSearch fetches all objects with embeddings, computes cosine similarity
// against vector, applies any ObjectFilter constraints, and returns the top-K
// results in descending similarity order.
func (s *ObjectStore) VectorSearch(ctx context.Context, vector []float32, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	if len(vector) == 0 {
		return nil, fmt.Errorf("vector search: empty query vector")
	}

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
		       o.remind_at, o.reminded_at,
		       bm25(objects_fts) AS score
		FROM objects_fts
		JOIN objects o ON objects_fts.id = o.id
		WHERE objects_fts MATCH ?`
	args := []any{query}

	if filter.Type != "" {
		q += " AND o.type = ?"
		args = append(args, filter.Type)
	}

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
			score                                               float64
		)
		err := rows.Scan(
			&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
			&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
			&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
			&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
			&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed, &obj.Status, &obj.InboxNote,
			&remindAt, &remindedAt,
			&score,
		)
		if err != nil {
			return nil, fmt.Errorf("fts search scan: %w", err)
		}
		unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
			mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
			createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt,
			remindAt, remindedAt)
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
