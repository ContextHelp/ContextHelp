package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SetReminder sets remind_at on the object (clears reminded_at).
func (s *ObjectStore) SetReminder(ctx context.Context, id string, at time.Time) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE objects SET remind_at=$1, reminded_at=NULL, updated_at=$2 WHERE id=$3`,
		at.UTC(), time.Now().UTC(), id,
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
		`UPDATE objects SET remind_at=NULL, reminded_at=NULL, updated_at=$1 WHERE id=$2`,
		time.Now().UTC(), id,
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
		`UPDATE objects SET reminded_at=$1, updated_at=$2 WHERE id=$3`,
		now.UTC(), now.UTC(), id,
	)
	return err
}

// ListDueReminders returns objects whose remind_at <= now and reminded_at IS NULL.
func (s *ObjectStore) ListDueReminders(ctx context.Context, now time.Time) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx, objectSelectCols+`
	FROM objects
	WHERE remind_at IS NOT NULL AND remind_at <= $1 AND reminded_at IS NULL
	ORDER BY remind_at ASC`, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("list due reminders: %w", err)
	}
	defer rows.Close()
	return collectPgReminderRows(rows)
}

// ListPendingReminders returns all objects with a non-null remind_at.
func (s *ObjectStore) ListPendingReminders(ctx context.Context) ([]*storage.KnowledgeObject, error) {
	rows, err := s.db.QueryContext(ctx, objectSelectCols+`
	FROM objects
	WHERE remind_at IS NOT NULL
	ORDER BY remind_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list pending reminders: %w", err)
	}
	defer rows.Close()
	return collectPgReminderRows(rows)
}

func collectPgReminderRows(rows *sql.Rows) ([]*storage.KnowledgeObject, error) {
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
