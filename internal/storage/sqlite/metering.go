package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MeteringStore implements storage.MeteringStore on SQLite.
type MeteringStore struct {
	db *sql.DB
}

// Record inserts one metering event. The event must have a non-empty ID.
func (s *MeteringStore) Record(ctx context.Context, event *storage.MeteringEvent) error {
	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO metering_events (id, registry_name, event_type, namespace, count, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.RegistryName,
		string(event.EventType),
		event.Namespace,
		event.Count,
		occurredAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("record metering event: %w", err)
	}
	return nil
}

// Aggregate returns summed counts per registry_name + event_type, optionally
// filtered by RegistryName, EventType, After, and Before.
func (s *MeteringStore) Aggregate(
	ctx context.Context,
	filter storage.MeteringFilter,
) ([]*storage.MeteringAggregate, error) {
	q := `SELECT registry_name, event_type, COALESCE(SUM(count), 0)
	      FROM metering_events WHERE 1=1`
	args := []any{}
	q, args = applyMeteringFilter(q, args, filter)
	q += ` GROUP BY registry_name, event_type ORDER BY registry_name, event_type`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate metering: %w", err)
	}
	defer rows.Close()

	var out []*storage.MeteringAggregate
	for rows.Next() {
		var a storage.MeteringAggregate
		var et string
		if err := rows.Scan(&a.RegistryName, &et, &a.Total); err != nil {
			return nil, fmt.Errorf("scan aggregate: %w", err)
		}
		a.EventType = storage.MeteringEventType(et)
		out = append(out, &a)
	}
	return out, rows.Err()
}

// List returns raw metering events matching the filter.
func (s *MeteringStore) List(
	ctx context.Context,
	filter storage.MeteringFilter,
) ([]*storage.MeteringEvent, error) {
	q := `SELECT id, registry_name, event_type, COALESCE(namespace,''), count, occurred_at
	      FROM metering_events WHERE 1=1`
	args := []any{}
	q, args = applyMeteringFilter(q, args, filter)
	q += ` ORDER BY occurred_at DESC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list metering: %w", err)
	}
	defer rows.Close()

	var out []*storage.MeteringEvent
	for rows.Next() {
		var e storage.MeteringEvent
		var et, ts string
		if err := rows.Scan(&e.ID, &e.RegistryName, &et, &e.Namespace, &e.Count, &ts); err != nil {
			return nil, fmt.Errorf("scan metering event: %w", err)
		}
		e.EventType = storage.MeteringEventType(et)
		t, err := time.Parse(time.RFC3339, ts)
		if err == nil {
			e.OccurredAt = t
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// applyMeteringFilter appends WHERE clauses for the filter fields.
func applyMeteringFilter(q string, args []any, f storage.MeteringFilter) (string, []any) {
	if f.RegistryName != "" {
		q += " AND registry_name = ?"
		args = append(args, f.RegistryName)
	}
	if f.EventType != "" {
		q += " AND event_type = ?"
		args = append(args, string(f.EventType))
	}
	if !f.After.IsZero() {
		q += " AND occurred_at >= ?"
		args = append(args, f.After.Format(time.RFC3339))
	}
	if !f.Before.IsZero() {
		q += " AND occurred_at <= ?"
		args = append(args, f.Before.Format(time.RFC3339))
	}
	return q, args
}
