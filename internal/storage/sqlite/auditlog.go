package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type auditStore struct{ db *sql.DB }

func (s *auditStore) Append(ctx context.Context, e *storage.AuditEntry) error {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("audit append: marshal payload: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, event_type, object_id, actor, payload, created_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, e.EventType, e.ObjectID, e.Actor,
		string(payload),
		e.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("audit append: %w", err)
	}
	return nil
}

func (s *auditStore) List(ctx context.Context, f storage.AuditFilter) ([]*storage.AuditEntry, int, error) {
	where := "1=1"
	var args []interface{}

	if f.ObjectID != "" {
		where += " AND object_id=?"
		args = append(args, f.ObjectID)
	}
	if f.EventType != "" {
		where += " AND event_type=?"
		args = append(args, f.EventType)
	}
	if f.Actor != "" {
		where += " AND actor=?"
		args = append(args, f.Actor)
	}
	if !f.After.IsZero() {
		where += " AND created_at > ?"
		args = append(args, f.After.UTC().Format(time.RFC3339))
	}
	if !f.Before.IsZero() {
		where += " AND created_at < ?"
		args = append(args, f.Before.UTC().Format(time.RFC3339))
	}

	// Count total.
	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE "+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Apply pagination.
	query := "SELECT id, event_type, object_id, actor, payload, created_at FROM audit_log WHERE " +
		where + " ORDER BY created_at ASC"
	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	if f.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*storage.AuditEntry
	for rows.Next() {
		e, err := scanAuditEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *auditStore) GetObjectHistory(ctx context.Context, objectID string) ([]*storage.AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, object_id, actor, payload, created_at
         FROM audit_log WHERE object_id=? ORDER BY created_at ASC`,
		objectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*storage.AuditEntry
	for rows.Next() {
		e, err := scanAuditEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanAuditEntry(rows *sql.Rows) (*storage.AuditEntry, error) {
	var e storage.AuditEntry
	var payloadStr, createdStr string
	if err := rows.Scan(&e.ID, &e.EventType, &e.ObjectID, &e.Actor, &payloadStr, &createdStr); err != nil {
		return nil, err
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	if payloadStr != "" {
		_ = json.Unmarshal([]byte(payloadStr), &e.Payload)
	}
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	return &e, nil
}
