package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// uiSessionStore persists web UI sessions and login codes in the
// ui_sessions and ui_login_codes tables (migration 041, rebuilt by 049
// to reference principal_identities; the steward elevation columns are
// 064's). Times are Unix milliseconds, UTC.
type uiSessionStore struct{ db *sql.DB }

// uiSessionInsertColumns are the columns Create writes: a new session
// is never elevated.
const uiSessionInsertColumns = `id, secret_hash, principal_id, identity_id, scope, user_agent, remote_addr,
	created_at, last_seen_at, idle_expires_at, expires_at, revoked_at, revoke_reason`

// uiSessionColumns are the columns every read selects.
const uiSessionColumns = uiSessionInsertColumns + `, steward_grant_id, steward_active_at`

func toMillis(t time.Time) int64 { return t.UTC().UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func (s *uiSessionStore) CreateLoginCode(ctx context.Context, c *storage.UILoginCode) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ui_login_codes
		(code_hash, principal_id, identity_id, scope, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		c.CodeHash, c.PrincipalID, c.IdentityID, c.Scope, toMillis(c.CreatedAt), toMillis(c.ExpiresAt))
	if err != nil {
		return fmt.Errorf("ui login code create: %w", err)
	}
	return nil
}

func (s *uiSessionStore) ConsumeLoginCode(ctx context.Context, codeHash string, now time.Time) (*storage.UILoginCode, error) {
	c := &storage.UILoginCode{CodeHash: codeHash}
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `DELETE FROM ui_login_codes WHERE code_hash = ?
		RETURNING principal_id, identity_id, scope, created_at, expires_at`, codeHash,
	).Scan(&c.PrincipalID, &c.IdentityID, &c.Scope, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ui login code consume: %w", err)
	}
	c.CreatedAt, c.ExpiresAt = fromMillis(created), fromMillis(expires)
	if !now.Before(c.ExpiresAt) {
		return nil, storage.ErrNotFound
	}
	return c, nil
}

func (s *uiSessionStore) Create(ctx context.Context, u *storage.UISession) error {
	var revoked any
	if u.RevokedAt != nil {
		revoked = toMillis(*u.RevokedAt)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ui_sessions (`+uiSessionInsertColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.SecretHash, u.PrincipalID, u.IdentityID, u.Scope, u.UserAgent, u.RemoteAddr,
		toMillis(u.CreatedAt), toMillis(u.LastSeenAt), toMillis(u.IdleExpiresAt), toMillis(u.ExpiresAt),
		revoked, u.RevokeReason)
	if err != nil {
		return fmt.Errorf("ui session create: %w", err)
	}
	return nil
}

func scanUISession(row interface{ Scan(...any) error }) (*storage.UISession, error) {
	u := &storage.UISession{}
	var created, seen, idle, expires int64
	var revoked, stewardActive sql.NullInt64
	var stewardGrant sql.NullString
	if err := row.Scan(&u.ID, &u.SecretHash, &u.PrincipalID, &u.IdentityID, &u.Scope, &u.UserAgent, &u.RemoteAddr,
		&created, &seen, &idle, &expires, &revoked, &u.RevokeReason, &stewardGrant, &stewardActive); err != nil {
		return nil, err
	}
	u.CreatedAt, u.LastSeenAt = fromMillis(created), fromMillis(seen)
	u.IdleExpiresAt, u.ExpiresAt = fromMillis(idle), fromMillis(expires)
	if revoked.Valid {
		t := fromMillis(revoked.Int64)
		u.RevokedAt = &t
	}
	if stewardGrant.Valid && stewardGrant.String != "" && stewardActive.Valid {
		t := fromMillis(stewardActive.Int64)
		u.StewardGrantID, u.StewardActiveAt = stewardGrant.String, &t
	}
	return u, nil
}

func (s *uiSessionStore) Lookup(ctx context.Context, secretHash string, now time.Time) (*storage.UISession, error) {
	n := toMillis(now)
	u, err := scanUISession(s.db.QueryRowContext(ctx, `SELECT `+uiSessionColumns+` FROM ui_sessions
		WHERE secret_hash = ? AND revoked_at IS NULL AND idle_expires_at > ? AND expires_at > ?`,
		secretHash, n, n))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ui session lookup: %w", err)
	}
	return u, nil
}

func (s *uiSessionStore) Get(ctx context.Context, id string) (*storage.UISession, error) {
	u, err := scanUISession(s.db.QueryRowContext(ctx,
		`SELECT `+uiSessionColumns+` FROM ui_sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ui session get %q: %w", id, err)
	}
	return u, nil
}

// exists distinguishes "no such session" from "update matched nothing
// because the session is revoked".
func (s *uiSessionStore) exists(ctx context.Context, id string) error {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM ui_sessions WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ErrNotFound
	}
	return err
}

