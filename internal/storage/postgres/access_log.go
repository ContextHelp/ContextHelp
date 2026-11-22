package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// accessLogStore is the read audit: access_log and access_log_objects
// (migration 31), receipt-shaped by migration 38 and with the steward
// columns of migration 46, and, for steward entries, the chain head in
// access_log_chain.
type accessLogStore struct{ db *sql.DB }

// accessColumns are written by insertAccessEntry and read by
// scanAccessEntry; the last five are NULL on an ordinary entry.
const accessColumns = `id, at, principal_id, identity_id, route, decision, checks, operation_digest, policy_versions, object_count, entity_count, ` +
	`steward_grant_id, chain_seq, prev_digest, entry_digest, objects_digest`

func (s *accessLogStore) Append(ctx context.Context, entries []*storage.AccessEntry) error {
	if len(entries) == 0 {
		return nil
	}
	now := time.Now()
	for _, e := range entries {
		if err := storage.PrepareAccessEntry(e, now); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("access log append: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, e := range entries {
		if err := insertAccessEntry(ctx, tx, e); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("access log append: commit: %w", err)
	}
	return nil
}

// AppendSteward writes one steward entry chained after the head. The
// head row is locked (FOR UPDATE) for the transaction: a concurrent
// append waits for this one to commit and then reads the head it
// wrote, so the chain stays linear under READ COMMITTED.
func (s *accessLogStore) AppendSteward(ctx context.Context, e *storage.AccessEntry) error {
	if err := storage.PrepareStewardEntry(e, time.Now()); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("steward audit append: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var head storage.AccessChainHead
	if err := tx.QueryRowContext(ctx, `SELECT seq, head FROM access_log_chain WHERE id = 1 FOR UPDATE`).Scan(&head.Seq, &head.Digest); err != nil {
		return fmt.Errorf("steward audit append: chain head: %w", err)
	}
	next := storage.LinkAccessEntry(head, e)
	if err := insertAccessEntry(ctx, tx, e); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE access_log_chain SET seq = $1, head = $2 WHERE id = 1 AND seq = $3`, next.Seq, next.Digest, head.Seq)
	if err != nil {
		return fmt.Errorf("steward audit append: move head: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("steward audit append: the chain head moved under the append")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("steward audit append: commit: %w", err)
	}
	return nil
}

// insertAccessEntry writes e and its object ids in tx.
func insertAccessEntry(ctx context.Context, tx *sql.Tx, e *storage.AccessEntry) error {
	checks, err := json.Marshal(e.Checks)
	if err != nil {
		return fmt.Errorf("access log append %q checks: %w", e.ID, err)
	}
	var identity, grant, seq, prev, digest, objects any
	if e.IdentityID != "" {
		identity = e.IdentityID
	}
	if e.StewardGrantID != "" {
		grant = e.StewardGrantID
	}
	if l := e.Chain; l != nil {
		seq, prev, digest, objects = l.Seq, l.PrevDigest, l.EntryDigest, l.ObjectsDigest
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO access_log (`+accessColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		e.ID, e.At, e.PrincipalID, identity, e.Route, string(e.Decision), string(checks), e.OperationDigest,
		e.PolicyVersions, e.ObjectCount, e.EntityCount, grant, seq, prev, digest, objects); err != nil {
		return fmt.Errorf("access log append %q: %w", e.ID, err)
	}
	for _, id := range e.ObjectIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_log_objects (access_id, object_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, e.ID, id); err != nil {
			return fmt.Errorf("access log append %q objects: %w", e.ID, err)
		}
	}
	return nil
}

// accessRowScanner is a *sql.Row or *sql.Rows.
type accessRowScanner interface{ Scan(dest ...any) error }

// scanAccessEntry reads one row of accessColumns.
func scanAccessEntry(sc accessRowScanner) (*storage.AccessEntry, error) {
	e := &storage.AccessEntry{}
	var identity, grant, prev, digest, objects sql.NullString
	var seq sql.NullInt64
	var decision, checks string
	if err := sc.Scan(&e.ID, &e.At, &e.PrincipalID, &identity, &e.Route, &decision, &checks, &e.OperationDigest,
		&e.PolicyVersions, &e.ObjectCount, &e.EntityCount, &grant, &seq, &prev, &digest, &objects); err != nil {
		return nil, err
	}
	e.Decision = storage.AccessDecision(decision)
	if err := json.Unmarshal([]byte(checks), &e.Checks); err != nil {
		return nil, fmt.Errorf("access log %q checks: %w", e.ID, err)
	}
	e.At = e.At.UTC()
	e.IdentityID = identity.String
	e.StewardGrantID = grant.String
	if seq.Valid {
		e.Chain = &storage.AccessChainLink{Seq: seq.Int64, PrevDigest: prev.String, EntryDigest: digest.String, ObjectsDigest: objects.String}
	}
	return e, nil
}

