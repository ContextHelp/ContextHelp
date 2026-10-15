package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SessionKindUI is the kind of a web UI browser session, stored with
// each session and login code. Its scopes are UISessionScopes.
const SessionKindUI = "ui"

// SessionScopes returns the fixed scope set of a session kind: nil for
// a kind this build does not know, so such a session reaches nothing.
func SessionScopes(kind string) []Scope {
	if kind == SessionKindUI {
		return UISessionScopes
	}
	return nil
}

// Principal.Meta keys set on a principal that authenticated with a
// session cookie.
const (
	// MetaVia is "session" for a cookie-authenticated principal.
	MetaVia = "via"
	// MetaSessionKind names the session's kind (SessionKindUI).
	MetaSessionKind = "session_kind"
	// MetaSessionID is the session's public ID.
	MetaSessionID = "session_id"
	// ViaSession is the MetaVia value of a session principal.
	ViaSession = "session"
)

// Session revocation reasons recorded in the store. The cascades from
// the principal registry write storage.RevokeReasonIdentityRevoked and
// storage.RevokeReasonPrincipalRevoked.
const (
	RevokeReasonLogout  = "logout"
	RevokeReasonRevoked = "revoked"
)

// Defaults for SessionOptions fields left zero.
const (
	DefaultSessionIdleTTL = 12 * time.Hour
	DefaultSessionMaxTTL  = 7 * 24 * time.Hour
	DefaultLoginCodeTTL   = 60 * time.Second
	// DefaultSessionTouchEvery bounds how often a busy session writes
	// its last-seen time: polling UIs would otherwise write per request.
	DefaultSessionTouchEvery = time.Minute
)

// secretBytes is the entropy of login codes and cookie secrets.
const secretBytes = 32

// IsSession reports whether p authenticated with a session cookie.
func (p *Principal) IsSession() bool {
	return p != nil && p.Meta[MetaVia] == ViaSession
}

// SessionKind returns the kind of a session principal's session, or ""
// for any other principal.
func (p *Principal) SessionKind() string {
	if !p.IsSession() {
		return ""
	}
	return p.Meta[MetaSessionKind]
}

