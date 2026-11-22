package http

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CodeVisibilityBackstop is the error code of the 500 a strict
// instance answers when the backstop fired: the response carried an
// ID or a value the viewer may not see, which is a bug in the scoped
// reads or the render layer.
const CodeVisibilityBackstop = "VISIBILITY_BACKSTOP"

// View-as refusals (ADR-080 §2, owner bypass).
const (
	// CodeViewAsOwnerOnly is the 403 a non-owner gets for sending
	// X-Ctxt-View-As.
	CodeViewAsOwnerOnly = "VIEW_AS_OWNER_ONLY"
	// CodeViewAsReadOnly is the 400 the owner gets for sending
	// X-Ctxt-View-As on a route that writes.
	CodeViewAsReadOnly = "VIEW_AS_READ_ONLY"
)

// VisibilityConfig is what the visibility middleware needs beyond the
// route table.
type VisibilityConfig struct {
	// Strict answers 500 CodeVisibilityBackstop on any hit instead of
	// stripping it (--strict-sharing; always on in tests).
	Strict bool
	// Security receives the security.visibility_backstop events;
	// nil-safe.
	Security *security.Emitter
	// Audit receives the read audit entry of every restricted request
	// (ADR-080 §9); nil-safe.
	Audit *sharing.AuditWriter
	// Viewers resolves X-Ctxt-View-As targets; nil answers every
	// target as unknown.
	Viewers *sharing.Resolver
}

// Visibility returns the middleware that wraps a restricted viewer's
// request on the route mounted on method and pattern (ADR-080 §8):
// it opens the request's receipt (sharing.Receipt), which the scope and
// capability checks behind it and the checks below append to, and
// finishes it once on every way out, early refusals included: one read
// audit entry (§9) with the decision, the ordered checks and the
// operation digest. It also attaches the request's ledger, which the
// scoped reads record every released row into; buffers the response;
// walks the ID paths of the route's declared shape and searches the
// body for the withheld values, stripping anything the ledger does not
// hold; and logs, counts and (strictly) refuses a hit. An unrestricted
// viewer passes through untouched and unbuffered: the owner pays
// nothing. A route with no declared shape fails closed for a restricted
// viewer.
//
// The owner previewing another viewer (X-Ctxt-View-As: a principal id
// or a share policy name) has its viewer swapped here, before the
// ledger, so the request runs through that view, render, backstop and
// audit included (§2, owner bypass); the audit names the owner. Only
// the owner may send the header, and only on a route that reads
// (scope verb read, or a safe method).
func Visibility(method, pattern string, scope authn.Scope, cfg VisibilityConfig) func(http.Handler) http.Handler {
	shape, known := RouteShape(method, pattern)
	route := routeKey(method, pattern)
	reads := method == "" || isSafeMethod(method) || scope.Verb() == "read"
	streams := RouteStreams(method, pattern)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v, ok := sharing.FromContext(r.Context())
			target := strings.TrimSpace(r.Header.Get(sharing.HeaderViewAs))
			restricted := !ok || !v.IsUnrestricted()
			if !restricted && target == "" {
				next.ServeHTTP(w, r)
				return
			}
			// One receipt per audited request, finished once on every
			// way out, early refusals included (ADR-080 §9).
			rcpt := sharing.NewReceipt(operationFor(r, route, v))
			r = r.WithContext(sharing.WithReceipt(r.Context(), rcpt))
			// A panic below still leaves the entry: Finish commits an
			// elevated one durably, and does nothing once released.
			defer func() { rcpt.Finish(cfg.Audit) }()
			serve := func(w http.ResponseWriter, r *http.Request) {
				serveAudited(w, r, auditedRun{
					shape: shape, known: known, route: route, reads: reads, streams: streams, target: target,
					restricted: restricted, viewer: v, attached: ok, receipt: rcpt,
				}, cfg, next)
			}
			if !rcpt.Elevated() {
				serve(w, r)
				return
			}
			// An elevated request (ADR-083 §5, option B): whatever the
			// answer, a refusal included, it is held until the request's
			// entry is committed, and replaced by 503 AUDIT_UNAVAILABLE
			// when the entry cannot be.
			keep := w.Header().Clone()
			held := newRecorder(w)
			serve(held, r)
			releaseElevated(w, r, held, keep, rcpt, cfg.Audit)
		})
	}
}

// auditedRun is what serveAudited needs of one audited request.
type auditedRun struct {
	shape      sharing.Shape
	known      bool
	route      string
	reads      bool
	streams    bool
	target     string
	restricted bool
	viewer     sharing.Viewer
	attached   bool
	receipt    *sharing.Receipt
}

