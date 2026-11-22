package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// elevated is the rig steward's viewer under its grant.
func (r *stewardRig) elevated(t *testing.T) sharing.Viewer {
	t.Helper()
	v, err := r.viewers.Resolve(context.Background(), r.steward, storage.PrincipalKindPerson, "", sharing.AsSteward(r.grant.ID))
	require.NoError(t, err)
	return v
}

// serveElevated runs handler behind Visibility on GET pattern as the
// steward under v, with the audit on audit, writing to w.
func (r *stewardRig) serveElevated(w http.ResponseWriter, v sharing.Viewer, audit *sharing.AuditWriter, pattern string, handler http.HandlerFunc) {
	h := Visibility(http.MethodGet, pattern, authn.ScopeReadObjects, VisibilityConfig{Audit: audit})(handler)
	p := r.principal(map[string]string{authn.MetaStewardGrantID: r.grant.ID})
	ctx := sharing.WithViewer(authn.Attach(context.Background(), p), v)
	h.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1"+pattern, nil))
}

// releases is a handler that releases obj-1 with a value worth hiding,
// and sets the headers a list answer carries.
func releases(w http.ResponseWriter, r *http.Request) {
	sharing.LedgerFrom(r.Context()).AddObject("obj-1")
	w.Header().Set("X-Total-Count", "1")
	WriteJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"id": "obj-1", "title": "Supplier contract"}}, "total": 1})
}

// An elevated viewer's backstop is strict even on an instance that
// strips: a hit is refused, not removed.
func TestVisibilityIsStrictForAnElevatedViewer(t *testing.T) {
	r := newStewardRig(t)
	ordinary := restrictedViewer(t)
	leak := func(w http.ResponseWriter, req *http.Request) {
		sharing.LedgerFrom(req.Context()).AddObject("a")
		WriteJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"id": "a"}, {"id": "hidden"}}, "total": 2})
	}
	rec := serveVisibility(t, ordinary, false, "/objects", leak)
	assert.Equal(t, http.StatusOK, rec.Code, "an ordinary viewer on a lenient instance has the hit stripped")
	rec = httptest.NewRecorder()
	r.serveElevated(rec, r.elevated(t), r.audit, "/objects", leak)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), CodeVisibilityBackstop)
	assert.NotContains(t, rec.Body.String(), "hidden")
	got := r.entries(t, r.grant.ID)
	require.Len(t, got, 1)
	assert.Equal(t, storage.AccessDecisionDenied, got[0].Decision)
}

// An elevated read is answered with its entry committed: grant id,
// the steward_grant check, the object ids released.
func TestVisibilityElevatedReadIsRecorded(t *testing.T) {
	r := newStewardRig(t)
	rec := httptest.NewRecorder()
	r.serveElevated(rec, r.elevated(t), r.audit, "/objects", releases)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Supplier contract")
	assert.Zero(t, r.audit.Buffered(), "nothing waits in the buffer")
	got := r.entries(t, r.grant.ID)
	require.Len(t, got, 1, "committed before the answer was released")
	e := got[0]
	assert.Equal(t, storage.AccessDecisionAllowed, e.Decision)
	assert.Equal(t, []string{"obj-1"}, e.ObjectIDs)
	assert.Equal(t, 1, e.ObjectCount)
	assert.Equal(t, "GET /objects", e.Route)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomePass, Reason: sharing.StewardReasonGrantActive}, e.Checks[1])
	assert.Contains(t, e.PolicyVersions, "steward="+r.grant.ID)
}