// HashSecret returns the hex SHA-256 of a login code or cookie secret:
// the only form in which either is stored.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// SessionOptions tunes Sessions. Zero fields take the Default* values.
type SessionOptions struct {
	// IdleTTL ends a session this long after its last request.
	IdleTTL time.Duration
	// MaxTTL ends a session this long after sign-in, however active.
	MaxTTL time.Duration
	// CodeTTL is how long a login code can be exchanged.
	CodeTTL time.Duration
	// TouchEvery is the minimum gap between two last-seen writes.
	TouchEvery time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (o SessionOptions) withDefaults() SessionOptions {
	if o.IdleTTL <= 0 {
		o.IdleTTL = DefaultSessionIdleTTL
	}
	if o.MaxTTL <= 0 {
		o.MaxTTL = DefaultSessionMaxTTL
	}
	if o.CodeTTL <= 0 {
		o.CodeTTL = DefaultLoginCodeTTL
	}
	if o.TouchEvery <= 0 {
		o.TouchEvery = DefaultSessionTouchEvery
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// ClientInfo describes the browser that exchanges a login code. It is
// recorded for listing only and never checked.
type ClientInfo struct {
	UserAgent  string
	RemoteAddr string
}

// Minted is the result of a successful code exchange.
type Minted struct {
	// Secret is the cookie value. It is returned once and never stored.
	Secret    string
	Session   *storage.UISession
	Principal *Principal
}

// Sessions mints login codes, exchanges them for browser sessions and
// authenticates session cookies. A login code records the identity
// (the registry API key) of the caller that minted it; the session
// acts as that identity's principal with the identity's scopes narrowed
// to the session kind's set, and ends at its idle or absolute expiry,
// on revocation, and as soon as the identity or the principal is
// revoked (the stores cascade; Authenticate checks again).
type Sessions struct {
	store storage.UISessionStore
	auth  storage.AuthStore
	keys  *Keys
	opts  SessionOptions
}

// NewSessions builds a session manager over store and the principal
// registry in auth. keys resolves the scopes of the key behind each
// session; nil narrows the principal's role scopes instead.
func NewSessions(store storage.UISessionStore, auth storage.AuthStore, keys *Keys, opts SessionOptions) (*Sessions, error) {
	if store == nil {
		return nil, errors.New("auth: sessions need a session store")
	}
	if auth == nil {
		return nil, errors.New("auth: sessions need the principal registry")
	}
	return &Sessions{store: store, auth: auth, keys: keys, opts: opts.withDefaults()}, nil
}

// IdleTTL returns the effective idle timeout.
func (s *Sessions) IdleTTL() time.Duration { return s.opts.IdleTTL }

// MaxTTL returns the effective absolute session lifetime.
func (s *Sessions) MaxTTL() time.Duration { return s.opts.MaxTTL }

// CodeTTL returns the effective login code lifetime.
func (s *Sessions) CodeTTL() time.Duration { return s.opts.CodeTTL }

func randomSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: random secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// wellFormedSecret reports whether s has the shape randomSecret mints:
// 32 bytes of base64url. Anything else was never issued by this server.
func wellFormedSecret(s string) bool {
	if len(s) != base64.RawURLEncoding.EncodedLen(secretBytes) {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == secretBytes
}

func newSessionID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: session id: %w", err)
	}
	return "uis_" + hex.EncodeToString(b), nil
}

// ErrNoIdentity is a login code requested by a caller whose credential
// has no identity row: a key declared in server.auth.keys. Only a
// registry key (dpkms auth key create) can sign a browser in, because
// the session must die with the credential that minted it.
var ErrNoIdentity = fmt.Errorf("%w: the credential has no identity to bind a session to", ErrInvalidCredential)

// MintCode issues a single-use login code for p, the authenticated
// caller, bound to the identity that authenticated it. The code is
// returned once; only its hash is stored. A caller without an identity
// (ErrNoIdentity) or that is itself a session cannot mint.
func (s *Sessions) MintCode(ctx context.Context, p *Principal) (string, time.Time, error) {
	if p == nil {
		return "", time.Time{}, ErrNoCredential
	}
	if p.IsSession() {
		return "", time.Time{}, fmt.Errorf("%w: a session cannot mint a session", ErrInvalidCredential)
	}
	idn := p.Meta[MetaIdentityID]
	if idn == "" {
		return "", time.Time{}, ErrNoIdentity
	}
	code, err := randomSecret()
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.opts.Now().UTC()
	expires := now.Add(s.opts.CodeTTL)
	if err := s.store.CreateLoginCode(ctx, &storage.UILoginCode{
		CodeHash:    HashSecret(code),
		PrincipalID: p.ID,
		IdentityID:  idn,
		Scope:       SessionKindUI,
		CreatedAt:   now,
		ExpiresAt:   expires,
	}); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// Exchange trades a login code for a new session. An unknown, used or
// expired code, or one whose identity or principal is no longer active,
// is ErrInvalidCredential; the code is spent either way.
func (s *Sessions) Exchange(ctx context.Context, code string, ci ClientInfo) (*Minted, error) {
	if code == "" {
		return nil, ErrNoCredential
	}
	now := s.opts.Now().UTC()
	codeHash := HashSecret(code)
	lc, err := s.store.ConsumeLoginCode(ctx, codeHash, now)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrInvalidCredential
	}
	if err != nil {
		return nil, err
	}
	// The store found the row by this hash; compare again in constant
	// time so no store can hand back a row for a near miss.
	if subtle.ConstantTimeCompare([]byte(lc.CodeHash), []byte(codeHash)) != 1 {
		return nil, ErrInvalidCredential
	}
	return s.mintFor(ctx, lc.IdentityID, lc.PrincipalID, lc.Scope, ci)
}

// MintFor opens a new session for principalID through identityID, the
// credential the sign-in ceremony (an invite, a sign-in link) bound to
// it. The session carries the principal's role scopes narrowed to the
// kind's set, since a link identity has no scopes of its own, and ends
// when the identity or the principal is revoked. An identity or
// principal that is gone, revoked, suspended or mismatched is
// ErrInvalidCredential.
func (s *Sessions) MintFor(ctx context.Context, principalID, identityID string, ci ClientInfo) (*Minted, error) {
	return s.mintFor(ctx, identityID, principalID, SessionKindUI, ci)
}

func (s *Sessions) mintFor(ctx context.Context, identityID, principalID, kind string, ci ClientInfo) (*Minted, error) {
	p, idn, scopes, err := s.resolve(ctx, identityID, principalID)
	if err != nil {
		return nil, err
	}
	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	now := s.opts.Now().UTC()
	expires := now.Add(s.opts.MaxTTL)
	sess := &storage.UISession{
		ID:            id,
		SecretHash:    HashSecret(secret),
		PrincipalID:   p.ID,
		IdentityID:    idn.ID,
		Scope:         kind,
		UserAgent:     truncate(ci.UserAgent, 256),
		RemoteAddr:    truncate(ci.RemoteAddr, 64),
		CreatedAt:     now,
		LastSeenAt:    now,
		IdleExpiresAt: minTime(now.Add(s.opts.IdleTTL), expires),
		ExpiresAt:     expires,
	}
	if err := s.store.Create(ctx, sess); err != nil {
		return nil, err
	}
	return &Minted{Secret: secret, Session: sess, Principal: sessionPrincipal(p, scopes, sess)}, nil
}

// ListByPrincipal returns the sessions of principalID, newest first:
// the active ones only unless all is set.
func (s *Sessions) ListByPrincipal(ctx context.Context, principalID string, all bool) ([]*storage.UISession, error) {
	return s.store.List(ctx, storage.UISessionFilter{
		ActiveOnly: !all, Now: s.opts.Now().UTC(), PrincipalID: principalID,
	})
}

// Now returns the session clock's time.
func (s *Sessions) Now() time.Time { return s.opts.Now().UTC() }

// resolve loads the identity and principal behind a code or session
// and the scopes the identity's credential holds. An identity or
// principal that is gone, revoked or suspended, or bound to another
// principal, is ErrInvalidCredential.
func (s *Sessions) resolve(ctx context.Context, identityID, principalID string) (*storage.Principal, *storage.Identity, []Scope, error) {
	idn, err := s.auth.Identities().Get(ctx, identityID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil, nil, fmt.Errorf("%w: identity %s is gone", ErrInvalidCredential, identityID)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if !idn.Active() || idn.PrincipalID != principalID {
		return nil, nil, nil, fmt.Errorf("%w: identity %s was revoked", ErrInvalidCredential, identityID)
	}
	p, err := s.auth.Principals().Get(ctx, principalID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil, nil, fmt.Errorf("%w: principal %s is gone", ErrInvalidCredential, principalID)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if !p.Active() {
		return nil, nil, nil, fmt.Errorf("%w: principal %s is %s", ErrInvalidCredential, p.ID, p.Status)
	}
	scopes := ScopesForRoles(p.Roles)
	if s.keys != nil {
		if scopes, err = s.keys.ScopesOf(ctx, p, idn); err != nil {
			return nil, nil, nil, err
		}
	}
	return p, idn, scopes, nil
}

// Authenticate resolves a cookie secret to its session and principal.
// Anything but an active session whose identity and principal are
// still active is ErrInvalidCredential. A session the registry has
// outrun (its identity or principal revoked without the cascade) is
// revoked on the spot.
func (s *Sessions) Authenticate(ctx context.Context, secret string) (*Principal, *storage.UISession, error) {
	if secret == "" {
		return nil, nil, ErrNoCredential
	}
	sess, p, scopes, err := s.active(ctx, HashSecret(secret))
	if err != nil {
		return nil, nil, err
	}
	now := s.opts.Now().UTC()
	if now.Sub(sess.LastSeenAt) >= s.opts.TouchEvery {
		if err := s.store.Touch(ctx, sess.ID, now, now.Add(s.opts.IdleTTL)); err != nil {
			return nil, nil, err
		}
		sess.LastSeenAt = now
		sess.IdleExpiresAt = minTime(now.Add(s.opts.IdleTTL), sess.ExpiresAt)
	}
	return sessionPrincipal(p, scopes, sess), sess, nil
}

// Check reports whether the session with secretHash is still active,
// without touching it. Long-lived requests (the event stream) call it
// to end when their session does.
func (s *Sessions) Check(ctx context.Context, secretHash string) error {
	_, _, _, err := s.active(ctx, secretHash)
	return err
}

func (s *Sessions) active(ctx context.Context, secretHash string) (*storage.UISession, *storage.Principal, []Scope, error) {
	now := s.opts.Now().UTC()
	sess, err := s.store.Lookup(ctx, secretHash, now)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil, nil, ErrInvalidCredential
	}
	if err != nil {
		return nil, nil, nil, err
	}
	p, _, scopes, err := s.resolve(ctx, sess.IdentityID, sess.PrincipalID)
	if errors.Is(err, ErrInvalidCredential) {
		if rerr := s.store.Revoke(ctx, sess.ID, now, storage.RevokeReasonIdentityRevoked); rerr != nil {
			return nil, nil, nil, rerr
		}
		return nil, nil, nil, err
	}
	if err != nil {
		return nil, nil, nil, err
	}
	return sess, p, scopes, nil
}

// Revoke ends the session with id. An unknown id is storage.ErrNotFound.
func (s *Sessions) Revoke(ctx context.Context, id, reason string) error {
	return s.store.Revoke(ctx, id, s.opts.Now().UTC(), reason)
}

// Get returns the session with id whatever its state; an unknown id is
// storage.ErrNotFound.
func (s *Sessions) Get(ctx context.Context, id string) (*storage.UISession, error) {
	return s.store.Get(ctx, id)
}

// sessionPrincipal is p acting through sess: same ID, name, kind and
// roles, so entitlements, metering and policy apply unchanged; its
// effective scopes are the minting credential's intersected with the
// session kind's fixed set, plus the session-only signout:ui
// (SessionScopesFor), so a session never holds more than its key, nor
// more than a browser needs. Plus the session markers the HTTP layer
// reads.
func sessionPrincipal(p *storage.Principal, scopes []Scope, sess *storage.UISession) *Principal {
	out := registryPrincipal(p, SessionScopesFor(scopes, sess.Scope), MethodSession)
	out.Meta = map[string]string{
		MetaVia:         ViaSession,
		MetaSessionKind: sess.Scope,
		MetaSessionID:   sess.ID,
		MetaIdentityID:  sess.IdentityID,
	}
	return out
}

// Method returns the session Method over s: the instance's own cookie,
// ambient.
func (s *Sessions) Method() *SessionMethod { return &SessionMethod{sessions: s} }

// SessionMethod is the session Method: the web UI session cookie.
type SessionMethod struct{ sessions *Sessions }

// Name implements Method.
func (*SessionMethod) Name() string { return MethodSession }

// Ambient implements Method: a browser sends the cookie on every
// request, chosen or not.
func (*SessionMethod) Ambient() bool { return true }

// Authenticate implements Method. No cookie is absent. A cookie that
// could never have been issued here (wrong shape) fails its integrity
// check and is refused; one that was issued but is expired, unknown or
// revoked is absent, so the chain clears it and another method or a
// fresh sign-in can take over (ADR-079 §3).
func (m *SessionMethod) Authenticate(ctx context.Context, cred Credential) (*Principal, bool, error) {
	if cred.Cookie == "" {
		return nil, false, nil
	}
	if !wellFormedSecret(cred.Cookie) {
		return nil, false, fmt.Errorf("%w: malformed session cookie", ErrInvalidCredential)
	}
	p, _, err := m.sessions.Authenticate(ctx, cred.Cookie)
	if errors.Is(err, ErrInvalidCredential) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("auth: session: %w", err)
	}
	return p, true, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
