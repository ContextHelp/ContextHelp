package storage

import (
	"context"
	"time"
)

// UISession is one browser sign-in to the web UI: the server half of a
// session cookie. The cookie carries a random secret; only its hash is
// stored, so a leaked database cannot be replayed as cookies.
//
// A session is active while it is not revoked and now is before both
// IdleExpiresAt and ExpiresAt. Touch moves IdleExpiresAt forward, never
// past ExpiresAt.
type UISession struct {
	// ID is the public handle used to list and revoke the session. It
	// grants nothing on its own.
	ID string `json:"id"`
	// SecretHash is the hex SHA-256 of the cookie secret.
	SecretHash string `json:"-"`
	// PrincipalID is the principal the session acts as.
	PrincipalID string `json:"principal_id"`
	// TokenHash is the hex SHA-256 of the static token that minted the
	// session. The session ends when that token leaves the config.
	TokenHash string `json:"-"`
	// Scope holds the session kind ("ui", stored in the scope column).
	// The kind fixes the scope set the session narrows its token's
	// scopes to (auth.SessionScopes).
	Scope string `json:"kind"`
	// UserAgent and RemoteAddr describe the browser that signed in, for
	// listing only.
	UserAgent  string `json:"user_agent,omitempty"`
	RemoteAddr string `json:"remote_addr,omitempty"`

	CreatedAt     time.Time `json:"created_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	IdleExpiresAt time.Time `json:"idle_expires_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	// RevokedAt is set once the session was revoked; RevokeReason says
	// why ("logout", "revoked", "token removed").
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	RevokeReason string     `json:"revoke_reason,omitempty"`
}

// Active reports whether the session may still authenticate at now.
func (s *UISession) Active(now time.Time) bool {
	return s != nil && s.RevokedAt == nil && now.Before(s.IdleExpiresAt) && now.Before(s.ExpiresAt)
}

// UILoginCode is a one-time code that a browser exchanges for a
// UISession. Only its hash is stored.
type UILoginCode struct {
	// CodeHash is the hex SHA-256 of the code.
	CodeHash    string
	PrincipalID string
	// TokenHash is the hex SHA-256 of the static token that minted the
	// code; the session inherits it.
	TokenHash string
	// Scope holds the kind of the session the code opens ("ui").
	Scope     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// UISessionFilter narrows UISessionStore.List.
type UISessionFilter struct {
	// ActiveOnly keeps sessions that are active at Now.
	ActiveOnly bool
	Now        time.Time
	// PrincipalID, when set, keeps that principal's sessions only.
	PrincipalID string
	// Limit caps the result; 0 means no cap.
	Limit int
}

// UISessionStore persists web UI sessions and their login codes.
type UISessionStore interface {
	// CreateLoginCode stores a one-time login code.
	CreateLoginCode(ctx context.Context, c *UILoginCode) error
	// ConsumeLoginCode deletes and returns the code with codeHash in one
	// step, so two exchanges of one code cannot both succeed. An
	// unknown, already used or expired (at now) code is ErrNotFound.
	ConsumeLoginCode(ctx context.Context, codeHash string, now time.Time) (*UILoginCode, error)

	// Create stores a new session.
	Create(ctx context.Context, s *UISession) error
	// Lookup returns the session whose secret hashes to secretHash if it
	// is active at now, and ErrNotFound otherwise.
	Lookup(ctx context.Context, secretHash string, now time.Time) (*UISession, error)
	// Get returns the session with id whatever its state, or ErrNotFound.
	Get(ctx context.Context, id string) (*UISession, error)
	// Touch records activity at at: LastSeenAt becomes at and
	// IdleExpiresAt becomes idleUntil, capped at ExpiresAt. A revoked
	// session is left alone. An unknown id is ErrNotFound.
	Touch(ctx context.Context, id string, at, idleUntil time.Time) error
	// Revoke ends the session with id at at. Revoking a revoked session
	// keeps its first revocation. An unknown id is ErrNotFound.
	Revoke(ctx context.Context, id string, at time.Time, reason string) error
	// RevokeByTokenHash revokes every unrevoked session minted by the
	// token with tokenHash and returns how many it revoked.
	RevokeByTokenHash(ctx context.Context, tokenHash string, at time.Time, reason string) (int, error)
	// List returns sessions, newest first.
	List(ctx context.Context, f UISessionFilter) ([]*UISession, error)
	// Prune deletes sessions that ended (expired or were revoked) before
	// before, and login codes that expired before it. It returns the
	// number of sessions deleted.
	Prune(ctx context.Context, before time.Time) (int, error)
}
