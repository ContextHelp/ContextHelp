package sqlite

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
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
	uri "hop.top/cite/scheme"
)

// errObjectNotFound is returned by scanObject when no row matches.
// Wraps storage.ErrNotFound so callers outside this driver can
// recognise a missing row without matching on message text.
var errObjectNotFound = fmt.Errorf("object %w", storage.ErrNotFound)

type ObjectStore struct {
	db *sql.DB
	// emb is the per-model embedding store VectorSearch ranks through.
	emb storage.EmbeddingStore
	// scope, when set, is the restricted viewer's visibility every read
	// ANDs into its SQL (see scope.go); nil is the driver's own store.
	scope *storage.Scope
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
		obj.Status = storage.ObjectStatusActive
	}

	// An empty TextContent defaults to the body the embedding step used
	// (projection.BodyText), so stored text and embedded text agree.
	obj.TextContent = projection.BodyText(obj)

	// Derive FTS bodies from projection — single source of truth for
	// indexed text: the raw body and its masked shadow (ADR-080 §7).
	index := projection.ProjectIndex(obj)
	projectedFTSBody := index.FTSBody

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
		registry_influences, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, projected_fts_body, source_key,
		projection_version, classes, projected_fts_body_masked
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		obj.ID, obj.Type, obj.Subtype, obj.RawContent, obj.ContentType, obj.TextContent,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, obj.Pipeline, obj.Source,
		f.influences, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.CreatedAt.Format(time.RFC3339), obj.UpdatedAt.Format(time.RFC3339),
		boolToInt(obj.FTSIndexed), obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt, obj.ProfileID, graphJSON, projectedFTSBody, obj.SourceKey,
		indexsig.ProjectionVersion, f.classes, index.MaskedFTSBody,
	)
	if err != nil {
		if errors.Is(constraintErr(err), storage.ErrAlreadyExists) {
			return fmt.Errorf("create object: %w: %w", storage.ErrAlreadyExists, err)
		}
		return fmt.Errorf("create object: %w", err)
	}
	if err := s.upsertObjectNodesTx(ctx, tx, obj.ID, obj.Graph); err != nil {
		return fmt.Errorf("create object nodes: %w", err)
	}
	if err := insertObjectFTSTx(ctx, tx, obj.ID, index); err != nil {
		return fmt.Errorf("create object fts index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create object: commit: %w", err)
	}
	return nil
}

// insertObjectFTSTx indexes the object's projected bodies: the raw one
// in objects_fts and the masked one in objects_fts_masked (ADR-080 §7).
// An empty body is not indexed (see deleteObjectFTSTx).
func insertObjectFTSTx(ctx context.Context, tx *sql.Tx, id string, index storage.IndexProjection) error {
	if index.FTSBody != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO objects_fts(rowid, id, projected_fts_body) VALUES ((SELECT rowid FROM objects WHERE id = ?), ?, ?)`,
			id, id, index.FTSBody); err != nil {
			return err
		}
	}
	if index.MaskedFTSBody != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO objects_fts_masked(rowid, id, projected_fts_body_masked) VALUES ((SELECT rowid FROM objects WHERE id = ?), ?, ?)`,
			id, id, index.MaskedFTSBody); err != nil {
			return fmt.Errorf("masked: %w", err)
		}
	}
	return nil
}

func (s *ObjectStore) Get(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	// #nosec G202 -- andScope is the viewer's compiled predicate; its values are bound in scopeArgs.
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key, change_seq, classes
	FROM objects WHERE id = ?`+s.andScope(), append([]any{id}, s.scopeArgs()...)...)
	obj, err := scanObject(row)
	if err != nil {
		return nil, err
	}
	obj.AttachmentIDs, err = s.listAttachmentIDs(ctx, id)
	return obj, err
}

// andScope is " AND (<scope>)" for a scoped store, empty otherwise: a
// hidden row answers exactly as a missing one.
func (s *ObjectStore) andScope() string {
	term, _ := scopeTerm(s.scope)
	if term == "" {
		return ""
	}
	return " AND " + term
}

// scopeArgs are the scope's bound arguments, none without a scope.
func (s *ObjectStore) scopeArgs() []any {
	_, args := scopeTerm(s.scope)
	return args
}

// listAttachmentIDs returns the IDs of all attachments for objectID.
func (s *ObjectStore) listAttachmentIDs(ctx context.Context, objectID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM attachments WHERE object_id = ? ORDER BY created_at ASC`, objectID)
	if err != nil {
		return nil, fmt.Errorf("list attachment ids: %w", err)
	}
	defer rows.Close()
	return rowscan.All(rows, func(rows *sql.Rows) (string, error) {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		return id, nil
	})
}

