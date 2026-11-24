package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type holdStore struct{ db *sql.DB }
type tombstoneStore struct{ db *sql.DB }
type obligationStore struct{ db *sql.DB }

const holdColumns = `id, object_id, source_key, hold_id, source, reason, placed_by, placed_at, released_by, released_at, release_reason`

func scanHold(row interface{ Scan(...any) error }) (storage.ObjectHold, error) {
	var h storage.ObjectHold
	var released sql.NullTime
	if err := row.Scan(
		&h.ID, &h.ObjectID, &h.SourceKey, &h.HoldID, &h.Source, &h.Reason,
		&h.PlacedBy, &h.PlacedAt, &h.ReleasedBy, &released, &h.ReleaseReason,
	); err != nil {
		return storage.ObjectHold{}, err
	}
	h.PlacedAt = h.PlacedAt.UTC()
	h.ReleasedAt = timePtr(released)
	return h, nil
}

func (s *holdStore) Place(ctx context.Context, h *storage.ObjectHold) error {
	if h == nil {
		return errors.New("place hold: nil hold")
	}
	if strings.TrimSpace(h.ObjectID) == "" && strings.TrimSpace(h.SourceKey) == "" {
		return errors.New("place hold: object_id or source_key is required")
	}
	if strings.TrimSpace(h.HoldID) == "" {
		return errors.New("place hold: hold_id is required")
	}
	if strings.TrimSpace(h.Source) == "" {
		return errors.New("place hold: source is required")
	}
	if h.ID == "" {
		h.ID = storage.NewULID()
	}
	if h.PlacedAt.IsZero() {
		h.PlacedAt = time.Now().UTC()
	}
	_, err := conn(ctx, s.db).ExecContext(ctx,
		`INSERT INTO object_holds (`+holdColumns+`)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '', NULL, '')
		 ON CONFLICT (object_id, hold_id) WHERE released_at IS NULL DO NOTHING`,
		h.ID, h.ObjectID, h.SourceKey, h.HoldID, h.Source, h.Reason, h.PlacedBy, h.PlacedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("place hold: %w", err)
	}
	return nil
}

