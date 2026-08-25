package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newGatedServer builds an authenticated router with the inbound
// entitlement gate and a threshold-1 security emitter, seeded with one
// entity in a granted namespace and one outside it. The test principal
// "ops" (token tok-valid) is granted "ai.*".
func newGatedServer(t *testing.T) (*httptest.Server, *registry.InboundGate, <-chan security.Alert) {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	svc := service.New(driver, q, builtins.Registry(), search.NewEngine(driver), "", nil)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for slug, ns := range map[string]string{
		"ai.bert":    "ai.models",
		"med.claims": "med.records",
	} {
		require.NoError(t, driver.Entities().Upsert(ctx, &storage.Entity{
			Slug: slug, Title: slug, Namespace: ns, CreatedAt: now, UpdatedAt: now,
		}))
	}
	require.NoError(t, driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
		RegistryName: "ops",
		Plan:         "inbound",
		Namespaces:   []string{"ai.*"},
		FetchedAt:    now,
	}))

	gate := registry.NewInboundGate(driver.Entitlements(), driver.Metering())
	em, alerts := newAlertCapture(t)
	ts := httptest.NewServer(NewRouterWithConfig(svc, RouterConfig{
		Auth:         testProvider(t),
		Security:     em,
		Entitlements: gate,
	}))
	t.Cleanup(ts.Close)
	return ts, gate, alerts
}

func gatedDo(t *testing.T, method, url string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok-valid")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()
	return resp, body
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var env ErrorEnvelope
	require.NoError(t, json.Unmarshal(body, &env))
	return env.Error.Code
}

func TestEntityGateAllowsGrantedNamespace(t *testing.T) {
	ts, _, _ := newGatedServer(t)

	resp, _ := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/ai.bert")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestEntityGateDeniesUngrantedNamespace(t *testing.T) {
	ts, _, alerts := newGatedServer(t)

	resp, body := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/med.claims")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "ENTITLEMENT_REQUIRED", errorCode(t, body))

	a := waitAlert(t, alerts)
	assert.Equal(t, security.EventACLDenial, a.Kind)
	assert.Equal(t, "ops", a.Principal)
}

func TestEntityGateFiltersListing(t *testing.T) {
	ts, _, _ := newGatedServer(t)

	resp, body := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Data []storage.Entity `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.Data, 1, "listing must exclude ungranted namespaces")
	assert.Equal(t, "ai.bert", out.Data[0].Slug)
}

func TestEntityGateQuotaExhausted(t *testing.T) {
	ts, gate, alerts := newGatedServer(t)
	gate.SetQuota("ops", storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 1})

	resp, _ := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/ai.bert")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, body := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/ai.bert")
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "QUOTA_EXHAUSTED", errorCode(t, body))

	a := waitAlert(t, alerts)
	assert.Equal(t, security.EventQuotaExhausted, a.Kind)
	assert.Equal(t, "ops", a.Principal)
}

func TestEntityGateBacklinksDenied(t *testing.T) {
	ts, _, _ := newGatedServer(t)

	resp, body := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/med.claims/backlinks")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "ENTITLEMENT_REQUIRED", errorCode(t, body))
}

// A slug the store cannot resolve must fail closed on the gated
// backlinks path: a lookup error never skips the gate, so the response
// is 404 — not an ungated backlink listing.
func TestEntityGateBacklinksUnknownEntityFailsClosed(t *testing.T) {
	ts, _, _ := newGatedServer(t)

	resp, body := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/ghost/backlinks")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "NOT_FOUND", errorCode(t, body))
}

// Same fail-closed rule on the gated pull path: an unresolvable entity
// is refused before any pull work runs, never authorized implicitly.
func TestEntityGatePullUnknownEntityFailsClosed(t *testing.T) {
	ts, _, _ := newGatedServer(t)

	resp, body := gatedDo(t, http.MethodPost, ts.URL+"/api/v1/entities/ghost/pull")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "NOT_FOUND", errorCode(t, body))
}

func TestEntityGatePullDenied(t *testing.T) {
	ts, _, _ := newGatedServer(t)

	resp, body := gatedDo(t, http.MethodPost, ts.URL+"/api/v1/entities/med.claims/pull")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "ENTITLEMENT_REQUIRED", errorCode(t, body))

	// Granted namespace passes the gate; the pull itself then fails on
	// the missing registry definition, never on entitlement.
	resp, _ = gatedDo(t, http.MethodPost, ts.URL+"/api/v1/entities/ai.bert/pull")
	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode)
	assert.NotEqual(t, http.StatusTooManyRequests, resp.StatusCode)
}
