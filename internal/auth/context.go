package auth

import "context"

type ctxKey struct{}

var principalKey ctxKey

// WithPrincipal returns a context carrying the authenticated principal.
// Transport layers attach it after a successful Authenticate so handlers,
// the policy engine, and metering can read the caller identity.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	if p == nil {
		return ctx
	}
	return context.WithValue(ctx, principalKey, p)
}

// FromContext extracts the authenticated principal, if any.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey).(*Principal)
	return p, ok && p != nil
}
