package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// uiSessionStore persists web UI sessions and login codes in the
// ui_sessions and ui_login_codes tables (migration 19).
type uiSessionStore struct{ db *sql.DB }

const uiSessionColumns = `id, secret_hash, principal_id, token_hash, scope, user_agent, remote_addr,
	created_at, last_seen_at, idle_expires_at, expires_at, revoked_at, revoke_reason`

func (s *uiSessionStore) CreateLoginCode(ctx context.Context, c *storage.UILoginCode) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ui_login_codes
		(code_hash, principal_id, token_hash, scope, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		c.CodeHash, c.PrincipalID, c.TokenHash, c.Scope, c.CreatedAt.UTC(), c.ExpiresAt.UTC())
	if err != nil {
		return fmt.Errorf("ui login code create: %w", err)
	}
	return nil
}

func (s *uiSessionStore) ConsumeLoginCode(ctx context.Context, codeHash string, now time.Time) (*storage.UILoginCode, error) {
	c := &storage.UILoginCode{CodeHash: codeHash}
	err := s.db.QueryRowContext(ctx, `DELETE FROM ui_login_codes WHERE code_hash = $1
		RETURNING principal_id, token_hash, scope, created_at, expires_at`, codeHash,
	).Scan(&c.PrincipalID, &c.TokenHash, &c.Scope, &c.CreatedAt, &c.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ui login code consume: %w", err)
	}
	c.CreatedAt, c.ExpiresAt = c.CreatedAt.UTC(), c.ExpiresAt.UTC()
	if !now.Before(c.ExpiresAt) {
		return nil, storage.ErrNotFound
	}
	return c, nil
}

func (s *uiSessionStore) Create(ctx context.Context, u *storage.UISession) error {
	var revoked any
	if u.RevokedAt != nil {
		revoked = u.RevokedAt.UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ui_sessions (`+uiSessionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		u.ID, u.SecretHash, u.PrincipalID, u.TokenHash, u.Scope, u.UserAgent, u.RemoteAddr,
		u.CreatedAt.UTC(), u.LastSeenAt.UTC(), u.IdleExpiresAt.UTC(), u.ExpiresAt.UTC(),
		revoked, u.RevokeReason)
	if err != nil {
		return fmt.Errorf("ui session create: %w", err)
	}
	return nil
}

func scanUISession(row interface{ Scan(...any) error }) (*storage.UISession, error) {
	u := &storage.UISession{}
	var revoked sql.NullTime
	if err := row.Scan(&u.ID, &u.SecretHash, &u.PrincipalID, &u.TokenHash, &u.Scope, &u.UserAgent, &u.RemoteAddr,
		&u.CreatedAt, &u.LastSeenAt, &u.IdleExpiresAt, &u.ExpiresAt, &revoked, &u.RevokeReason); err != nil {
		return nil, err
	}
	u.CreatedAt, u.LastSeenAt = u.CreatedAt.UTC(), u.LastSeenAt.UTC()
	u.IdleExpiresAt, u.ExpiresAt = u.IdleExpiresAt.UTC(), u.ExpiresAt.UTC()
	if revoked.Valid {
		t := revoked.Time.UTC()
		u.RevokedAt = &t
	}
	return u, nil
}

func (s *uiSessionStore) Lookup(ctx context.Context, secretHash string, now time.Time) (*storage.UISession, error) {
	u, err := scanUISession(s.db.QueryRowContext(ctx, `SELECT `+uiSessionColumns+` FROM ui_sessions
		WHERE secret_hash = $1 AND revoked_at IS NULL AND idle_expires_at > $2 AND expires_at > $2`,
		secretHash, now.UTC()))
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
		`SELECT `+uiSessionColumns+` FROM ui_sessions WHERE id = $1`, id))
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
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM ui_sessions WHERE id = $1`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ErrNotFound
	}
	return err
}

func (s *uiSessionStore) Touch(ctx context.Context, id string, at, idleUntil time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE ui_sessions
		SET last_seen_at = $1, idle_expires_at = LEAST($2::timestamptz, expires_at)
		WHERE id = $3 AND revoked_at IS NULL`, at.UTC(), idleUntil.UTC(), id)
	if err != nil {
		return fmt.Errorf("ui session touch %q: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	return s.exists(ctx, id)
}

func (s *uiSessionStore) Revoke(ctx context.Context, id string, at time.Time, reason string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE ui_sessions SET revoked_at = $1, revoke_reason = $2
		WHERE id = $3 AND revoked_at IS NULL`, at.UTC(), reason, id)
	if err != nil {
		return fmt.Errorf("ui session revoke %q: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	return s.exists(ctx, id)
}

func (s *uiSessionStore) RevokeByTokenHash(ctx context.Context, tokenHash string, at time.Time, reason string) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE ui_sessions SET revoked_at = $1, revoke_reason = $2
		WHERE token_hash = $3 AND revoked_at IS NULL`, at.UTC(), reason, tokenHash)
	if err != nil {
		return 0, fmt.Errorf("ui session revoke by token: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *uiSessionStore) List(ctx context.Context, f storage.UISessionFilter) ([]*storage.UISession, error) {
	// LIMIT NULL is no limit.
	var limit any
	if f.Limit > 0 {
		limit = f.Limit
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+uiSessionColumns+` FROM ui_sessions
		WHERE (NOT $1::boolean OR (revoked_at IS NULL AND idle_expires_at > $2 AND expires_at > $2))
		  AND ($3::text = '' OR principal_id = $3)
		ORDER BY created_at DESC, id DESC LIMIT $4`,
		f.ActiveOnly, f.Now.UTC(), f.PrincipalID, limit)
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
	b := before.UTC()
	res, err := s.db.ExecContext(ctx, `DELETE FROM ui_sessions
		WHERE (revoked_at IS NOT NULL AND revoked_at < $1) OR idle_expires_at < $1 OR expires_at < $1`, b)
	if err != nil {
		return 0, fmt.Errorf("ui session prune: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM ui_login_codes WHERE expires_at < $1`, b); err != nil {
		return 0, fmt.Errorf("ui login code prune: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}
