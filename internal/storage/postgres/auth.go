package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// authStore is the principal registry (migration 28): principals,
// principal_identities, auth_tickets and kv.
type authStore struct{ db *sql.DB }

func (s *authStore) Principals() storage.PrincipalStore { return &principalStore{db: s.db} }
func (s *authStore) Identities() storage.IdentityStore  { return &identityStore{db: s.db} }
func (s *authStore) Tickets() storage.TicketStore       { return &ticketStore{db: s.db} }
func (s *authStore) KV() storage.KVStore                { return &kvStore{db: s.db} }

// constraintErr maps a constraint violation to the storage sentinel it
// stands for: a unique violation is ErrAlreadyExists, a foreign key
// violation is ErrNotFound (the referenced principal). Any other error
// maps to nil.
func constraintErr(err error) error {
	var pe *pq.Error
	if !errors.As(err, &pe) {
		return nil
	}
	switch pe.Code {
	case "23505":
		return storage.ErrAlreadyExists
	case "23503":
		return storage.ErrNotFound
	}
	return nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func timePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time.UTC()
	return &t
}

// principals

type principalStore struct{ db *sql.DB }

const principalColumns = `id, kind, display_name, roles, status, created_at, revoked_at`

func (s *principalStore) Create(ctx context.Context, p *storage.Principal) error {
	if err := storage.PrepareNewPrincipal(p, time.Now()); err != nil {
		return err
	}
	roles, err := storage.RolesJSON(p.Roles)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO principals (`+principalColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		p.ID, p.Kind, p.DisplayName, roles, p.Status, p.CreatedAt.UTC(), nullTime(p.RevokedAt))
	if sentinel := constraintErr(err); sentinel != nil {
		return fmt.Errorf("principal %q: %w", p.ID, sentinel)
	}
	if err != nil {
		return fmt.Errorf("principal create: %w", err)
	}
	return nil
}

func scanPrincipal(row interface{ Scan(...any) error }) (*storage.Principal, error) {
	p := &storage.Principal{}
	var roles string
	var revoked sql.NullTime
	if err := row.Scan(&p.ID, &p.Kind, &p.DisplayName, &roles, &p.Status, &p.CreatedAt, &revoked); err != nil {
		return nil, err
	}
	var err error
	if p.Roles, err = storage.ParseRolesJSON(roles); err != nil {
		return nil, err
	}
	p.CreatedAt = p.CreatedAt.UTC()
	p.RevokedAt = timePtr(revoked)
	return p, nil
}

func (s *principalStore) Get(ctx context.Context, id string) (*storage.Principal, error) {
	p, err := scanPrincipal(s.db.QueryRowContext(ctx,
		`SELECT `+principalColumns+` FROM principals WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("principal %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("principal get %q: %w", id, err)
	}
	return p, nil
}

func (s *principalStore) List(ctx context.Context, f storage.PrincipalFilter) ([]*storage.Principal, error) {
	// LIMIT NULL is no limit.
	var limit any
	if f.Limit > 0 {
		limit = f.Limit
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+principalColumns+` FROM principals
		WHERE ($1::text = '' OR kind = $1) AND ($2::text = '' OR status = $2)
		ORDER BY created_at, id LIMIT $3`, f.Kind, f.Status, limit)
	if err != nil {
		return nil, fmt.Errorf("principal list: %w", err)
	}
	defer rows.Close()
	out := []*storage.Principal{}
	for rows.Next() {
		p, err := scanPrincipal(rows)
		if err != nil {
			return nil, fmt.Errorf("principal list: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// guard loads the principal and refuses the status changes the store
// never makes: none on the owner.
func (s *principalStore) guard(ctx context.Context, id string) (*storage.Principal, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Kind == storage.PrincipalKindOwner {
		return nil, storage.ErrOwnerProtected
	}
	return p, nil
}

func (s *principalStore) Suspend(ctx context.Context, id string) error {
	p, err := s.guard(ctx, id)
	if err != nil {
		return err
	}
	switch p.Status {
	case storage.PrincipalSuspended:
		return nil
	case storage.PrincipalRevoked:
		return fmt.Errorf("principal %q: %w", id, storage.ErrPrincipalInactive)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE principals SET status = $1 WHERE id = $2 AND status = $3`,
		storage.PrincipalSuspended, id, storage.PrincipalActive); err != nil {
		return fmt.Errorf("principal suspend %q: %w", id, err)
	}
	return nil
}

func (s *principalStore) Revoke(ctx context.Context, id string, at time.Time) error {
	p, err := s.guard(ctx, id)
	if err != nil {
		return err
	}
	if p.Status == storage.PrincipalRevoked {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("principal revoke %q: begin: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	at = at.UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE principals SET status = $1, revoked_at = $2
		WHERE id = $3 AND status <> $1`, storage.PrincipalRevoked, at, id); err != nil {
		return fmt.Errorf("principal revoke %q: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE principal_identities SET revoked_at = $1
		WHERE principal_id = $2 AND revoked_at IS NULL`, at, id); err != nil {
		return fmt.Errorf("principal revoke %q: identities: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("principal revoke %q: commit: %w", id, err)
	}
	return nil
}

// identities

type identityStore struct{ db *sql.DB }

const identityColumns = `id, principal_id, method, issuer, subject, name, created_at, revoked_at, last_used_at`

func (s *identityStore) Create(ctx context.Context, i *storage.Identity) error {
	if err := storage.PrepareNewIdentity(i, time.Now()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO principal_identities (`+identityColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		i.ID, i.PrincipalID, i.Method, i.Issuer, i.Subject, i.Name,
		i.CreatedAt.UTC(), nullTime(i.RevokedAt), nullTime(i.LastUsedAt))
	if sentinel := constraintErr(err); sentinel != nil {
		if errors.Is(sentinel, storage.ErrNotFound) {
			return fmt.Errorf("identity %q: principal %q: %w", i.ID, i.PrincipalID, sentinel)
		}
		return fmt.Errorf("identity %s/%s/%s: %w", i.Method, i.Issuer, i.Subject, sentinel)
	}
	if err != nil {
		return fmt.Errorf("identity create: %w", err)
	}
	return nil
}

func scanIdentity(row interface{ Scan(...any) error }) (*storage.Identity, error) {
	i := &storage.Identity{}
	var revoked, used sql.NullTime
	if err := row.Scan(&i.ID, &i.PrincipalID, &i.Method, &i.Issuer, &i.Subject, &i.Name,
		&i.CreatedAt, &revoked, &used); err != nil {
		return nil, err
	}
	i.CreatedAt = i.CreatedAt.UTC()
	i.RevokedAt, i.LastUsedAt = timePtr(revoked), timePtr(used)
	return i, nil
}

func (s *identityStore) Get(ctx context.Context, id string) (*storage.Identity, error) {
	i, err := scanIdentity(s.db.QueryRowContext(ctx,
		`SELECT `+identityColumns+` FROM principal_identities WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("identity %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("identity get %q: %w", id, err)
	}
	return i, nil
}

func (s *identityStore) Find(ctx context.Context, method, issuer, subject string) (*storage.Identity, error) {
	i, err := scanIdentity(s.db.QueryRowContext(ctx, `SELECT `+identityColumns+` FROM principal_identities
		WHERE method = $1 AND issuer = $2 AND subject = $3`, method, issuer, subject))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("identity %s/%s/%s: %w", method, issuer, subject, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("identity find: %w", err)
	}
	return i, nil
}

func (s *identityStore) ListByPrincipal(ctx context.Context, principalID string) ([]*storage.Identity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+identityColumns+` FROM principal_identities
		WHERE principal_id = $1 ORDER BY created_at, id`, principalID)
	if err != nil {
		return nil, fmt.Errorf("identity list: %w", err)
	}
	defer rows.Close()
	out := []*storage.Identity{}
	for rows.Next() {
		i, err := scanIdentity(rows)
		if err != nil {
			return nil, fmt.Errorf("identity list: %w", err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// exists distinguishes "no such identity" from "update matched nothing
// because the identity is revoked or was touched recently".
func (s *identityStore) exists(ctx context.Context, id string) error {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM principal_identities WHERE id = $1`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("identity %q: %w", id, storage.ErrNotFound)
	}
	return err
}

func (s *identityStore) Revoke(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE principal_identities SET revoked_at = $1
		WHERE id = $2 AND revoked_at IS NULL`, at.UTC(), id)
	if err != nil {
		return fmt.Errorf("identity revoke %q: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	return s.exists(ctx, id)
}

func (s *identityStore) Touch(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE principal_identities SET last_used_at = $1
		WHERE id = $2 AND revoked_at IS NULL AND (last_used_at IS NULL OR last_used_at <= $3)`,
		at.UTC(), id, at.Add(-storage.IdentityTouchEvery).UTC())
	if err != nil {
		return fmt.Errorf("identity touch %q: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	return s.exists(ctx, id)
}

// tickets

type ticketStore struct{ db *sql.DB }

const ticketColumns = `kind, principal_id, grants, created_by, created_at, expires_at, redeemed_at`

func (s *ticketStore) Issue(ctx context.Context, t storage.TicketIssue) (string, error) {
	if err := storage.PrepareTicketIssue(&t); err != nil {
		return "", err
	}
	secret, err := storage.NewTicketSecret()
	if err != nil {
		return "", err
	}
	var principal any
	if t.PrincipalID != "" {
		principal = t.PrincipalID
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO auth_tickets (hash, `+ticketColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULL)`,
		storage.TicketHash(secret), t.Kind, principal, string(t.Payload), t.CreatedBy,
		t.Now, t.Now.Add(t.TTL))
	if sentinel := constraintErr(err); sentinel != nil {
		return "", fmt.Errorf("ticket issue: principal: %w", sentinel)
	}
	if err != nil {
		return "", fmt.Errorf("ticket issue: %w", err)
	}
	return secret, nil
}

func scanTicket(row interface{ Scan(...any) error }) (*storage.Ticket, error) {
	t := &storage.Ticket{}
	var principal sql.NullString
	var grants string
	var redeemed sql.NullTime
	if err := row.Scan(&t.Kind, &principal, &grants, &t.CreatedBy, &t.CreatedAt, &t.ExpiresAt, &redeemed); err != nil {
		return nil, err
	}
	t.PrincipalID = principal.String
	t.Payload = []byte(grants)
	t.CreatedAt, t.ExpiresAt = t.CreatedAt.UTC(), t.ExpiresAt.UTC()
	t.RedeemedAt = timePtr(redeemed)
	return t, nil
}

// Redeem is one conditional UPDATE ... RETURNING: the row is marked
// redeemed only while it is unredeemed, unexpired, of the asked kind
// and bound to no principal or an active one. Under READ COMMITTED a
// concurrent redemption blocks on the row lock and re-evaluates the
// WHERE on the committed row, so exactly one sees a row change. A miss
// is classified afterwards, read-only.
func (s *ticketStore) Redeem(ctx context.Context, kind, secret string, now time.Time) (*storage.Ticket, error) {
	hash := storage.TicketHash(secret)
	now = now.UTC()
	t, err := scanTicket(s.db.QueryRowContext(ctx, `UPDATE auth_tickets t SET redeemed_at = $1
		WHERE t.hash = $2 AND t.kind = $3 AND t.redeemed_at IS NULL AND t.expires_at > $1
		  AND (t.principal_id IS NULL OR EXISTS (
		        SELECT 1 FROM principals p WHERE p.id = t.principal_id AND p.status = $4))
		RETURNING `+ticketColumns, now, hash, kind, storage.PrincipalActive))
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("ticket redeem: %w", err)
	}
	// Tell a ticket of a suspended or revoked principal from every
	// other refusal; the ticket itself stays unredeemed.
	var status sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT p.status FROM auth_tickets t
		LEFT JOIN principals p ON p.id = t.principal_id
		WHERE t.hash = $1 AND t.kind = $2 AND t.redeemed_at IS NULL AND t.expires_at > $3`,
		hash, kind, now).Scan(&status)
	if err == nil && status.Valid && status.String != storage.PrincipalActive {
		return nil, fmt.Errorf("ticket: %w", storage.ErrPrincipalInactive)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("ticket redeem: %w", err)
	}
	return nil, fmt.Errorf("ticket: %w", storage.ErrNotFound)
}

// kv

type kvStore struct{ db *sql.DB }

func (s *kvStore) Put(ctx context.Context, key string, value []byte) error {
	if value == nil {
		value = []byte{}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO kv (key, value, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		key, value, time.Now().UTC()); err != nil {
		return fmt.Errorf("kv put %q: %w", key, err)
	}
	return nil
}

func (s *kvStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	var value []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("kv get %q: %w", key, err)
	}
	if value == nil {
		value = []byte{}
	}
	return value, true, nil
}

func (s *kvStore) Delete(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM kv WHERE key = $1`, key); err != nil {
		return fmt.Errorf("kv delete %q: %w", key, err)
	}
	return nil
}

// likePrefix escapes prefix for a LIKE pattern anchored at the start.
func likePrefix(prefix string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(prefix) + "%"
}

func (s *kvStore) List(ctx context.Context, prefix string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key FROM kv WHERE key LIKE $1 ESCAPE '\' ORDER BY key COLLATE "C"`, likePrefix(prefix))
	if err != nil {
		return nil, fmt.Errorf("kv list %q: %w", prefix, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("kv list %q: %w", prefix, err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (*kvStore) Close() error { return nil }
