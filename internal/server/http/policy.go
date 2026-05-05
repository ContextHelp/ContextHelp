package http

import (
	"context"
	"errors"
	"net/http"

	kitpolicy "hop.top/kit/go/runtime/policy"
)

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
func writePolicyError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var pde *kitpolicy.PolicyDeniedError
	if !errors.As(err, &pde) {
		return false
	}
	WriteError(w, http.StatusConflict, "POLICY_DENIED", pde.Error())
	return true
}