// When the entry cannot be committed the answer is 503
// AUDIT_UNAVAILABLE and nothing of it leaves: no row, no value, no
// header the handler set. The headers the request came in with stay.
// The deferral is recorded once the store takes it.
func TestVisibilityElevatedAuditFailureDisclosesNothing(t *testing.T) {
	r := newStewardRig(t)
	fails := 1
	audit := sharing.NewAuditWriter(downAccessLog{AccessLogStore: r.drv.Sharing().AccessLog(), fails: &fails}, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = audit.Close(context.Background()) })
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Dpkms-Upgrade", "none")
	r.serveElevated(rec, r.elevated(t), audit, "/objects", releases)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, sharing.CodeAuditUnavailable, errorCode(t, rec.Body.Bytes()))
	for _, leaked := range []string{"obj-1", "Supplier contract", `"total"`} {
		assert.NotContains(t, rec.Body.String(), leaked)
	}
	assert.Empty(t, rec.Header().Get("X-Total-Count"), "a header of the withheld answer leaked")
	assert.NotEqual(t, "57", rec.Header().Get("Content-Length"))
	assert.Equal(t, "none", rec.Header().Get("X-Dpkms-Upgrade"), "the headers set ahead of the route stay")

	got := r.entries(t, r.grant.ID)
	require.Len(t, got, 1, "the deferral, once the store took it")
	assert.Equal(t, storage.AccessDecisionDeferred, got[0].Decision)
	last := got[0].Checks[len(got[0].Checks)-1]
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckAudit, Outcome: storage.AccessOutcomeUnknown, Reason: sharing.ReasonAuditUnavailable}, last)

	// A store that stays down: still 503, still nothing.
	down := sharing.NewAuditWriter(downAccessLog{AccessLogStore: r.drv.Sharing().AccessLog()}, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = down.Close(context.Background()) })
	rec = httptest.NewRecorder()
	r.serveElevated(rec, r.elevated(t), down, "/objects", releases)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "obj-1")
}

// orderWriter records whether anything reached the client.
type orderWriter struct {
	*httptest.ResponseRecorder
	wrote atomic.Bool
}

func (o *orderWriter) WriteHeader(code int) {
	o.wrote.Store(true)
	o.ResponseRecorder.WriteHeader(code)
}

func (o *orderWriter) Write(b []byte) (int, error) {
	o.wrote.Store(true)
	return o.ResponseRecorder.Write(b)
}

// watchedAccessLog notes, at each steward append, whether the answer
// had already reached the client.
type watchedAccessLog struct {
	storage.AccessLogStore
	w           *orderWriter
	afterAnswer atomic.Bool
	appends     atomic.Int32
}

func (l *watchedAccessLog) AppendSteward(ctx context.Context, e *storage.AccessEntry) error {
	l.appends.Add(1)
	if l.w.wrote.Load() {
		l.afterAnswer.Store(true)
	}
	return l.AccessLogStore.AppendSteward(ctx, e)
}

// No elevated answer is released before its entry is committed: when
// the store takes the entry, nothing has reached the client yet; a
// refusal (here the backstop's) waits for its entry the same way.
func TestVisibilityElevatedCommitsBeforeRelease(t *testing.T) {
	r := newStewardRig(t)
	for name, handler := range map[string]http.HandlerFunc{
		"a read": releases,
		"a refusal": func(w http.ResponseWriter, _ *http.Request) {
			WriteJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"id": "never-released"}}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := &orderWriter{ResponseRecorder: httptest.NewRecorder()}
			log := &watchedAccessLog{AccessLogStore: r.drv.Sharing().AccessLog(), w: w}
			audit := sharing.NewAuditWriter(log, sharing.AuditOptions{Interval: time.Hour})
			t.Cleanup(func() { _ = audit.Close(context.Background()) })
			r.serveElevated(w, r.elevated(t), audit, "/objects", handler)
			require.Equal(t, int32(1), log.appends.Load())
			assert.False(t, log.afterAnswer.Load(), "the answer reached the client before its entry was committed")
			assert.True(t, w.wrote.Load(), "the answer was released after the commit")
		})
	}
}

// K concurrent elevated requests leave K entries on one linear chain:
// none dropped, none forked.
func TestVisibilityElevatedConcurrentRequests(t *testing.T) {
	r := newStewardRig(t)
	v := r.elevated(t)
	const k = 24
	var wg sync.WaitGroup
	codes := make(chan int, k)
	for range k {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			r.serveElevated(rec, v, r.audit, "/objects", releases)
			codes <- rec.Code
		}()
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		assert.Equal(t, http.StatusOK, c)
	}
	assert.Len(t, r.entries(t, r.grant.ID), k)
	head, chain, err := r.drv.Sharing().AccessLog().StewardChain(context.Background())
	require.NoError(t, err)
	require.NoError(t, storage.VerifyAccessChain(head, chain, time.Time{}))
	assert.Equal(t, int64(k), head.Seq)
}

