package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// outboxStore is the event_outbox table (migration 37, ADR-075 §4).
type outboxStore struct{ db *sql.DB }

const defaultOutboxLimit = 500

// outboxLockKey is the advisory lock appends take, so seq order is
// commit order: a reader that has seen seq N never misses an
// uncommitted lower seq. The lock is taken at the end of the mutation,
// just before its insert, and released with its commit.
const outboxLockKey int64 = 0x63747874_6f757462 // "ctxtoutb"

// Outbox returns the driver's event outbox.
func (d *Driver) Outbox() storage.OutboxStore { return &outboxStore{db: d.db} }

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// appendOutboxTx writes the event attached to ctx
// (storage.WithOutboxEvent), if any, through tx: the row commits or
// rolls back with the mutation.
func appendOutboxTx(ctx context.Context, tx execer) error {
	ev := storage.OutboxEventFrom(ctx)
	if ev == nil {
		return nil
	}
	return writeOutbox(ctx, tx, ev)
}

func writeOutbox(ctx context.Context, tx execer, ev *storage.OutboxEvent) error {
	payload, err := marshalOutboxPayload(ev.Payload)
	if err != nil {
		return fmt.Errorf("event outbox: payload: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("event outbox: id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, outboxLockKey); err != nil {
		return fmt.Errorf("event outbox: lock: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO instance_meta (singleton, instance_id) VALUES (1, $1) ON CONFLICT (singleton) DO NOTHING`,
		uuid.NewString()); err != nil {
		return fmt.Errorf("event outbox: instance id: %w", err)
	}
	var instance string
	if err := tx.QueryRowContext(ctx,
		`SELECT instance_id FROM instance_meta WHERE singleton = 1`).Scan(&instance); err != nil {
		return fmt.Errorf("event outbox: instance id: %w", err)
	}
	at := ev.OccurredAt
	if at.IsZero() {
		at = time.Now()
	}
	var seq int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO event_outbox (id, topic, source, occurred_at, origin, actor, instance_id, object_id, payload)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING seq`,
		id.String(), ev.Topic, ev.Source, at.UTC(), ev.Origin, ev.Actor, instance, ev.ObjectID, payload).Scan(&seq)
	if err != nil {
		return fmt.Errorf("event outbox: insert: %w", err)
	}
	ev.Seq, ev.ID, ev.InstanceID = seq, id.String(), instance
	return nil
}

func marshalOutboxPayload(p any) (string, error) {
	switch v := p.(type) {
	case nil:
		return "null", nil
	case json.RawMessage:
		return string(v), nil
	}
	b, err := json.Marshal(p)
	return string(b), err
}

// Append implements storage.OutboxStore.
func (s *outboxStore) Append(ctx context.Context, ev *storage.OutboxEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("event outbox: begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := writeOutbox(ctx, tx, ev); err != nil {
		return err
	}
	return tx.Commit()
}

// Since implements storage.OutboxStore.
func (s *outboxStore) Since(ctx context.Context, after int64, limit int) ([]storage.OutboxRow, error) {
	if limit <= 0 {
		limit = defaultOutboxLimit
	}
	var pruned int64
	err := s.db.QueryRowContext(ctx,
		`SELECT outbox_pruned_through FROM instance_meta WHERE singleton = 1`).Scan(&pruned)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("event outbox: floor: %w", err)
	}
	if after < pruned {
		oldest, oerr := s.Oldest(ctx)
		if oerr != nil {
			return nil, oerr
		}
		return nil, &storage.OutboxExpiredError{Oldest: oldest}
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, id, topic, source, occurred_at, origin, actor, instance_id, object_id, payload
		 FROM event_outbox WHERE seq > $1 ORDER BY seq LIMIT $2`, after, limit)
	if err != nil {
		return nil, fmt.Errorf("event outbox: read: %w", err)
	}
	defer rows.Close()
	var out []storage.OutboxRow
	for rows.Next() {
		var r storage.OutboxRow
		var payload string
		if err := rows.Scan(&r.Seq, &r.ID, &r.Topic, &r.Source, &r.OccurredAt, &r.Origin, &r.Actor,
			&r.InstanceID, &r.ObjectID, &payload); err != nil {
			return nil, fmt.Errorf("event outbox: scan: %w", err)
		}
		r.OccurredAt, r.Payload = r.OccurredAt.UTC(), []byte(payload)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Head implements storage.OutboxStore. The sequence keeps the highest
// seq ever allocated, so it survives pruning.
func (s *outboxStore) Head(ctx context.Context) (int64, error) {
	var head sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT CASE WHEN is_called THEN last_value END FROM event_outbox_seq_seq`).Scan(&head)
	if err != nil {
		return 0, fmt.Errorf("event outbox: head: %w", err)
	}
	return head.Int64, nil
}

// Oldest implements storage.OutboxStore.
func (s *outboxStore) Oldest(ctx context.Context) (int64, error) {
	var oldest sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(seq) FROM event_outbox`).Scan(&oldest); err != nil {
		return 0, fmt.Errorf("event outbox: oldest: %w", err)
	}
	return oldest.Int64, nil
}

// Prune implements storage.OutboxStore.
func (s *outboxStore) Prune(ctx context.Context, cutoff time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("event outbox: prune: begin tx: %w", err)
	}
	defer tx.Rollback()
	// Appends hold this lock until commit; taking it keeps the prune
	// from racing a row that is about to land below the floor.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, outboxLockKey); err != nil {
		return 0, fmt.Errorf("event outbox: prune lock: %w", err)
	}
	var top sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM event_outbox WHERE occurred_at < $1 AND seq < (SELECT MAX(seq) FROM event_outbox)`,
		cutoff.UTC()).Scan(&top); err != nil {
		return 0, fmt.Errorf("event outbox: prune: %w", err)
	}
	if !top.Valid {
		return 0, nil
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM event_outbox WHERE seq <= $1`, top.Int64)
	if err != nil {
		return 0, fmt.Errorf("event outbox: prune: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE instance_meta SET outbox_pruned_through = GREATEST(outbox_pruned_through, $1) WHERE singleton = 1`,
		top.Int64); err != nil {
		return 0, fmt.Errorf("event outbox: prune floor: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}
