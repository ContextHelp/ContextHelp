package registry

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// TokenSource looks up the stored token for a registry by name. Get
// returns ErrNoToken when nothing is stored. *TokenStore satisfies it.
type TokenSource interface {
	Get(registryName string) (string, error)
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

func (l *lazyTokens) Get(registryName string) (string, error) {
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
// A nil *Credentials authenticates nothing.
type Credentials struct {
	registries []credRegistry
	tokens     TokenSource
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
	c := &Credentials{tokens: tokens}
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
		_, found, err := c.token(reg.cfg.Name)
		e.TokenSent = found && err == nil
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
func (c *Credentials) token(name string) (tok string, found bool, err error) {
	if c.tokens == nil || name == "" {
		return "", false, nil
	}
	tok, err = c.tokens.Get(name)
	if err != nil {
		var none ErrNoToken
		if errors.As(err, &none) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("registry %q: read stored token: %w", name, err)
	}
	return tok, tok != "", nil
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
	tok, found, err := t.creds.token(reg.cfg.Name)
	if err != nil {
		closeBody(req)
		return nil, err
	}
	if !found {
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
