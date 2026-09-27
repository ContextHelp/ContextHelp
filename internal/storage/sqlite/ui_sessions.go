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
// ui_sessions and ui_login_codes tables (migration 041). Times are Unix
// milliseconds, UTC.
type uiSessionStore struct{ db *sql.DB }

const uiSessionColumns = `id, secret_hash, principal_id, token_hash, scope, user_agent, remote_addr,
	created_at, last_seen_at, idle_expires_at, expires_at, revoked_at, revoke_reason`

func toMillis(t time.Time) int64 { return t.UTC().UnixMilli() }

func fromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func (s *uiSessionStore) CreateLoginCode(ctx context.Context, c *storage.UILoginCode) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ui_login_codes
		(code_hash, principal_id, token_hash, scope, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		c.CodeHash, c.PrincipalID, c.TokenHash, c.Scope, toMillis(c.CreatedAt), toMillis(c.ExpiresAt))
	if err != nil {
		return fmt.Errorf("ui login code create: %w", err)
	}
	return nil
}

func (s *uiSessionStore) ConsumeLoginCode(ctx context.Context, codeHash string, now time.Time) (*storage.UILoginCode, error) {
	c := &storage.UILoginCode{CodeHash: codeHash}
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `DELETE FROM ui_login_codes WHERE code_hash = ?
		RETURNING principal_id, token_hash, scope, created_at, expires_at`, codeHash,
	).Scan(&c.PrincipalID, &c.TokenHash, &c.Scope, &created, &expires)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO ui_sessions (`+uiSessionColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.SecretHash, u.PrincipalID, u.TokenHash, u.Scope, u.UserAgent, u.RemoteAddr,
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
	var revoked sql.NullInt64
	if err := row.Scan(&u.ID, &u.SecretHash, &u.PrincipalID, &u.TokenHash, &u.Scope, &u.UserAgent, &u.RemoteAddr,
		&created, &seen, &idle, &expires, &revoked, &u.RevokeReason); err != nil {
		return nil, err
	}
	u.CreatedAt, u.LastSeenAt = fromMillis(created), fromMillis(seen)
	u.IdleExpiresAt, u.ExpiresAt = fromMillis(idle), fromMillis(expires)
	if revoked.Valid {
		t := fromMillis(revoked.Int64)
		u.RevokedAt = &t
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

func (s *uiSessionStore) RevokeByTokenHash(ctx context.Context, tokenHash string, at time.Time, reason string) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE ui_sessions SET revoked_at = ?, revoke_reason = ?
		WHERE token_hash = ? AND revoked_at IS NULL`, toMillis(at), reason, tokenHash)
	if err != nil {
		return 0, fmt.Errorf("ui session revoke by token: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
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
