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

// sharingStore is the sharing registry (migration 31): audiences,
// audience_members, share_policies, share_grants, access_log and the
// sharing columns of principals and principal_identities.
type sharingStore struct{ db *sql.DB }

func (s *sharingStore) Audiences() storage.AudienceStore   { return &audienceStore{db: s.db} }
func (s *sharingStore) Policies() storage.SharePolicyStore { return &sharePolicyStore{db: s.db} }
func (s *sharingStore) Grants() storage.ShareGrantStore    { return &shareGrantStore{db: s.db} }
func (s *sharingStore) AccessLog() storage.AccessLogStore  { return &accessLogStore{db: s.db} }

func (s *sharingStore) DenyClasses(ctx context.Context, principalID string) ([]string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT deny_classes FROM principals WHERE id = $1`, principalID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("principal %q: %w", principalID, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("deny classes %q: %w", principalID, err)
	}
	classes, err := storage.ParseClassesJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("deny classes %q: %w", principalID, err)
	}
	return classes, nil
}

func (s *sharingStore) SetDenyClasses(ctx context.Context, principalID string, classes []string) error {
	for _, c := range classes {
		if err := storage.ValidateClassName(c); err != nil {
			return fmt.Errorf("deny classes %q: %w", principalID, err)
		}
	}
	raw, err := storage.ClassesJSON(classes)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE principals SET deny_classes = $1 WHERE id = $2`, raw, principalID)
	if err != nil {
		return fmt.Errorf("set deny classes %q: %w", principalID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("principal %q: %w", principalID, storage.ErrNotFound)
	}
	return nil
}

func (s *sharingStore) IdentityPolicy(ctx context.Context, identityID string) (string, error) {
	var policy sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT share_policy_id FROM principal_identities WHERE id = $1`, identityID).Scan(&policy)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("identity %q: %w", identityID, storage.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("identity policy %q: %w", identityID, err)
	}
	return policy.String, nil
}

func (s *sharingStore) BindIdentityPolicy(ctx context.Context, identityID, policyID string) error {
	var policy any
	if policyID != "" {
		policy = policyID
	}
	res, err := s.db.ExecContext(ctx, `UPDATE principal_identities SET share_policy_id = $1 WHERE id = $2`, policy, identityID)
	if sentinel := constraintErr(err); sentinel != nil {
		return fmt.Errorf("share policy %q: %w", policyID, sentinel)
	}
	if err != nil {
		return fmt.Errorf("bind identity policy %q: %w", identityID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("identity %q: %w", identityID, storage.ErrNotFound)
	}
	return nil
}

// audiences

type audienceStore struct{ db *sql.DB }

const audienceColumns = `id, name, created_at`

func scanAudience(row interface{ Scan(...any) error }) (*storage.Audience, error) {
	a := &storage.Audience{}
	if err := row.Scan(&a.ID, &a.Name, &a.CreatedAt); err != nil {
		return nil, err
	}
	a.CreatedAt = a.CreatedAt.UTC()
	return a, nil
}

func (s *audienceStore) Create(ctx context.Context, a *storage.Audience) error {
	if err := storage.PrepareNewAudience(a, time.Now()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audiences (`+audienceColumns+`) VALUES ($1, $2, $3)`,
		a.ID, a.Name, a.CreatedAt)
	if sentinel := constraintErr(err); sentinel != nil {
		return fmt.Errorf("audience %q: %w", a.Name, sentinel)
	}
	if err != nil {
		return fmt.Errorf("audience create: %w", err)
	}
	return nil
}

func (s *audienceStore) get(ctx context.Context, column, value string) (*storage.Audience, error) {
	a, err := scanAudience(s.db.QueryRowContext(ctx,
		`SELECT `+audienceColumns+` FROM audiences WHERE `+column+` = $1`, value))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("audience %q: %w", value, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("audience get %q: %w", value, err)
	}
	return a, nil
}

func (s *audienceStore) Get(ctx context.Context, id string) (*storage.Audience, error) {
	return s.get(ctx, "id", id)
}

func (s *audienceStore) GetByName(ctx context.Context, name string) (*storage.Audience, error) {
	return s.get(ctx, "name", name)
}

