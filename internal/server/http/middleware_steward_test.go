package http

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/auth/authtest"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// stewardRig is a store with a steward holding an ordinary grant and
// a steward grant, a resolver wired for steward grants, the read audit
// over the store and a security emitter whose steward refusals the
// test can wait on.
type stewardRig struct {
	drv      storage.StorageDriver
	steward  string
	grant    *storage.StewardGrant
	settings *sharing.StewardSettings
	viewers  *sharing.Resolver
	audit    *sharing.AuditWriter
	sec      *security.Emitter
	refused  chan security.Alert
	now      time.Time
}

func newStewardRig(t *testing.T) *stewardRig {
	t.Helper()
	t0 := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	r := &stewardRig{
		drv: storageutil.NewTestDriver(t), steward: "prn_steward", now: t0.Add(time.Hour),
		settings: &sharing.StewardSettings{Enabled: true, MaxDuration: 14 * 24 * time.Hour},
		refused:  make(chan security.Alert, 16),
	}
	sharingtest.EnsurePrincipal(t, r.drv, r.steward, storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	sharingtest.GrantEverything(t, r.drv, r.steward)
	r.grant = sharingtest.GrantSteward(t, r.drv, r.steward, `{"policies":[{"data":"type==contact"}]}`, t0, t0.Add(48*time.Hour))
	r.viewers = sharing.NewResolver(r.drv.Sharing()).WithRegistry(r.drv.Auth()).
		WithClock(func() time.Time { return r.now }).
		WithSteward(sharingtest.StewardOptions(func() sharing.StewardSettings { return *r.settings }))
	r.audit = sharing.NewAuditWriter(r.drv.Sharing().AccessLog(), sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = r.audit.Close(context.Background()) })
	r.sec = security.New(security.DefaultConfig(), nil)
	r.sec.AddHandler(func(_ context.Context, a security.Alert) {
		if a.Kind == security.EventStewardElevationRefused {
			r.refused <- a
		}
	})
	t.Cleanup(func() { _ = r.sec.Close(context.Background()) })
	return r
}

// principal is the steward as its authenticator hands it over, with
// meta in its Meta.
func (r *stewardRig) principal(meta map[string]string) *authn.Principal {
	p := &authn.Principal{
		ID: r.steward, Kind: storage.PrincipalKindPerson, Provider: authn.MethodAPIKey,
		Roles: []string{authn.RoleReader, authn.RoleStewardRequester}, Meta: meta,
	}
	p.Scopes = authn.ScopesForRoles(p.Roles)
	return p
}

// serve runs a request authenticated as the steward, with meta in its
// principal's Meta, through RequireAuth, and returns the answer and the
// viewer the handler saw.
func (r *stewardRig) serve(t *testing.T, meta map[string]string) (*httptest.ResponseRecorder, sharing.Viewer, bool) {
	t.Helper()
	probe, seen := viewerProbe(t)
	h := RequireAuth(authtest.Fixed(r.principal(meta)), r.sec, false, r.viewers, nil, r.audit)(probe)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	v, ok := seen()
	return rec, v, ok
}

// entries lists what the audit holds for grant in append order: by
// chain position, which two entries logged in one millisecond do not
// share.
func (r *stewardRig) entries(t *testing.T, grant string) []*storage.AccessEntry {
	t.Helper()
	got, err := r.drv.Sharing().AccessLog().Query(context.Background(), storage.AccessFilter{StewardGrantID: grant})
	require.NoError(t, err)
	for _, e := range got {
		require.NotNil(t, e.Chain, "steward entry %s is not chained", e.ID)
	}
	slices.SortFunc(got, func(a, b *storage.AccessEntry) int { return cmp.Compare(a.Chain.Seq, b.Chain.Seq) })
	return got
}

// waitRefused waits for the security event of a refused elevation.
func (r *stewardRig) waitRefused(t *testing.T) security.Alert {
	t.Helper()
	select {
	case a := <-r.refused:
		return a
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the steward refusal event")
		return security.Alert{}
	}
}

