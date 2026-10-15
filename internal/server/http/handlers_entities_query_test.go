package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/auth/authtest"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// seedQueryEntities stores the entities the search and resolve tests
// read: two match "checkout" by title, one carries aliases.
func seedQueryEntities(t *testing.T, es storage.EntityStore) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	for _, e := range []*storage.Entity{
		{Slug: "ui.checkout-flow", Title: "Checkout Flow", Namespace: "ui"},
		{Slug: "ui.cart", Title: "Shopping Cart", Namespace: "ui", Aliases: []string{"basket"}},
		{Slug: "zz.late", Title: "Late CHECKOUT page", Namespace: "zz"},
		{Slug: "resolve", Title: "An entity named like the route", Namespace: "zz"},
	} {
		// Registry-served, so a granted non-owner sees them without a
		// mention (ADR-080 §6).
		e.RegistryURL = "https://registry.test"
		e.CreatedAt, e.UpdatedAt = now, now
		require.NoError(t, es.Upsert(ownerCtx(), e))
	}
}

func entitySlugs(t *testing.T, body []byte) []string {
	t.Helper()
	var out struct {
		Data []storage.Entity `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &out), string(body))
	slugs := make([]string, 0, len(out.Data))
	for _, e := range out.Data {
		slugs = append(slugs, e.Slug)
	}
	return slugs
}

func TestSearchEntities(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	seedQueryEntities(t, ts.svc.Store(ownerCtx()).Entities())

	cases := []struct {
		query string
		want  []string
	}{
		{"q=checkout", []string{"ui.checkout-flow", "zz.late"}},
		{"q=BASKET", []string{"ui.cart"}},
		{"q=checkout&limit=1", []string{"ui.checkout-flow"}},
		{"q=checkout&namespace=zz", []string{"zz.late"}},
		{"q=no-such-thing", []string{}},
	}
	for _, tc := range cases {
		resp, body := roleDo(t, http.MethodGet, ts.URL+"/api/v1/entities?"+tc.query, "", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", tc.query, body)
		assert.Equal(t, tc.want, entitySlugs(t, body), tc.query)
	}
}

func TestResolveEntity(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	seedQueryEntities(t, ts.svc.Store(ownerCtx()).Entities())

	for mention, want := range map[string]string{
		"ui.cart": "ui.cart", // slug
		"basket":  "ui.cart", // alias
		"resolve": "resolve", // the slug the static route shadows
	} {
		resp, body := roleDo(t, http.MethodGet,
			ts.URL+"/api/v1/entities/resolve?mention="+url.QueryEscape(mention), "", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", mention, body)
		var e storage.Entity
		require.NoError(t, json.Unmarshal(body, &e))
		assert.Equal(t, want, e.Slug, mention)
	}

	resp, body := roleDo(t, http.MethodGet, ts.URL+"/api/v1/entities/resolve?mention=nope", "", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, string(body))
	assert.Equal(t, "NOT_FOUND", errorCode(t, body))

	for _, q := range []string{"", "?mention="} {
		resp, body = roleDo(t, http.MethodGet, ts.URL+"/api/v1/entities/resolve"+q, "", nil)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "%q: %s", q, body)
		assert.Equal(t, "INVALID_PARAM", errorCode(t, body), q)
	}
}

// A reader reaches search and resolve (read:objects); no token is 401;
// a principal without read:objects gets 403 naming it.
func TestEntityQueryRoutesRequireReadObjects(t *testing.T) {
	svc := newScopeTestService(t)
	seedQueryEntities(t, svc.Store(ownerCtx()).Entities())
	// The reader is a non-owner principal: it sees entities only
	// through a share grant.
	sharingtest.EnsurePrincipal(t, svc.Store(ownerCtx()), "tablet", storage.PrincipalKindPerson, authn.RoleReader)
	sharingtest.GrantEverything(t, svc.Store(ownerCtx()), "tablet")
	provider := authtest.TokenChain(
		authtest.Token{Token: "tok-reader-fixture-0123456789abcdef0123456789", Principal: "tablet", Roles: []string{authn.RoleReader}},
	)
	ts := newScopedServer(t, svc, provider)

	for _, path := range []string{"/api/v1/entities?q=cart", "/api/v1/entities/resolve?mention=basket"} {
		resp, body := roleDo(t, http.MethodGet, ts+path, "tok-reader-fixture-0123456789abcdef0123456789", nil)
		assert.Equal(t, http.StatusOK, resp.StatusCode, "reader %s: %s", path, body)

		resp, body = roleDo(t, http.MethodGet, ts+path, "", nil)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "no token %s: %s", path, body)
	}

	scopeless := newScopedServer(t, svc, authtest.Fixed(&authn.Principal{
		ID: "almost", Scopes: []authn.Scope{authn.ScopeReadInbox, authn.ScopeWriteObjects},
	}))
	for _, path := range []string{"/api/v1/entities?q=cart", "/api/v1/entities/resolve?mention=basket"} {
		resp, body := roleDo(t, http.MethodGet, scopeless+path, "any", nil)
		assertScopeDenied(t, resp, body, authn.ScopeReadObjects)
	}
}

// Search filters by entitlement like the listing; resolve is gated and
// metered like GET /entities/{slug}.
func TestEntityQueryRoutesGateNonAdmin(t *testing.T) {
	ts, _, _, driver := newGatedServerWithDriver(t, storageutil.NewTestDriver(t))

	resp, body := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities?q=.")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.Equal(t, []string{"ai.bert"}, entitySlugs(t, body), "search must drop ungranted namespaces")

	resp, body = gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/resolve?mention=med.claims")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))
	assert.Equal(t, "ENTITLEMENT_REQUIRED", errorCode(t, body))

	resp, body = gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/resolve?mention=ai.bert")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	events, err := driver.Metering().List(ownerCtx(), storage.MeteringFilter{RegistryName: "ops"})
	require.NoError(t, err)
	require.Len(t, events, 1, "a resolve is a metered entity read")
	assert.Equal(t, storage.MeteringEventEntityResolve, events[0].EventType)
}

// newScopedServer serves svc behind chain and returns its base URL.
func newScopedServer(t *testing.T, svc *service.Service, chain *authn.Chain) string {
	t.Helper()
	ts := httptest.NewServer(NewRouterWithConfig(svc, RouterConfig{Auth: chain}))
	t.Cleanup(ts.Close)
	return ts.URL
}