func (s *audienceStore) List(ctx context.Context) ([]*storage.Audience, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+audienceColumns+` FROM audiences ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("audience list: %w", err)
	}
	defer rows.Close()
	out := []*storage.Audience{}
	for rows.Next() {
		a, err := scanAudience(rows)
		if err != nil {
			return nil, fmt.Errorf("audience list: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const memberColumns = `audience_id, principal_id, expires_at, added_by, added_at`

func scanMember(row interface{ Scan(...any) error }) (*storage.AudienceMember, error) {
	m := &storage.AudienceMember{}
	var expires sql.NullTime
	if err := row.Scan(&m.AudienceID, &m.PrincipalID, &expires, &m.AddedBy, &m.AddedAt); err != nil {
		return nil, err
	}
	m.ExpiresAt = timePtr(expires)
	m.AddedAt = m.AddedAt.UTC()
	return m, nil
}

func (s *audienceStore) AddMember(ctx context.Context, m *storage.AudienceMember) error {
	if err := storage.PrepareNewMember(m, time.Now()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audience_members (`+memberColumns+`) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (audience_id, principal_id) DO UPDATE SET
			expires_at = EXCLUDED.expires_at, added_by = EXCLUDED.added_by, added_at = EXCLUDED.added_at`,
		m.AudienceID, m.PrincipalID, nullTime(m.ExpiresAt), m.AddedBy, m.AddedAt)
	if sentinel := constraintErr(err); sentinel != nil {
		return fmt.Errorf("audience member %q/%q: %w", m.AudienceID, m.PrincipalID, sentinel)
	}
	if err != nil {
		return fmt.Errorf("audience add member: %w", err)
	}
	return nil
}

func (s *audienceStore) RemoveMember(ctx context.Context, audienceID, principalID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audience_members WHERE audience_id = $1 AND principal_id = $2`,
		audienceID, principalID)
	if err != nil {
		return fmt.Errorf("audience remove member: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("audience member %q/%q: %w", audienceID, principalID, storage.ErrNotFound)
	}
	return nil
}

func (s *audienceStore) listMembers(ctx context.Context, query string, args ...any) ([]*storage.AudienceMember, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*storage.AudienceMember{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *audienceStore) ListMembers(ctx context.Context, audienceID string) ([]*storage.AudienceMember, error) {
	out, err := s.listMembers(ctx, `SELECT `+memberColumns+` FROM audience_members
		WHERE audience_id = $1 ORDER BY added_at, principal_id`, audienceID)
	if err != nil {
		return nil, fmt.Errorf("audience list members: %w", err)
	}
	return out, nil
}

func (s *audienceStore) ListMemberships(ctx context.Context, principalID string, now time.Time) ([]*storage.AudienceMember, error) {
	out, err := s.listMembers(ctx, `SELECT `+memberColumns+` FROM audience_members
		WHERE principal_id = $1 AND (expires_at IS NULL OR expires_at > $2)
		ORDER BY added_at, audience_id`, principalID, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("audience list memberships: %w", err)
	}
	return out, nil
}

// policies

type sharePolicyStore struct{ db *sql.DB }

const policyColumns = `id, name, version, spec, updated_at`

func scanPolicy(row interface{ Scan(...any) error }) (*storage.SharePolicy, error) {
	p := &storage.SharePolicy{}
	var spec []byte
	if err := row.Scan(&p.ID, &p.Name, &p.Version, &spec, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.Spec = json.RawMessage(spec)
	p.UpdatedAt = p.UpdatedAt.UTC()
	return p, nil
}

func (s *sharePolicyStore) Create(ctx context.Context, p *storage.SharePolicy) error {
	if err := storage.PrepareNewSharePolicy(p, time.Now()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO share_policies (`+policyColumns+`) VALUES ($1, $2, $3, $4, $5)`,
		p.ID, p.Name, p.Version, string(p.Spec), p.UpdatedAt)
	if sentinel := constraintErr(err); sentinel != nil {
		return fmt.Errorf("share policy %q: %w", p.Name, sentinel)
	}
	if err != nil {
		return fmt.Errorf("share policy create: %w", err)
	}
	return nil
}

func (s *sharePolicyStore) get(ctx context.Context, column, value string) (*storage.SharePolicy, error) {
	p, err := scanPolicy(s.db.QueryRowContext(ctx,
		`SELECT `+policyColumns+` FROM share_policies WHERE `+column+` = $1`, value))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("share policy %q: %w", value, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("share policy get %q: %w", value, err)
	}
	return p, nil
}

