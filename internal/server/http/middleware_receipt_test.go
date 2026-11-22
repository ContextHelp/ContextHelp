package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// auditSink is an AccessLogStore that keeps what it is appended.
type auditSink struct {
	mu      sync.Mutex
	entries []*storage.AccessEntry
	appends int
}

func (s *auditSink) Append(_ context.Context, e []*storage.AccessEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appends++
	s.entries = append(s.entries, e...)
	return nil
}

func (s *auditSink) Query(context.Context, storage.AccessFilter) ([]*storage.AccessEntry, error) {
	return nil, nil
}
func (s *auditSink) Prune(context.Context, time.Time) (int64, error) { return 0, nil }

func (s *auditSink) AppendSteward(_ context.Context, e *storage.AccessEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return nil
}

func (s *auditSink) PruneSteward(context.Context, time.Time) (int64, error) { return 0, nil }

func (s *auditSink) StewardChain(context.Context) (storage.AccessChainHead, []*storage.AccessEntry, error) {
	return storage.AccessChainHead{}, nil, nil
}

// receiptRig serves one route through the same stack mountAPIRoutes
// builds (Visibility, then RequireScope, then RequireCapability) behind
// a router, as principal p with viewer v, and reports the entries.
type receiptRig struct {
	t      *testing.T
	sink   *auditSink
	writer *sharing.AuditWriter
	router chi.Router
}

func newReceiptRig(t *testing.T, strict bool, routes ...apiRoute) *receiptRig {
	t.Helper()
	sink := &auditSink{}
	w := sharing.NewAuditWriter(sink, sharing.AuditOptions{Interval: time.Hour, Batch: 1 << 20})
	rig := &receiptRig{t: t, sink: sink, writer: w, router: chi.NewRouter()}
	mountAPIRoutes(rig.router, routes, RouterConfig{Auth: &authn.Chain{}, StrictSharing: strict, Audit: w}, nil)
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	return rig
}

var receiptPrincipal = &authn.Principal{
	ID: "prn_vis", Provider: authn.MethodAPIKey, Scopes: []authn.Scope{authn.ScopeReadObjects},
	Meta: map[string]string{authn.MetaIdentityID: "idn_vis"},
}

func (r *receiptRig) do(v sharing.Viewer, p *authn.Principal, method, target string, hdr ...string) *httptest.ResponseRecorder {
	r.t.Helper()
	ctx := sharing.WithViewer(authn.Attach(context.Background(), p), v)
	req := httptest.NewRequestWithContext(ctx, method, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	r.router.ServeHTTP(rec, req)
	return rec
}

// serveOn serves a GET on router with ctx as the request context.
func (r *receiptRig) serveOn(ctx context.Context, router http.Handler, target string) *httptest.ResponseRecorder {
	r.t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil))
	return rec
}

func (r *receiptRig) entries() []*storage.AccessEntry {
	r.t.Helper()
	require.NoError(r.t, r.writer.Flush(context.Background()))
	r.sink.mu.Lock()
	defer r.sink.mu.Unlock()
	return append([]*storage.AccessEntry(nil), r.sink.entries...)
}

func checkTrail(e *storage.AccessEntry) []string {
	out := make([]string, 0, len(e.Checks))
	for _, c := range e.Checks {
		out = append(out, c.Check+"="+string(c.Outcome)+":"+c.Reason)
	}
	return out
}

func objectsRoute(h http.Handler) apiRoute {
	return apiRoute{Method: http.MethodGet, Pattern: "/objects", Scope: authn.ScopeReadObjects, Handler: h}
}

func releasing(ids ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := sharing.LedgerFrom(r.Context())
		l.MarkScoped()
		data := []map[string]any{}
		for _, id := range ids {
			data = append(data, map[string]any{"id": id})
		}
		for _, id := range ids[:1] {
			l.AddObject(id)
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": data})
	}
}

