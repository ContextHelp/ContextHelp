package sqlite

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
	var placed int64
	var released sql.NullInt64
	if err := row.Scan(
		&h.ID, &h.ObjectID, &h.SourceKey, &h.HoldID, &h.Source, &h.Reason,
		&h.PlacedBy, &placed, &h.ReleasedBy, &released, &h.ReleaseReason,
	); err != nil {
		return storage.ObjectHold{}, err
	}
	h.PlacedAt = fromMillis(placed)
	h.ReleasedAt = millisPtr(released)
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', NULL, '')
		 ON CONFLICT (object_id, hold_id) WHERE released_at IS NULL DO NOTHING`,
		h.ID, h.ObjectID, h.SourceKey, h.HoldID, h.Source, h.Reason, h.PlacedBy, toMillis(h.PlacedAt),
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
		 SET released_by = ?, released_at = ?, release_reason = ?
		 WHERE object_id = ? AND hold_id = ? AND released_at IS NULL`,
		releasedBy, toMillis(at), reason, objectID, holdID,
	)
	if err != nil {
		return false, fmt.Errorf("release hold: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *holdStore) ListActive(ctx context.Context, objectID string) ([]storage.ObjectHold, error) {
	return s.queryHolds(ctx, `SELECT `+holdColumns+` FROM object_holds WHERE object_id = ? AND released_at IS NULL ORDER BY placed_at ASC, id ASC`, objectID)
}

func (s *holdStore) ListActiveBySourceKey(ctx context.Context, sourceKey string) ([]storage.ObjectHold, error) {
	if sourceKey == "" {
		return nil, nil
	}
	return s.queryHolds(ctx, `SELECT `+holdColumns+` FROM object_holds WHERE source_key = ? AND released_at IS NULL ORDER BY placed_at ASC, id ASC`, sourceKey)
}

func (s *holdStore) ListAllActive(ctx context.Context, f storage.HoldFilter) ([]storage.ObjectHold, error) {
	where := []string{"released_at IS NULL"}
	var args []any
	if f.ObjectID != "" {
		where = append(where, "object_id = ?")
		args = append(args, f.ObjectID)
	}
	if f.SourceKey != "" {
		where = append(where, "source_key = ?")
		args = append(args, f.SourceKey)
	}
	if f.HoldID != "" {
		where = append(where, "hold_id = ?")
		args = append(args, f.HoldID)
	}
	if f.Source != "" {
		where = append(where, "source = ?")
		args = append(args, f.Source)
	}
	q := `SELECT ` + holdColumns + ` FROM object_holds WHERE ` + strings.Join(where, " AND ") + ` ORDER BY placed_at ASC, id ASC`
	q, args = appendPagination(q, args, f.Limit, f.Offset)
	return s.queryHolds(ctx, q, args...)
}

func appendPagination(q string, args []any, limit, offset int) (string, []any) {
	if limit <= 0 {
		return q, args
	}
	if offset > 0 {
		return q + ` LIMIT ? OFFSET ?`, append(args, limit, offset)
	}
	return q + ` LIMIT ?`, append(args, limit)
}

func (s *holdStore) IsHeld(ctx context.Context, objectID string) (bool, error) {
	var n int
	if err := conn(ctx, s.db).QueryRowContext(ctx,
		`SELECT COUNT(1) FROM object_holds WHERE object_id = ? AND released_at IS NULL`, objectID,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("check hold: %w", err)
	}
	return n > 0, nil
}

type holdQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// assertNotHeldTx enforces the single storage-layer hold guard inside an active
// transaction (or db handle). Any active hold on objectID causes it to return
// *storage.HoldError wrapping storage.ErrObjectHeld.
func assertNotHeldTx(ctx context.Context, q holdQuerier, objectID string) error {
	if objectID == "" {
		return nil
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+holdColumns+` FROM object_holds WHERE object_id = ? AND released_at IS NULL ORDER BY placed_at ASC, id ASC`,
		objectID,
	)
	if err != nil {
		return fmt.Errorf("check hold on %s: %w", objectID, err)
	}
	defer rows.Close()
	active := []storage.ObjectHold{}
	for rows.Next() {
		h, serr := scanHold(rows)
		if serr != nil {
			return fmt.Errorf("scan hold on %s: %w", objectID, serr)
		}
		active = append(active, h)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate holds on %s: %w", objectID, err)
	}
	if len(active) > 0 {
		return &storage.HoldError{ObjectID: objectID, Holds: active}
	}
	return nil
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
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (source_key) DO NOTHING`,
		t.SourceKey, t.Source, t.ObligationID, toMillis(t.ErasedAt), t.ErasedBy,
	)
	if err != nil {
		return fmt.Errorf("put tombstone: %w", err)
	}
	// Cancel queued/pending jobs and scrub payloads of completed jobs for
	// the erased source_key so no queued task resurrects it or retains PII.
	if _, err := conn(ctx, s.db).ExecContext(ctx,
		`DELETE FROM jobs WHERE user_source_key = ? AND status IN ('pending', 'queued')`, t.SourceKey,
	); err != nil {
		return fmt.Errorf("put tombstone: cancel queued jobs: %w", err)
	}
	if _, err := conn(ctx, s.db).ExecContext(ctx,
		`UPDATE jobs SET payload = '', user_data = NULL, user_note = NULL, user_hints = NULL, user_ambient = NULL WHERE user_source_key = ?`, t.SourceKey,
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
		`SELECT COUNT(1) FROM erasure_tombstones WHERE source_key = ?`, sourceKey,
	).Scan(&n); err != nil {
		return false, fmt.Errorf("tombstone exists: %w", err)
	}
	return n > 0, nil
}

func (s *tombstoneStore) Get(ctx context.Context, sourceKey string) (*storage.ErasureTombstone, error) {
	var t storage.ErasureTombstone
	var erasedAt int64
	err := conn(ctx, s.db).QueryRowContext(ctx,
		`SELECT source_key, source, obligation_id, erased_at, erased_by FROM erasure_tombstones WHERE source_key = ?`, sourceKey,
	).Scan(&t.SourceKey, &t.Source, &t.ObligationID, &erasedAt, &t.ErasedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("tombstone %q: %w", sourceKey, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get tombstone: %w", err)
	}
	t.ErasedAt = fromMillis(erasedAt)
	return &t, nil
}

func (s *tombstoneStore) List(ctx context.Context, limit, offset int) ([]storage.ErasureTombstone, error) {
	q := `SELECT source_key, source, obligation_id, erased_at, erased_by FROM erasure_tombstones ORDER BY erased_at DESC, source_key ASC`
	q, args := appendPagination(q, nil, limit, offset)
	rows, err := conn(ctx, s.db).QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list tombstones: %w", err)
	}
	defer rows.Close()
	out := []storage.ErasureTombstone{}
	for rows.Next() {
		var t storage.ErasureTombstone
		var erasedAt int64
		if err := rows.Scan(&t.SourceKey, &t.Source, &t.ObligationID, &erasedAt, &t.ErasedBy); err != nil {
			return nil, fmt.Errorf("scan tombstone: %w", err)
		}
		t.ErasedAt = fromMillis(erasedAt)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tombstones: %w", err)
	}
	return out, nil
}

func (s *obligationStore) Get(ctx context.Context, id string) (*storage.ObligationRecord, error) {
	var r storage.ObligationRecord
	var created, updated int64
	err := conn(ctx, s.db).QueryRowContext(ctx,
		`SELECT id, kind, request_sha256, state, status_json, created_at, updated_at FROM bos_obligations WHERE id = ?`, id,
	).Scan(&r.ID, &r.Kind, &r.RequestSHA256, &r.State, &r.StatusJSON, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("obligation %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get obligation: %w", err)
	}
	r.CreatedAt = fromMillis(created)
	r.UpdatedAt = fromMillis(updated)
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
		err := q.QueryRowContext(txCtx, `SELECT request_sha256 FROM bos_obligations WHERE id = ?`, rec.ID).Scan(&existingSHA)
		if err == nil {
			if existingSHA != rec.RequestSHA256 {
				return fmt.Errorf("obligation %q: %w", rec.ID, storage.ErrObligationIDReused)
			}
			_, err = q.ExecContext(txCtx,
				`UPDATE bos_obligations SET state = ?, status_json = ?, updated_at = ? WHERE id = ?`,
				rec.State, rec.StatusJSON, toMillis(rec.UpdatedAt), rec.ID,
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
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			rec.ID, rec.Kind, rec.RequestSHA256, rec.State, rec.StatusJSON, toMillis(rec.CreatedAt), toMillis(rec.UpdatedAt),
		)
		if err != nil {
			return fmt.Errorf("insert obligation: %w", err)
		}
		return nil
	})
}
