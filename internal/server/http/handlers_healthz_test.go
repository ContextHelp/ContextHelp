package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestHealthzReturns200WhenHealthy verifies the happy path returns 200
// and a fully-populated envelope.
func TestHealthzReturns200WhenHealthy(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var env HealthzEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))

	assert.Equal(t, HealthHealthy, env.Health)
	assert.Equal(t, "ok", env.Checks.Process)
	assert.Equal(t, "ok", env.Checks.RESTAPI)
	assert.Equal(t, "ok", env.Checks.DB.Status)
	assert.NotNil(t, env.Checks.Watchers, "watchers must be non-nil array, not null")
}

// TestHealthzEnvelopeSchemaIsStable asserts every documented top-level
// key is present so T-0580 (which extends the envelope) can rely on a
// fixed base shape.
func TestHealthzEnvelopeSchemaIsStable(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	var raw map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))

	for _, key := range []string{"health", "version", "uptime_seconds", "checks"} {
		_, ok := raw[key]
		assert.True(t, ok, "top-level key %q is missing from envelope", key)
	}

	var checks map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["checks"], &checks))
	for _, key := range []string{"process", "rest_api", "grpc_api", "db", "queue", "watchers"} {
		_, ok := checks[key]
		assert.True(t, ok, "checks.%s is missing", key)
	}
}

// TestHealthzReturns503WhenDBClosed verifies failed DB flips the
// envelope to "failed" and the HTTP status to 503.
func TestHealthzReturns503WhenDBClosed(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipes, engine, "", nil)
	ts := httptest.NewServer(NewRouter(svc, false, nil))
	defer ts.Close()

	require.NoError(t, svc.Store.Close(context.Background()))

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var env HealthzEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))

	assert.Equal(t, HealthFailed, env.Health)
	assert.Equal(t, "failed", env.Checks.DB.Status)
}

// TestHealthzDegradedWhenQueueHasFailures asserts the verdict downgrades
// to "degraded" (not "failed") when only the failed-jobs counter is
// non-zero. Status must remain 200 because the daemon is still serving.
func TestHealthzDegradedWhenQueueHasFailures(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	require.NoError(t, ts.svc.Store.Jobs().Create(context.Background(), &storage.Job{
		ID: "job_failed_1", Type: "ingest:text", Status: storage.JobFailed,
		Payload: "x", Pipeline: "text.short", Source: "test",
		MaxRetries: 3, CreatedAt: now, UpdatedAt: now,
	}))

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var env HealthzEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))

	assert.Equal(t, HealthDegraded, env.Health)
	assert.GreaterOrEqual(t, env.Checks.Queue.Failed, 1)
}

// TestHealthzUpgradeProbeFlipsHealthAndAttachesEnvelope verifies T-0580's
// extension: a probe that reports an in-progress upgrade flips the
// top-level Health to "upgrading" AND attaches the upgrade envelope to
// the response.
func TestHealthzUpgradeProbeFlipsHealthAndAttachesEnvelope(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipes, engine, "", nil)

	probes := HealthzProbes{
		Started: time.Now().Add(-10 * time.Second),
		Upgrade: func(_ context.Context) *UpgradeSnapshot {
			return &UpgradeSnapshot{
				State:      "in_progress",
				Bucket:     "reingest_selective",
				Done:       47,
				Total:      120,
				Progress:   0.391,
				EtaSeconds: 32,
				StartedAt:  "2026-05-07T13:42:00Z",
			}
		},
	}

	ts := httptest.NewServer(NewRouterWithProbes(svc, false, nil, probes))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var env HealthzEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	assert.Equal(t, HealthUpgrading, env.Health,
		"in-progress upgrade must flip top-level health to upgrading")
	require.NotNil(t, env.Upgrade, "upgrade envelope must be attached")
	assert.Equal(t, "in_progress", env.Upgrade.State)
	assert.Equal(t, "reingest_selective", env.Upgrade.Bucket)
	assert.Equal(t, 47, env.Upgrade.Done)
	assert.Equal(t, 120, env.Upgrade.Total)
	assert.Equal(t, 32, env.Upgrade.EtaSeconds)
}

// TestHealthzNoUpgradeProbeOmitsField confirms that without an Upgrade
// probe the envelope serialises without the upgrade key (omitempty).
// This protects existing /healthz consumers that parse strictly.
func TestHealthzNoUpgradeProbeOmitsField(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	var raw map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
	_, hasUpgrade := raw["upgrade"]
	assert.False(t, hasUpgrade, "upgrade key must be absent when no probe is wired")
}

// TestHealthzWithProbesPopulatesVersionAndGRPC verifies the
// NewRouterWithProbes constructor injects probe-supplied data.
func TestHealthzWithProbesPopulatesVersionAndGRPC(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipes, engine, "", nil)

	probes := HealthzProbes{
		Version: "v1.2.3-test",
		Started: time.Now().Add(-30 * time.Second),
		GRPC:    func(_ context.Context) bool { return true },
		Watchers: func(_ context.Context) []WatcherCheck {
			return []WatcherCheck{{Name: "fs.notes", Subscriptions: 2}}
		},
	}

	ts := httptest.NewServer(NewRouterWithProbes(svc, false, nil, probes))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	var env HealthzEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))

	assert.Equal(t, "v1.2.3-test", env.Version)
	assert.Equal(t, "ok", env.Checks.GRPCAPI)
	assert.GreaterOrEqual(t, env.UptimeSeconds, int64(20))
	require.Len(t, env.Checks.Watchers, 1)
	assert.Equal(t, "fs.notes", env.Checks.Watchers[0].Name)
}
