package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// managerProbe is the HealthzProbes.Upgrade dpkms serve wires: the
// manager's snapshot, nil when idle.
func managerProbe(m *upgrade.Manager) func(context.Context) *UpgradeSnapshot {
	return func(context.Context) *UpgradeSnapshot { return NewUpgradeSnapshot(m.Snapshot()) }
}

func serveUpgradeHeader(t *testing.T, probe func(context.Context) *UpgradeSnapshot) http.Header {
	t.Helper()
	h := UpgradeHeader(probe)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil))
	return rec.Header()
}

// An active or failed upgrade puts its summary on the response; idle, or
// no probe at all, leaves the header off.
func TestUpgradeHeaderOnlyWhileNotIdle(t *testing.T) {
	m := upgrade.NewManager("")
	_, set := serveUpgradeHeader(t, managerProbe(m))[upgrade.HeaderName]
	assert.False(t, set, "idle manager must not set %s", upgrade.HeaderName)

	require.NoError(t, m.Start(upgrade.BucketReingestSelective, 120))
	require.NoError(t, m.Tick(47))
	got := serveUpgradeHeader(t, managerProbe(m)).Get(upgrade.HeaderName)
	st, err := upgrade.DecodeHeader(got)
	require.NoError(t, err, "header %q", got)
	assert.Equal(t, upgrade.StateInProgress, st.State)
	assert.Equal(t, upgrade.BucketReingestSelective, st.Bucket)
	assert.Equal(t, 47, st.Done)
	assert.Equal(t, 120, st.Total)

	require.NoError(t, m.Fail(errors.New("disk full")))
	st, err = upgrade.DecodeHeader(serveUpgradeHeader(t, managerProbe(m)).Get(upgrade.HeaderName))
	require.NoError(t, err)
	assert.Equal(t, upgrade.StateFailed, st.State)
	assert.Equal(t, "disk full", st.LastError)

	require.NoError(t, m.Start(upgrade.BucketReindexAuto, 1))
	require.NoError(t, m.Complete())
	_, set = serveUpgradeHeader(t, managerProbe(m))[upgrade.HeaderName]
	assert.False(t, set, "completed run must clear %s", upgrade.HeaderName)

	_, set = serveUpgradeHeader(t, nil)[upgrade.HeaderName]
	assert.False(t, set, "no probe, no header")
}

// On the router, the header rides every /api/v1 response to an
// authenticated caller, like the verbose /healthz; a rejected credential
// learns nothing about the upgrade.
func TestRouterUpgradeHeaderBehindAuth(t *testing.T) {
	m := upgrade.NewManager("")
	require.NoError(t, m.Start(upgrade.BucketReindexAuto, 10))
	r := NewRouterWithConfig(newScopeTestService(t), RouterConfig{
		Auth:   testProvider(t),
		Probes: HealthzProbes{Upgrade: managerProbe(m)},
	})

	call := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/objects/obj_missing", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	ok := call("tok-valid")
	assert.Equal(t, http.StatusNotFound, ok.Code)
	assert.NotEmpty(t, ok.Header().Get(upgrade.HeaderName), "authenticated API response must carry the header")

	for _, tok := range []string{"", "tok-wrong"} {
		denied := call(tok)
		assert.Equal(t, http.StatusUnauthorized, denied.Code)
		assert.Empty(t, denied.Header().Get(upgrade.HeaderName), "401 must not carry the header")
	}

	health := httptest.NewRecorder()
	r.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Empty(t, health.Header().Get(upgrade.HeaderName), "only /api/v1 responses carry the header")
}
