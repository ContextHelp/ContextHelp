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
	"maps"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SchemeSession is a web UI session cookie. The HTTP middleware reads
// it only from the instance's own cookie, never from a header, so a
// session secret pasted into Authorization is just an invalid bearer.
const SchemeSession = "session"

// ScopeUI is the route set a browser session may reach: reads, search
// and the web UI's own writes. The HTTP layer owns the route table.
const ScopeUI = "ui"

// Principal.Meta keys set on a principal that authenticated with a
// session cookie.
const (
	// MetaVia is "session" for a cookie-authenticated principal.
	MetaVia = "via"
	// MetaScope names the session's route scope (ScopeUI).
	MetaScope = "scope"
	// MetaSessionID is the session's public ID.
	MetaSessionID = "session_id"
	// ViaSession is the MetaVia value of a session principal.
	ViaSession = "session"
)

// Session revocation reasons recorded in the store.
const (
	RevokeReasonLogout       = "logout"
	RevokeReasonRevoked      = "revoked"
	RevokeReasonTokenRemoved = "token removed"
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

// SessionScope returns the route scope of a session principal, or ""
// for any other principal.
func (p *Principal) SessionScope() string {
	if !p.IsSession() {
		return ""
	}
	return p.Meta[MetaScope]
}

// HashSecret returns the hex SHA-256 of a token, login code or cookie
// secret: the only form in which any of them is stored.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// TokenHashResolver maps the hash of a configured token to the
// principal the config assigns it today. Sessions consult it on every
// request, so a session dies with the token that minted it.
type TokenHashResolver interface {
	PrincipalForTokenHash(hash string) (*Principal, bool)
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
// authenticates session cookies. A session acts as the principal of the
// static token that minted its code, with a reduced scope (ScopeUI),
// and ends at its idle or absolute expiry, on revocation, or as soon as
// that token is no longer configured.
type Sessions struct {
	store    storage.UISessionStore
	resolver TokenHashResolver
	opts     SessionOptions
}

// NewSessions builds a session manager over store. resolver is the
// configured token provider (see StaticProvider).
func NewSessions(store storage.UISessionStore, resolver TokenHashResolver, opts SessionOptions) (*Sessions, error) {
	if store == nil {
		return nil, errors.New("auth: sessions need a session store")
	}
	if resolver == nil {
		return nil, errors.New("auth: sessions need a token resolver")
	}
	return &Sessions{store: store, resolver: resolver, opts: opts.withDefaults()}, nil
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

func newSessionID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: session id: %w", err)
	}
	return "uis_" + hex.EncodeToString(b), nil
}

// MintCode issues a single-use login code for the principal of token,
// a configured static token. The code is returned once; only its hash
// is stored.
func (s *Sessions) MintCode(ctx context.Context, token string) (string, time.Time, error) {
	if token == "" {
		return "", time.Time{}, ErrNoCredential
	}
	tokenHash := HashSecret(token)
	p, ok := s.resolver.PrincipalForTokenHash(tokenHash)
	if !ok {
		return "", time.Time{}, ErrInvalidCredential
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
		TokenHash:   tokenHash,
		Scope:       ScopeUI,
		CreatedAt:   now,
		ExpiresAt:   expires,
	}); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// Exchange trades a login code for a new session. An unknown, used or
// expired code, or one whose minting token is gone, is
// ErrInvalidCredential; the code is spent either way.
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
	p, ok := s.resolver.PrincipalForTokenHash(lc.TokenHash)
	if !ok || p.ID != lc.PrincipalID {
		return nil, ErrInvalidCredential
	}
	secret, err := randomSecret()
	if err != nil {
		return nil, err
	}
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	expires := now.Add(s.opts.MaxTTL)
	sess := &storage.UISession{
		ID:            id,
		SecretHash:    HashSecret(secret),
		PrincipalID:   p.ID,
		TokenHash:     lc.TokenHash,
		Scope:         lc.Scope,
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
	return &Minted{Secret: secret, Session: sess, Principal: sessionPrincipal(p, sess)}, nil
}

// Authenticate resolves a cookie secret to its session and principal.
// Anything but an active session whose minting token is still
// configured for the same principal is ErrInvalidCredential. A session
// whose token is gone is revoked on the spot.
func (s *Sessions) Authenticate(ctx context.Context, secret string) (*Principal, *storage.UISession, error) {
	if secret == "" {
		return nil, nil, ErrNoCredential
	}
	sess, p, err := s.active(ctx, HashSecret(secret))
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
	return sessionPrincipal(p, sess), sess, nil
}

// Check reports whether the session with secretHash is still active,
// without touching it. Long-lived requests (the event stream) call it
// to end when their session does.
func (s *Sessions) Check(ctx context.Context, secretHash string) error {
	_, _, err := s.active(ctx, secretHash)
	return err
}

func (s *Sessions) active(ctx context.Context, secretHash string) (*storage.UISession, *Principal, error) {
	now := s.opts.Now().UTC()
	sess, err := s.store.Lookup(ctx, secretHash, now)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil, ErrInvalidCredential
	}
	if err != nil {
		return nil, nil, err
	}
	p, ok := s.resolver.PrincipalForTokenHash(sess.TokenHash)
	if !ok || p.ID != sess.PrincipalID {
		if err := s.store.Revoke(ctx, sess.ID, now, RevokeReasonTokenRemoved); err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrInvalidCredential
	}
	return sess, p, nil
}