// A principal whose Meta names a steward grant gets the elevated
// viewer; without the Meta it gets its ordinary view, unchanged.
func TestRequireAuthResolvesAnElevatedViewer(t *testing.T) {
	r := newStewardRig(t)

	rec, v, ok := r.serve(t, map[string]string{authn.MetaIdentityID: ""})
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, ok)
	assert.Empty(t, v.StewardGrantID(), "no grant named: the ordinary view")
	assert.False(t, v.StrictBackstop())

	rec, v, ok = r.serve(t, map[string]string{authn.MetaStewardGrantID: r.grant.ID})
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.True(t, ok)
	assert.Equal(t, r.grant.ID, v.StewardGrantID())
	assert.True(t, v.StrictBackstop())
	assert.Equal(t, r.steward, v.PrincipalID())
}

// A grant that fails a check refuses the request with the check's code:
// the handler never runs, so nothing falls back to the ordinary view.
// Every refusal is a durable entry naming the grant the request named,
// with the steward_grant check failed on its code, and a security
// event.
func TestRequireAuthRefusesAFailedElevation(t *testing.T) {
	r := newStewardRig(t)
	named := map[string]string{authn.MetaStewardGrantID: r.grant.ID}

	require.NoError(t, r.drv.Sharing().StewardGrants().Revoke(context.Background(), r.grant.ID, storage.OwnerPrincipal, "closed", r.now))
	rec, _, ok := r.serve(t, named)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "STEWARD_GRANT_REVOKED", errorCode(t, rec.Body.Bytes()))
	assert.False(t, ok, "the handler must not run behind a refused elevation")
	got := r.entries(t, r.grant.ID)
	require.Len(t, got, 1, "the refusal is committed before it is answered")
	assert.Equal(t, storage.AccessDecisionDenied, got[0].Decision)
	assert.Equal(t, []storage.AccessCheck{
		{Check: "authn.api_key", Outcome: storage.AccessOutcomePass, Reason: sharing.ReasonAuthenticated},
		{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeFail, Reason: sharing.StewardReasonGrantRevoked},
	}, got[0].Checks)
	assert.Equal(t, r.steward, got[0].PrincipalID)
	assert.Empty(t, got[0].ObjectIDs)
	a := r.waitRefused(t)
	assert.Equal(t, r.steward, a.Principal)
	assert.Equal(t, map[string]string{
		"grant_id": r.grant.ID, "reason": sharing.StewardReasonGrantRevoked, "deferred": "false",
	}, a.Meta)

	rec, _, ok = r.serve(t, map[string]string{authn.MetaStewardGrantID: "stg_nothing"})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "STEWARD_GRANT_UNKNOWN", errorCode(t, rec.Body.Bytes()))
	assert.False(t, ok)
	unknown := r.entries(t, "stg_nothing")
	require.Len(t, unknown, 1)
	assert.Equal(t, sharing.StewardReasonGrantUnknown, unknown[0].Checks[1].Reason)
	assert.Equal(t, sharing.StewardReasonGrantUnknown, r.waitRefused(t).Meta["reason"])

	r.settings.Enabled = false
	rec, _, ok = r.serve(t, named)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "STEWARD_STEWARD_DISABLED", errorCode(t, rec.Body.Bytes()))
	assert.False(t, ok)
	assert.NotContains(t, rec.Body.String(), r.grant.ID, "a refusal does not echo the grant")
	assert.Len(t, r.entries(t, r.grant.ID), 2)

	head, chain, err := r.drv.Sharing().AccessLog().StewardChain(context.Background())
	require.NoError(t, err)
	require.NoError(t, storage.VerifyAccessChain(head, chain, time.Time{}))
	assert.Len(t, chain, 3)
}