// serveAudited runs an audited request (a restricted viewer's, or the
// owner's preview): the view-as swap, then the handler behind the
// ledger, then the backstop over its buffered answer, written to w.
func serveAudited(w http.ResponseWriter, r *http.Request, run auditedRun, cfg VisibilityConfig, next http.Handler) {
	rcpt, v, ok, target, restricted := run.receipt, run.viewer, run.attached, run.target, run.restricted
	shape, known, route, reads := run.shape, run.known, run.route, run.reads
	if target != "" {
		pv, refused := viewAs(w, r, cfg.Viewers, v, ok, reads, target)
		if refused != "" {
			if restricted {
				rcpt.Refuse(sharing.CheckViewAs, refused)
			} else {
				rcpt.Discard() // the owner's own requests are not logged
			}
			return
		}
		v, ok = pv, true
		r = r.WithContext(previewContext(r.Context(), pv, target))
		p, _ := sharing.PreviewFrom(r.Context())
		rcpt.SetPreview(p, pv)
	}
	if ok && v.IsUnrestricted() {
		// The owner previewing the owner is the owner: untouched
		// and not logged.
		rcpt.Discard()
		next.ServeHTTP(w, r)
		return
	}
	if run.streams {
		// The handler filters each frame for the viewer; a buffered
		// check would hold the stream until it ends. Its receipt is
		// written when the stream ends, stamped with when it began. An
		// elevated request never gets this far: the capability check
		// refuses it (steward_no_stream) before the handler.
		next.ServeHTTP(w, r)
		if !rcpt.Refused() {
			rcpt.SettleStream()
		}
		return
	}
	if !known {
		slog.Error("visibility: route declares no response shape", "route", route)
		rcpt.Refuse(sharing.CheckRouteAccess, sharing.ReasonRouteUnclassified)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "route declares no response shape")
		return
	}
	ledger := sharing.NewLedger()
	rcpt.SetLedger(ledger)
	ctx := sharing.WithLedger(r.Context(), ledger)
	rec := newRecorder(w)
	next.ServeHTTP(rec, r.WithContext(ctx))
	// A check that refused the request ended its trail there: the
	// handler never ran, and the checks after it are not recorded.
	// The response is still checked below, whatever its source.
	reached := !rcpt.Refused()
	if reached {
		rcpt.SettlePolicy()
	}
	answer(w, r, rec, backstopRun{shape: shape, route: route, viewer: v, ledger: ledger, reached: reached}, cfg, rcpt)
}

// backstopRun is what answer needs to check one buffered response.
type backstopRun struct {
	shape   sharing.Shape
	route   string
	viewer  sharing.Viewer
	ledger  *sharing.Ledger
	reached bool
}

// answer runs the backstop over the buffered response and writes what
// passes (ADR-080 §8), recording the backstop check on the receipt:
// refused on a strict instance, stripped otherwise, unreadable when the
// body cannot be checked. A request a check refused before the handler
// ran (reached false) is still checked, but the check is not recorded,
// since the trail ended at the refusal.
func answer(w http.ResponseWriter, r *http.Request, rec *recorder, run backstopRun, cfg VisibilityConfig, rcpt *sharing.Receipt) {
	res, err := sharing.Backstop(rec.body.Bytes(), sharing.HTTPPaths(run.shape), run.ledger)
	if err != nil {
		slog.Error("visibility: backstop cannot check the response", "route", run.route, "err", err)
		if run.reached {
			rcpt.Defer(sharing.CheckBackstop, sharing.ReasonResponseUnreadable)
		}
		w.Header().Del("Content-Length")
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "response cannot be checked for visibility")
		return
	}
	if len(res.Hits) > 0 {
		recordBackstop(r, cfg.Security, run.route, run.viewer, res.Hits)
		// An elevated request is strict whatever the instance says: it
		// must not receive a silently incomplete answer (ADR-083 §3).
		if cfg.Strict || run.viewer.StrictBackstop() {
			if run.reached {
				rcpt.Refuse(sharing.CheckBackstop, sharing.ReasonHitRefused)
			}
			w.Header().Del("Content-Length")
			WriteError(w, http.StatusInternalServerError, CodeVisibilityBackstop, "the response carried data outside the viewer's visibility and was refused")
			return
		}
	}
	switch {
	case !run.reached:
	case len(res.Hits) > 0 || res.Absent:
		rcpt.Fail(sharing.CheckBackstop, sharing.ReasonHitStripped)
	default:
		rcpt.Pass(sharing.CheckBackstop, sharing.ReasonClean)
	}
	if res.Absent {
		w.Header().Del("Content-Length")
		WriteError(w, http.StatusNotFound, CodeNotFound, "resource not found")
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(res.Body)))
	w.WriteHeader(rec.status)
	_, _ = w.Write(res.Body)
}