// An admitted request leaves the ordered PASS trail, every check run.
func TestReceiptHTTPAllowed(t *testing.T) {
	rig := newReceiptRig(t, true, objectsRoute(releasing("a")))
	rec := rig.do(restrictedViewer(t), receiptPrincipal, http.MethodGet, "/objects?limit=5")
	require.Equal(t, http.StatusOK, rec.Code)
	es := rig.entries()
	require.Len(t, es, 1)
	assert.Equal(t, storage.AccessDecisionAllowed, es[0].Decision)
	assert.Equal(t, []string{
		"authn.api_key=PASS:authenticated", "scope=PASS:scope_held", "route_access=PASS:route_open",
		"share_policy=PASS:rows_scoped", "deny_class=NOT_APPLICABLE:no_ceiling", "backstop=PASS:clean",
	}, checkTrail(es[0]))
	assert.Equal(t, "prn_vis", es[0].PrincipalID)
	assert.Equal(t, "idn_vis", es[0].IdentityID)
	assert.Equal(t, "GET /objects", es[0].Route)
	assert.Equal(t, 1, es[0].ObjectCount)
}

// A route that reads nothing through the viewer's grants does not claim
// the share policy passed.
func TestReceiptHTTPUnscopedRouteIsNotApplicable(t *testing.T) {
	rig := newReceiptRig(t, true, apiRoute{Method: http.MethodGet, Pattern: "/whoami", Scope: authn.ScopeReadObjects,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			WriteJSON(w, http.StatusOK, map[string]any{"principal": "prn_vis"})
		})})
	require.Equal(t, http.StatusOK, rig.do(restrictedViewer(t), receiptPrincipal, http.MethodGet, "/whoami").Code)
	es := rig.entries()
	require.Len(t, es, 1)
	assert.Equal(t, storage.AccessDecisionAllowed, es[0].Decision)
	assert.Equal(t, []string{
		"authn.api_key=PASS:authenticated", "scope=PASS:scope_held", "route_access=PASS:route_open",
		"share_policy=NOT_APPLICABLE:no_scoped_read", "deny_class=NOT_APPLICABLE:no_scoped_read", "backstop=PASS:clean",
	}, checkTrail(es[0]))
}

// A scope refusal is an entry that ends at the scope check.
func TestReceiptHTTPScopeDenied(t *testing.T) {
	rig := newReceiptRig(t, true, objectsRoute(releasing("a")))
	noScope := &authn.Principal{ID: "prn_vis", Provider: authn.MethodAPIKey, Scopes: []authn.Scope{authn.ScopeReadJobs}}
	rec := rig.do(restrictedViewer(t), noScope, http.MethodGet, "/objects")
	require.Equal(t, http.StatusForbidden, rec.Code)
	es := rig.entries()
	require.Len(t, es, 1)
	assert.Equal(t, storage.AccessDecisionDenied, es[0].Decision)
	assert.Equal(t, []string{"authn.api_key=PASS:authenticated", "scope=FAIL:scope_missing"}, checkTrail(es[0]))
	assert.Zero(t, es[0].ObjectCount)
}

// A route the capability table closes (owner-only, unclassified) or a
// capability the viewer lacks is an entry that ends at route_access.
func TestReceiptHTTPRouteAccessDenied(t *testing.T) {
	v := restrictedViewer(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, http.StatusOK, map[string]any{}) })
	noCaps, err := sharing.Compile(sharing.Input{PrincipalID: "prn_vis", Grants: []sharing.Grant{{ID: "g", Audience: "a", Policy: sharing.Policy{Name: "bare", Data: "type==contact"}}}})
	require.NoError(t, err)
	cases := []struct {
		name, pattern, reason string
		viewer                sharing.Viewer
	}{
		{"owner only", "/jobs", sharing.ReasonOwnerOnly, v},
		{"capability missing", "/entities", sharing.ReasonCapabilityMissing, noCaps},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rig := newReceiptRig(t, true, apiRoute{Method: http.MethodGet, Pattern: c.pattern, Scope: authn.ScopeReadObjects, Handler: handler})
			p := *receiptPrincipal
			rec := rig.do(c.viewer, &p, http.MethodGet, c.pattern)
			require.Equal(t, http.StatusForbidden, rec.Code, "%s", rec.Body)
			es := rig.entries()
			require.Len(t, es, 1)
			assert.Equal(t, storage.AccessDecisionDenied, es[0].Decision)
			assert.Equal(t, []string{"authn.api_key=PASS:authenticated", "scope=PASS:scope_held", "route_access=FAIL:" + c.reason}, checkTrail(es[0]))
		})
	}
}

