package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
		`INSERT INTO derivation_signatures (sig, pipeline, entries, recorded_at) VALUES ($1, $2, $3::jsonb, $4)
		 ON CONFLICT (sig) DO UPDATE SET pipeline = EXCLUDED.pipeline, entries = EXCLUDED.entries`,
		sig, pipeline, string(raw), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("upsert derivation signature %s: %w", sig, err)
	}
	return nil
}

// DerivationSignatureEntries implements storage.RederiveStore.
func (s *ObjectStore) DerivationSignatureEntries(ctx context.Context, sig string) ([]string, bool, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT entries FROM derivation_signatures WHERE sig = $1`, sig).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read derivation signature %s: %w", sig, err)
	}
	var entries []string
	if err := json.Unmarshal(raw, &entries); err != nil {
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
		ph := make([]string, len(current))
		for i := range current {
			ph[i] = "$" + strconv.Itoa(i+1)
		}
		where += ` AND NOT EXISTS (SELECT 1 FROM rederive_skips k WHERE k.object_id = objects.id AND k.target_sig IN (` +
			strings.Join(ph, ", ") + `))`
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
		 LEFT JOIN rederive_attempts a ON a.object_id = o.id AND a.target_sig = $1
		 WHERE o.status <> 'discarded' AND COALESCE(o.pipeline, '') = $2 AND o.derivation_sig = $3
		   AND NOT EXISTS (SELECT 1 FROM rederive_skips k WHERE k.object_id = o.id AND k.target_sig = $1)
		   AND (COALESCE(a.tries, 0) > $4 OR (COALESCE(a.tries, 0) = $4 AND o.id > $5))
		 ORDER BY att, o.id LIMIT $6`,
		q.Target, q.Pipeline, q.Sig, q.After.Tries, q.After.ID, q.Limit)
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
		`UPDATE objects SET derivation_sig = $1 WHERE COALESCE(pipeline, '') = $2 AND derivation_sig = $3 AND status <> 'discarded'`, to, pipeline, from)
	if err != nil {
		return 0, fmt.Errorf("stamp group: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RecordRederiveTry implements storage.RederiveStore.
func (s *ObjectStore) RecordRederiveTry(ctx context.Context, objectID, target string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rederive_attempts (object_id, target_sig, tries, strikes, at) VALUES ($1, $2, 1, 0, $3)
		 ON CONFLICT (object_id, target_sig) DO UPDATE SET tries = rederive_attempts.tries + 1, at = EXCLUDED.at`,
		objectID, target, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("record rederive try %s: %w", objectID, goneOr(err))
	}
	return nil
}

// RecordRederiveStrike implements storage.RederiveStore.
func (s *ObjectStore) RecordRederiveStrike(ctx context.Context, objectID, target string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO rederive_attempts (object_id, target_sig, tries, strikes, at) VALUES ($1, $2, 0, 1, $3)
		 ON CONFLICT (object_id, target_sig) DO UPDATE SET strikes = rederive_attempts.strikes + 1, at = EXCLUDED.at
		 RETURNING strikes`, objectID, target, time.Now().UTC()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("record rederive strike %s: %w", objectID, goneOr(err))
	}
	return n, nil
}

// ClearRederiveAttempts implements storage.RederiveStore.
func (s *ObjectStore) ClearRederiveAttempts(ctx context.Context, objectID, target string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM rederive_attempts WHERE object_id = $1 AND target_sig = $2`, objectID, target); err != nil {
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
		`INSERT INTO rederive_skips (object_id, target_sig, reason, detail, at) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (object_id, target_sig) DO UPDATE SET reason = EXCLUDED.reason, detail = EXCLUDED.detail, at = EXCLUDED.at`,
		k.ObjectID, k.TargetSig, k.Reason, k.Detail, at.UTC())
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
	args := make([]any, 0, len(keep)+len(keepPipelines))
	ph := make([]string, len(keep))
	for i, k := range keep {
		ph[i] = "$" + strconv.Itoa(i+1)
		args = append(args, k)
	}
	where := " WHERE target_sig NOT IN (" + strings.Join(ph, ", ") + ")"
	if len(keepPipelines) > 0 {
		pp := make([]string, len(keepPipelines))
		for i, p := range keepPipelines {
			pp[i] = "$" + strconv.Itoa(len(keep)+i+1)
			args = append(args, p)
		}
		where += " AND target_sig NOT IN (SELECT sig FROM derivation_signatures WHERE pipeline IN (" + strings.Join(pp, ", ") + "))"
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
