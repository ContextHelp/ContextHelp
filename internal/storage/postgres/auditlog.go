package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type AuditStore struct{ db *sql.DB }

func (s *AuditStore) Append(ctx context.Context, e *storage.AuditEntry) error {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("audit append: marshal payload: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, event_type, object_id, actor, payload, created_at)
         VALUES ($1, $2, $3, $4, $5, $6)`,
		e.ID, e.EventType, e.ObjectID, e.Actor,
		payload, e.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("audit append: %w", err)
	}
	return nil
}

func (s *AuditStore) List(ctx context.Context, f storage.AuditFilter) ([]*storage.AuditEntry, int, error) {
	where := "1=1"
	var args []any
	idx := 1

	if f.ObjectID != "" {
		where += fmt.Sprintf(" AND object_id=$%d", idx)
		args = append(args, f.ObjectID)
		idx++
	}
	if f.EventType != "" {
		where += fmt.Sprintf(" AND event_type=$%d", idx)
		args = append(args, f.EventType)
		idx++
	}
	if f.Actor != "" {
		where += fmt.Sprintf(" AND actor=$%d", idx)
		args = append(args, f.Actor)
		idx++
	}
	if !f.After.IsZero() {
		where += fmt.Sprintf(" AND created_at > $%d", idx)
		args = append(args, f.After.UTC())
		idx++
	}
	if !f.Before.IsZero() {
		where += fmt.Sprintf(" AND created_at < $%d", idx)
		args = append(args, f.Before.UTC())
		idx++
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE "+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

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

func (s *AuditStore) GetObjectHistory(ctx context.Context, objectID string) ([]*storage.AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, object_id, actor, payload, created_at
         FROM audit_log WHERE object_id=$1 ORDER BY created_at ASC`,
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
	var payloadJSON []byte
	if err := rows.Scan(&e.ID, &e.EventType, &e.ObjectID, &e.Actor, &payloadJSON, &e.CreatedAt); err != nil {
		return nil, err
	}
	if len(payloadJSON) > 0 {
		_ = json.Unmarshal(payloadJSON, &e.Payload)
	}
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	return &e, nil
}
