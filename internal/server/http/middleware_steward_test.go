package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/auth/authtest"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// stewardRig is a store with a steward holding an ordinary grant and
// a steward grant, and a resolver wired for steward grants.
type stewardRig struct {
	drv      storage.StorageDriver
	steward  string
	grant    *storage.StewardGrant
	settings *sharing.StewardSettings
	viewers  *sharing.Resolver
	now      time.Time
}

func newStewardRig(t *testing.T) *stewardRig {
	t.Helper()
	t0 := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	r := &stewardRig{
		drv: storageutil.NewTestDriver(t), steward: "prn_steward", now: t0.Add(time.Hour),
		settings: &sharing.StewardSettings{Enabled: true, MaxDuration: 14 * 24 * time.Hour},
	}
	sharingtest.EnsurePrincipal(t, r.drv, r.steward, storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	sharingtest.GrantEverything(t, r.drv, r.steward)
	r.grant = sharingtest.GrantSteward(t, r.drv, r.steward, `{"policies":[{"data":"type==contact"}]}`, t0, t0.Add(48*time.Hour))
	r.viewers = sharing.NewResolver(r.drv.Sharing()).WithRegistry(r.drv.Auth()).
		WithClock(func() time.Time { return r.now }).
		WithSteward(sharingtest.StewardOptions(func() sharing.StewardSettings { return *r.settings }))
	return r
}

// serve runs a request authenticated as the steward, with meta in its
// principal's Meta, through RequireAuth, and returns the answer and the
// viewer the handler saw.
func (r *stewardRig) serve(t *testing.T, meta map[string]string) (*httptest.ResponseRecorder, sharing.Viewer, bool) {
	t.Helper()
	p := &authn.Principal{
		ID: r.steward, Kind: storage.PrincipalKindPerson, Provider: authn.MethodAPIKey,
		Roles: []string{authn.RoleReader, authn.RoleStewardRequester}, Meta: meta,
	}
	p.Scopes = authn.ScopesForRoles(p.Roles)
	probe, seen := viewerProbe(t)
	h := RequireAuth(authtest.Fixed(p), nil, false, r.viewers)(probe)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	v, ok := seen()
	return rec, v, ok
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
func TestRequireAuthRefusesAFailedElevation(t *testing.T) {
	r := newStewardRig(t)
	named := map[string]string{authn.MetaStewardGrantID: r.grant.ID}

	require.NoError(t, r.drv.Sharing().StewardGrants().Revoke(context.Background(), r.grant.ID, storage.OwnerPrincipal, "closed", r.now))
	rec, _, ok := r.serve(t, named)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "STEWARD_GRANT_REVOKED", errorCode(t, rec.Body.Bytes()))
	assert.False(t, ok, "the handler must not run behind a refused elevation")

	rec, _, ok = r.serve(t, map[string]string{authn.MetaStewardGrantID: "stg_nothing"})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "STEWARD_GRANT_UNKNOWN", errorCode(t, rec.Body.Bytes()))
	assert.False(t, ok)

	r.settings.Enabled = false
	rec, _, ok = r.serve(t, named)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "STEWARD_STEWARD_DISABLED", errorCode(t, rec.Body.Bytes()))
	assert.False(t, ok)
	assert.NotContains(t, rec.Body.String(), r.grant.ID, "a refusal does not echo the grant")
}

// A store that cannot be read refuses with 503: the grant is not
// served and the cause is not shown.
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

// An elevated viewer's backstop is strict even on an instance that
// strips: a hit is refused, not removed.
func TestVisibilityIsStrictForAnElevatedViewer(t *testing.T) {
	r := newStewardRig(t)
	elevated, err := r.viewers.Resolve(context.Background(), r.steward, storage.PrincipalKindPerson, "", sharing.AsSteward(r.grant.ID))
	require.NoError(t, err)
	ordinary := restrictedViewer(t)
	leak := func(w http.ResponseWriter, req *http.Request) {
		sharing.LedgerFrom(req.Context()).AddObject("a")
		WriteJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"id": "a"}, {"id": "hidden"}}, "total": 2})
	}
	rec := serveVisibility(t, ordinary, false, "/objects", leak)
	assert.Equal(t, http.StatusOK, rec.Code, "an ordinary viewer on a lenient instance has the hit stripped")
	rec = serveVisibility(t, elevated, false, "/objects", leak)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), CodeVisibilityBackstop)
	assert.NotContains(t, rec.Body.String(), "hidden")
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
		h := RequireAuth(authtest.Fixed(p), nil, false, r.viewers)(RequireCapability("GET", "/events")(events))
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
	h := RequireAuth(authtest.Fixed(owner), nil, false, r.viewers)(probe)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "STEWARD_NOT_STEWARD", errorCode(t, rec.Body.Bytes()))
	_, ran := seen()
	assert.False(t, ran, "the handler must not run")
}