// operationFor describes the request for its receipt: the route
// template, the consequential params (query and path), and the caller as
// the authentication middleware attached it. A request whose principal
// was not attached falls back to the viewer's, so a viewer is always
// attributed to the principal it was compiled for.
func operationFor(r *http.Request, route string, v sharing.Viewer) sharing.Operation {
	op := sharing.Operation{
		Method: r.Method, Route: route, Params: httpParams(r), Viewer: v,
		Principal: v.PrincipalID(), Identity: v.IdentityID(),
	}
	if p, ok := authn.FromContext(r.Context()); ok {
		op.Principal, op.Identity = p.ID, p.Meta[authn.MetaIdentityID]
		if p.Provider != "" && p.Provider != "none" {
			op.AuthMethod = p.Provider
		}
	}
	return op
}

// httpParams lists the request's consequential params for the
// operation digest: every query parameter as "query.<name>" and every
// route parameter as "path.<name>".
func httpParams(r *http.Request) map[string][]string {
	params := map[string][]string{}
	for k, vs := range r.URL.Query() {
		params["query."+k] = vs
	}
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		for i, k := range rctx.URLParams.Keys {
			if i < len(rctx.URLParams.Values) {
				params["path."+k] = append(params["path."+k], rctx.URLParams.Values[i])
			}
		}
	}
	return params
}

// viewAs resolves an X-Ctxt-View-As target for the owner. When the
// request may not go on it has written the refusal and returns its
// reason code: a non-owner sender (403, owner_only), a route that
// writes (400), an unknown target (404) or a resolution failure (500).
// The reason is empty when the preview is on.
func viewAs(w http.ResponseWriter, r *http.Request, viewers *sharing.Resolver, v sharing.Viewer, ok, reads bool, target string) (sharing.Viewer, string) {
	if !ok || !v.IsUnrestricted() {
		WriteError(w, http.StatusForbidden, CodeViewAsOwnerOnly, sharing.HeaderViewAs+" is for the owner")
		return v, sharing.ReasonOwnerOnly
	}
	if !reads {
		WriteError(w, http.StatusBadRequest, CodeViewAsReadOnly, sharing.HeaderViewAs+" previews read routes only")
		return v, sharing.ReasonViewAsReadOnly
	}
	pv, err := viewers.ViewAs(r.Context(), target)
	switch {
	case errors.Is(err, sharing.ErrViewAsUnknown):
		WriteError(w, http.StatusNotFound, CodeNotFound, "no principal or share policy named "+strconv.Quote(target))
		return v, sharing.ReasonViewAsUnknownTarget
	case err != nil:
		slog.Error("view-as: resolve failed", "target", target, "err", err)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "view-as resolution failed")
		return v, sharing.ReasonViewAsResolutionFailed
	}
	return pv, ""
}

// previewContext attaches the previewed viewer and the preview marker
// (the owner's own principal and credential, for the audit).
func previewContext(ctx context.Context, pv sharing.Viewer, target string) context.Context {
	p := sharing.Preview{Target: target, Principal: storage.OwnerPrincipal}
	if owner, ok := authn.FromContext(ctx); ok {
		p.Principal, p.Identity = owner.ID, owner.Meta[authn.MetaIdentityID]
	}
	return sharing.WithPreview(sharing.WithViewer(ctx, pv), p)
}

// recordBackstop logs and emits a security.visibility_backstop event
// for a hit: route, principal, policy versions and the hits.
func recordBackstop(r *http.Request, sec *security.Emitter, route string, v sharing.Viewer, hits []string) {
	principal := ""
	if p, ok := authn.FromContext(r.Context()); ok {
		principal = p.ID
	}
	meta := map[string]string{
		"route":           route,
		"policy_versions": v.PolicyKey(),
		"hits":            strconv.Itoa(len(hits)),
		"first_hit":       hits[0],
	}
	slog.Warn(security.EventVisibilityBackstop, "route", route, "principal", principal, "policy_versions", v.PolicyKey(), "hits", hits)
	if sec != nil {
		sec.RecordVisibilityBackstop(r.Context(), principal, meta)
	}
}

// recorder buffers a handler's response so the backstop can check it
// before it leaves. Headers go to the real writer's map as the handler
// sets them; nothing is flushed until the middleware writes. Streams
// are owner-only today, so a restricted viewer's response is always
// bounded; Flush is a no-op.
type recorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func newRecorder(w http.ResponseWriter) *recorder {
	return &recorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *recorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.body.Write(b)
}

// Flush is a no-op: the response is written whole after the check.
func (r *recorder) Flush() {}
