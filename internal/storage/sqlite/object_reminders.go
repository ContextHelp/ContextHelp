package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SetReminder sets remind_at on the object (clears any previous reminded_at).
func (s *ObjectStore) SetReminder(ctx context.Context, id string, at time.Time) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE objects SET remind_at=?, reminded_at=NULL, updated_at=? WHERE id=?`,
		at.Format(time.RFC3339), time.Now().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("set reminder: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s not found", id)
	}
	return nil
}

// ClearReminder removes remind_at and reminded_at from the object.
func (s *ObjectStore) ClearReminder(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE objects SET remind_at=NULL, reminded_at=NULL, updated_at=? WHERE id=?`,
		time.Now().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("clear reminder: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s not found", id)
	}
	return nil
}

// MarkReminded records that the reminder was delivered.
func (s *ObjectStore) MarkReminded(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE objects SET reminded_at=?, updated_at=? WHERE id=?`,
		now.Format(time.RFC3339), now.Format(time.RFC3339), id,
	)
	return err
}

// ListDueReminders returns objects whose remind_at <= now and reminded_at IS NULL.
func (s *ObjectStore) ListDueReminders(ctx context.Context, now time.Time) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx, reminderSelectCols+`
	FROM objects
	WHERE remind_at IS NOT NULL AND remind_at <= ? AND reminded_at IS NULL
	ORDER BY remind_at ASC`, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("list due reminders: %w", err)
	}
	defer rows.Close()
	return collectReminderRows(rows)
}

// ListPendingReminders returns all objects with a non-null remind_at (upcoming + overdue).
func (s *ObjectStore) ListPendingReminders(ctx context.Context) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx, reminderSelectCols+`
	FROM objects
	WHERE remind_at IS NOT NULL
	ORDER BY remind_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list pending reminders: %w", err)
	}
	defer rows.Close()
	return collectReminderRows(rows)
}

// reminderSelectCols is the shared SELECT column list used by reminder queries.
const reminderSelectCols = `SELECT
	id, type, subtype, raw_content, content_type, text_content,
	metadata, summaries, sections, tags, mentions,
	decisions, tasks, pipeline, source,
	registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
	created_at, updated_at, fts_indexed, vector_indexed, status, inbox_note,
	remind_at, reminded_at, profile_id
`

func collectReminderRows(rows *sql.Rows) ([]*storage.KnowledgeObject, error) {
	var objects []*storage.KnowledgeObject
	for rows.Next() {
		obj, err := scanObjectFromRows(rows)
		if err != nil {
			return nil, err
		}
		objects = append(objects, obj)
	}
	return objects, rows.Err()
}
