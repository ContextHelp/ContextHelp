package registry

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// TokenSource looks up the stored token, and the origin it is bound to,
// for a registry by name. Get returns ErrNoToken when nothing is stored.
// *TokenStore satisfies it.
type TokenSource interface {
	Get(registryName string) (StoredToken, error)
}

// KeychainTokens returns a TokenSource over the OS keychain that opens
// the keychain store on its first lookup, never before. Credentials only
// look up tokens for registries with an auth.type, so a process whose
// config has no authenticated registry never touches the keychain.
func KeychainTokens() TokenSource {
	return newLazyTokens(func() TokenSource { return NewTokenStore() })
}

// lazyTokens defers opening its TokenSource to the first Get.
type lazyTokens struct {
	once  sync.Once
	open  func() TokenSource
	store TokenSource
}

func newLazyTokens(open func() TokenSource) *lazyTokens {
	return &lazyTokens{open: open}
}

func (l *lazyTokens) Get(registryName string) (StoredToken, error) {
	l.once.Do(func() { l.store = l.open() })
	return l.store.Get(registryName)
}

// Credentials attaches stored registry tokens to outgoing requests.
//
// A request is matched to a configured registry by its URL: same scheme
// and host (port included) as the registry's url, preferring the registry
// whose url path is the longest prefix of the request path; a host shared
// by several registries with no path match gets no token. Matching runs
// on every hop, so a redirect to another host, or from https to http,
// carries no token.
//
// Only registries whose auth.type is set are authenticated; a registry
// without one is public and the token store is never consulted for it.
//
// A stored token is released only to the origin recorded with it at login
// (StoredToken.Origin), and only over https or to a loopback host. A
// config entry that points a logged-in name at another origin (say, a
// project-local config layer) gets no token, and a one-line warning
// naming the registry and both origins goes to the warnings writer.
//
// A nil *Credentials authenticates nothing.
type Credentials struct {
	registries []credRegistry
	tokens     TokenSource

	warnMu sync.Mutex
	warn   io.Writer
	warned map[string]bool
}

type credRegistry struct {
	cfg    config.RegistryConfig
	scheme string
	host   string
	path   string
}

// NewCredentials builds Credentials for the configured registries, with
// tokens read from tokens on each request. Entries without a parseable
// http(s) url are ignored.
func NewCredentials(registries []config.RegistryConfig, tokens TokenSource) *Credentials {
	c := &Credentials{tokens: tokens, warn: os.Stderr, warned: map[string]bool{}}
	for _, r := range registries {
		u, err := url.Parse(r.URL)
		if err != nil {
			continue
		}
		scheme := strings.ToLower(u.Scheme)
		if scheme != "http" && scheme != "https" {
			continue
		}
		c.registries = append(c.registries, credRegistry{
			cfg:    r,
			scheme: scheme,
			host:   canonicalHost(u),
			path:   strings.TrimSuffix(u.Path, "/"),
		})
	}
	return c
}

// WithWarnings sends the withheld-token warnings to w (default
// os.Stderr). Each distinct warning is written once.
func (c *Credentials) WithWarnings(w io.Writer) *Credentials {
	c.warn = w
	return c
}

// Client returns a copy of base whose transport authenticates registry
// requests; nil base means a zero http.Client. A nil receiver returns base
// unchanged.
func (c *Credentials) Client(base *http.Client) *http.Client {
	if c == nil {
		return base
	}
	var cl http.Client
	if base != nil {
		cl = *base
	}
	cl.Transport = &authTransport{creds: c, next: cl.Transport}
	return &cl
}

// CheckResponse returns an ErrRegistryAuth when resp is a 401 or 403 from
// a configured registry, naming the login command; otherwise nil. Call it
// before interpreting the status code.
func (c *Credentials) CheckResponse(resp *http.Response) error {
	if c == nil || resp == nil || resp.Request == nil {
		return nil
	}
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		return nil
	}
	reg, ok := c.match(resp.Request.URL)
	if !ok || reg.cfg.Name == "" {
		return nil
	}
	e := ErrRegistryAuth{
		RegistryName: reg.cfg.Name,
		StatusCode:   resp.StatusCode,
		Configured:   reg.cfg.Auth.Type != "",
	}
	if e.Configured {
		_, sent, err := c.release(reg, resp.Request.URL)
		e.TokenSent = sent && err == nil
	}
	return e
}

// ErrRegistryAuth reports a registry that refused a request (401/403).
type ErrRegistryAuth struct {
	RegistryName string
	StatusCode   int
	// Configured is true when the registry has an auth.type.
	Configured bool
	// TokenSent is true when a stored token went with the request.
	TokenSent bool
}

func (e ErrRegistryAuth) Error() string {
	switch {
	case e.TokenSent:
		return fmt.Sprintf("registry %q rejected the stored token (HTTP %d) — run: ctxt registry login %s",
			e.RegistryName, e.StatusCode, e.RegistryName)
	case e.Configured:
		return fmt.Sprintf("registry %q requires authentication (HTTP %d) and no token is stored — run: ctxt registry login %s",
			e.RegistryName, e.StatusCode, e.RegistryName)
	default:
		return fmt.Sprintf("registry %q requires authentication (HTTP %d) — set registries[].auth.type for it, then run: ctxt registry login %s",
			e.RegistryName, e.StatusCode, e.RegistryName)
	}
}

// Unwrap exposes ErrNoToken when the registry needs a token and none is
// stored.
func (e ErrRegistryAuth) Unwrap() error {
	if e.Configured && !e.TokenSent {
		return ErrNoToken{RegistryName: e.RegistryName}
	}
	return nil
}

