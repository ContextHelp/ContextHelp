package auth

import (
	"context"

	kitpolicy "hop.top/kit/go/runtime/policy"
)

// Attach binds the authenticated principal to ctx for every downstream
// consumer at once: handlers and metering read it back via FromContext,
// and the kit policy engine's CEL `principal` binding resolves it
// through ContextPrincipalKey. Transports call Attach after a
// successful Authenticate so the two views can never drift.
//
// The kit Principal is a narrower shape: Role carries the primary
// (first) role, Source carries the provider name.
func Attach(ctx context.Context, p *Principal) context.Context {
	if p == nil {
		return ctx
	}
	ctx = WithPrincipal(ctx, p)
	role := ""
	if len(p.Roles) > 0 {
		role = p.Roles[0]
	}
	return context.WithValue(ctx, kitpolicy.ContextPrincipalKey, kitpolicy.Principal{
		ID:     p.ID,
		Role:   role,
		Source: p.Provider,
	})
}
