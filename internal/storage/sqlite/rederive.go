package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var _ storage.RederiveStore = (*ObjectStore)(nil)

// UpsertDerivationSignature implements storage.RederiveStore.
func (s *ObjectStore) UpsertDerivationSignature(ctx context.Context, sig, pipeline string, entries []string) error {
	raw, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("upsert derivation signature %s: %w", sig, err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO derivation_signatures (sig, pipeline, entries, recorded_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(sig) DO UPDATE SET pipeline = excluded.pipeline, entries = excluded.entries`,
		sig, pipeline, string(raw), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("upsert derivation signature %s: %w", sig, err)
	}
	return nil
}

// DerivationSignatureEntries implements storage.RederiveStore.
func (s *ObjectStore) DerivationSignatureEntries(ctx context.Context, sig string) ([]string, bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT entries FROM derivation_signatures WHERE sig = ?`, sig).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read derivation signature %s: %w", sig, err)
	}
	var entries []string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, false, fmt.Errorf("decode derivation signature %s: %w", sig, err)
	}
	return entries, true, nil
}

// ListStaleGroups implements storage.RederiveStore.
func (s *ObjectStore) ListStaleGroups(ctx context.Context, current []string) ([]storage.StaleGroup, error) {
	where, args := staleClause(current)
	// A discarded object is on its way out: no work is spent on it.
	where += ` AND status <> 'discarded'`
	if len(current) > 0 {
		where += ` AND NOT EXISTS (SELECT 1 FROM rederive_skips k WHERE k.object_id = objects.id AND k.target_sig IN (?` +
			strings.Repeat(`, ?`, len(current)-1) + `))`
		for _, c := range current {
			args = append(args, c)
		}
	}
	rows, err := s.db.QueryContext(ctx,
		staleQuery(`SELECT COALESCE(pipeline, ''), derivation_sig, COUNT(*)`, where,
			` GROUP BY COALESCE(pipeline, ''), derivation_sig ORDER BY 1, 2`), args...)
	if err != nil {
		return nil, fmt.Errorf("list stale groups: %w", err)
	}
	defer rows.Close()
	var out []storage.StaleGroup
	for rows.Next() {
		var g storage.StaleGroup
		if err := rows.Scan(&g.Pipeline, &g.Sig, &g.Objects); err != nil {
			return nil, fmt.Errorf("list stale groups: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListRederivable implements storage.RederiveStore.
func (s *ObjectStore) ListRederivable(ctx context.Context, q storage.RederiveQuery) ([]storage.StaleObject, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.id, COALESCE(a.tries, 0) AS att
		 FROM objects o
		 LEFT JOIN rederive_attempts a ON a.object_id = o.id AND a.target_sig = ?
		 WHERE o.status <> 'discarded' AND COALESCE(o.pipeline, '') = ? AND o.derivation_sig = ?
		   AND NOT EXISTS (SELECT 1 FROM rederive_skips k WHERE k.object_id = o.id AND k.target_sig = ?)
		   AND (COALESCE(a.tries, 0) > ? OR (COALESCE(a.tries, 0) = ? AND o.id > ?))
		 ORDER BY att, o.id LIMIT ?`,
		q.Target, q.Pipeline, q.Sig, q.Target, q.After.Tries, q.After.Tries, q.After.ID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("list rederivable: %w", err)
	}
	defer rows.Close()
	out := make([]storage.StaleObject, 0, max(q.Limit, 0))
	for rows.Next() {
		o := storage.StaleObject{Pipeline: q.Pipeline, Sig: q.Sig}
		if err := rows.Scan(&o.ID, &o.Tries); err != nil {
			return nil, fmt.Errorf("list rederivable: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// StampGroup implements storage.RederiveStore.
func (s *ObjectStore) StampGroup(ctx context.Context, pipeline, from, to string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE objects SET derivation_sig = ? WHERE COALESCE(pipeline, '') = ? AND derivation_sig = ? AND status <> 'discarded'`, to, pipeline, from)
	if err != nil {
		return 0, fmt.Errorf("stamp group: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RecordRederiveTry implements storage.RederiveStore.
func (s *ObjectStore) RecordRederiveTry(ctx context.Context, objectID, target string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rederive_attempts (object_id, target_sig, tries, strikes, at) VALUES (?, ?, 1, 0, ?)
		 ON CONFLICT(object_id, target_sig) DO UPDATE SET tries = tries + 1, at = excluded.at`,
		objectID, target, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record rederive try %s: %w", objectID, goneOr(err))
	}
	return nil
}

// RecordRederiveStrike implements storage.RederiveStore.
func (s *ObjectStore) RecordRederiveStrike(ctx context.Context, objectID, target string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO rederive_attempts (object_id, target_sig, tries, strikes, at) VALUES (?, ?, 0, 1, ?)
		 ON CONFLICT(object_id, target_sig) DO UPDATE SET strikes = strikes + 1, at = excluded.at
		 RETURNING strikes`, objectID, target, time.Now().UTC().Format(time.RFC3339)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("record rederive strike %s: %w", objectID, goneOr(err))
	}
	return n, nil
}

// ClearRederiveAttempts implements storage.RederiveStore.
func (s *ObjectStore) ClearRederiveAttempts(ctx context.Context, objectID, target string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM rederive_attempts WHERE object_id = ? AND target_sig = ?`, objectID, target); err != nil {
		return fmt.Errorf("clear rederive attempts %s: %w", objectID, err)
	}
	return nil
}

// RecordRederiveSkip implements storage.RederiveStore.
func (s *ObjectStore) RecordRederiveSkip(ctx context.Context, k storage.RederiveSkip) error {
	at := k.At
	if at.IsZero() {
		at = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rederive_skips (object_id, target_sig, reason, detail, at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(object_id, target_sig) DO UPDATE SET reason = excluded.reason, detail = excluded.detail, at = excluded.at`,
		k.ObjectID, k.TargetSig, k.Reason, k.Detail, at.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record rederive skip %s: %w", k.ObjectID, goneOr(err))
	}
	return nil
}

// goneOr is storage.ErrNotFound when err is a foreign key miss (the object
// of a counter or a skip was deleted), else err itself.
func goneOr(err error) error {
	if errors.Is(constraintErr(err), storage.ErrNotFound) {
		return storage.ErrNotFound
	}
	return err
}

// PruneRederiveRows implements storage.RederiveStore.
func (s *ObjectStore) PruneRederiveRows(ctx context.Context, keep, keepPipelines []string) (int, error) {
	if len(keep) == 0 {
		return 0, nil
	}
	in := `(?` + strings.Repeat(`, ?`, len(keep)-1) + `)`
	args := make([]any, 0, len(keep)+len(keepPipelines))
	for _, k := range keep {
		args = append(args, k)
	}
	where := " WHERE target_sig NOT IN " + in
	if len(keepPipelines) > 0 {
		where += ` AND target_sig NOT IN (SELECT sig FROM derivation_signatures WHERE pipeline IN (?` +
			strings.Repeat(`, ?`, len(keepPipelines)-1) + `))`
		for _, p := range keepPipelines {
			args = append(args, p)
		}
	}
	total := 0
	for _, table := range []string{"rederive_attempts", "rederive_skips"} {
		res, err := s.db.ExecContext(ctx, pruneQuery(table, where), args...)
		if err != nil {
			return total, fmt.Errorf("prune %s: %w", table, err)
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}
	return total, nil
}

// pruneQuery joins the fixed parts of a prune statement: table is one of
// two constant names and where holds only placeholders, never a value.
func pruneQuery(table, where string) string {
	var b strings.Builder
	b.WriteString("DELETE FROM ")
	b.WriteString(table)
	b.WriteString(where)
	return b.String()
}
