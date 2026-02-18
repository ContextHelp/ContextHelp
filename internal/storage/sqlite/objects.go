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
	metadata, _ := json.Marshal(obj.Metadata)
	summaries, _ := json.Marshal(obj.Summaries)
	sections, _ := json.Marshal(obj.Sections)
	tags, _ := json.Marshal(obj.Tags)
	mentions, _ := json.Marshal(obj.Mentions)
	decisions, _ := json.Marshal(obj.Decisions)
	tasks, _ := json.Marshal(obj.Tasks)
	influences, _ := json.Marshal(obj.RegistryInfluences)
	plugins, _ := json.Marshal(obj.Plugins)

	_, err := s.db.ExecContext(ctx, `INSERT INTO objects (
		id, type, subtype, raw_content, content_type,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, embeddings, pipeline, source,
		registry_influences, plugins, created_at, updated_at,
		fts_indexed, vector_indexed
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		obj.ID, obj.Type, obj.Subtype, obj.RawContent, obj.ContentType,
		string(metadata), string(summaries), string(sections), string(tags), string(mentions),
		string(decisions), string(tasks), nil, obj.Pipeline, obj.Source,
		string(influences), string(plugins),
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
		registry_influences, plugins, created_at, updated_at,
		fts_indexed, vector_indexed
	FROM objects WHERE id = ?`, id)
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
		registry_influences, plugins, created_at, updated_at,
		fts_indexed, vector_indexed
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
	metadata, _ := json.Marshal(obj.Metadata)
	summaries, _ := json.Marshal(obj.Summaries)
	sections, _ := json.Marshal(obj.Sections)
	tags, _ := json.Marshal(obj.Tags)
	mentions, _ := json.Marshal(obj.Mentions)
	decisions, _ := json.Marshal(obj.Decisions)
	tasks, _ := json.Marshal(obj.Tasks)
	influences, _ := json.Marshal(obj.RegistryInfluences)
	plugins, _ := json.Marshal(obj.Plugins)

	result, err := s.db.ExecContext(ctx, `UPDATE objects SET
		type=?, subtype=?, raw_content=?, content_type=?,
		metadata=?, summaries=?, sections=?, tags=?, mentions=?,
		decisions=?, tasks=?, pipeline=?, source=?,
		registry_influences=?, plugins=?, updated_at=?,
		fts_indexed=?, vector_indexed=?
	WHERE id=?`,
		obj.Type, obj.Subtype, obj.RawContent, obj.ContentType,
		string(metadata), string(summaries), string(sections), string(tags), string(mentions),
		string(decisions), string(tasks), obj.Pipeline, obj.Source,
		string(influences), string(plugins), obj.UpdatedAt.Format(time.RFC3339),
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
		registry_influences, plugins, created_at, updated_at,
		fts_indexed, vector_indexed
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
		metadataJSON, summariesJSON, sectionsJSON, tagsJSON      string
		mentionsJSON, decisionsJSON, tasksJSON                   string
		influencesJSON, pluginsJSON                              string
		createdAt, updatedAt                                     string
		ftsIndexed, vectorIndexed                                int
	)

	err := row.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &createdAt, &updatedAt,
		&ftsIndexed, &vectorIndexed,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("object not found")
		}
		return nil, fmt.Errorf("scan object: %w", err)
	}

	unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		createdAt, updatedAt, ftsIndexed, vectorIndexed)
	return &obj, nil
}

func scanObjectFromRows(rows *sql.Rows) (*storage.KnowledgeObject, error) {
	var obj storage.KnowledgeObject
	var (
		metadataJSON, summariesJSON, sectionsJSON, tagsJSON      string
		mentionsJSON, decisionsJSON, tasksJSON                   string
		influencesJSON, pluginsJSON                              string
		createdAt, updatedAt                                     string
		ftsIndexed, vectorIndexed                                int
	)

	err := rows.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &createdAt, &updatedAt,
		&ftsIndexed, &vectorIndexed,
	)
	if err != nil {
		return nil, fmt.Errorf("scan object row: %w", err)
	}

	unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		createdAt, updatedAt, ftsIndexed, vectorIndexed)
	return &obj, nil
}

func unmarshalObjectJSON(obj *storage.KnowledgeObject,
	metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
	mentionsJSON, decisionsJSON, tasksJSON,
	influencesJSON, pluginsJSON,
	createdAt, updatedAt string,
	ftsIndexed, vectorIndexed int,
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
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
