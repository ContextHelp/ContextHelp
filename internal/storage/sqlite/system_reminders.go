package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type ReminderStore struct {
	db *sql.DB
}

func (s *ReminderStore) Create(ctx context.Context, reminder *storage.SystemReminder) error {
	var dismissed int
	if reminder.Dismissed {
		dismissed = 1
	}

	_, err := s.db.ExecContext(ctx, `INSERT INTO system_reminders (
		id, type, title, message, source, action_url, dismissed,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		reminder.ID, reminder.Type, reminder.Title, reminder.Message,
		reminder.Source, reminder.ActionURL, dismissed,
		reminder.CreatedAt.Format(time.RFC3339),
		reminder.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create reminder: %w", err)
	}
	return nil
}

func (s *ReminderStore) Get(ctx context.Context, id string) (*storage.SystemReminder, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, title, message, source, action_url, dismissed,
		created_at, updated_at
	FROM system_reminders WHERE id = ?`, id)
	return scanReminder(row)
}

func (s *ReminderStore) List(ctx context.Context, activeOnly bool) ([]*storage.SystemReminder, int, error) {
	var conditions []string
	var args []any

	if activeOnly {
		conditions = append(conditions, "dismissed = 0")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + joinAnd(conditions)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_reminders "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count reminders: %w", err)
	}

	query := `SELECT
		id, type, title, message, source, action_url, dismissed,
		created_at, updated_at
	FROM system_reminders ` + where + " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query reminders: %w", err)
	}
	defer rows.Close()

	var reminders []*storage.SystemReminder
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, 0, err
		}
		reminders = append(reminders, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate reminders: %w", err)
	}

	return reminders, total, nil
}

func (s *ReminderStore) Dismiss(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE system_reminders SET
		dismissed = 1, updated_at = ?
	WHERE id = ?`,
		time.Now().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("dismiss reminder: %w", err)
	}
	return nil
}

func scanReminder(row interface{ Scan(...any) error }) (*storage.SystemReminder, error) {
	var r storage.SystemReminder
	var dismissed int
	var createdAt, updatedAt string

	err := row.Scan(
		&r.ID, &r.Type, &r.Title, &r.Message,
		&r.Source, &r.ActionURL, &dismissed,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan reminder: %w", err)
	}

	r.Dismissed = dismissed == 1

	r.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}

	r.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	return &r, nil
}
