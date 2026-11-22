package auth

import (
	"context"
	"errors"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A browser session's steward elevation (ADR-083 §3): the session row
// names one steward grant, and every request the session authenticates
// carries the grant's id in its principal's Meta (MetaStewardGrantID),
// from which the transports resolve the grant-only viewer. Nothing a
// client sends sets it: only Elevate, after the caller checked the
// grant, writes the row, and only the session authenticator reads it.
// An elevation ends when the steward ends it, when it has been idle
// for steward.elevation_idle, or when a request under it is refused;
// the session lives on.

// Why an elevation ended (ElevationEvent.Reason).
const (
	// ElevationEndedByCaller: the steward ended it.
	ElevationEndedByCaller = "ended"
	// ElevationEndedIdle: no request used it for steward.elevation_idle.
	ElevationEndedIdle = "idle"
	// ElevationEndedRefused: a request under it was refused because the
	// grant can no longer be used (revoked, expired, ...).
	ElevationEndedRefused = "refused"
)

// ErrElevationNeedsSession is an elevation asked by a principal that is
// not a web UI browser session: in v1 only a browser session elevates
// (ADR-083 §3, §8), never an API credential.
var ErrElevationNeedsSession = errors.New("auth: only a web UI browser session can elevate into a steward grant")

// ElevationEvent is an elevation that started or ended, reported to
// SessionOptions.OnElevation after it is stored.
type ElevationEvent struct {
	// Started is true for an elevation that began, false for one that
	// ended.
	Started     bool
	PrincipalID string
	SessionID   string
	GrantID     string
	// Reason says why an elevation ended (ElevationEnded*); "" for a
	// start.
	Reason string
	At     time.Time
}

// ElevationIdle returns the effective elevation idle timeout.
func (s *Sessions) ElevationIdle() time.Duration { return s.opts.ElevationIdle }

// browserSession returns the id of p's web UI session, or
// ErrElevationNeedsSession.
func browserSession(p *Principal) (string, error) {
	if p.SessionKind() != SessionKindUI || p.Meta[MetaSessionID] == "" {
		return "", ErrElevationNeedsSession
	}
	return p.Meta[MetaSessionID], nil
}

// Elevate elevates p's browser session into steward grant grantID now
// and returns the moment. p must be a web UI session principal
// (ErrElevationNeedsSession). A session already elevated into grantID
// is refreshed; one elevated into another grant is
// storage.ErrSessionElevated. Whether p may read under the grant is the
// caller's to check first: the next request resolves it again anyway.
func (s *Sessions) Elevate(ctx context.Context, p *Principal, grantID string) (time.Time, error) {
	id, err := browserSession(p)
	if err != nil {
		return time.Time{}, err
	}
	now := s.opts.Now().UTC()
	if err := s.store.Elevate(ctx, id, grantID, now); err != nil {
		return time.Time{}, err
	}
	s.notify(ctx, ElevationEvent{Started: true, PrincipalID: p.ID, SessionID: id, GrantID: grantID, At: now})
	return now, nil
}

// EndElevation ends p's browser session's elevation into grantID, for
// reason (ElevationEnded*), and reports whether there was one.
func (s *Sessions) EndElevation(ctx context.Context, p *Principal, grantID, reason string) (bool, error) {
	id, err := browserSession(p)
	if err != nil {
		return false, err
	}
	return s.endElevation(ctx, p.ID, id, grantID, reason)
}

func (s *Sessions) endElevation(ctx context.Context, principalID, sessionID, grantID, reason string) (bool, error) {
	ended, err := s.store.EndElevation(ctx, sessionID, grantID)
	if err != nil || !ended {
		return false, err
	}
	s.notify(ctx, ElevationEvent{PrincipalID: principalID, SessionID: sessionID, GrantID: grantID, Reason: reason, At: s.opts.Now().UTC()})
	return true, nil
}

// elevation returns the grant sess's request runs under at now, ""
// for none: an elevation idle for ElevationIdle ends here (the request
// is the ordinary view), a live one has its activity refreshed.
func (s *Sessions) elevation(ctx context.Context, sess *storage.UISession, now time.Time) (string, error) {
	grant := sess.StewardGrantID
	if grant == "" {
		return "", nil
	}
	if readOnlyElevation(ctx) {
		// An observer of the elevation (a stream's recheck): it names a
		// live grant but never refreshes it nor ends an idle one, so
		// watching does not keep an elevation alive.
		if sess.StewardActiveAt == nil || !now.Before(sess.StewardActiveAt.Add(s.opts.ElevationIdle)) {
			return "", nil
		}
		return grant, nil
	}
	if sess.StewardActiveAt == nil || !now.Before(sess.StewardActiveAt.Add(s.opts.ElevationIdle)) {
		if _, err := s.endElevation(ctx, sess.PrincipalID, sess.ID, grant, ElevationEndedIdle); err != nil {
			return "", err
		}
		sess.StewardGrantID, sess.StewardActiveAt = "", nil
		return "", nil
	}
	live, err := s.store.TouchElevation(ctx, sess.ID, grant, now)
	if err != nil {
		return "", err
	}
	if !live {
		// Ended or replaced since the row was read: no elevation.
		sess.StewardGrantID, sess.StewardActiveAt = "", nil
		return "", nil
	}
	sess.StewardActiveAt = &now
	return grant, nil
}

// notify reports e to OnElevation, when set.
func (s *Sessions) notify(ctx context.Context, e ElevationEvent) {
	if s.opts.OnElevation != nil {
		s.opts.OnElevation(ctx, e)
	}
}

type readOnlyElevationKey struct{}

// WithReadOnlyElevation marks ctx so that authenticating a browser
// session reports its elevation without refreshing or ending it. A
// long-lived request that re-checks its credential uses it: only a
// request of the steward's own is activity under the elevation.
func WithReadOnlyElevation(ctx context.Context) context.Context {
	return context.WithValue(ctx, readOnlyElevationKey{}, true)
}

func readOnlyElevation(ctx context.Context) bool {
	v, _ := ctx.Value(readOnlyElevationKey{}).(bool)
	return v
}