// A store that cannot be read refuses with 503: the grant is not
// served and the cause is not shown. The entry is deferred.
func TestRequireAuthDefersWhenTheStoreIsUnavailable(t *testing.T) {
	r := newStewardRig(t)
	boom := errors.New("dial tcp 10.0.0.7:5432: connection refused")
	r.viewers = sharing.NewResolver(failingStewardStore{SharingStore: r.drv.Sharing(), err: boom}).WithRegistry(r.drv.Auth()).
		WithClock(func() time.Time { return r.now }).
		WithSteward(sharingtest.StewardOptions(func() sharing.StewardSettings { return *r.settings }))
	rec, _, ok := r.serve(t, map[string]string{authn.MetaStewardGrantID: r.grant.ID})
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "STEWARD_GRANT_UNAVAILABLE", errorCode(t, rec.Body.Bytes()))
	assert.False(t, ok)
	assert.NotContains(t, rec.Body.String(), "10.0.0.7")
	got := r.entries(t, r.grant.ID)
	require.Len(t, got, 1)
	assert.Equal(t, storage.AccessDecisionDeferred, got[0].Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeUnknown, Reason: sharing.StewardReasonGrantUnavailable}, got[0].Checks[1])
	assert.Equal(t, "true", r.waitRefused(t).Meta["deferred"])
}

// A refusal whose entry cannot be committed is not answered with its
// code either: 503 AUDIT_UNAVAILABLE, as every elevated request whose
// record failed.
func TestRequireAuthRefusalWithoutItsEntry(t *testing.T) {
	r := newStewardRig(t)
	require.NoError(t, r.drv.Sharing().StewardGrants().Revoke(context.Background(), r.grant.ID, storage.OwnerPrincipal, "closed", r.now))
	r.audit = sharing.NewAuditWriter(downAccessLog{AccessLogStore: r.drv.Sharing().AccessLog()}, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = r.audit.Close(context.Background()) })
	rec, _, ok := r.serve(t, map[string]string{authn.MetaStewardGrantID: r.grant.ID})
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, sharing.CodeAuditUnavailable, errorCode(t, rec.Body.Bytes()))
	assert.False(t, ok)
	assert.NotContains(t, rec.Body.String(), "REVOKED")
}

// Mounted as the server mounts it, ahead of the route table, a refused
// elevation is recorded under the route it asked for and the operation
// digest its granted twin records, never under its raw path.
func TestRequireAuthRefusalNamesTheRoute(t *testing.T) {
	r := newStewardRig(t)
	named := map[string]string{authn.MetaStewardGrantID: r.grant.ID}
	route := apiRoute{Method: http.MethodGet, Pattern: "/objects/{id}", Scope: authn.ScopeReadObjects, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": CodeNotFound}})
	})}
	chain := authtest.Fixed(r.principal(named))
	root := chi.NewRouter()
	root.Route(apiPrefix, func(sub chi.Router) {
		sub.Use(RequireAuth(chain, r.sec, false, r.viewers, nil, r.audit))
		mountAPIRoutes(sub, []apiRoute{route}, RouterConfig{Auth: chain, Audit: r.audit, StrictSharing: true}, r.viewers)
	})
	call := func() int {
		req := httptest.NewRequest(http.MethodGet, apiPrefix+"/objects/obj_secret_id?fields=title", nil)
		req.Header.Set("Authorization", "Bearer anything")
		rec := httptest.NewRecorder()
		root.ServeHTTP(rec, req)
		return rec.Code
	}
	require.Equal(t, http.StatusNotFound, call(), "the grant is in force: the route answers")
	require.NoError(t, r.drv.Sharing().StewardGrants().Revoke(context.Background(), r.grant.ID, storage.OwnerPrincipal, "closed", r.now))
	require.Equal(t, http.StatusForbidden, call())

	got := r.entries(t, r.grant.ID)
	require.Len(t, got, 2)
	granted, refused := got[0], got[1]
	assert.Equal(t, sharing.StewardReasonGrantActive, granted.Checks[1].Reason)
	assert.Equal(t, sharing.StewardReasonGrantRevoked, refused.Checks[1].Reason)
	assert.Equal(t, "GET /objects/{id}", refused.Route)
	assert.Equal(t, granted.Route, refused.Route)
	assert.Equal(t, granted.OperationDigest, refused.OperationDigest, "one operation, one digest, whichever middleware records it")
	assert.Equal(t, sharing.StewardReasonGrantRevoked, r.waitRefused(t).Meta["reason"])
}