func (s *sharePolicyStore) Get(ctx context.Context, id string) (*storage.SharePolicy, error) {
	return s.get(ctx, "id", id)
}

func (s *sharePolicyStore) GetByName(ctx context.Context, name string) (*storage.SharePolicy, error) {
	return s.get(ctx, "name", name)
}

func (s *sharePolicyStore) List(ctx context.Context) ([]*storage.SharePolicy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+policyColumns+` FROM share_policies ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("share policy list: %w", err)
	}
	defer rows.Close()
	out := []*storage.SharePolicy{}
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, fmt.Errorf("share policy list: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sharePolicyStore) Update(ctx context.Context, id string, spec json.RawMessage, now time.Time) (*storage.SharePolicy, error) {
	if err := storage.ValidatePolicySpec(spec); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE share_policies SET spec = $1, version = version + 1, updated_at = $2
		WHERE id = $3`, string(spec), now.UTC(), id)
	if err != nil {
		return nil, fmt.Errorf("share policy update %q: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("share policy %q: %w", id, storage.ErrNotFound)
	}
	return s.Get(ctx, id)
}

// grants

type shareGrantStore struct{ db *sql.DB }

const grantColumns = `id, audience_id, policy_id, expires_at, created_by, created_at, revoked_at`

func scanGrant(row interface{ Scan(...any) error }) (*storage.ShareGrant, error) {
	g := &storage.ShareGrant{}
	var expires, revoked sql.NullTime
	if err := row.Scan(&g.ID, &g.AudienceID, &g.PolicyID, &expires, &g.CreatedBy, &g.CreatedAt, &revoked); err != nil {
		return nil, err
	}
	g.ExpiresAt = timePtr(expires)
	g.CreatedAt = g.CreatedAt.UTC()
	g.RevokedAt = timePtr(revoked)
	return g, nil
}

