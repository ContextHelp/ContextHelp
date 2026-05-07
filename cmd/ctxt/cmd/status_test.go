package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHealthzServer returns an httptest server that responds to /healthz
// with the supplied status code + envelope JSON.
func fakeHealthzServer(t *testing.T, status int, env any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(env)
	})
	return httptest.NewServer(mux)
}

// withFormat saves and restores the global format viper setting around
// the test body. We deliberately avoid viper.Reset() because other tests
// in this package rely on a populated viper state.
func withFormat(t *testing.T, value string) {
	t.Helper()
	prev := viper.GetString("format")
	viper.Set("format", value)
	t.Cleanup(func() { viper.Set("format", prev) })
}

func TestStatusHealthyTableOutput(t *testing.T) {
	env := statusEnvelope{
		Health:        "healthy",
		Version:       "v0.0.1",
		UptimeSeconds: 42,
		Checks: statusChecks{
			Process: "ok", RESTAPI: "ok", GRPCAPI: "ok",
			DB:       statusDBCheck{Status: "ok"},
			Watchers: []statusWatcher{},
		},
	}
	srv := fakeHealthzServer(t, http.StatusOK, env)
	defer srv.Close()

	withFormat(t, "")

	cmd := statusCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := runStatus(cmd, nil)
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "HEALTHY")
	assert.Contains(t, out, "v0.0.1")
	assert.Contains(t, out, "process")
	assert.Contains(t, out, "db")
}

func TestStatusJSONOutputPassThrough(t *testing.T) {
	env := statusEnvelope{
		Health: "healthy", Version: "v9", UptimeSeconds: 1,
		Checks: statusChecks{Process: "ok", RESTAPI: "ok", GRPCAPI: "ok",
			DB: statusDBCheck{Status: "ok"}, Watchers: []statusWatcher{}},
	}
	srv := fakeHealthzServer(t, http.StatusOK, env)
	defer srv.Close()

	withFormat(t, "json")

	cmd := statusCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, runStatus(cmd, nil))

	var got statusEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "v9", got.Version)
	assert.Equal(t, "healthy", got.Health)
}

// TestStatusReturnsErrorOn503 verifies that a server reporting failed
// causes runStatus to return a non-nil error (which the cobra runner
// maps to a non-zero exit).
func TestStatusReturnsErrorOn503(t *testing.T) {
	env := statusEnvelope{
		Health: "failed", Version: "v0.0.1", UptimeSeconds: 5,
		Checks: statusChecks{Process: "ok", RESTAPI: "ok", GRPCAPI: "unknown",
			DB: statusDBCheck{Status: "failed"}, Watchers: []statusWatcher{}},
	}
	srv := fakeHealthzServer(t, http.StatusServiceUnavailable, env)
	defer srv.Close()

	withFormat(t, "")

	cmd := statusCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := runStatus(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

// TestStatusUnreachableReturnsError verifies that an unreachable server
// returns a non-nil error (does not panic, does not silently exit 0).
func TestStatusUnreachableReturnsError(t *testing.T) {
	withFormat(t, "")

	cmd := statusCmd
	// 127.0.0.1:1 is reliably unbound on test runners.
	require.NoError(t, cmd.Flags().Set("server", "http://127.0.0.1:1"))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := runStatus(cmd, nil)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "healthcheck") || strings.Contains(err.Error(), "connection"),
		"error should mention transport/healthcheck failure: %v", err)
}
