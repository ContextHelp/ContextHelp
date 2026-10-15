package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

// MeteringStore implements storage.MeteringStore on Postgres, over the
// metering_events table (migration 32).
type MeteringStore struct {
	db *sql.DB
}

// meteringLockClass is the first key of the two-key transaction advisory
// lock RecordCapped takes per (registry_name, event_type); the second key
// hashes the pair. The two-key space does not overlap the single-key
// space the migration lock uses. Arbitrary but stable: "metr" in ASCII.
const meteringLockClass int32 = 0x6d657472

// Record inserts one metering event. The event must have a non-empty ID.
func (s *MeteringStore) Record(ctx context.Context, event *storage.MeteringEvent) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO metering_events (id, registry_name, event_type, namespace, count, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		event.ID, event.RegistryName, string(event.EventType), event.Namespace, event.Count,
		meteringOccurredAt(event))
	if err != nil {
		return fmt.Errorf("record metering event: %w", err)
	}
	return nil
}

// RecordCapped atomically appends event only when the summed count for
// (event.RegistryName, event.EventType) since periodStart stays below
// limit. Under READ COMMITTED an INSERT ... SELECT guarded by a SUM does
// not serialize concurrent recorders (each sees the others' rows only
// after they commit), so the transaction first takes an advisory lock on
// the pair: recorders of one registry and event type queue on it, and
// each check sees every insert committed before it. Returns whether the
// event was recorded. A zero periodStart means all-time.
func (s *MeteringStore) RecordCapped(
	ctx context.Context,
	event *storage.MeteringEvent,
	periodStart time.Time,
	limit int,
) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("record capped metering event: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1::integer, hashtext($2::text || '/' || $3::text))`,
		meteringLockClass, event.RegistryName, string(event.EventType)); err != nil {
		return false, fmt.Errorf("record capped metering event: lock: %w", err)
	}

	args := []any{
		event.ID, event.RegistryName, string(event.EventType), event.Namespace, event.Count,
		meteringOccurredAt(event), limit,
	}
	cond := ""
	if !periodStart.IsZero() {
		args = append(args, periodStart.UTC())
		cond = " AND occurred_at >= $" + strconv.Itoa(len(args))
	}
	// #nosec G202 -- cond is empty or the fixed occurred_at predicate; its value is bound in args.
	res, err := tx.ExecContext(ctx, `
		INSERT INTO metering_events (id, registry_name, event_type, namespace, count, occurred_at)
		SELECT $1::text, $2::text, $3::text, $4::text, $5::integer, $6::timestamptz
		WHERE (SELECT COALESCE(SUM(count), 0) FROM metering_events
		       WHERE registry_name = $2 AND event_type = $3`+cond+`) < $7::bigint`,
		args...)
	if err != nil {
		return false, fmt.Errorf("record capped metering event: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("record capped metering event: rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("record capped metering event: commit: %w", err)
	}
	return n == 1, nil
}

// Aggregate returns summed counts per registry_name + event_type, optionally
// filtered by RegistryName, EventType, After, and Before.
func (s *MeteringStore) Aggregate(
	ctx context.Context,
	filter storage.MeteringFilter,
) ([]*storage.MeteringAggregate, error) {
	where, args := meteringFilterSQL(filter)
	// Byte-order collation so the order matches SQLite's BINARY collation
	// whatever the database locale.
	// #nosec G202 -- where joins fixed column predicates whose values are bound placeholders in args.
	rows, err := s.db.QueryContext(ctx, `
		SELECT registry_name, event_type, COALESCE(SUM(count), 0)
		  FROM metering_events`+where+`
		 GROUP BY registry_name, event_type
		 ORDER BY registry_name COLLATE "C", event_type COLLATE "C"`, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate metering: %w", err)
	}
	defer rows.Close()

	return rowscan.All(rows, func(rows *sql.Rows) (*storage.MeteringAggregate, error) {
		var a storage.MeteringAggregate
		var et string
		if err := rows.Scan(&a.RegistryName, &et, &a.Total); err != nil {
			return nil, fmt.Errorf("scan aggregate: %w", err)
		}
		a.EventType = storage.MeteringEventType(et)
		return &a, nil
	})
}

// List returns raw metering events matching the filter, newest first.
func (s *MeteringStore) List(
	ctx context.Context,
	filter storage.MeteringFilter,
) ([]*storage.MeteringEvent, error) {
	where, args := meteringFilterSQL(filter)
	// #nosec G202 -- where joins fixed column predicates whose values are bound placeholders in args.
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, registry_name, event_type, namespace, count, occurred_at
		  FROM metering_events`+where+`
		 ORDER BY occurred_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list metering: %w", err)
	}
	defer rows.Close()

	return rowscan.All(rows, func(rows *sql.Rows) (*storage.MeteringEvent, error) {
		var e storage.MeteringEvent
		var et string
		if err := rows.Scan(&e.ID, &e.RegistryName, &et, &e.Namespace, &e.Count, &e.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan metering event: %w", err)
		}
		e.EventType = storage.MeteringEventType(et)
		e.OccurredAt = e.OccurredAt.UTC()
		return &e, nil
	})
}

// meteringOccurredAt is the event's occurred_at in UTC, now when unset.
func meteringOccurredAt(event *storage.MeteringEvent) time.Time {
	if event.OccurredAt.IsZero() {
		return time.Now().UTC()
	}
	return event.OccurredAt.UTC()
}

// meteringFilterSQL builds the WHERE clause for the filter fields, with
// numbered placeholders, and its arguments in placeholder order.
func meteringFilterSQL(f storage.MeteringFilter) (string, []any) {
	where := ""
	var args []any
	add := func(pred string, v any) {
		args = append(args, v)
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		where += pred + " $" + strconv.Itoa(len(args))
	}
	if f.RegistryName != "" {
		add("registry_name =", f.RegistryName)
	}
	if f.EventType != "" {
		add("event_type =", string(f.EventType))
	}
	if !f.After.IsZero() {
		add("occurred_at >=", f.After.UTC())
	}
	if !f.Before.IsZero() {
		add("occurred_at <=", f.Before.UTC())
	}
	return where, args
}