// match returns the configured registry that covers u.
func (c *Credentials) match(u *url.URL) (*credRegistry, bool) {
	if u == nil {
		return nil, false
	}
	scheme := strings.ToLower(u.Scheme)
	host := canonicalHost(u)
	var best, only *credRegistry
	bestLen, sameHost := -1, 0
	for i := range c.registries {
		r := &c.registries[i]
		if r.scheme != scheme || r.host != host {
			continue
		}
		sameHost++
		only = r
		if pathUnder(u.Path, r.path) && len(r.path) > bestLen {
			best, bestLen = r, len(r.path)
		}
	}
	if best != nil {
		return best, true
	}
	if sameHost == 1 {
		return only, true
	}
	return nil, false
}

// token reads the stored token for name; found is false when none is
// stored.
func (c *Credentials) token(name string) (tok StoredToken, found bool, err error) {
	if c.tokens == nil || name == "" {
		return StoredToken{}, false, nil
	}
	tok, err = c.tokens.Get(name)
	if err != nil {
		var none ErrNoToken
		if errors.As(err, &none) {
			return StoredToken{}, false, nil
		}
		return StoredToken{}, false, fmt.Errorf("registry %q: read stored token: %w", name, err)
	}
	return tok, tok.Token != "" && tok.Origin != "", nil
}

// release returns reg's stored token when it may go to u: u's origin is
// the one recorded at login, and u is https or a loopback host. A
// withheld token is warned about, never printed.
func (c *Credentials) release(reg *credRegistry, u *url.URL) (string, bool, error) {
	tok, found, err := c.token(reg.cfg.Name)
	if err != nil || !found {
		return "", false, err
	}
	origin := requestOrigin(u)
	if tok.Origin != origin {
		c.warnOnce(fmt.Sprintf("warning: registry %q: stored token is bound to %s, not sent to %s (run: ctxt registry login %s)",
			reg.cfg.Name, tok.Origin, origin, reg.cfg.Name))
		return "", false, nil
	}
	if InsecureOrigin(origin) {
		c.warnOnce(fmt.Sprintf("warning: registry %q: token not sent over plain http to %s; use https",
			reg.cfg.Name, origin))
		return "", false, nil
	}
	return tok.Token, true, nil
}

// requestOrigin is u's CanonicalOrigin; for a URL that has none it is
// the raw scheme://host, which never equals a stored token's origin.
func requestOrigin(u *url.URL) string {
	if origin, err := CanonicalOrigin(u.String()); err == nil {
		return origin
	}
	return u.Scheme + "://" + u.Host
}

func (c *Credentials) warnOnce(msg string) {
	c.warnMu.Lock()
	defer c.warnMu.Unlock()
	if c.warn == nil || c.warned[msg] {
		return
	}
	c.warned[msg] = true
	fmt.Fprintln(c.warn, msg)
}

// CanonicalOrigin returns the origin of an http(s) URL: lower-cased
// scheme and host, the scheme's default port dropped, no path. A stored
// token is bound to the CanonicalOrigin of the registry url at login.
func CanonicalOrigin(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("registry: parse url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("registry: url %q is not http(s)", rawURL)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("registry: url %q has no host", rawURL)
	}
	host := canonicalHost(u)
	if u.Port() == "" || host == strings.ToLower(u.Hostname()) {
		// No port left: bracket an IPv6 literal.
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	}
	return scheme + "://" + host, nil
}

// InsecureOrigin reports whether a token must not be sent to origin: plain
// http to a host other than localhost or a loopback address.
func InsecureOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return true
	}
	if strings.ToLower(u.Scheme) == "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

// authHeader returns the header and value prefix auth uses.
func authHeader(auth config.RegistryAuthConfig) (name, prefix string, err error) {
	switch auth.Type {
	case config.RegistryAuthBearer, config.RegistryAuthOAuth2:
		return "Authorization", "Bearer ", nil
	case config.RegistryAuthAPIKey:
		if auth.HeaderName != "" {
			return auth.HeaderName, "", nil
		}
		return config.DefaultRegistryAPIKeyHeader, "", nil
	default:
		return "", "", fmt.Errorf("unsupported auth.type %q (must be one of %s, %s, %s)",
			auth.Type, config.RegistryAuthBearer, config.RegistryAuthAPIKey, config.RegistryAuthOAuth2)
	}
}

// authTransport sets the matched registry's token on a clone of each
// request it sends.
type authTransport struct {
	creds *Credentials
	next  http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	reg, ok := t.creds.match(req.URL)
	if !ok || reg.cfg.Auth.Type == "" {
		return next.RoundTrip(req)
	}
	header, prefix, err := authHeader(reg.cfg.Auth)
	if err != nil {
		closeBody(req)
		return nil, fmt.Errorf("registry %q: %w", reg.cfg.Name, err)
	}
	tok, sent, err := t.creds.release(reg, req.URL)
	if err != nil {
		closeBody(req)
		return nil, err
	}
	if !sent {
		return next.RoundTrip(req)
	}
	authed := req.Clone(req.Context())
	authed.Header.Set(header, prefix+tok)
	return next.RoundTrip(authed)
}

func closeBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}

// canonicalHost is u's lower-cased host with the scheme's default port
// dropped, so https://h and https://h:443 compare equal.
func canonicalHost(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	scheme := strings.ToLower(u.Scheme)
	if port == "" || (port == "80" && scheme == "http") || (port == "443" && scheme == "https") {
		return host
	}
	return net.JoinHostPort(host, port)
}

// pathUnder reports whether p is base or below it.
func pathUnder(p, base string) bool {
	if base == "" {
		return true
	}
	return p == base || strings.HasPrefix(p, base+"/")
}
