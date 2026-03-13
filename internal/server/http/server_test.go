package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

type testServerBundle struct {
	*httptest.Server
	svc *service.Service
}

func newTestServerBundle(t *testing.T) *testServerBundle {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipes, engine, "", nil)
	return &testServerBundle{
		Server: httptest.NewServer(NewRouter(svc)),
		svc:    svc,
	}
}

func TestHealthEndpoint(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("body: got %v", body)
	}
}

func TestNotFound(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}

	var body ErrorEnvelope
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Error.Code != "NOT_FOUND" {
		t.Errorf("code: got %q", body.Error.Code)
	}
}

func TestRequestID(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	reqID := resp.Header.Get("X-Request-ID")
	if reqID == "" {
		t.Error("X-Request-ID header is missing")
	}
}

func TestHealthEndpointUnhealthy(t *testing.T) {
	// Create a dedicated driver (not shared) so we can close it without
	// interfering with other tests. We skip to t.Cleanup auto-close by
	// building to bundle manually.
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, q, pipes, engine, "", nil)
	ts := httptest.NewServer(NewRouter(svc))
	defer ts.Close()

	// Close the storage driver so that Health() returns an error.
	err := svc.Store.Close(context.Background())
	require.NoError(t, err)

	resp, err := http.Get(ts.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var env ErrorEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	assert.Equal(t, "UNHEALTHY", env.Error.Code)
	assert.NotEmpty(t, env.Error.Message)
}
