package http

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// apiPrefix is where the route table is mounted; route keys are
// relative to it.
const apiPrefix = "/api/v1"

// refuseElevation answers a request whose steward grant failed a check
// (ADR-083 §3, §4, §5), in this order:
//
//  1. the refusal is a read audit entry like any other elevated
//     request's, the steward_grant check failed with the resolution's
//     code, committed durably before anything else;
//  2. one security.steward.elevation.refused event names the grant and
//     the code;
//  3. when the answer is no and the entry is committed, the browser
//     session's elevation ends (sessions, nil-safe): an elevation the
//     grant can no longer serve does not survive it, and the session
//     lives on in its ordinary view;
//  4. the answer: the check's code, 403 (503 when deferred).
//
// A deferred refusal (a store could not be read) keeps the elevation for
// the next request to try again. So does a refusal whose entry could
// not be committed: it is answered 503 AUDIT_UNAVAILABLE, as every
// elevated request whose record failed, and its event says deferred.
func refuseElevation(w http.ResponseWriter, r *http.Request, princ *authn.Principal, refusal *sharing.StewardRefusal, sec *security.Emitter, sessions *authn.Sessions, audit *sharing.AuditWriter) {
	route, params := requestRoute(r)
	op := sharing.Operation{
		Method: r.Method, Route: route, Params: params,
		Principal: princ.ID, Identity: princ.Meta[authn.MetaIdentityID], AuthMethod: authMethod(princ),
		StewardGrant: princ.Meta[authn.MetaStewardGrantID],
	}
	rcpt := sharing.NewReceipt(op)
	rcpt.StewardRefused(refusal)
	committed := rcpt.Commit(r.Context(), audit) == nil
	deferred := refusal.Deferred || !committed
	steward.RecordElevationRefused(r.Context(), sec, princ.ID, op.StewardGrant, refusal.Reason, deferred)
	if !deferred {
		endRefusedElevation(r.Context(), princ, op.StewardGrant, sessions)
	}
	if !committed {
		writeAuditUnavailable(w, nil)
		return
	}
	writeStewardRefusal(w, princ, refusal)
}

// endRefusedElevation ends the browser session's elevation into grant
// after a refusal; a principal that is no browser session (a key) has
// none to end.
func endRefusedElevation(ctx context.Context, princ *authn.Principal, grant string, sessions *authn.Sessions) {
	if sessions == nil || !princ.IsSession() || grant == "" {
		return
	}
	if _, err := sessions.EndElevation(ctx, princ, grant, authn.ElevationEndedRefused); err != nil {
		slog.Warn("auth: ending a refused steward elevation failed", "principal", princ.ID, "err", err)
	}
}

// authMethod is the method that admitted p, as a receipt names it; ""
// when none did.
func authMethod(p *authn.Principal) string {
	if p == nil || p.Provider == "" || p.Provider == "none" {
		return ""
	}
	return p.Provider
}

// writeAuditUnavailable replaces an elevated request's answer when its
// entry could not be committed: 503 AUDIT_UNAVAILABLE and nothing else.
// The headers the request carried in (keep, from the middleware ahead
// of it) are restored and every header the handler set is dropped, so
// no count, cursor or length of the withheld answer leaves either.
func writeAuditUnavailable(w http.ResponseWriter, keep http.Header) {
	h := w.Header()
	for k := range h {
		delete(h, k)
	}
	for k, v := range keep {
		h[k] = v
	}
	WriteError(w, http.StatusServiceUnavailable, sharing.CodeAuditUnavailable,
		"the request could not be recorded; nothing was released")
}

// releaseElevated sends an elevated request's held answer once its
// entry is committed (ADR-083 §5, option B), or 503 AUDIT_UNAVAILABLE
// in its place when the commit fails.
func releaseElevated(w http.ResponseWriter, r *http.Request, held *recorder, keep http.Header, rcpt *sharing.Receipt, audit *sharing.AuditWriter) {
	if err := rcpt.Commit(r.Context(), audit); err != nil {
		writeAuditUnavailable(w, keep)
		return
	}
	w.WriteHeader(held.status)
	_, _ = w.Write(held.body.Bytes())
}

// requestRoute names the route of a request that never reached it (a
// refused elevation is answered ahead of the router's route): the
// route key of the table entry the router would match, relative to
// /api/v1, with the request's query and path params as the visibility
// middleware lists them. A path no table route matches is recorded as
// its method and "(unmatched)", never the raw path, whose segments may
// be ids the caller asked for.
func requestRoute(r *http.Request) (string, map[string][]string) {
	params := map[string][]string{}
	for k, vs := range r.URL.Query() {
		params["query."+k] = vs
	}
	rctx := chi.RouteContext(r.Context())
	if rctx == nil || rctx.Routes == nil {
		return routeKey(r.Method, unmatchedRoute), params
	}
	found := chi.NewRouteContext()
	pattern := rctx.Routes.Find(found, r.Method, r.URL.Path)
	rel, ok := strings.CutPrefix(pattern, apiPrefix)
	if pattern == "" || !ok {
		return routeKey(r.Method, unmatchedRoute), params
	}
	for i, k := range found.URLParams.Keys {
		if i >= len(found.URLParams.Values) {
			break
		}
		v := found.URLParams.Values[i]
		if k == "*" {
			// The mount's wildcard: chi empties it on the way into the
			// sub-router, so the route's own receipt sees "" too.
			v = ""
		}
		params["path."+k] = append(params["path."+k], v)
	}
	return routeKey(tableMethod(r.Method, rel), rel), params
}

// unmatchedRoute stands for a path no route of the table matches.
const unmatchedRoute = "(unmatched)"

var (
	tableMethodsOnce sync.Once
	tableMethods     map[string]bool
)

// tableMethod is method when the table mounts pattern on it, and ""
// when the table mounts pattern on every method (the MCP mount), so
// the key matches the one the visibility middleware records.
func tableMethod(method, pattern string) string {
	tableMethodsOnce.Do(func() {
		tableMethods = map[string]bool{}
		for _, s := range APIRouteSpecs() {
			tableMethods[routeKey(s.Method, s.Pattern)] = true
		}
	})
	if !tableMethods[routeKey(method, pattern)] && tableMethods[routeKey("", pattern)] {
		return ""
	}
	return method
}

// seesStewardObjects reports whether the caller may see the object id
// lists of steward entries (ADR-083 §5): the owner and approve:steward
// holders. No principal on the request is a private instance, where
// every caller is the owner.
func seesStewardObjects(r *http.Request) bool {
	p, ok := authn.FromContext(r.Context())
	if !ok {
		return true
	}
	return p.Kind == storage.PrincipalKindOwner || p.HasScope(authn.ScopeApproveSteward)
}

// logStewardRefusal logs a refused elevation; the cause may name
// internals and is never shown to the caller.
func logStewardRefusal(princ *authn.Principal, refusal *sharing.StewardRefusal) {
	slog.Warn("auth: steward grant refused", "principal", princ.ID, "reason", refusal.Reason, "deferred", refusal.Deferred, "err", refusal.Err)
}
