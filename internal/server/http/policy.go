package http

import (
	"context"
	"errors"
	"net/http"

	kitpolicy "hop.top/kit/go/runtime/policy"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
)

// securityEmitterKey stashes the router-scoped security emitter in
// request contexts so error mappers (writePolicyError) can record
// security events without threading the emitter through every handler
// constructor.
type securityEmitterKeyType struct{}

var securityEmitterKey securityEmitterKeyType

// withSecurityEmitterCtx attaches the emitter to ctx.
func withSecurityEmitterCtx(ctx context.Context, e *security.Emitter) context.Context {
	if e == nil {
		return ctx
	}
	return context.WithValue(ctx, securityEmitterKey, e)
}

// securityEmitterFrom reads the emitter back; nil when none was wired.
func securityEmitterFrom(ctx context.Context) *security.Emitter {
	e, _ := ctx.Value(securityEmitterKey).(*security.Emitter)
	return e
}

// WithSecurityEvents is router middleware that makes the emitter
// reachable from every request context.
func WithSecurityEvents(e *security.Emitter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(withSecurityEmitterCtx(r.Context(), e)))
		})
	}
}

// HeaderCtxtNote is the request header CLIs use to plumb their --note|-n
// flag through to the policy engine. The HTTP layer copies its value
// into ctx via policy.ContextAttrsKey before invoking the service so
// CEL rules see `context.note`.
const HeaderCtxtNote = "X-Ctxt-Note"

// withPolicyContext extracts request attributes (today: --note via the
// X-Ctxt-Note header) and stuffs them into ctx via
// policy.ContextAttrsKey. The service layer may merge additional
// attributes (e.g. action="archive") on top — see service.withPolicyAction.
func withPolicyContext(r *http.Request) context.Context {
	ctx := r.Context()
	attrs := map[string]any{
		"note": r.Header.Get(HeaderCtxtNote),
	}
	return context.WithValue(ctx, kitpolicy.ContextAttrsKey, attrs)
}

// writePolicyError translates a *policy.PolicyDeniedError into an
// HTTP 409 response with a stable code so CLI consumers can map it to
// exit code 4. Returns true when the error was a policy denial and was
// written to w; callers fall through to other error mappings otherwise.
// Each denial is recorded as an ACL-denial security event against the
// authenticated principal (or "anonymous" on private instances).
func writePolicyError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	var pde *kitpolicy.PolicyDeniedError
	if !errors.As(err, &pde) {
		return false
	}
	if sec := securityEmitterFrom(r.Context()); sec != nil {
		principal := "anonymous"
		if p, ok := authn.FromContext(r.Context()); ok {
			principal = p.ID
		}
		sec.RecordACLDenial(r.Context(), principal)
	}
	WriteError(w, http.StatusConflict, "POLICY_DENIED", pde.Error())
	return true
}