func (s *uiSessionStore) Touch(ctx context.Context, id string, at, idleUntil time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE ui_sessions
		SET last_seen_at = ?, idle_expires_at = MIN(?, expires_at)
		WHERE id = ? AND revoked_at IS NULL`, toMillis(at), toMillis(idleUntil), id)
	if err != nil {
		return fmt.Errorf("ui session touch %q: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	return s.exists(ctx, id)
}

func (s *uiSessionStore) Revoke(ctx context.Context, id string, at time.Time, reason string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE ui_sessions SET revoked_at = ?, revoke_reason = ?
		WHERE id = ? AND revoked_at IS NULL`, toMillis(at), reason, id)
	if err != nil {
		return fmt.Errorf("ui session revoke %q: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	return s.exists(ctx, id)
}

func (s *uiSessionStore) Elevate(ctx context.Context, id, grantID string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE ui_sessions SET steward_grant_id = ?, steward_active_at = ?
		WHERE id = ? AND revoked_at IS NULL AND (steward_grant_id IS NULL OR steward_grant_id = ?)`,
		grantID, toMillis(at), id, grantID)
	if err != nil {
		return fmt.Errorf("ui session elevate %q: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	// Nothing matched: no such session, a revoked one, or one elevated
	// into another grant.
	var revoked sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT revoked_at FROM ui_sessions WHERE id = ?`, id).Scan(&revoked)
	switch {
	case errors.Is(err, sql.ErrNoRows), err == nil && revoked.Valid:
		return storage.ErrNotFound
	case err != nil:
		return fmt.Errorf("ui session elevate %q: %w", id, err)
	}
	return storage.ErrSessionElevated
}

func (s *uiSessionStore) TouchElevation(ctx context.Context, id, grantID string, at time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE ui_sessions SET steward_active_at = ?
		WHERE id = ? AND steward_grant_id = ? AND revoked_at IS NULL`, toMillis(at), id, grantID)
	if err != nil {
		return false, fmt.Errorf("ui session touch elevation %q: %w", id, err)
	}
	return s.matched(ctx, id, res)
}

func (s *uiSessionStore) EndElevation(ctx context.Context, id, grantID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE ui_sessions SET steward_grant_id = NULL, steward_active_at = NULL
		WHERE id = ? AND steward_grant_id = ?`, id, grantID)
	if err != nil {
		return false, fmt.Errorf("ui session end elevation %q: %w", id, err)
	}
	return s.matched(ctx, id, res)
}

// matched reports whether res changed session id's row: an update that
// matched nothing is false, or ErrNotFound when there is no such
// session.
func (s *uiSessionStore) matched(ctx context.Context, id string, res sql.Result) (bool, error) {
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	return false, s.exists(ctx, id)
}

func (s *uiSessionStore) List(ctx context.Context, f storage.UISessionFilter) ([]*storage.UISession, error) {
	// SQLite reads a negative LIMIT as no limit.
	limit := -1
	if f.Limit > 0 {
		limit = f.Limit
	}
	n := toMillis(f.Now)
	rows, err := s.db.QueryContext(ctx, `SELECT `+uiSessionColumns+` FROM ui_sessions
		WHERE (? = 0 OR (revoked_at IS NULL AND idle_expires_at > ? AND expires_at > ?))
		  AND (? = '' OR principal_id = ?)
		ORDER BY created_at DESC, id DESC LIMIT ?`,
		f.ActiveOnly, n, n, f.PrincipalID, f.PrincipalID, limit)
	if err != nil {
		return nil, fmt.Errorf("ui session list: %w", err)
	}
	defer rows.Close()
	out := []*storage.UISession{}
	for rows.Next() {
		u, err := scanUISession(rows)
		if err != nil {
			return nil, fmt.Errorf("ui session list: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *uiSessionStore) Prune(ctx context.Context, before time.Time) (int, error) {
	b := toMillis(before)
	res, err := s.db.ExecContext(ctx, `DELETE FROM ui_sessions
		WHERE (revoked_at IS NOT NULL AND revoked_at < ?) OR idle_expires_at < ? OR expires_at < ?`, b, b, b)
	if err != nil {
		return 0, fmt.Errorf("ui session prune: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM ui_login_codes WHERE expires_at < ?`, b); err != nil {
		return 0, fmt.Errorf("ui login code prune: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}