// A route with no classification fails closed before the handler, and
// before the scope is looked at: the trail ends at route_access.
func TestReceiptHTTPUnclassifiedRoute(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, http.StatusOK, map[string]any{}) })
	rig := newReceiptRig(t, true, apiRoute{Method: http.MethodGet, Pattern: "/no-such-route", Scope: authn.ScopeReadObjects, Handler: handler})
	require.Equal(t, http.StatusInternalServerError, rig.do(restrictedViewer(t), receiptPrincipal, http.MethodGet, "/no-such-route").Code)
	es := rig.entries()
	require.Len(t, es, 1)
	assert.Equal(t, storage.AccessDecisionDenied, es[0].Decision)
	assert.Equal(t, []string{"authn.api_key=PASS:authenticated", "route_access=FAIL:route_unclassified"}, checkTrail(es[0]))
}

// A viewer no grant applies to is refused by the share policy.
func TestReceiptHTTPNoGrantDenied(t *testing.T) {
	v, err := sharing.Compile(sharing.Input{PrincipalID: "prn_vis"})
	require.NoError(t, err)
	// The route is open to a viewer with no grant; the handler reads
	// through the scoped driver and finds nothing.
	rig := newReceiptRig(t, true, objectsRoute(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sharing.LedgerFrom(r.Context()).MarkScoped()
		WriteJSON(w, http.StatusOK, map[string]any{"data": []any{}})
	})))
	require.Equal(t, http.StatusOK, rig.do(v, receiptPrincipal, http.MethodGet, "/objects").Code)
	es := rig.entries()
	require.Len(t, es, 1)
	assert.Equal(t, storage.AccessDecisionDenied, es[0].Decision)
	assert.Contains(t, checkTrail(es[0]), "share_policy=FAIL:no_applicable_grant")
}

// The backstop firing is recorded as the failing check: refused on a
// strict instance, stripped otherwise; a response it cannot read
// defers.
func TestReceiptHTTPBackstop(t *testing.T) {
	v := restrictedViewer(t)
	t.Run("strict hit is denied", func(t *testing.T) {
		rig := newReceiptRig(t, true, objectsRoute(releasing("a", "hidden")))
		require.Equal(t, http.StatusInternalServerError, rig.do(v, receiptPrincipal, http.MethodGet, "/objects").Code)
		es := rig.entries()
		require.Len(t, es, 1)
		assert.Equal(t, storage.AccessDecisionDenied, es[0].Decision)
		assert.Equal(t, "backstop=FAIL:hit_refused", checkTrail(es[0])[len(es[0].Checks)-1])
	})
	t.Run("lax hit is stripped and allowed", func(t *testing.T) {
		rig := newReceiptRig(t, false, objectsRoute(releasing("a", "hidden")))
		require.Equal(t, http.StatusOK, rig.do(v, receiptPrincipal, http.MethodGet, "/objects").Code)
		es := rig.entries()
		require.Len(t, es, 1)
		assert.Equal(t, storage.AccessDecisionAllowed, es[0].Decision)
		assert.Equal(t, "backstop=FAIL:hit_stripped", checkTrail(es[0])[len(es[0].Checks)-1])
	})
	t.Run("unreadable response is deferred", func(t *testing.T) {
		rig := newReceiptRig(t, true, objectsRoute(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) })))
		require.Equal(t, http.StatusInternalServerError, rig.do(v, receiptPrincipal, http.MethodGet, "/objects").Code)
		es := rig.entries()
		require.Len(t, es, 1)
		assert.Equal(t, storage.AccessDecisionDeferred, es[0].Decision)
		assert.Equal(t, "backstop=UNKNOWN:response_unreadable", checkTrail(es[0])[len(es[0].Checks)-1])
	})
}