// An ordinary restricted read keeps the buffered path: its entry waits
// in the buffer, and nothing goes through the steward append.
func TestVisibilityOrdinaryReadStaysBuffered(t *testing.T) {
	r := newStewardRig(t)
	log := &watchedAccessLog{AccessLogStore: r.drv.Sharing().AccessLog(), w: &orderWriter{ResponseRecorder: httptest.NewRecorder()}}
	audit := sharing.NewAuditWriter(log, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = audit.Close(context.Background()) })
	h := Visibility(http.MethodGet, "/objects", authn.ScopeReadObjects, VisibilityConfig{Audit: audit})(http.HandlerFunc(releases))
	ordinary, err := r.viewers.Resolve(context.Background(), r.steward, storage.PrincipalKindPerson, "")
	require.NoError(t, err)
	ctx := sharing.WithViewer(authn.Attach(context.Background(), r.principal(nil)), ordinary)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/objects", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, audit.Buffered())
	assert.Zero(t, log.appends.Load())
}

// The access log listing (admin:audit) shows steward entries with their
// grant, route and counts to every holder, never the grant's reason; the
// object ids of a steward entry go to the owner and approve:steward
// holders only, and ?object= does not find a steward entry for anyone
// else.
func TestShareAuditStewardObjectsForApproversOnly(t *testing.T) {
	r := newStewardRig(t)
	rec := httptest.NewRecorder()
	r.serveElevated(rec, r.elevated(t), r.audit, "/objects", releases)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, r.drv.Sharing().AccessLog().Append(context.Background(), []*storage.AccessEntry{{
		PrincipalID: "prn_colleague", Route: "GET /objects", Decision: storage.AccessDecisionAllowed,
		OperationDigest: storage.DigestPrefix + "00", ObjectCount: 1, ObjectIDs: []string{"obj-1"},
	}}))
	routes := newShareRoutes(RouterConfig{Sharing: r.drv.Sharing(), Registry: r.drv.Auth()})
	list := func(p *authn.Principal, query string) []storage.AccessEntry {
		t.Helper()
		ctx := context.Background()
		if p != nil {
			ctx = authn.Attach(ctx, p)
		}
		rec := httptest.NewRecorder()
		routes.ListShareAudit(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/share/audit"+query, nil))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), "a long enough reason", "the grant's reason never shows")
		var out struct {
			Data []storage.AccessEntry `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out.Data
	}
	steward := func(es []storage.AccessEntry) *storage.AccessEntry {
		for i := range es {
			if es[i].StewardGrantID != "" {
				return &es[i]
			}
		}
		return nil
	}
	auditor := &authn.Principal{ID: "prn_auditor", Kind: storage.PrincipalKindPerson, Scopes: []authn.Scope{authn.ScopeAdminAudit}}
	approver := &authn.Principal{ID: "prn_approver", Kind: storage.PrincipalKindPerson, Scopes: []authn.Scope{authn.ScopeAdminAudit, authn.ScopeApproveSteward}}
	owner := &authn.Principal{ID: storage.OwnerPrincipal, Kind: storage.PrincipalKindOwner, Scopes: authn.AllScopes}

	all := list(auditor, "")
	s := steward(all)
	require.NotNil(t, s, "admin:audit still lists the steward entry")
	assert.Equal(t, r.grant.ID, s.StewardGrantID)
	assert.Equal(t, "GET /objects", s.Route)
	assert.Equal(t, 1, s.ObjectCount)
	assert.Empty(t, s.ObjectIDs, "admin:audit without approve:steward sees no steward object ids")
	byObject := list(auditor, "?object=obj-1")
	assert.Nil(t, steward(byObject), "?object= must not find a steward entry for admin:audit alone")
	assert.Len(t, byObject, 1, "the ordinary entry is still found")

	for name, p := range map[string]*authn.Principal{"approver": approver, "owner": owner, "private instance": nil} {
		s := steward(list(p, "?object=obj-1"))
		if assert.NotNil(t, s, name) {
			assert.Equal(t, []string{"obj-1"}, s.ObjectIDs, name)
		}
	}
	assert.False(t, strings.Contains(rec.Body.String(), r.grant.ID), "the read itself does not echo the grant")
}