// Revoke ends the session with id. An unknown id is storage.ErrNotFound.
func (s *Sessions) Revoke(ctx context.Context, id, reason string) error {
	return s.store.Revoke(ctx, id, s.opts.Now().UTC(), reason)
}

// SweepRemovedTokens revokes every active session whose minting token
// is no longer configured, and returns how many it revoked. Serve runs
// it at start so listings match what requests would see.
func (s *Sessions) SweepRemovedTokens(ctx context.Context) (int, error) {
	now := s.opts.Now().UTC()
	active, err := s.store.List(ctx, storage.UISessionFilter{ActiveOnly: true, Now: now})
	if err != nil {
		return 0, err
	}
	gone := map[string]bool{}
	for _, sess := range active {
		if p, ok := s.resolver.PrincipalForTokenHash(sess.TokenHash); !ok || p.ID != sess.PrincipalID {
			gone[sess.TokenHash] = true
		}
	}
	revoked := 0
	for hash := range gone {
		n, err := s.store.RevokeByTokenHash(ctx, hash, now, RevokeReasonTokenRemoved)
		if err != nil {
			return revoked, err
		}
		revoked += n
	}
	return revoked, nil
}

// sessionPrincipal is p acting through sess: same ID, name, provider
// and roles, so entitlements, metering and policy apply unchanged, plus
// the session markers the HTTP scope guard reads.
func sessionPrincipal(p *Principal, sess *storage.UISession) *Principal {
	out := *p
	out.Roles = append([]string(nil), p.Roles...)
	out.Scopes = append([]Scope(nil), p.Scopes...)
	out.Meta = make(map[string]string, len(p.Meta)+3)
	maps.Copy(out.Meta, p.Meta)
	out.Meta[MetaVia] = ViaSession
	out.Meta[MetaScope] = sess.Scope
	out.Meta[MetaSessionID] = sess.ID
	return &out
}

// Provider returns base extended with session cookies: a SchemeSession
// credential authenticates against the session store, anything else
// goes to base unchanged.
func (s *Sessions) Provider(base Provider) Provider {
	return &sessionProvider{base: base, sessions: s}
}

type sessionProvider struct {
	base     Provider
	sessions *Sessions
}

func (p *sessionProvider) Name() string { return p.base.Name() }

func (p *sessionProvider) Authenticate(ctx context.Context, cred Credential) (*Principal, error) {
	if cred.Scheme != SchemeSession {
		return p.base.Authenticate(ctx, cred)
	}
	princ, _, err := p.sessions.Authenticate(ctx, cred.Token)
	return princ, err
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

// Get returns the session with id whatever its state; an unknown id is
// storage.ErrNotFound.
func (s *Sessions) Get(ctx context.Context, id string) (*storage.UISession, error) {
	return s.store.Get(ctx, id)
}