func (s *holdStore) Release(ctx context.Context, objectID, holdID, releasedBy, reason string, at time.Time) (bool, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	res, err := conn(ctx, s.db).ExecContext(ctx,
		`UPDATE object_holds
		 SET released_by = $1, released_at = $2, release_reason = $3
		 WHERE object_id = $4 AND hold_id = $5 AND released_at IS NULL`,
		releasedBy, at.UTC(), reason, objectID, holdID,
	)
	if err != nil {
		return false, fmt.Errorf("release hold: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *holdStore) ListActive(ctx context.Context, objectID string) ([]storage.ObjectHold, error) {
	return s.queryHolds(ctx, `SELECT `+holdColumns+` FROM object_holds WHERE object_id = $1 AND released_at IS NULL ORDER BY placed_at ASC, id ASC`, objectID)
}

func (s *holdStore) ListActiveBySourceKey(ctx context.Context, sourceKey string) ([]storage.ObjectHold, error) {
	if sourceKey == "" {
		return nil, nil
	}
	return s.queryHolds(ctx, `SELECT `+holdColumns+` FROM object_holds WHERE source_key = $1 AND released_at IS NULL ORDER BY placed_at ASC, id ASC`, sourceKey)
}

func (s *holdStore) ListAllActive(ctx context.Context, f storage.HoldFilter) ([]storage.ObjectHold, error) {
	where := []string{"released_at IS NULL"}
	var args []any
	add := func(col, val string) {
		if val == "" {
			return
		}
		args = append(args, val)
		where = append(where, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	add("object_id", f.ObjectID)
	add("source_key", f.SourceKey)
	add("hold_id", f.HoldID)
	add("source", f.Source)
	q := `SELECT ` + holdColumns + ` FROM object_holds WHERE ` + strings.Join(where, " AND ") + ` ORDER BY placed_at ASC, id ASC`
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += fmt.Sprintf(` LIMIT $%d`, len(args))
		if f.Offset > 0 {
			args = append(args, f.Offset)
			q += fmt.Sprintf(` OFFSET $%d`, len(args))
		}
	}
	return s.queryHolds(ctx, q, args...)
}

func (s *holdStore) IsHeld(ctx context.Context, objectID string) (bool, error) {
	var n int
	if err := conn(ctx, s.db).QueryRowContext(ctx,
		`SELECT COUNT(1) FROM object_holds WHERE object_id = $1 AND released_at IS NULL`, objectID,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("check hold: %w", err)
	}
	return n > 0, nil
}

func (s *holdStore) queryHolds(ctx context.Context, query string, args ...any) ([]storage.ObjectHold, error) {
	rows, err := conn(ctx, s.db).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list holds: %w", err)
	}
	defer rows.Close()
	out := []storage.ObjectHold{}
	for rows.Next() {
		h, err := scanHold(rows)
		if err != nil {
			return nil, fmt.Errorf("scan hold: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate holds: %w", err)
	}
	return out, nil
}

func (s *tombstoneStore) Put(ctx context.Context, t *storage.ErasureTombstone) error {
	if t == nil || strings.TrimSpace(t.SourceKey) == "" {
		return errors.New("put tombstone: source_key is required")
	}
	if t.ErasedAt.IsZero() {
		t.ErasedAt = time.Now().UTC()
	}
	_, err := conn(ctx, s.db).ExecContext(ctx,
		`INSERT INTO erasure_tombstones (source_key, source, obligation_id, erased_at, erased_by)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (source_key) DO NOTHING`,
		t.SourceKey, t.Source, t.ObligationID, t.ErasedAt.UTC(), t.ErasedBy,
	)
	if err != nil {
		return fmt.Errorf("put tombstone: %w", err)
	}
	if _, err := conn(ctx, s.db).ExecContext(ctx,
		`DELETE FROM jobs WHERE user_source_key = $1 AND status IN ('pending', 'queued')`, t.SourceKey,
	); err != nil {
		return fmt.Errorf("put tombstone: cancel queued jobs: %w", err)
	}
	if _, err := conn(ctx, s.db).ExecContext(ctx,
		`UPDATE jobs SET payload = '', user_data = NULL, user_note = NULL, user_hints = NULL, user_ambient = NULL WHERE user_source_key = $1`, t.SourceKey,
	); err != nil {
		return fmt.Errorf("put tombstone: scrub jobs: %w", err)
	}
	return nil
}

func (s *tombstoneStore) Exists(ctx context.Context, sourceKey string) (bool, error) {
	if sourceKey == "" {
		return false, nil
	}
	var n int
	if err := conn(ctx, s.db).QueryRowContext(ctx,
		`SELECT COUNT(1) FROM erasure_tombstones WHERE source_key = $1`, sourceKey,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("tombstone exists: %w", err)
	}
	return n > 0, nil
}

func (s *tombstoneStore) Get(ctx context.Context, sourceKey string) (*storage.ErasureTombstone, error) {
	var t storage.ErasureTombstone
	err := conn(ctx, s.db).QueryRowContext(ctx,
		`SELECT source_key, source, obligation_id, erased_at, erased_by FROM erasure_tombstones WHERE source_key = $1`, sourceKey,
	).Scan(&t.SourceKey, &t.Source, &t.ObligationID, &t.ErasedAt, &t.ErasedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("tombstone %q: %w", sourceKey, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get tombstone: %w", err)
	}
	t.ErasedAt = t.ErasedAt.UTC()
	return &t, nil
}

func (s *tombstoneStore) List(ctx context.Context, limit, offset int) ([]storage.ErasureTombstone, error) {
	q := `SELECT source_key, source, obligation_id, erased_at, erased_by FROM erasure_tombstones ORDER BY erased_at DESC, source_key ASC`
	var args []any
	if limit > 0 {
		args = append(args, limit)
		q += fmt.Sprintf(` LIMIT $%d`, len(args))
		if offset > 0 {
			args = append(args, offset)
			q += fmt.Sprintf(` OFFSET $%d`, len(args))
		}
	}
	rows, err := conn(ctx, s.db).QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list tombstones: %w", err)
	}
	defer rows.Close()
	out := []storage.ErasureTombstone{}
	for rows.Next() {
		var t storage.ErasureTombstone
		if err := rows.Scan(&t.SourceKey, &t.Source, &t.ObligationID, &t.ErasedAt, &t.ErasedBy); err != nil {
			return nil, fmt.Errorf("scan tombstone: %w", err)
		}
		t.ErasedAt = t.ErasedAt.UTC()
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tombstones: %w", err)
	}
	return out, nil
}

func (s *obligationStore) Get(ctx context.Context, id string) (*storage.ObligationRecord, error) {
	var r storage.ObligationRecord
	err := conn(ctx, s.db).QueryRowContext(ctx,
		`SELECT id, kind, request_sha256, state, status_json, created_at, updated_at FROM bos_obligations WHERE id = $1`, id,
	).Scan(&r.ID, &r.Kind, &r.RequestSHA256, &r.State, &r.StatusJSON, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("obligation %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get obligation: %w", err)
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return &r, nil
}

func (s *obligationStore) Save(ctx context.Context, rec *storage.ObligationRecord) error {
	if rec == nil || strings.TrimSpace(rec.ID) == "" {
		return errors.New("save obligation: id is required")
	}
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	return inTx(ctx, s.db, func(txCtx context.Context) error {
		q := conn(txCtx, s.db)
		var existingSHA string
		err := q.QueryRowContext(txCtx, `SELECT request_sha256 FROM bos_obligations WHERE id = $1 FOR UPDATE`, rec.ID).Scan(&existingSHA)
		if err == nil {
			if existingSHA != rec.RequestSHA256 {
				return fmt.Errorf("obligation %q: %w", rec.ID, storage.ErrObligationIDReused)
			}
			_, err = q.ExecContext(txCtx,
				`UPDATE bos_obligations SET state = $1, status_json = $2, updated_at = $3 WHERE id = $4`,
				rec.State, rec.StatusJSON, rec.UpdatedAt.UTC(), rec.ID,
			)
			if err != nil {
				return fmt.Errorf("update obligation: %w", err)
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("lookup obligation: %w", err)
		}
		_, err = q.ExecContext(txCtx,
			`INSERT INTO bos_obligations (id, kind, request_sha256, state, status_json, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			rec.ID, rec.Kind, rec.RequestSHA256, rec.State, rec.StatusJSON, rec.CreatedAt.UTC(), rec.UpdatedAt.UTC(),
		)
		if err != nil {
			return fmt.Errorf("insert obligation: %w", err)
		}
		return nil
	})
}