// The owner is not logged, refused or not; a non-owner sending
// X-Ctxt-View-As is refused at view_as; the owner's preview is logged as
// the owner's request.
func TestReceiptHTTPViewAs(t *testing.T) {
	owner := &authn.Principal{ID: storage.OwnerPrincipal, Provider: authn.MethodAPIKey, Scopes: []authn.Scope{authn.ScopeReadObjects}}
	rig := newReceiptRig(t, true, objectsRoute(releasing("a")))

	require.Equal(t, http.StatusOK, rig.do(sharing.Unrestricted, owner, http.MethodGet, "/objects").Code)
	require.Equal(t, http.StatusNotFound, rig.do(sharing.Unrestricted, owner, http.MethodGet, "/objects", sharing.HeaderViewAs, "nobody").Code)
	assert.Empty(t, rig.entries(), "the owner's requests and refused previews are not logged")

	rec := rig.do(restrictedViewer(t), receiptPrincipal, http.MethodGet, "/objects", sharing.HeaderViewAs, "prn_other")
	require.Equal(t, http.StatusForbidden, rec.Code)
	es := rig.entries()
	require.Len(t, es, 1)
	assert.Equal(t, storage.AccessDecisionDenied, es[0].Decision)
	assert.Equal(t, []string{"authn.api_key=PASS:authenticated", "view_as=FAIL:owner_only"}, checkTrail(es[0]))
}

// The digest is the same whatever order the query arrives in, differs
// when a consequential param differs, and counts path params.
func TestReceiptHTTPDigest(t *testing.T) {
	v := restrictedViewer(t)
	byID := apiRoute{Method: http.MethodGet, Pattern: "/objects/{id}", Scope: authn.ScopeReadObjects, Handler: releasing("a")}
	rig := newReceiptRig(t, true, objectsRoute(releasing("a")), byID)
	for _, target := range []string{
		"/objects?limit=5&sort=created_at", "/objects?sort=created_at&limit=5", "/objects?limit=6&sort=created_at",
		"/objects/a?x=1", "/objects/b?x=1",
	} {
		rig.do(v, receiptPrincipal, http.MethodGet, target)
	}
	es := rig.entries()
	require.Len(t, es, 5)
	d := func(i int) string { return es[i].OperationDigest }
	assert.Equal(t, d(0), d(1), "param order is not consequential")
	assert.NotEqual(t, d(0), d(2), "a different value is a different operation")
	assert.NotEqual(t, d(3), d(4), "a different path param is a different operation")
	assert.NotEqual(t, d(0), d(3))
	assert.Equal(t, "GET /objects/{id}", es[3].Route, "the route is the template, not the path")
}

// A stream's receipt is written when the stream ends, stamped with the
// time it began, and its share policy is NOT_APPLICABLE: the handler
// filters every frame for the viewer.
func TestReceiptHTTPStream(t *testing.T) {
	rig := newReceiptRig(t, true, apiRoute{Method: http.MethodGet, Pattern: "/events", Scope: authn.ScopeReadObjects,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(40 * time.Millisecond)
			_, _ = w.Write([]byte("data: {}\n\n"))
		})})
	started := time.Now()
	require.Equal(t, http.StatusOK, rig.do(restrictedViewer(t), receiptPrincipal, http.MethodGet, "/events").Code)
	es := rig.entries()
	require.Len(t, es, 1)
	assert.Equal(t, storage.AccessDecisionAllowed, es[0].Decision)
	assert.Equal(t, []string{
		"authn.api_key=PASS:authenticated", "scope=PASS:scope_held", "route_access=PASS:route_open",
		"share_policy=NOT_APPLICABLE:stream_filtered", "deny_class=NOT_APPLICABLE:no_scoped_read",
	}, checkTrail(es[0]))
	assert.WithinDuration(t, started, es[0].At, 20*time.Millisecond, "the start of the stream, not its end")
}

// An unauthenticated request never reaches an entry; denied requests go
// through the buffer and never write the store on the request path.
func TestReceiptHTTPDenialsRideTheBuffer(t *testing.T) {
	rig := newReceiptRig(t, true, apiRoute{Method: http.MethodGet, Pattern: "/jobs", Scope: authn.ScopeReadObjects,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, http.StatusOK, map[string]any{}) })})
	for i := 0; i < 50; i++ {
		require.Equal(t, http.StatusForbidden, rig.do(restrictedViewer(t), receiptPrincipal, http.MethodGet, "/jobs").Code)
	}
	assert.Equal(t, 50, rig.writer.Buffered(), "denials are buffered")
	rig.sink.mu.Lock()
	assert.Zero(t, rig.sink.appends, "no store write on the request path")
	rig.sink.mu.Unlock()
	assert.Len(t, rig.entries(), 50)
}
