package http

import (
	"bytes"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// CodeVisibilityBackstop is the error code of the 500 a strict
// instance answers when the backstop fired: the response carried an
// ID or a value the viewer may not see, which is a bug in the scoped
// reads or the render layer.
const CodeVisibilityBackstop = "VISIBILITY_BACKSTOP"

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
}

// Visibility returns the middleware that wraps a restricted viewer's
// request on the route mounted on method and pattern (ADR-080 §8):
// it attaches the request's ledger, which the scoped reads record
// every released row into; buffers the response; records the read
// audit entry (§9); walks the ID paths of the route's declared shape
// and searches the body for the withheld values, stripping anything
// the ledger does not hold; and logs, counts and (strictly) refuses a
// hit. An unrestricted viewer
// passes through untouched and unbuffered: the owner pays nothing.
// A route with no declared shape fails closed for a restricted viewer.
func Visibility(method, pattern string, cfg VisibilityConfig) func(http.Handler) http.Handler {
	shape, known := RouteShape(method, pattern)
	route := routeKey(method, pattern)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v, ok := sharing.FromContext(r.Context())
			if ok && v.IsUnrestricted() {
				next.ServeHTTP(w, r)
				return
			}
			if !known {
				slog.Error("visibility: route declares no response shape", "route", route)
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "route declares no response shape")
				return
			}
			ledger := sharing.NewLedger()
			ctx := sharing.WithLedger(r.Context(), ledger)
			rec := newRecorder(w)
			next.ServeHTTP(rec, r.WithContext(ctx))
			cfg.Audit.Record(sharing.AccessEntryFor(ctx, v, route, ledger, time.Now()))

			res, err := sharing.Backstop(rec.body.Bytes(), sharing.HTTPPaths(shape), ledger)
			if err != nil {
				slog.Error("visibility: backstop cannot check the response", "route", route, "err", err)
				w.Header().Del("Content-Length")
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "response cannot be checked for visibility")
				return
			}
			if len(res.Hits) > 0 {
				recordBackstop(r, cfg.Security, route, v, res.Hits)
				if cfg.Strict {
					w.Header().Del("Content-Length")
					WriteError(w, http.StatusInternalServerError, CodeVisibilityBackstop, "the response carried data outside the viewer's visibility and was refused")
					return
				}
			}
			status, body := rec.status, res.Body
			if res.Absent {
				w.Header().Del("Content-Length")
				WriteError(w, http.StatusNotFound, CodeNotFound, "resource not found")
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(status)
			_, _ = w.Write(body)
		})
	}
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
