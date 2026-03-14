package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func (s *ReminderStore) Create(ctx context.Context, reminder *storage.SystemReminder) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO system_reminders (
		id, type, title, message, source, action_url, dismissed, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		reminder.ID, reminder.Type, reminder.Title, reminder.Message,
		reminder.Source, reminder.ActionURL, reminder.Dismissed,
		reminder.CreatedAt.UTC(), reminder.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create reminder: %w", err)
	}
	return nil
}

func (s *ReminderStore) Get(ctx context.Context, id string) (*storage.SystemReminder, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, title, message, source, action_url, dismissed, created_at, updated_at
	FROM system_reminders WHERE id = $1`, id)
	return scanReminder(row)
}

func (s *ReminderStore) List(ctx context.Context, activeOnly bool) ([]*storage.SystemReminder, int, error) {
	var conditions []string

	if activeOnly {
		conditions = append(conditions, "dismissed = FALSE")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_reminders "+where).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count reminders: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT
		id, type, title, message, source, action_url, dismissed, created_at, updated_at
	FROM system_reminders `+where+" ORDER BY created_at DESC")
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
	return reminders, total, rows.Err()
}

func (s *ReminderStore) Dismiss(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE system_reminders SET
		dismissed = TRUE, updated_at = $1
	WHERE id = $2`, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("dismiss reminder: %w", err)
	}
	return nil
}

func scanReminder(row interface{ Scan(...any) error }) (*storage.SystemReminder, error) {
	var r storage.SystemReminder
	err := row.Scan(
		&r.ID, &r.Type, &r.Title, &r.Message,
		&r.Source, &r.ActionURL, &r.Dismissed,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("reminder not found")
		}
		return nil, fmt.Errorf("scan reminder: %w", err)
	}
	return &r, nil
}
