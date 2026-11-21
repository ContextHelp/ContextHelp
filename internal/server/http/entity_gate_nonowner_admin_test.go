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

// The non-owner administrators of the gated fixture: a device and a
// person holding the admin role, each granted every object and "ai.*".
const (
	adminDeviceID = "prn_gate_admin_device"
	adminPersonID = "prn_gate_admin_person"
)

// adminTokens maps each non-owner administrator to its bearer token.
var adminTokens = map[string]string{
	adminDeviceID: "tok-admin-device-fixture-0123456789abcdef0123",
	adminPersonID: "tok-admin-person-fixture-0123456789abcdef0123",
}

func TestEntityGateHoldsNonOwnerAdmins(t *testing.T) {
	runNonOwnerAdminEntityGate(t, storageutil.NewTestDriver(t))
}

// runNonOwnerAdminEntityGate drives every entity surface as each
// non-owner administrator over driver. Administering an instance is not
// access to its data (ADR-082 §10, ADR-083 §10): the admin role is gated like any
// other consumer, so the entities outside its ai.* grant are refused or
// left out, and the reads inside it are metered and held to its quota.
// Shared with the Postgres parity test.
func runNonOwnerAdminEntityGate(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	ts, gate, _, driver := newGatedServerWithDriver(t, driver)
	for _, id := range []string{adminDeviceID, adminPersonID} {
		token := adminTokens[id]
		gate.SetQuota(id, storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 1})
		gate.SetQuota(id, storage.MeteringEventContentPull, storage.QuotaConfig{Limit: 1})
		do := func(method, path string) (*http.Response, []byte) {
			return gatedDoAs(t, token, method, ts.URL+path)
		}

		// Outside the grant: every read of the entity is refused.
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/entities/med.claims"},
			{http.MethodGet, "/api/v1/entities/resolve?mention=med.claims"},
			{http.MethodGet, "/api/v1/entities/med.claims/backlinks"},
			{http.MethodPost, "/api/v1/entities/med.claims/pull"},
		} {
			resp, body := do(c.method, c.path)
			require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s %s: %s", id, c.method, c.path, body)
			assert.Equal(t, "ENTITLEMENT_REQUIRED", errorCode(t, body), "%s %s %s", id, c.method, c.path)
		}

		// Listings, search and counts leave it out.
		resp, body := do(http.MethodGet, "/api/v1/entities")
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", id, body)
		assert.Equal(t, []string{"ai.bert"}, entitySlugs(t, body), "%s listing", id)
		resp, body = do(http.MethodGet, "/api/v1/entities?q=.")
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", id, body)
		assert.Equal(t, []string{"ai.bert"}, entitySlugs(t, body), "%s search", id)
		resp, body = do(http.MethodGet, "/api/v1/stats")
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", id, body)
		var st struct {
			Entities int `json:"entities"`
		}
		require.NoError(t, json.Unmarshal(body, &st))
		assert.Equal(t, 1, st.Entities, "%s counts only the entity its grant covers", id)

		// Inside the grant: metered, and refused once the quota of one
		// is spent.
		resp, body = do(http.MethodGet, "/api/v1/entities/ai.bert")
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", id, body)
		resp, body = do(http.MethodGet, "/api/v1/entities/ai.bert")
		require.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "%s: %s", id, body)
		assert.Equal(t, "QUOTA_EXHAUSTED", errorCode(t, body))
		resp, body = do(http.MethodPost, "/api/v1/entities/ai.bert/pull")
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", id, body)
		resp, body = do(http.MethodPost, "/api/v1/entities/ai.bert/pull")
		require.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "%s: %s", id, body)

		events, err := driver.Metering().List(ownerCtx(), storage.MeteringFilter{RegistryName: id})
		require.NoError(t, err)
		got := map[storage.MeteringEventType]int{}
		for _, e := range events {
			assert.Equal(t, "ai.models", e.Namespace)
			got[e.EventType] += e.Count
		}
		assert.Equal(t, map[storage.MeteringEventType]int{
			storage.MeteringEventEntityResolve: 1,
			storage.MeteringEventContentPull:   1,
		}, got, "%s reads are metered", id)
	}

	// The owner, under the same ai.* grant, still sees both.
	resp, body := gatedDoAs(t, "tok-admin-fixture-0123456789abcdef0123456789", http.MethodGet, ts.URL+"/api/v1/stats")
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var st struct {
		Entities int `json:"entities"`
	}
	require.NoError(t, json.Unmarshal(body, &st))
	assert.Equal(t, 2, st.Entities, "the owner counts every entity")
}