// GetByContentHash returns the object of profileID with content hash
// hash: dedup stays inside a profile (ADR-080 §4).
func (s *ObjectStore) GetByContentHash(ctx context.Context, hash, profileID string) (*storage.KnowledgeObject, error) {
	if hash == "" {
		return nil, nil
	}
	// #nosec G202 -- andScope is the viewer's compiled predicate; its values are bound in scopeArgs.
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key, change_seq, classes
	FROM objects WHERE content_hash = ? AND profile_id = ?`+s.andScope()+` LIMIT 1`,
		append([]any{hash, profileID}, s.scopeArgs()...)...)
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
	// #nosec G202 -- andScope is the viewer's compiled predicate; its values are bound in scopeArgs.
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key, change_seq, classes
	FROM objects WHERE source_key = ?`+s.andScope()+` LIMIT 1`, append([]any{key}, s.scopeArgs()...)...)
	obj, err := scanObject(row)
	if errors.Is(err, errObjectNotFound) {
		return nil, nil
	}
	return obj, err
}

func (s *ObjectStore) List(ctx context.Context, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	countSQL, pageSQL, args := objectListSQL(filter, s.scope)

	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count objects: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, pageSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list objects: %w", err)
	}
	defer rows.Close()

	objects, err := rowscan.All(rows, scanObjectFromRows)
	if err != nil {
		return nil, 0, err
	}
	return objects, total, nil
}

// objectListSQL is the pair of statements List runs over filter, the exact
// COUNT(*) of every matching object and the ordered page, both binding
// args in order. The count takes the predicates as they are; the page
// takes them with the default status scope hinted (objectListConditions)
// so the planner reaches the page through the sort column's index.
func objectListSQL(filter storage.ObjectFilter, scope *storage.Scope) (countSQL, pageSQL string, args []any) {
	conditions, args := objectListConditions(filter, false, scope)
	countSQL = "SELECT COUNT(*) FROM objects" + whereClause(conditions)

	pageConditions, _ := objectListConditions(filter, true, scope)
	// #nosec G201 -- whereClause joins objectListConditions' fixed predicates (values bound in args); the order is a ListOrder literal.
	pageSQL = fmt.Sprintf(`SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key, change_seq, classes
	FROM objects%s ORDER BY %s`, whereClause(pageConditions), filter.ListOrder())
	if filter.Limit > 0 {
		pageSQL += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		pageSQL += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}
	return countSQL, pageSQL, args
}

// whereClause joins conditions into a WHERE clause, leading space
// included, or is empty when there are none.
func whereClause(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conditions, " AND ")
}

