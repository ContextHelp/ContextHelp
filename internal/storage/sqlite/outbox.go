package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// outboxStore is the event_outbox table (migration 056, ADR-075 §4).
type outboxStore struct{ db *sql.DB }

const defaultOutboxLimit = 500

// Outbox returns the driver's event outbox.
func (d *Driver) Outbox() storage.OutboxStore { return &outboxStore{db: d.db} }

// execer is what appendOutboxTx writes through: a *sql.Tx or *sql.DB.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// appendOutboxTx writes the event attached to ctx (storage.WithOutboxEvent),
// if any, through tx: the row commits or rolls back with the mutation. The
// event's Seq, ID and InstanceID are filled in; a rolled-back mutation
// leaves them set, but the row does not exist, and callers publish only
// after a successful commit.
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
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO instance_meta (singleton, instance_id) VALUES (1, ?)`,
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
	res, err := tx.ExecContext(ctx,
		`INSERT INTO event_outbox (id, topic, source, occurred_at, origin, actor, instance_id, object_id, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.String(), ev.Topic, ev.Source, at.UTC().Format(time.RFC3339Nano),
		ev.Origin, ev.Actor, instance, ev.ObjectID, payload)
	if err != nil {
		return fmt.Errorf("event outbox: insert: %w", err)
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("event outbox: seq: %w", err)
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
	if err != nil && err != sql.ErrNoRows {
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
		 FROM event_outbox WHERE seq > ? ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, fmt.Errorf("event outbox: read: %w", err)
	}
	defer rows.Close()
	var out []storage.OutboxRow
	for rows.Next() {
		var r storage.OutboxRow
		var at, payload string
		if err := rows.Scan(&r.Seq, &r.ID, &r.Topic, &r.Source, &at, &r.Origin, &r.Actor,
			&r.InstanceID, &r.ObjectID, &payload); err != nil {
			return nil, fmt.Errorf("event outbox: scan: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, fmt.Errorf("event outbox: occurred_at %q: %w", at, err)
		}
		r.OccurredAt, r.Payload = t, []byte(payload)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Head implements storage.OutboxStore. sqlite_sequence keeps the highest
// seq ever allocated, so it survives pruning.
func (s *outboxStore) Head(ctx context.Context) (int64, error) {
	var head sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT seq FROM sqlite_sequence WHERE name = 'event_outbox'`).Scan(&head)
	if err != nil && err != sql.ErrNoRows {
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

// Prune implements storage.OutboxStore. It records the highest seq it
// deleted, so a cursor behind it is expired even where seq has gaps.
func (s *outboxStore) Prune(ctx context.Context, cutoff time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("event outbox: prune: begin tx: %w", err)
	}
	defer tx.Rollback()
	const doomed = `occurred_at < ? AND seq < (SELECT MAX(seq) FROM event_outbox)`
	at := cutoff.UTC().Format(time.RFC3339Nano)
	var top sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM event_outbox WHERE `+doomed, at).Scan(&top); err != nil {
		return 0, fmt.Errorf("event outbox: prune: %w", err)
	}
	if !top.Valid {
		return 0, nil
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM event_outbox WHERE seq <= ?`, top.Int64)
	if err != nil {
		return 0, fmt.Errorf("event outbox: prune: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE instance_meta SET outbox_pruned_through = MAX(outbox_pruned_through, ?) WHERE singleton = 1`,
		top.Int64); err != nil {
		return 0, fmt.Errorf("event outbox: prune floor: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}
