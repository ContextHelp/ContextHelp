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
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"hop.top/uri"
)

// errObjectNotFound is returned by scanObjectRow when no row matches.
var errObjectNotFound = errors.New("object not found")

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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create object: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `INSERT INTO objects (
		id, type, subtype, raw_content, content_type,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, embedding, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
		remind_at, reminded_at, graph_json, source_key
	) VALUES (
		$1, $2, $3, $4, $5,
		$6, $7, $8, $9, $10,
		$11, $12, $13, $14, $15,
		$16, $17, $18, $19, $20,
		$21, $22, $23, $24, $25, $26,
		$27, $28, $29, $30
	)`,
		obj.ID, obj.Type, obj.Subtype, obj.RawContent, obj.ContentType,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, f.embedding, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.CreatedAt.UTC(), obj.UpdatedAt.UTC(), obj.FTSIndexed, obj.VectorIndexed, obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt, graphJSON, obj.SourceKey,
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

	sortCol := "created_at"
	if filter.Sort == "updated_at" {
		sortCol = "updated_at"
	}
	dir := "DESC"
	if filter.Dir == "asc" {
		dir = "ASC"
	}

	query := objectSelectCols + ` FROM objects ` + where + fmt.Sprintf(` ORDER BY %s %s`, sortCol, dir)
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update object: begin tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE objects SET
		type=$1, subtype=$2, raw_content=$3, content_type=$4,
		metadata=$5, summaries=$6, sections=$7, tags=$8, mentions=$9,
		decisions=$10, tasks=$11, embedding=$12, pipeline=$13, source=$14,
		registry_influences=$15, plugins=$16, content_hash=$17,
		reinforcement_count=$18, last_reinforced_at=$19,
		updated_at=$20, fts_indexed=$21, vector_indexed=$22, status=$23, inbox_note=$24,
		remind_at=$25, reminded_at=$26, graph_json=$27
	WHERE id=$28`,
		obj.Type, obj.Subtype, obj.RawContent, obj.ContentType,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, f.embedding, obj.Pipeline, obj.Source,
		f.influences, f.plugins, obj.ContentHash,
		obj.ReinforcementCount, f.lastReinforcedAt,
		obj.UpdatedAt.UTC(), obj.FTSIndexed, obj.VectorIndexed, obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt, graphJSON,
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

	json.Unmarshal(tagsJSON, &obj.Tags)
	var mentionStrs []string
	json.Unmarshal(mentionsJSON, &mentionStrs)
	obj.Mentions = mentions.ParseSlice(mentionStrs)

	now := time.Now().UTC()
	merged := mergeTags(obj.Tags, mergeData.Tags)
	mergedTagsJSON, _ := json.Marshal(merged)
	mergedMentionStrs := mergeStrings(mentionsToStrings(obj.Mentions), mentionsToStrings(mergeData.Mentions))
	mergedMentionsJSON, _ := json.Marshal(mergedMentionStrs)

	if mergeData.RawContent != "" && mergeData.RawContent != hash {
		_, err = tx.ExecContext(ctx, `UPDATE objects SET
			reinforcement_count = reinforcement_count + 1,
			last_reinforced_at = $1,
			tags = $2,
			mentions = $3,
			updated_at = $4,
			fts_indexed = FALSE,
			vector_indexed = FALSE,
			raw_content = $5
		WHERE content_hash = $6`,
			now, mergedTagsJSON, mergedMentionsJSON, now,
			mergeData.RawContent, hash,
		)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE objects SET
			reinforcement_count = reinforcement_count + 1,
			last_reinforced_at = $1,
			tags = $2,
			mentions = $3,
			updated_at = $4,
			fts_indexed = FALSE,
			vector_indexed = FALSE
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

	query := objectSelectCols + ` FROM objects`
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
		obj, err := scanObjectRows(rows)
		if err != nil {
			return nil, 0, err
		}
		objects = append(objects, obj)
	}
	return objects, total, rows.Err()
}

// ListWithoutEmbeddings returns active objects that have no stored embedding.
func (s *ObjectStore) ListWithoutEmbeddings(ctx context.Context) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx, objectSelectCols+` FROM objects WHERE embedding IS NULL AND status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("list without embeddings: %w", err)
	}
	defer rows.Close()

	var objects []*storage.KnowledgeObject
	for rows.Next() {
		obj, err := scanObjectRows(rows)
		if err != nil {
			return nil, err
		}
		objects = append(objects, obj)
	}
	return objects, rows.Err()
}

