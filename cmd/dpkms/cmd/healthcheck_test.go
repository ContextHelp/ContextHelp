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

func fakeDpkmsHealthzServer(t *testing.T, status int, env HealthzEnvelope) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(env)
	})
	return httptest.NewServer(mux)
}

func TestHealthcheckHealthyTable(t *testing.T) {
	env := HealthzEnvelope{
		Health: "healthy", Version: "v0.0.1", UptimeSeconds: 7,
		Checks: HealthzChecks{
			Process: "ok", RESTAPI: "ok", GRPCAPI: "ok",
			DB:       HealthzDBCheck{Status: "ok"},
			Watchers: []HealthzWatcher{},
		},
	}
	srv := fakeDpkmsHealthzServer(t, http.StatusOK, env)
	defer srv.Close()

	prevServer := viper.GetString("server.url")
	prevFormat := viper.GetString("output.format")
	viper.Set("server.url", srv.URL)
	viper.Set("output.format", "")
	defer func() { viper.Set("server.url", prevServer); viper.Set("output.format", prevFormat) }()

	var buf bytes.Buffer
	healthcheckCmd.SetOut(&buf)
	healthcheckCmd.SetErr(&buf)

	err := runHealthcheck(healthcheckCmd, nil)
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "HEALTHY")
	assert.Contains(t, out, "v0.0.1")
}

func TestHealthcheckJSONPassThrough(t *testing.T) {
	env := HealthzEnvelope{
		Health: "healthy", Version: "v9", UptimeSeconds: 1,
		Checks: HealthzChecks{
			Process: "ok", RESTAPI: "ok", GRPCAPI: "ok",
			DB: HealthzDBCheck{Status: "ok"}, Watchers: []HealthzWatcher{},
		},
	}
	srv := fakeDpkmsHealthzServer(t, http.StatusOK, env)
	defer srv.Close()

	prevServer := viper.GetString("server.url")
	prevFormat := viper.GetString("output.format")
	viper.Set("server.url", srv.URL)
	viper.Set("output.format", "json")
	defer func() { viper.Set("server.url", prevServer); viper.Set("output.format", prevFormat) }()

	var buf bytes.Buffer
	healthcheckCmd.SetOut(&buf)
	healthcheckCmd.SetErr(&buf)

	require.NoError(t, runHealthcheck(healthcheckCmd, nil))

	var got HealthzEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "v9", got.Version)
}

func TestHealthcheckExitsNonZeroOn503(t *testing.T) {
	env := HealthzEnvelope{
		Health: "failed", UptimeSeconds: 5,
		Checks: HealthzChecks{
			Process: "ok", RESTAPI: "ok", GRPCAPI: "unknown",
			DB: HealthzDBCheck{Status: "failed"}, Watchers: []HealthzWatcher{},
		},
	}
	srv := fakeDpkmsHealthzServer(t, http.StatusServiceUnavailable, env)
	defer srv.Close()

	prevServer := viper.GetString("server.url")
	viper.Set("server.url", srv.URL)
	defer viper.Set("server.url", prevServer)

	var buf bytes.Buffer
	healthcheckCmd.SetOut(&buf)
	healthcheckCmd.SetErr(&buf)

	err := runHealthcheck(healthcheckCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

func TestHealthcheckUnreachable(t *testing.T) {
	prevServer := viper.GetString("server.url")
	viper.Set("server.url", "http://127.0.0.1:1")
	defer viper.Set("server.url", prevServer)

	var buf bytes.Buffer
	healthcheckCmd.SetOut(&buf)
	healthcheckCmd.SetErr(&buf)

	err := runHealthcheck(healthcheckCmd, nil)
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "healthcheck") ||
			strings.Contains(err.Error(), "connection") ||
			strings.Contains(err.Error(), "refused"),
		"unexpected error: %v", err)
}

// TestHealthcheckQuietSuppressesStdout ensures --quiet does not produce
// any stdout output even when the server is healthy.
func TestHealthcheckQuietSuppressesStdout(t *testing.T) {
	env := HealthzEnvelope{
		Health: "healthy", Version: "v0.0.1", UptimeSeconds: 1,
		Checks: HealthzChecks{
			Process: "ok", RESTAPI: "ok", GRPCAPI: "ok",
			DB: HealthzDBCheck{Status: "ok"}, Watchers: []HealthzWatcher{},
		},
	}
	srv := fakeDpkmsHealthzServer(t, http.StatusOK, env)
	defer srv.Close()

	prevServer := viper.GetString("server.url")
	viper.Set("server.url", srv.URL)
	defer viper.Set("server.url", prevServer)

	var stdout bytes.Buffer
	healthcheckCmd.SetOut(&stdout)
	healthcheckCmd.SetErr(&bytes.Buffer{})
	require.NoError(t, healthcheckCmd.Flags().Set("quiet", "true"))
	defer healthcheckCmd.Flags().Set("quiet", "false")

	require.NoError(t, runHealthcheck(healthcheckCmd, nil))
	assert.Empty(t, stdout.String(), "--quiet must not write to stdout")
}