type failingStewardStore struct {
	storage.SharingStore
	err error
}

func (f failingStewardStore) StewardGrants() storage.StewardGrantStore {
	return failingStewardGrants{StewardGrantStore: f.SharingStore.StewardGrants(), err: f.err}
}

type failingStewardGrants struct {
	storage.StewardGrantStore
	err error
}

func (f failingStewardGrants) Get(context.Context, string) (*storage.StewardGrant, error) {
	return nil, f.err
}

// errAuditDown is what a down access log answers a steward append.
var errAuditDown = errors.New("access log: database is locked")

// downAccessLog is an access log whose steward appends fail (fails of
// them, or every one when fails is 0).
type downAccessLog struct {
	storage.AccessLogStore
	fails *int
}

func (d downAccessLog) AppendSteward(ctx context.Context, e *storage.AccessEntry) error {
	if d.fails == nil {
		return errAuditDown
	}
	if *d.fails > 0 {
		*d.fails--
		return errAuditDown
	}
	return d.AccessLogStore.AppendSteward(ctx, e)
}

// A steward-elevated request is refused at the event stream by the
// route table, before the handler runs: 403 NOT_SHARED, whatever the
// grant's scope says. The same steward without an elevation reaches
// the stream like any restricted viewer.
func TestElevatedRequestIsRefusedAtEvents(t *testing.T) {
	r := newStewardRig(t)
	reached := false
	events := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	})
	serve := func(meta map[string]string) *httptest.ResponseRecorder {
		reached = false
		p := &authn.Principal{
			ID: r.steward, Kind: storage.PrincipalKindPerson, Provider: authn.MethodAPIKey,
			Roles: []string{authn.RoleReader, authn.RoleStewardRequester}, Meta: meta,
		}
		p.Scopes = authn.ScopesForRoles(p.Roles)
		h := RequireAuth(authtest.Fixed(p), nil, false, r.viewers, nil, nil)(RequireCapability("GET", "/events")(events))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
		req.Header.Set("Authorization", "Bearer anything")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := serve(map[string]string{authn.MetaStewardGrantID: r.grant.ID})
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Equal(t, CodeNotShared, errorCode(t, rec.Body.Bytes()))
	assert.False(t, reached, "the stream handler must not run for an elevated request")

	rec = serve(map[string]string{authn.MetaIdentityID: ""})
	assert.Equal(t, http.StatusNoContent, rec.Code, "the ordinary view keeps its stream")
	assert.True(t, reached)
}

// The owner presenting a steward grant is refused not_steward: the
// owner is Unrestricted and never reads under a grant, so naming one
// buys nothing and the handler never runs.
func TestRequireAuthRefusesTheOwnerPresentingAGrant(t *testing.T) {
	r := newStewardRig(t)
	owner := &authn.Principal{
		ID: storage.OwnerPrincipal, Kind: storage.PrincipalKindOwner, Provider: authn.MethodAPIKey,
		Scopes: authn.AllScopes, Meta: map[string]string{authn.MetaStewardGrantID: r.grant.ID},
	}
	probe, seen := viewerProbe(t)
	h := RequireAuth(authtest.Fixed(owner), nil, false, r.viewers, nil, r.audit)(probe)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "STEWARD_NOT_STEWARD", errorCode(t, rec.Body.Bytes()))
	_, ran := seen()
	assert.False(t, ran, "the handler must not run")
}
