package auth

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// This file is the pre-K2 local form of hop.top/kit proposal K2
// (ADR-079 §6: api.Chain and api.Method). kit's Method returns
// (claims, matched, err) and declares whether its credential is carried
// or ambient; its Chain runs carried methods before ambient ones, stops
// on the first match and the first refusal, treats an expired or
// unknown ambient credential as absent (clearing a cookie when it can)
// and applies the browser rules to unsafe requests only an ambient
// method authenticated. Chain and Method here have that shape over
// ctxt's Credential and Principal, so the swap is mechanical once K2
// ships: NewChain becomes api.Chain, each Method an api.Method, and the
// HTTP middleware api.Auth(chain). The browser rules live with the HTTP
// middleware (RequireAuth), which is where kit's chain applies them.

// Method names, as server.auth.methods lists them.
const (
	// MethodAPIKey authenticates an API key carried in Authorization:
	// Bearer or X-API-Key.
	MethodAPIKey = config.AuthMethodAPIKey
	// MethodSession authenticates the web UI session cookie: ambient,
	// since a browser sends it on every request.
	MethodSession = config.AuthMethodSession
)

// Method resolves one kind of credential to a principal.
// Implementations must be safe for concurrent use.
type Method interface {
	// Name is the method's name in server.auth.methods.
	Name() string
	// Ambient reports whether the method's credential rides along with
	// every request the client sends without choosing it: a cookie, a
	// proxy header. A carried credential (key, certificate, JWT) is one
	// the client chose for this request.
	Ambient() bool
	// Authenticate resolves cred. It returns (p, true, nil) on a match,
	// (nil, false, nil) when cred carries nothing for this method, and
	// (nil, false, err) when it carries something this method refuses:
	// err wraps ErrInvalidCredential for a credential that is wrong,
	// expired or revoked; any other error is an internal failure.
	Authenticate(ctx context.Context, cred Credential) (p *Principal, matched bool, err error)
}

// Result is what the chain resolved a request to.
type Result struct {
	// Principal is the authenticated caller; nil when nothing matched.
	Principal *Principal
	// Method names the method that matched.
	Method string
	// Ambient reports whether an ambient method matched: the request
	// then gets the browser rules (Origin check and CSRF header on
	// unsafe methods) and the principal's scopes are already narrowed
	// to the session kind's set.
	Ambient bool
	// ClearCookie reports that the request carried a session cookie no
	// ambient method accepted (expired, unknown, revoked): the transport
	// clears it so the browser stops sending it.
	ClearCookie bool
}

// Chain is an ordered list of methods tried on every request.
type Chain struct {
	methods []Method
}

// NewChain returns a chain that runs methods in order, with every
// carried method ahead of every ambient one whatever the order given:
// a request with both a bearer key and a session cookie runs as the
// key's principal, which lets a script behind a browser use its own
// narrower key. Within each group the given order applies.
func NewChain(methods ...Method) *Chain {
	ordered := make([]Method, 0, len(methods))
	for _, m := range methods {
		if !m.Ambient() {
			ordered = append(ordered, m)
		}
	}
	for _, m := range methods {
		if m.Ambient() {
			ordered = append(ordered, m)
		}
	}
	return &Chain{methods: ordered}
}

// ChainFor returns the chain for names (server.auth.methods order),
// picking each from available by Name. A name none of available has is
// an error: config validation refuses unknown and reserved names, so
// this guards a build that forgot to supply a method.
func ChainFor(names []string, available ...Method) (*Chain, error) {
	byName := map[string]Method{}
	for _, m := range available {
		byName[m.Name()] = m
	}
	picked := make([]Method, 0, len(names))
	for _, name := range names {
		m, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("auth: method %q is configured but not available on this build", name)
		}
		picked = append(picked, m)
	}
	return NewChain(picked...), nil
}

// Methods returns the method names in run order.
func (c *Chain) Methods() []string {
	out := make([]string, len(c.methods))
	for i, m := range c.methods {
		out[i] = m.Name()
	}
	return out
}

// Has reports whether the chain runs the named method.
func (c *Chain) Has(name string) bool {
	return c != nil && slices.Contains(c.Methods(), name)
}

// String names the methods in run order, for startup output.
func (c *Chain) String() string { return strings.Join(c.Methods(), ", ") }

// Authenticate runs the chain over cred: the first method that matches
// wins; the first that refuses ends the request with its error, so a
// bad bearer key is never rescued by a cookie behind it. When no method
// matches, the error is ErrNoCredential, and Result.ClearCookie says
// whether a session cookie was presented and found stale, so the
// transport can clear it.
func (c *Chain) Authenticate(ctx context.Context, cred Credential) (Result, error) {
	for _, m := range c.methods {
		p, ok, err := m.Authenticate(ctx, cred)
		if err != nil {
			return Result{}, err
		}
		if ok {
			return Result{Principal: p, Method: m.Name(), Ambient: m.Ambient()}, nil
		}
	}
	// Nothing matched. A cookie that reached this point was looked at by
	// the session method (or no session method runs) and is stale
	// either way: have the browser drop it.
	return Result{ClearCookie: cred.Cookie != ""}, ErrNoCredential
}
