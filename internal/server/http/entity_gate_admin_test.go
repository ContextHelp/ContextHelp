package http

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestAdminBypassesEntityGate(t *testing.T) {
	runAdminEntityGateBypass(t, storageutil.NewTestDriver(t))
}

func TestEntityGateMetersNonAdmin(t *testing.T) {
	runNonAdminEntityGateMetering(t, storageutil.NewTestDriver(t))
}

// runAdminEntityGateBypass drives the entity surface as the admin owner
// and as a gated writer over driver. The admin ignores its restrictive
// grant and a one-read quota and records no metering event; the writer
// stays gated and metered. Shared with the Postgres parity test.
func runAdminEntityGateBypass(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	ts, gate, _, driver := newGatedServerWithDriver(t, driver)
	gate.SetQuota("owner", storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 1})
	gate.SetQuota("owner", storage.MeteringEventContentPull, storage.QuotaConfig{Limit: 1})

	// Metered read outside the grant, twice past a quota of one.
	for i := 0; i < 2; i++ {
		resp, body := gatedDoAs(t, "tok-admin-fixture-0123456789abcdef0123456789", http.MethodGet, ts.URL+"/api/v1/entities/med.claims")
		require.Equal(t, http.StatusOK, resp.StatusCode, "admin read %d: %s", i+1, body)
	}

	// Listing is not filtered by the grant.
	resp, body := gatedDoAs(t, "tok-admin-fixture-0123456789abcdef0123456789", http.MethodGet, ts.URL+"/api/v1/entities")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var list struct {
		Data []storage.Entity `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	assert.Len(t, list.Data, 2, "admin listing must include every namespace")

	resp, body = gatedDoAs(t, "tok-admin-fixture-0123456789abcdef0123456789", http.MethodGet, ts.URL+"/api/v1/entities/med.claims/backlinks")
	assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	// Search is not filtered by the grant; resolve is neither gated nor
	// metered, past the same quota of one.
	resp, body = gatedDoAs(t, "tok-admin-fixture-0123456789abcdef0123456789", http.MethodGet, ts.URL+"/api/v1/entities?q=.")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.Equal(t, []string{"ai.bert", "med.claims"}, entitySlugs(t, body), "admin search must include every namespace")
	for i := 0; i < 2; i++ {
		resp, body = gatedDoAs(t, "tok-admin-fixture-0123456789abcdef0123456789", http.MethodGet, ts.URL+"/api/v1/entities/resolve?mention=med.claims")
		require.Equal(t, http.StatusOK, resp.StatusCode, "admin resolve %d: %s", i+1, body)
	}

	// The pull passes the gate; it then fails on the missing registry
	// definition, never on entitlement or quota.
	for i := 0; i < 2; i++ {
		resp, body = gatedDoAs(t, "tok-admin-fixture-0123456789abcdef0123456789", http.MethodPost, ts.URL+"/api/v1/entities/med.claims/pull")
		assert.NotEqual(t, http.StatusForbidden, resp.StatusCode, string(body))
		assert.NotEqual(t, http.StatusTooManyRequests, resp.StatusCode, string(body))
	}

	ownerEvents, err := driver.Metering().List(ownerCtx(), storage.MeteringFilter{RegistryName: "owner"})
	require.NoError(t, err)
	assert.Empty(t, ownerEvents, "admin entity reads must not be metered")

	// The gate stays for everyone else: metered inside the grant,
	// refused outside it.
	resp, body = gatedDoAs(t, "tok-valid-fixture-0123456789abcdef0123456789", http.MethodGet, ts.URL+"/api/v1/entities/ai.bert")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	opsEvents, err := driver.Metering().List(ownerCtx(), storage.MeteringFilter{RegistryName: "ops"})
	require.NoError(t, err)
	require.Len(t, opsEvents, 1, "non-admin read must be metered")
	assert.Equal(t, storage.MeteringEventEntityResolve, opsEvents[0].EventType)

	resp, body = gatedDoAs(t, "tok-valid-fixture-0123456789abcdef0123456789", http.MethodGet, ts.URL+"/api/v1/entities/med.claims")
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))
	assert.Equal(t, "ENTITLEMENT_REQUIRED", errorCode(t, body))
}

// runNonAdminEntityGateMetering drives the metered entity reads as a
// gated writer over driver: show and pull inside the grant are charged
// to the writer's quota (entity_resolve and content_pull) and refused
// with 429 once it is spent. The gate fails closed, so a driver whose
// metering store errors turns every one of these reads into a 500.
// Shared with the Postgres parity test.
func runNonAdminEntityGateMetering(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	ts, gate, _, driver := newGatedServerWithDriver(t, driver)
	gate.SetQuota("ops", storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 1})
	gate.SetQuota("ops", storage.MeteringEventContentPull, storage.QuotaConfig{Limit: 1})

	resp, body := gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/ai.bert")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	resp, body = gatedDo(t, http.MethodGet, ts.URL+"/api/v1/entities/ai.bert")
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode, string(body))
	assert.Equal(t, "QUOTA_EXHAUSTED", errorCode(t, body))

	// The pull passes the gate and is charged; the fixture entity is
	// already full, so it is served as stored.
	resp, body = gatedDo(t, http.MethodPost, ts.URL+"/api/v1/entities/ai.bert/pull")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	resp, body = gatedDo(t, http.MethodPost, ts.URL+"/api/v1/entities/ai.bert/pull")
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode, string(body))
	assert.Equal(t, "QUOTA_EXHAUSTED", errorCode(t, body))

	events, err := driver.Metering().List(ownerCtx(), storage.MeteringFilter{RegistryName: "ops"})
	require.NoError(t, err)
	got := map[storage.MeteringEventType]int{}
	for _, e := range events {
		assert.Equal(t, "ai.models", e.Namespace)
		got[e.EventType] += e.Count
	}
	assert.Equal(t, map[storage.MeteringEventType]int{
		storage.MeteringEventEntityResolve: 1,
		storage.MeteringEventContentPull:   1,
	}, got, "only the admitted reads are metered")
}