func (s *ObjectStore) ListWithEmbeddings(ctx context.Context) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx, objectSelectCols+`, embedding FROM objects WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("list with embeddings: %w", err)
	}
	defer rows.Close()

	var objects []*storage.KnowledgeObject
	for rows.Next() {
		obj, emb, err := scanObjectRowWithEmbedding(rows)
		if err != nil {
			return nil, err
		}
		obj.Embeddings = emb
		objects = append(objects, obj)
	}
	return objects, rows.Err()
}

func (s *ObjectStore) VectorSearch(ctx context.Context, vector []float32, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	if len(vector) == 0 {
		return nil, fmt.Errorf("vector search: empty query vector")
	}

	// Build pgvector literal: '[0.1,0.2,...]'
	var sb strings.Builder
	sb.WriteString("'[")
	for i, v := range vector {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf("%g", v))
	}
	sb.WriteString("]'::vector")
	vecLiteral := sb.String()

	var conditions []string
	var args []any
	idx := 1

	conditions = append(conditions, "embedding IS NOT NULL")

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

	where := "WHERE " + strings.Join(conditions, " AND ")
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	query := objectSelectCols + fmt.Sprintf(`, 1 - (embedding <=> %s) AS score FROM objects %s ORDER BY embedding <=> %s LIMIT %d`,
		vecLiteral, where, vecLiteral, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var objects []*storage.KnowledgeObject
	for rows.Next() {
		obj, score, err := scanObjectRowWithScore(rows)
		if err != nil {
			return nil, err
		}
		if obj.Metadata == nil {
			obj.Metadata = make(map[string]any)
		}
		obj.Metadata["score"] = score
		objects = append(objects, obj)
	}
	return objects, rows.Err()
}

// FTSSearch is not implemented for the postgres backend.
func (s *ObjectStore) FTSSearch(ctx context.Context, query string, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	return nil, fmt.Errorf("FTSSearch: not implemented for postgres backend")
}

// FTSSearchNodeAware is not implemented for the postgres backend.
func (s *ObjectStore) FTSSearchNodeAware(_ context.Context, _ string, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, fmt.Errorf("FTSSearchNodeAware: not implemented for postgres backend")
}

// VectorSearchNodeAware is not implemented for the postgres backend.
func (s *ObjectStore) VectorSearchNodeAware(_ context.Context, _ []float32, _ storage.ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, fmt.Errorf("VectorSearchNodeAware: not implemented for postgres backend")
}

// objectSelectCols is the SELECT column list (no trailing FROM).
const objectSelectCols = `SELECT
	id, type, subtype, raw_content, content_type,
	metadata, summaries, sections, tags, mentions,
	decisions, tasks, pipeline, source,
	registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
	created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
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
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&obj.CreatedAt, &obj.UpdatedAt, &obj.FTSIndexed, &obj.VectorIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &graphJSON, &sourceKey,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errObjectNotFound
		}
		return nil, fmt.Errorf("scan object: %w", err)
	}
	unmarshalObjectFields(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		lastReinforcedAt, remindAt, remindedAt)
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
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&obj.CreatedAt, &obj.UpdatedAt, &obj.FTSIndexed, &obj.VectorIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &graphJSON, &sourceKey,
	)
	if err != nil {
		return nil, fmt.Errorf("scan object row: %w", err)
	}
	unmarshalObjectFields(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		lastReinforcedAt, remindAt, remindedAt)
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

// scanObjectRowWithEmbedding scans the base cols + embedding column as a pgvector string.
func scanObjectRowWithEmbedding(rows *sql.Rows) (*storage.KnowledgeObject, []float32, error) {
	var obj storage.KnowledgeObject
	var (
		metadataJSON, summariesJSON, sectionsJSON, tagsJSON []byte
		mentionsJSON, decisionsJSON, tasksJSON              []byte
		influencesJSON, pluginsJSON                         []byte
		lastReinforcedAt, remindAt, remindedAt              sql.NullTime
		graphJSON                                           []byte
		embStr                                              sql.NullString
	)
	err := rows.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&obj.CreatedAt, &obj.UpdatedAt, &obj.FTSIndexed, &obj.VectorIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &graphJSON,
		&embStr,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("scan object+embedding: %w", err)
	}
	unmarshalObjectFields(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		lastReinforcedAt, remindAt, remindedAt)
	if len(graphJSON) > 0 {
		g, err := unmarshalGraph(string(graphJSON))
		if err != nil {
			return nil, nil, fmt.Errorf("unmarshal graph: %w", err)
		}
		obj.Graph = g
	}
	vec := parsePgVector(embStr.String)
	return &obj, vec, nil
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
		score                                               float64
	)
	err := rows.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &pluginsJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&obj.CreatedAt, &obj.UpdatedAt, &obj.FTSIndexed, &obj.VectorIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &graphJSON,
		&score,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("scan object+score: %w", err)
	}
	unmarshalObjectFields(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON, pluginsJSON,
		lastReinforcedAt, remindAt, remindedAt)
	if len(graphJSON) > 0 {
		g, err := unmarshalGraph(string(graphJSON))
		if err != nil {
			return nil, 0, fmt.Errorf("unmarshal graph: %w", err)
		}
		obj.Graph = g
	}
	return &obj, score, nil
}

func unmarshalObjectFields(obj *storage.KnowledgeObject,
	metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
	mentionsJSON, decisionsJSON, tasksJSON,
	influencesJSON, pluginsJSON []byte,
	lastReinforcedAt, remindAt, remindedAt sql.NullTime,
) {
	json.Unmarshal(metadataJSON, &obj.Metadata)
	json.Unmarshal(summariesJSON, &obj.Summaries)
	json.Unmarshal(sectionsJSON, &obj.Sections)
	json.Unmarshal(tagsJSON, &obj.Tags)
	var mentionStrs []string
	json.Unmarshal(mentionsJSON, &mentionStrs)
	obj.Mentions = mentions.ParseSlice(mentionStrs)
	json.Unmarshal(decisionsJSON, &obj.Decisions)
	json.Unmarshal(tasksJSON, &obj.Tasks)
	json.Unmarshal(influencesJSON, &obj.RegistryInfluences)
	json.Unmarshal(pluginsJSON, &obj.Plugins)
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
}

type objectFields struct {
	metadata, summaries, sections, tags string
	mentions, decisions, tasks          string
	influences, plugins                 string
	embedding                           *string
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
	if len(obj.Embeddings) > 0 {
		s := encodePgVector(obj.Embeddings)
		f.embedding = &s
	}
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

// parsePgVector parses a pgvector string like "[0.1,0.2,0.3]" into []float32.
func parsePgVector(s string) []float32 {
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		var v float32
		fmt.Sscanf(strings.TrimSpace(p), "%g", &v)
		out = append(out, v)
	}
	return out
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
	objectID string, g *storage.ObjectGraph) error {
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
