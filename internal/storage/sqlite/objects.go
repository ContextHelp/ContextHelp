package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type ObjectStore struct {
	db *sql.DB
}

func (s *ObjectStore) Create(ctx context.Context, obj *storage.KnowledgeObject) error {
	f, err := marshalObjectFields(obj)
	if err != nil {
		return fmt.Errorf("create object: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO objects (
		id, type, subtype, raw_content, content_type,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, embeddings, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		obj.ID, obj.Type, obj.Subtype, obj.RawContent, obj.ContentType,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, nil, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.CreatedAt.Format(time.RFC3339), obj.UpdatedAt.Format(time.RFC3339),
		boolToInt(obj.FTSIndexed), boolToInt(obj.VectorIndexed),
	)
	if err != nil {
		return fmt.Errorf("create object: %w", err)
	}
	return nil
}

func (s *ObjectStore) Get(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, subtype, raw_content, content_type,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed
	FROM objects WHERE id = ?`, id)
	return scanObject(row)
}

func (s *ObjectStore) GetByContentHash(ctx context.Context, hash string) (*storage.KnowledgeObject, error) {
	if hash == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, subtype, raw_content, content_type,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed
	FROM objects WHERE content_hash = ? LIMIT 1`, hash)
	return scanObject(row)
}

func (s *ObjectStore) List(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	var conditions []string
	var args []any

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
		id, type, subtype, raw_content, content_type,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed
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
		type=?, subtype=?, raw_content=?, content_type=?,
		metadata=?, summaries=?, sections=?, tags=?, mentions=?,
		decisions=?, tasks=?, pipeline=?, source=?,
		registry_influences=?, plugins=?, content_hash=?, reinforcement_count=?, last_reinforced_at=?,
		updated_at=?, fts_indexed=?, vector_indexed=?
	WHERE id=?`,
		obj.Type, obj.Subtype, obj.RawContent, obj.ContentType,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.UpdatedAt.Format(time.RFC3339),
		boolToInt(obj.FTSIndexed), boolToInt(obj.VectorIndexed),
		obj.ID,
	)
	if err != nil {
		return fmt.Errorf("update object: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s not found", obj.ID)
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
	json.Unmarshal([]byte(mentionsJSON), &obj.Mentions)

	now := time.Now().Format(time.RFC3339)

	merged := mergeTags(obj.Tags, mergeData.Tags)
	mergedTagsJSON, _ := json.Marshal(merged)

	mergedMentions := mergeStrings(obj.Mentions, mergeData.Mentions)
	mergedMentionsJSON, _ := json.Marshal(mergedMentions)

	_, err = tx.ExecContext(ctx, `
		UPDATE objects SET
			reinforcement_count = reinforcement_count + 1,
			last_reinforced_at = ?,
			tags = ?,
			mentions = ?,
			updated_at = ?
		WHERE content_hash = ?
	`, now, string(mergedTagsJSON), string(mergedMentionsJSON), now, hash)
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
		id, type, subtype, raw_content, content_type,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed
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
		lastReinforcedAt                                    sql.NullString
	)

	err := row.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("object not found")
		}
		return nil, fmt.Errorf("scan object: %w", err)
	}

	unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt)
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
		lastReinforcedAt                                    sql.NullString
	)

	err := rows.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&createdAt, &updatedAt, &ftsIndexed, &vectorIndexed,
	)
	if err != nil {
		return nil, fmt.Errorf("scan object row: %w", err)
	}

	unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		createdAt, updatedAt, ftsIndexed, vectorIndexed, lastReinforcedAt)
	return &obj, nil
}

func unmarshalObjectJSON(obj *storage.KnowledgeObject,
	metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
	mentionsJSON, decisionsJSON, tasksJSON,
	influencesJSON, pluginsJSON,
	createdAt, updatedAt string,
	ftsIndexed, vectorIndexed int,
	lastReinforcedAt sql.NullString,
) {
	json.Unmarshal([]byte(metadataJSON), &obj.Metadata)
	json.Unmarshal([]byte(summariesJSON), &obj.Summaries)
	json.Unmarshal([]byte(sectionsJSON), &obj.Sections)
	json.Unmarshal([]byte(tagsJSON), &obj.Tags)
	json.Unmarshal([]byte(mentionsJSON), &obj.Mentions)
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
}

type objectFields struct {
	metadata, summaries, sections, tags       string
	mentions, decisions, tasks                string
	influences, plugins                       string
	lastReinforcedAt                          sql.NullString
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
	f.mentions = marshal("mentions", obj.Mentions)
	f.decisions = marshal("decisions", obj.Decisions)
	f.tasks = marshal("tasks", obj.Tasks)
	f.influences = marshal("influences", obj.RegistryInfluences)
	f.plugins = marshal("plugins", obj.Plugins)

	if obj.LastReinforcedAt != nil {
		f.lastReinforcedAt = sql.NullString{String: obj.LastReinforcedAt.Format(time.RFC3339), Valid: true}
	}

	return f, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