// objectListConditions turns filter into List's WHERE predicates and their
// bound arguments, in placeholder order. The count and the page take the
// same predicates except for the default status scope on the page, which
// is wrapped in likely(): SQLite plans without statistics, so it takes
// status = 'active', true for nearly every row, as selective, fetches
// every active row through idx_objects_status and sorts them for one
// page. The hint says the predicate keeps most rows, and the planner
// walks the sort column's index (idx_objects_created_at or
// idx_objects_updated_at) and stops at the page instead, while a
// selective filter such as profile_id still wins its own index. The
// count keeps the plain predicate: with nothing to order, the covering
// walk of idx_objects_status is its cheapest plan, and with another
// filter it stays as it was. Any other status is a small set and is
// never hinted. A scope's predicate goes last, so the count and the
// page of a restricted viewer are computed inside the scoped query.
func objectListConditions(filter storage.ObjectFilter, page bool, scope *storage.Scope) ([]string, []any) {
	var conditions []string
	var args []any

	if st, ok := filter.StatusScope(); ok {
		term := "status = ?"
		if page && st == storage.ObjectStatusActive {
			term = "likely(status = ?)"
		}
		conditions = append(conditions, term)
		args = append(args, st)
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
	// created_at holds RFC 3339 text in whatever offset the writer used
	// (the daemon stamps its local zone), so the bounds compare as
	// instants: datetime() normalizes both sides to UTC. A plain text
	// comparison is off by the offset difference.
	if filter.After != nil {
		conditions = append(conditions, "datetime(created_at) >= datetime(?)")
		args = append(args, filter.After.Format(time.RFC3339))
	}
	if filter.Before != nil {
		conditions = append(conditions, "datetime(created_at) <= datetime(?)")
		args = append(args, filter.Before.Format(time.RFC3339))
	}

	if filter.ChangedAfter != nil {
		conditions = append(conditions, "change_seq > ?")
		args = append(args, *filter.ChangedAfter)
	}

	// Metadata facet filters (US-0407).
	mc, ma := metadataFacetConditionsSQLite(filter)
	conditions = append(conditions, mc...)
	args = append(args, ma...)

	if term, sargs := scopeTerm(scope); term != "" {
		conditions = append(conditions, term)
		args = append(args, sargs...)
	}
	return conditions, args
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

	// Derive FTS bodies from projection — single source of truth for
	// indexed text: the raw body and its masked shadow (ADR-080 §7).
	index := projection.ProjectIndex(obj)
	projectedFTSBody := index.FTSBody

	if projectedFTSBody != "" {
		obj.FTSIndexed = true
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update object: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Drop the old FTS entries while the content row still holds the old
	// bodies; the new bodies are indexed after the UPDATE below.
	if err := deleteObjectFTSTx(ctx, tx, obj.ID); err != nil {
		return fmt.Errorf("update object fts index: %w", err)
	}

	result, err := tx.ExecContext(ctx, `UPDATE objects SET
		type=?, subtype=?, raw_content=?, content_type=?, text_content=?,
		metadata=?, summaries=?, sections=?, tags=?, mentions=?,
		decisions=?, tasks=?, pipeline=?, source=?,
		registry_influences=?, content_hash=?, reinforcement_count=?, last_reinforced_at=?,
		updated_at=?, fts_indexed=?, status=?, inbox_note=?,
		remind_at=?, reminded_at=?, profile_id=?, graph_json=?, projected_fts_body=?, source_key=?,
		projection_version=?, classes=?, projected_fts_body_masked=?
	WHERE id=?`,
		obj.Type, obj.Subtype, obj.RawContent, obj.ContentType, obj.TextContent,
		f.metadata, f.summaries, f.sections, f.tags, f.mentions,
		f.decisions, f.tasks, obj.Pipeline, obj.Source,
		f.influences, obj.ContentHash, obj.ReinforcementCount, f.lastReinforcedAt,
		obj.UpdatedAt.Format(time.RFC3339),
		boolToInt(obj.FTSIndexed), obj.Status, obj.InboxNote,
		f.remindAt, f.remindedAt, obj.ProfileID, graphJSON, projectedFTSBody, obj.SourceKey,
		indexsig.ProjectionVersion, f.classes, index.MaskedFTSBody, obj.ID,
	)
	if err != nil {
		return fmt.Errorf("update object: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("update object %s: %w", obj.ID, storage.ErrNotFound)
	}
	if err := s.upsertObjectNodesTx(ctx, tx, obj.ID, obj.Graph); err != nil {
		return fmt.Errorf("update object nodes: %w", err)
	}
	if err := insertObjectFTSTx(ctx, tx, obj.ID, index); err != nil {
		return fmt.Errorf("update object fts index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update object: commit: %w", err)
	}
	return nil
}

// TransitionStatus implements storage.ObjectStore. Status is not part of
// the FTS projection, so the index is left alone.
func (s *ObjectStore) TransitionStatus(ctx context.Context, id, from, to string, at time.Time) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE objects SET status=?, updated_at=? WHERE id=? AND status=?`,
		to, at.Format(time.RFC3339), id, from)
	if err != nil {
		return fmt.Errorf("transition object %s to %s: %w", id, to, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("transition object %s to %s: %w", id, to, err)
	}
	if n == 0 {
		return fmt.Errorf("object %s with status %s: %w", id, from, storage.ErrNotFound)
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
	// Attachments reference the row without a cascade and foreign keys
	// are on: they go first, in the same transaction, or the row's
	// delete fails. Their data is inline; no blob refers to them.
	if _, err := tx.ExecContext(ctx, "DELETE FROM attachments WHERE object_id = ?", id); err != nil {
		return fmt.Errorf("delete object attachments: %w", err)
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM objects WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("delete object %s: %w", id, storage.ErrNotFound)
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
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM objects_fts WHERE rowid = (
			SELECT rowid FROM objects WHERE id = ? AND projected_fts_body != ''
		)`, id); err != nil {
		return err
	}
	// The masked shadow index (ADR-080 §7) is maintained the same way
	// over projected_fts_body_masked.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM objects_fts_masked WHERE rowid = (
			SELECT rowid FROM objects WHERE id = ? AND projected_fts_body_masked != ''
		)`, id); err != nil {
		return fmt.Errorf("masked: %w", err)
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

	// Write first: a deferred transaction that reads before writing holds a
	// snapshot, and if another writer commits meanwhile the upgrade fails with
	// SQLITE_BUSY immediately, bypassing busy_timeout. Taking the write lock
	// up front makes concurrent reinforcements queue instead of failing, and
	// the read below then sees the latest committed tags and mentions.
	// Dedup stays inside a profile (ADR-080 §4): only the row of
	// mergeData's profile is reinforced; the same content under another
	// profile is another object, and no row here is sql.ErrNoRows.
	now := time.Now().Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `UPDATE objects SET
			reinforcement_count = reinforcement_count + 1,
			last_reinforced_at = ?
		WHERE content_hash = ? AND profile_id = ?`, now, hash, mergeData.ProfileID)
	if err != nil {
		return "", fmt.Errorf("reinforce: update: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return "", fmt.Errorf("reinforce: update: %w", err)
	} else if n == 0 {
		return "", fmt.Errorf("reinforce: lookup: %w", sql.ErrNoRows)
	}

	var obj storage.KnowledgeObject
	var (
		tagsJSON, mentionsJSON string
		lastReinforcedAt       sql.NullString
	)
	err = tx.QueryRowContext(ctx, `SELECT id, tags, mentions, last_reinforced_at
		FROM objects WHERE content_hash = ? AND profile_id = ?`, hash, mergeData.ProfileID).Scan(
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

	merged := mergeTags(obj.Tags, mergeData.Tags)
	mergedTagsJSON, _ := json.Marshal(merged)

	mergedMentionStrs := mergeStrings(mentionsToStrings(obj.Mentions), mentionsToStrings(mergeData.Mentions))
	mergedMentionsJSON, _ := json.Marshal(mergedMentionStrs)

	// Optionally update content if we now have more data (e.g. from text to url fetch)
	contentUpdate := ""
	contentArgs := []any{string(mergedTagsJSON), string(mergedMentionsJSON), now}
	if mergeData.RawContent != "" && mergeData.RawContent != hash {
		// text_content takes the default Create applies
		// (projection.BodyText), so a blob:// ref never becomes text.
		contentUpdate = ", raw_content = ?, text_content = ?"
		contentArgs = append(contentArgs, mergeData.RawContent, projection.BodyText(mergeData))
	}
	contentArgs = append(contentArgs, hash, mergeData.ProfileID)

	// fts_indexed is not cleared here: the row is re-indexed below, which
	// sets it when the new body is non-empty. Clearing it misreported
	// reinforced (deduplicated) objects as unindexed.
	// #nosec G201 -- contentUpdate is empty or a fixed SET fragment; its values are bound in contentArgs.
	query := fmt.Sprintf(`
		UPDATE objects SET
			tags = ?,
			mentions = ?,
			updated_at = ?
			%s
		WHERE content_hash = ? AND profile_id = ?
	`, contentUpdate)

	_, err = tx.ExecContext(ctx, query, contentArgs...)
	if err != nil {
		return "", fmt.Errorf("reinforce: update: %w", err)
	}
	// The text, tags and mentions just written feed the FTS body:
	// re-index the row as Update does, or search keeps matching what
	// it held before. (content_hash, profile_id) is unique: one row.
	if err := reindexRowTx(ctx, tx, obj.ID); err != nil {
		return "", fmt.Errorf("reinforce: reindex: %w", err)
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

func (s *ObjectStore) ListBySQL(ctx context.Context, where string, args []any, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error) {
	// List's predicates (status scope first) follow the caller's so its ?
	// placeholders keep their positions: SQLite binds args in order.
	conditions, condArgs := objectListConditions(filter, false, s.scope)
	pageConditions, _ := objectListConditions(filter, true, s.scope)
	if where != "" {
		conditions = append([]string{"(" + where + ")"}, conditions...)
		pageConditions = append([]string{"(" + where + ")"}, pageConditions...)
	}
	args = append(append([]any(nil), args...), condArgs...)

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM objects"+whereClause(conditions), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count by sql: %w", err)
	}

	// where is a compiled RSQL predicate (internal/search): literals are
	// already bound as ? placeholders in args, only operators and column
	// names reach the SQL text. whereClause adds objectListConditions'
	// fixed fragments (values in args; the page's default status scope
	// carries its likely() hint), order is a ListOrder literal, and
	// LIMIT/OFFSET are bound below.
	// #nosec G202 -- see above; caller values travel in args, not the string.
	query := `SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key, change_seq, classes
	FROM objects` + whereClause(pageConditions) + " ORDER BY " + filter.ListOrder()
	pageArgs := args
	if filter.Limit > 0 {
		query += " LIMIT ?"
		pageArgs = append(pageArgs, filter.Limit)
	}
	if filter.Offset > 0 {
		// SQLite requires LIMIT before OFFSET; supply an unbounded LIMIT
		// when the caller asked only for an offset.
		if filter.Limit <= 0 {
			query += " LIMIT -1"
		}
		query += " OFFSET ?"
		pageArgs = append(pageArgs, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list by sql: %w", err)
	}
	defer rows.Close()

	objects, err := rowscan.All(rows, scanObjectFromRows)
	if err != nil {
		return nil, 0, err
	}
	return objects, total, nil
}

// scanObject scans a single row into a KnowledgeObject.
func scanObject(row *sql.Row) (*storage.KnowledgeObject, error) {
	obj, err := scanObjectRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errObjectNotFound
		}
		return nil, fmt.Errorf("scan object: %w", err)
	}
	return obj, nil
}

func scanObjectFromRows(rows *sql.Rows) (*storage.KnowledgeObject, error) {
	obj, err := scanObjectRow(rows)
	if err != nil {
		return nil, fmt.Errorf("scan object row: %w", err)
	}
	return obj, nil
}

// scanObjectRow scans one object row in the order of the object select
// list (see Get). sql.ErrNoRows passes through for scanObject to map.
func scanObjectRow(row interface{ Scan(...any) error }) (*storage.KnowledgeObject, error) {
	var obj storage.KnowledgeObject
	var (
		metadataJSON, summariesJSON, sectionsJSON, tagsJSON string
		mentionsJSON, decisionsJSON, tasksJSON              string
		influencesJSON, classesJSON                         string
		createdAt, updatedAt                                string
		ftsIndexed                                          int
		lastReinforcedAt, remindAt, remindedAt              sql.NullString
		graphJSON                                           sql.NullString
		sourceKey                                           sql.NullString
	)

	err := row.Scan(
		&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
		&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
		&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
		&influencesJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
		&createdAt, &updatedAt, &ftsIndexed, &obj.Status, &obj.InboxNote,
		&remindAt, &remindedAt, &obj.ProfileID, &graphJSON, &sourceKey, &obj.ChangeSeq, &classesJSON,
	)
	if err != nil {
		return nil, err
	}

	if err := unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
		mentionsJSON, decisionsJSON, tasksJSON, influencesJSON,
		createdAt, updatedAt, ftsIndexed, lastReinforcedAt,
		remindAt, remindedAt); err != nil {
		return nil, err
	}
	if obj.Classes, err = storage.ParseClassesJSON(classesJSON); err != nil {
		return nil, err
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
	influencesJSON,
	createdAt, updatedAt string,
	ftsIndexed int,
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
	influences, classes                    string
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
	if f.classes, err = storage.ClassesJSON(obj.Classes); err != nil {
		return f, err
	}

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

// vectorOverfetch is the KNN over-fetch factor: the index is searched for
// limit*vectorOverfetch hits so post-filtering by status, type, subtype,
// pipeline, profile and metadata facets can still fill limit.
const vectorOverfetch = 10

// VectorSearch ranks objects through q.ModelID's per-model index: a KNN
// over-fetch through EmbeddingStore.Search (chunks already collapsed to the
// best one per object), then hydrate and filter in Go. Metadata["score"] is
// 1 - cosine distance.
func (s *ObjectStore) VectorSearch(ctx context.Context, q storage.VectorQuery, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	if len(q.Vector) == 0 {
		return nil, fmt.Errorf("vector search: empty query vector")
	}
	// Cosine distance to a zero-magnitude query is undefined for every
	// row: nothing can rank, so nothing is returned.
	if isZeroVector(q.Vector) {
		return nil, nil
	}
	if s.emb == nil {
		return nil, fmt.Errorf("vector search: no embedding store")
	}
	// A restricted viewer ranks the masked variant over visible objects
	// only, through the embedding store bound to its scope (ADR-080
	// §7); Get below is scoped too, so a hit hidden between the two
	// reads is skipped like a deleted one.
	emb := s.emb
	if s.scope != nil {
		sc, ok := emb.(storage.EmbeddingScoper)
		if !ok {
			return nil, fmt.Errorf("vector search: embedding store %T: %w", emb, storage.ErrNoScopedReads)
		}
		emb = sc.Scoped(*s.scope)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = q.TopK
	}
	if limit <= 0 {
		limit = 50
	}

	hits, err := emb.Search(ctx, storage.VectorQuery{
		ModelID: q.ModelID,
		Vector:  q.Vector,
		TopK:    limit * vectorOverfetch,
		Variant: q.Variant,
	})
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	out := make([]*storage.KnowledgeObject, 0, min(limit, len(hits)))
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		obj, err := s.Get(ctx, h.ObjectID)
		if err != nil {
			// Object may have been deleted between index and fetch; skip.
			continue
		}
		if !vectorHitMatches(obj, filter) {
			continue
		}
		if obj.Metadata == nil {
			obj.Metadata = make(map[string]any)
		}
		obj.Metadata["score"] = 1.0 - h.Distance
		out = append(out, obj)
	}
	return out, nil
}

// vectorHitMatches reports whether a hydrated KNN hit satisfies the
// filters VectorSearch applies in Go: the status scope, type, subtype,
// pipeline, profile and metadata facets.
func vectorHitMatches(obj *storage.KnowledgeObject, filter storage.ObjectFilter) bool {
	if st, ok := filter.StatusScope(); ok && obj.Status != st {
		return false
	}
	if filter.Type != "" && obj.Type != filter.Type {
		return false
	}
	if filter.Subtype != "" && obj.Subtype != filter.Subtype {
		return false
	}
	if filter.Pipeline != "" && obj.Pipeline != filter.Pipeline {
		return false
	}
	if filter.ProfileID != "" && obj.ProfileID != filter.ProfileID {
		return false
	}
	return matchesMetadataFacets(obj, filter)
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

	// A restricted viewer searches the masked shadow index (ADR-080
	// §7): terms a viewer may not see never match, rank or count.
	fts := "objects_fts"
	if s.scope != nil {
		fts = "objects_fts_masked"
	}
	// #nosec G201 -- fts is one of two literal table names.
	q := fmt.Sprintf(`
		SELECT o.id, o.type, o.subtype, o.raw_content, o.content_type, o.text_content,
		       o.metadata, o.summaries, o.sections, o.tags, o.mentions,
		       o.decisions, o.tasks, o.pipeline, o.source,
		       o.registry_influences, o.content_hash, o.reinforcement_count, o.last_reinforced_at,
		       o.created_at, o.updated_at, o.fts_indexed, o.status, o.inbox_note,
		       o.remind_at, o.reminded_at, o.profile_id, o.graph_json,
		       bm25(%[1]s) AS score
		FROM %[1]s
		JOIN objects o ON %[1]s.id = o.id
		WHERE %[1]s MATCH ?`, fts)
	args := []any{query}

	// The scope is evaluated on each matched row: objects_fts exposes
	// only id and the body, so the predicate's unqualified object
	// columns (a policy names type, subtype, tag, profile, class,
	// source, dates and metadata keys, never id) bind to o. Checking
	// the matches is far cheaper than materializing the visible set.
	if term, sargs := scopeTerm(s.scope); term != "" {
		// #nosec G202 -- term is compiled SQL whose values are bound in sargs.
		q += " AND " + term
		args = append(args, sargs...)
	}

	if st, ok := filter.StatusScope(); ok {
		q += " AND o.status = ?"
		args = append(args, st)
	}
	if filter.Type != "" {
		q += " AND o.type = ?"
		args = append(args, filter.Type)
	}
	if filter.ProfileID != "" {
		q += " AND o.profile_id = ?"
		args = append(args, filter.ProfileID)
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

	return rowscan.All(rows, func(rows *sql.Rows) (*storage.KnowledgeObject, error) {
		var obj storage.KnowledgeObject
		var (
			metadataJSON, summariesJSON, sectionsJSON, tagsJSON string
			mentionsJSON, decisionsJSON, tasksJSON              string
			influencesJSON                                      string
			createdAt, updatedAt                                string
			ftsIndexed                                          int
			lastReinforcedAt, remindAt, remindedAt              sql.NullString
			graphJSON                                           sql.NullString
			score                                               float64
		)
		err := rows.Scan(
			&obj.ID, &obj.Type, &obj.Subtype, &obj.RawContent, &obj.ContentType, &obj.TextContent,
			&metadataJSON, &summariesJSON, &sectionsJSON, &tagsJSON, &mentionsJSON,
			&decisionsJSON, &tasksJSON, &obj.Pipeline, &obj.Source,
			&influencesJSON, &obj.ContentHash, &obj.ReinforcementCount, &lastReinforcedAt,
			&createdAt, &updatedAt, &ftsIndexed, &obj.Status, &obj.InboxNote,
			&remindAt, &remindedAt, &obj.ProfileID, &graphJSON,
			&score,
		)
		if err != nil {
			return nil, fmt.Errorf("fts search scan: %w", err)
		}
		if err := unmarshalObjectJSON(&obj, metadataJSON, summariesJSON, sectionsJSON, tagsJSON,
			mentionsJSON, decisionsJSON, tasksJSON, influencesJSON,
			createdAt, updatedAt, ftsIndexed, lastReinforcedAt,
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
		return &obj, nil
	})
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
	naf storage.NodeAwareFilter,
) ([]*storage.NodeAwareResult, error) {
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

	out := make([]*storage.NodeAwareResult, 0, len(objects))
	for _, obj := range objects {
		if allowedIDs != nil {
			if _, ok := allowedIDs[obj.ID]; !ok {
				continue
			}
		}
		r := &storage.NodeAwareResult{Object: obj}
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
	q storage.VectorQuery,
	filter storage.ObjectFilter,
	naf storage.NodeAwareFilter,
) ([]*storage.NodeAwareResult, error) {
	objects, err := s.VectorSearch(ctx, q, filter)
	if err != nil {
		return nil, err
	}

	allowedIDs, err := s.nodeTypeObjectIDs(ctx, naf.NodeTypes)
	if err != nil {
		return nil, err
	}

	out := make([]*storage.NodeAwareResult, 0, len(objects))
	for _, obj := range objects {
		if allowedIDs != nil {
			if _, ok := allowedIDs[obj.ID]; !ok {
				continue
			}
		}
		r := &storage.NodeAwareResult{Object: obj}
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
