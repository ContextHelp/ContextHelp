// Package sharing carries per-viewer visibility through a request
// (ADR-080). A Viewer says what the caller may see; storage reads are
// scoped by it and the render layer masks by it.
//
// The package must stay free of internal/auth imports: transports
// resolve a principal (auth) and then attach a Viewer (sharing), so
// auth never depends on sharing and sharing never depends on auth.
package sharing

import "context"

// Viewer is what a request's caller is allowed to see.
//
// The zero Viewer is the most restrictive value: nothing visible. It
// is what a context holds when no transport attached a viewer, and
// what an unresolved or revoked principal resolves to, so a forgotten
// attach fails closed. Unrestricted is the only value that bypasses
// visibility and it must always be chosen explicitly.
//
// Fields are unexported on purpose: the compiled predicate, policy
// versions and render plan arrive later without changing any caller.
type Viewer struct {
	unrestricted bool
}

// Unrestricted sees everything. It is the owner's viewer, and the
// viewer of every caller on a private instance, where every request is
// the owner (ADR-079 §1). Attach it only where the caller's identity
// has been established; never as a default.
var Unrestricted = Viewer{unrestricted: true}

// IsUnrestricted reports whether v bypasses visibility entirely. The
// zero Viewer and every restricted viewer answer false.
func (v Viewer) IsUnrestricted() bool { return v.unrestricted }

type ctxKey struct{}

// WithViewer returns a context carrying v. Transports call it right
// after authentication, in the same step that attaches the principal,
// so a request can never reach a handler authenticated but unscoped.
func WithViewer(ctx context.Context, v Viewer) context.Context {
	return context.WithValue(ctx, ctxKey{}, v)
}

// FromContext returns the viewer attached to ctx and whether one was
// attached at all. A context without a viewer yields the zero Viewer,
// never Unrestricted: callers that read a missing viewer see nothing.
func FromContext(ctx context.Context) (Viewer, bool) {
	v, ok := ctx.Value(ctxKey{}).(Viewer)
	return v, ok
}