func (s *accessLogStore) Query(ctx context.Context, f storage.AccessFilter) ([]*storage.AccessEntry, error) {
	conds := []string{"TRUE"}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.PrincipalID != "" {
		conds = append(conds, "principal_id = "+bind(f.PrincipalID))
	}
	if f.ObjectID != "" {
		cond := "id IN (SELECT access_id FROM access_log_objects WHERE object_id = " + bind(f.ObjectID) + ")"
		if f.HideStewardObjects {
			cond += " AND steward_grant_id IS NULL"
		}
		conds = append(conds, cond)
	}
	if f.StewardGrantID != "" {
		conds = append(conds, "steward_grant_id = "+bind(f.StewardGrantID))
	}
	if f.Decision != "" {
		conds = append(conds, "decision = "+bind(string(f.Decision)))
	}
	if !f.Since.IsZero() {
		conds = append(conds, "at >= "+bind(f.Since.UTC()))
	}
	if !f.Until.IsZero() {
		conds = append(conds, "at < "+bind(f.Until.UTC()))
	}
	var lim any
	if f.Limit > 0 {
		lim = f.Limit
	}
	limit := bind(lim)
	// #nosec G202 -- conds are fixed fragments with generated $N markers; every value travels in args.
	rows, err := s.db.QueryContext(ctx, `SELECT `+accessColumns+` FROM access_log WHERE `+
		strings.Join(conds, " AND ")+` ORDER BY at DESC, id DESC LIMIT `+limit, args...)
	if err != nil {
		return nil, fmt.Errorf("access log query: %w", err)
	}
	defer rows.Close()
	out := []*storage.AccessEntry{}
	ids := []string{}
	for rows.Next() {
		e, err := scanAccessEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("access log query: %w", err)
		}
		out = append(out, e)
		if !f.HideStewardObjects || e.StewardGrantID == "" {
			ids = append(ids, e.ID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("access log query: %w", err)
	}
	if err := fillObjectIDs(ctx, s.db, out, ids); err != nil {
		return nil, err
	}
	return out, nil
}

// fillObjectIDs sets the retained object ids of the entries among out
// whose ids are listed.
func fillObjectIDs(ctx context.Context, q querier, out []*storage.AccessEntry, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	byID := make(map[string]*storage.AccessEntry, len(out))
	for _, e := range out {
		byID[e.ID] = e
	}
	rows, err := q.QueryContext(ctx, `SELECT access_id, object_id FROM access_log_objects
		WHERE access_id = ANY($1) ORDER BY access_id, object_id`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("access log objects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var accessID, objectID string
		if err := rows.Scan(&accessID, &objectID); err != nil {
			return fmt.Errorf("access log objects: %w", err)
		}
		if e := byID[accessID]; e != nil {
			e.ObjectIDs = append(e.ObjectIDs, objectID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("access log objects: %w", err)
	}
	return nil
}

func (s *accessLogStore) Prune(ctx context.Context, before time.Time) (int64, error) {
	return s.prune(ctx, before, "steward_grant_id IS NULL")
}

func (s *accessLogStore) PruneSteward(ctx context.Context, before time.Time) (int64, error) {
	return s.prune(ctx, before, "steward_grant_id IS NOT NULL")
}

// prune deletes the object id lists of the entries logged before
// before that which (a fixed fragment) selects.
func (s *accessLogStore) prune(ctx context.Context, before time.Time, which string) (int64, error) {
	// #nosec G202 -- which is one of two fixed fragments; the time travels in args.
	res, err := s.db.ExecContext(ctx, `DELETE FROM access_log_objects
		WHERE access_id IN (SELECT id FROM access_log WHERE at < $1 AND `+which+`)`, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("access log prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// StewardChain reads the head and the steward entries in one
// REPEATABLE READ transaction, so both come from one snapshot.
func (s *accessLogStore) StewardChain(ctx context.Context) (storage.AccessChainHead, []*storage.AccessEntry, error) {
	var head storage.AccessChainHead
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return head, nil, fmt.Errorf("steward chain: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT seq, head FROM access_log_chain WHERE id = 1`).Scan(&head.Seq, &head.Digest); err != nil {
		return head, nil, fmt.Errorf("steward chain: head: %w", err)
	}
	out, err := stewardEntries(ctx, tx)
	if err != nil {
		return head, nil, err
	}
	ids := make([]string, len(out))
	for i, e := range out {
		ids[i] = e.ID
	}
	if err := fillObjectIDs(ctx, tx, out, ids); err != nil {
		return head, nil, err
	}
	return head, out, tx.Commit()
}

// stewardEntries reads every steward entry in chain order, without its
// object ids.
func stewardEntries(ctx context.Context, q querier) ([]*storage.AccessEntry, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+accessColumns+` FROM access_log
		WHERE steward_grant_id IS NOT NULL ORDER BY chain_seq, id`)
	if err != nil {
		return nil, fmt.Errorf("steward chain: %w", err)
	}
	defer rows.Close()
	var out []*storage.AccessEntry
	for rows.Next() {
		e, err := scanAccessEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("steward chain: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("steward chain: %w", err)
	}
	return out, nil
}
