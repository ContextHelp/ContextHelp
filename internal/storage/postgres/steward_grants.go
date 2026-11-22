package postgres

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

// stewardGrantStore is the grant half of the steward grants (migration
// 44, mirroring SQLite 063): steward_grants. Every statement runs on
// conn(ctx), so the store joins the transaction of the approval that
// writes a grant.
type stewardGrantStore struct{ db *sql.DB }

const stewardGrantColumns = `id, request_id, steward_id, matter_ref, scope, scope_digest, status,
	not_before, expires_at, approved_at, revoked_at, revoked_by, revoke_reason, renews_id`

func (s *stewardGrantStore) Create(ctx context.Context, g *storage.StewardGrant) error {
	if err := storage.PrepareNewStewardGrant(g, time.Now()); err != nil {
		return err
	}
	var renews any
	if g.RenewsID != "" {
		renews = g.RenewsID
	}
	_, err := conn(ctx, s.db).ExecContext(ctx, `INSERT INTO steward_grants (`+stewardGrantColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL, '', '', $11)`,
		g.ID, g.RequestID, g.StewardID, g.MatterRef, string(g.Scope), g.ScopeDigest, string(g.Status),
		g.NotBefore.UTC(), g.ExpiresAt.UTC(), g.ApprovedAt.UTC(), renews)
	if sentinel := constraintErr(err); sentinel != nil {
		return fmt.Errorf("steward grant %q: %w", g.ID, sentinel)
	}
	if err != nil {
		return fmt.Errorf("steward grant create: %w", err)
	}
	return nil
}

func scanStewardGrant(row interface{ Scan(...any) error }) (*storage.StewardGrant, error) {
	g := &storage.StewardGrant{}
	var scope, status string
	var revoked sql.NullTime
	var renews sql.NullString
	if err := row.Scan(&g.ID, &g.RequestID, &g.StewardID, &g.MatterRef, &scope, &g.ScopeDigest, &status,
		&g.NotBefore, &g.ExpiresAt, &g.ApprovedAt, &revoked, &g.RevokedBy, &g.RevokeReason, &renews); err != nil {
		return nil, err
	}
	g.Scope = json.RawMessage(scope)
	g.Status = storage.StewardGrantStatus(status)
	g.NotBefore, g.ExpiresAt, g.ApprovedAt = g.NotBefore.UTC(), g.ExpiresAt.UTC(), g.ApprovedAt.UTC()
	g.RevokedAt = timePtr(revoked)
	g.RenewsID = renews.String
	return g, nil
}

func (s *stewardGrantStore) get(ctx context.Context, what, column, value string) (*storage.StewardGrant, error) {
	g, err := scanStewardGrant(conn(ctx, s.db).QueryRowContext(ctx,
		`SELECT `+stewardGrantColumns+` FROM steward_grants WHERE `+column+` = $1`, value))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%s %q: %w", what, value, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", what, value, err)
	}
	return g, nil
}

func (s *stewardGrantStore) Get(ctx context.Context, id string) (*storage.StewardGrant, error) {
	return s.get(ctx, "steward grant", "id", id)
}

func (s *stewardGrantStore) GetByRequest(ctx context.Context, requestID string) (*storage.StewardGrant, error) {
	return s.get(ctx, "steward grant of request", "request_id", requestID)
}

func (s *stewardGrantStore) List(ctx context.Context, f storage.StewardGrantFilter) ([]*storage.StewardGrant, error) {
	var where []string
	var args []any
	for _, c := range []struct{ col, val string }{{"steward_id", f.StewardID}, {"status", string(f.Status)}} {
		if c.val != "" {
			args = append(args, c.val)
			where = append(where, fmt.Sprintf("%s = $%d", c.col, len(args)))
		}
	}
	query := `SELECT ` + stewardGrantColumns + ` FROM steward_grants`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY approved_at DESC, id DESC`
	if f.Limit > 0 {
		args = append(args, f.Limit)
		query += fmt.Sprintf(` LIMIT $%d`, len(args))
	}
	rows, err := conn(ctx, s.db).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("steward grants: %w", err)
	}
	defer rows.Close()
	out := []*storage.StewardGrant{}
	for rows.Next() {
		g, err := scanStewardGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("steward grants: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("steward grants: %w", err)
	}
	return out, nil
}

func (s *stewardGrantStore) Revoke(ctx context.Context, id, by, reason string, at time.Time) error {
	if err := storage.ValidateStewardGrantRevocation(by); err != nil {
		return err
	}
	q := conn(ctx, s.db)
	res, err := q.ExecContext(ctx, `UPDATE steward_grants SET status = $1, revoked_at = $2, revoked_by = $3, revoke_reason = $4
		WHERE id = $5 AND status = $6`, string(storage.StewardGrantRevoked), at.UTC(), strings.TrimSpace(by), strings.TrimSpace(reason),
		id, string(storage.StewardGrantApproved))
	if err != nil {
		return fmt.Errorf("revoke steward grant %q: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var status string
	err = q.QueryRowContext(ctx, `SELECT status FROM steward_grants WHERE id = $1`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("steward grant %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("revoke steward grant %q: %w", id, err)
	}
	return fmt.Errorf("steward grant %q is %s: %w", id, status, storage.ErrStewardGrantNotApproved)
}