func (s *shareGrantStore) Create(ctx context.Context, g *storage.ShareGrant) error {
	if err := storage.PrepareNewShareGrant(g, time.Now()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO share_grants (`+grantColumns+`) VALUES ($1, $2, $3, $4, $5, $6, NULL)`,
		g.ID, g.AudienceID, g.PolicyID, nullTime(g.ExpiresAt), g.CreatedBy, g.CreatedAt)
	if sentinel := constraintErr(err); sentinel != nil {
		return fmt.Errorf("share grant %q: %w", g.ID, sentinel)
	}
	if err != nil {
		return fmt.Errorf("share grant create: %w", err)
	}
	return nil
}

func (s *shareGrantStore) Get(ctx context.Context, id string) (*storage.ShareGrant, error) {
	g, err := scanGrant(s.db.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM share_grants WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("share grant %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("share grant get %q: %w", id, err)
	}
	return g, nil
}

func (s *shareGrantStore) List(ctx context.Context, f storage.GrantFilter) ([]*storage.ShareGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+grantColumns+` FROM share_grants
		WHERE ($1 = '' OR audience_id = $1) AND ($2 = '' OR policy_id = $2) AND ($3 OR revoked_at IS NULL)
		ORDER BY created_at, id`, f.AudienceID, f.PolicyID, f.IncludeRevoked)
	if err != nil {
		return nil, fmt.Errorf("share grant list: %w", err)
	}
	defer rows.Close()
	out := []*storage.ShareGrant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("share grant list: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *shareGrantStore) Revoke(ctx context.Context, id string, at time.Time) error {
	var revoked sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT revoked_at FROM share_grants WHERE id = $1`, id).Scan(&revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("share grant %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("share grant revoke %q: %w", id, err)
	}
	if revoked.Valid {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE share_grants SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL`,
		at.UTC(), id); err != nil {
		return fmt.Errorf("share grant revoke %q: %w", id, err)
	}
	return nil
}

func (s *shareGrantStore) ListActiveForPrincipal(ctx context.Context, principalID string, now time.Time) ([]*storage.ActiveGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
			g.id, g.audience_id, g.policy_id, g.expires_at, g.created_by, g.created_at, g.revoked_at,
			a.id, a.name, a.created_at,
			p.id, p.name, p.version, p.spec, p.updated_at
		FROM audience_members m
		JOIN audiences a ON a.id = m.audience_id
		JOIN share_grants g ON g.audience_id = m.audience_id
		JOIN share_policies p ON p.id = g.policy_id
		WHERE m.principal_id = $1 AND (m.expires_at IS NULL OR m.expires_at > $2)
		  AND g.revoked_at IS NULL AND (g.expires_at IS NULL OR g.expires_at > $2)
		ORDER BY g.created_at, g.id`, principalID, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("share grant list active: %w", err)
	}
	defer rows.Close()
	out := []*storage.ActiveGrant{}
	for rows.Next() {
		var (
			ag                 storage.ActiveGrant
			gExpires, gRevoked sql.NullTime
			spec               []byte
		)
		if err := rows.Scan(
			&ag.Grant.ID, &ag.Grant.AudienceID, &ag.Grant.PolicyID, &gExpires, &ag.Grant.CreatedBy, &ag.Grant.CreatedAt, &gRevoked,
			&ag.Audience.ID, &ag.Audience.Name, &ag.Audience.CreatedAt,
			&ag.Policy.ID, &ag.Policy.Name, &ag.Policy.Version, &spec, &ag.Policy.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("share grant list active: %w", err)
		}
		ag.Grant.ExpiresAt = timePtr(gExpires)
		ag.Grant.CreatedAt = ag.Grant.CreatedAt.UTC()
		ag.Grant.RevokedAt = timePtr(gRevoked)
		ag.Audience.CreatedAt = ag.Audience.CreatedAt.UTC()
		ag.Policy.Spec = json.RawMessage(spec)
		ag.Policy.UpdatedAt = ag.Policy.UpdatedAt.UTC()
		out = append(out, &ag)
	}
	return out, rows.Err()
}

// access log

type accessLogStore struct{ db *sql.DB }

const accessColumns = `id, at, principal_id, identity_id, route, policy_versions, object_count, entity_count`

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
		var identity any
		if e.IdentityID != "" {
			identity = e.IdentityID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_log (`+accessColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			e.ID, e.At, e.PrincipalID, identity, e.Route, e.PolicyVersions, e.ObjectCount, e.EntityCount); err != nil {
			return fmt.Errorf("access log append %q: %w", e.ID, err)
		}
		for _, id := range e.ObjectIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO access_log_objects (access_id, object_id) VALUES ($1, $2)
				ON CONFLICT DO NOTHING`, e.ID, id); err != nil {
				return fmt.Errorf("access log append %q objects: %w", e.ID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("access log append: commit: %w", err)
	}
	return nil
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
		conds = append(conds, "id IN (SELECT access_id FROM access_log_objects WHERE object_id = "+bind(f.ObjectID)+")")
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
		e := &storage.AccessEntry{}
		var identity sql.NullString
		if err := rows.Scan(&e.ID, &e.At, &e.PrincipalID, &identity, &e.Route, &e.PolicyVersions, &e.ObjectCount, &e.EntityCount); err != nil {
			return nil, fmt.Errorf("access log query: %w", err)
		}
		e.At = e.At.UTC()
		e.IdentityID = identity.String
		out = append(out, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("access log query: %w", err)
	}
	if len(out) == 0 {
		return out, nil
	}
	byID := make(map[string]*storage.AccessEntry, len(out))
	for _, e := range out {
		byID[e.ID] = e
	}
	orows, err := s.db.QueryContext(ctx, `SELECT access_id, object_id FROM access_log_objects
		WHERE access_id = ANY($1) ORDER BY access_id, object_id`, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("access log objects: %w", err)
	}
	defer orows.Close()
	for orows.Next() {
		var accessID, objectID string
		if err := orows.Scan(&accessID, &objectID); err != nil {
			return nil, fmt.Errorf("access log objects: %w", err)
		}
		if e := byID[accessID]; e != nil {
			e.ObjectIDs = append(e.ObjectIDs, objectID)
		}
	}
	if err := orows.Err(); err != nil {
		return nil, fmt.Errorf("access log objects: %w", err)
	}
	return out, nil
}

func (s *accessLogStore) Prune(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM access_log_objects
		WHERE access_id IN (SELECT id FROM access_log WHERE at < $1)`, before.UTC())
	if err != nil {
		return 0, fmt.Errorf("access log prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
