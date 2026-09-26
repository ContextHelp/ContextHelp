package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
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
//
// These cases invoke the command function directly rather than going
// through the root, so the root's pre-run hook — which publishes the
// executing command to cliformat — never fires. Any command a previous
// test bound is still there, and its --format flag may still read as
// Changed, which would outrank the value set here. Rebind to the root
// with the flag reset so viper is authoritative for this test.
func withFormat(t *testing.T, value string) {
	t.Helper()
	prev := viper.GetString("format")
	viper.Set("format", value)

	formatFlag := rootCmd.PersistentFlags().Lookup("format")
	prevChanged := false
	if formatFlag != nil {
		prevChanged = formatFlag.Changed
		formatFlag.Changed = false
	}
	cliformat.Bind(rootCmd)

	t.Cleanup(func() {
		viper.Set("format", prev)
		if formatFlag != nil {
			formatFlag.Changed = prevChanged
		}
	})
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
	t.Cleanup(func() { cmd.SetOut(nil) })

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
		Checks: statusChecks{
			Process: "ok", RESTAPI: "ok", GRPCAPI: "ok",
			DB: statusDBCheck{Status: "ok"}, Watchers: []statusWatcher{},
		},
	}
	srv := fakeHealthzServer(t, http.StatusOK, env)
	defer srv.Close()

	withFormat(t, "json")

	cmd := statusCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	t.Cleanup(func() { cmd.SetOut(nil) })

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
		Checks: statusChecks{
			Process: "ok", RESTAPI: "ok", GRPCAPI: "unknown",
			DB: statusDBCheck{Status: "failed"}, Watchers: []statusWatcher{},
		},
	}
	srv := fakeHealthzServer(t, http.StatusServiceUnavailable, env)
	defer srv.Close()

	withFormat(t, "")

	cmd := statusCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	t.Cleanup(func() { cmd.SetOut(nil) })

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
	t.Cleanup(func() { cmd.SetOut(nil) })

	err := runStatus(cmd, nil)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "healthcheck") || strings.Contains(err.Error(), "connection"),
		"error should mention transport/healthcheck failure: %v", err)
}

// Without --server, `ctxt status` reads the primary of the configured
// server.urls with that entry's token, never the built-in default.
func TestStatus_UsesPrimaryConfiguredServer(t *testing.T) {
	healthy := statusEnvelope{Health: "healthy", Version: "v-primary"}
	primary := newRecordedServer(t, fakeHealthzServer(t, http.StatusOK, healthy).Config.Handler)
	secondary := newRecordedServer(t, fakeHealthzServer(t, http.StatusOK, healthy).Config.Handler)
	db := setupTestDB(t)
	appendConfig(t, db, "server:\n  urls:\n    - url: "+primary.URL+"\n      token: tok-primary\n    - "+secondary.URL+"\n")

	out, err := db.exec("status", "--format", "json")
	require.NoError(t, err, out)
	assert.Contains(t, out, "v-primary")
	assert.Equal(t, []string{"Bearer tok-primary"}, primary.hits())
	assert.Empty(t, secondary.hits())
}
