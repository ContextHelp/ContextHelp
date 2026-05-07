package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeUpgradeHealthzServer responds to /healthz with an envelope that
// embeds the supplied upgrade sub-object (or nil for the idle case).
func fakeUpgradeHealthzServer(t *testing.T, up *upgradeEnvelope) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		health := "healthy"
		if up != nil && up.State == "in_progress" {
			health = "upgrading"
		} else if up != nil && up.State == "failed" {
			health = "degraded"
		}
		env := map[string]any{
			"health":         health,
			"version":        "v0.0.1",
			"uptime_seconds": 1,
			"checks": map[string]any{
				"process": "ok", "rest_api": "ok", "grpc_api": "ok",
				"db":       map[string]any{"status": "ok"},
				"queue":    map[string]any{"pending": 0, "running": 0, "failed": 0},
				"watchers": []any{},
			},
		}
		if up != nil {
			env["upgrade"] = up
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(env)
	})
	return httptest.NewServer(mux)
}

// TestUpgradeStatusJSONReturnsEnvelope: --format json returns just the
// upgrade sub-envelope so jq pipelines target it directly.
func TestUpgradeStatusJSONReturnsEnvelope(t *testing.T) {
	up := &upgradeEnvelope{
		State:      "in_progress",
		Bucket:     "reingest_selective",
		Done:       47,
		Total:      120,
		Progress:   0.391,
		EtaSeconds: 32,
		StartedAt:  "2026-05-07T13:42:00Z",
	}
	srv := fakeUpgradeHealthzServer(t, up)
	defer srv.Close()

	withFormat(t, "json")

	cmd := upgradeStatusCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, runUpgradeStatus(cmd, nil))

	var got upgradeEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, "in_progress", got.State)
	assert.Equal(t, "reingest_selective", got.Bucket)
	assert.Equal(t, 47, got.Done)
	assert.Equal(t, 120, got.Total)
	assert.Equal(t, 32, got.EtaSeconds)
}

// TestUpgradeStatusIdleHumanReadable: with no upgrade probe wired, status
// renders "Upgrade state: idle" and exits 0.
func TestUpgradeStatusIdleHumanReadable(t *testing.T) {
	srv := fakeUpgradeHealthzServer(t, nil)
	defer srv.Close()

	withFormat(t, "")

	cmd := upgradeStatusCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, runUpgradeStatus(cmd, nil))
	assert.Contains(t, buf.String(), "Upgrade state: idle")
}

// TestUpgradeStatusInProgressHumanReadable: in_progress renders the full
// table view per the T-0580 spec.
func TestUpgradeStatusInProgressHumanReadable(t *testing.T) {
	srv := fakeUpgradeHealthzServer(t, &upgradeEnvelope{
		State:      "in_progress",
		Bucket:     "reingest_selective",
		Done:       47,
		Total:      120,
		Progress:   0.391,
		EtaSeconds: 32,
		StartedAt:  "2026-05-07T13:42:00Z",
	})
	defer srv.Close()

	withFormat(t, "")

	cmd := upgradeStatusCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, runUpgradeStatus(cmd, nil))
	out := buf.String()
	for _, frag := range []string{
		"Upgrade state: in_progress",
		"Bucket:    reingest_selective",
		"Progress:  47/120 (39%)",
		"ETA:       32s",
		"Started:   2026-05-07T13:42:00Z",
	} {
		assert.Contains(t, out, frag, "expected fragment %q in output", frag)
	}
}

// TestUpgradeStatusFailedExitsNonZero: state=failed must produce a non-nil
// error so the cobra runner returns exit 1.
func TestUpgradeStatusFailedExitsNonZero(t *testing.T) {
	srv := fakeUpgradeHealthzServer(t, &upgradeEnvelope{
		State:     "failed",
		Bucket:    "reingest_selective",
		LastError: "disk full",
	})
	defer srv.Close()

	withFormat(t, "")

	cmd := upgradeStatusCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := runUpgradeStatus(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

// TestUpgradePlanEmitsSummary: plan returns the canned summary for both
// human and JSON modes (T-0581 fills in real counts later).
func TestUpgradePlanEmitsSummary(t *testing.T) {
	srv := fakeUpgradeHealthzServer(t, nil)
	defer srv.Close()

	withFormat(t, "")

	cmd := upgradePlanCmd
	require.NoError(t, cmd.Flags().Set("server", srv.URL))
	t.Cleanup(func() { _ = cmd.Flags().Set("server", "") })

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, runUpgradePlan(cmd, nil))
	out := buf.String()
	assert.Contains(t, out, "Upgrade plan:")
	assert.Contains(t, out, "reindex_auto:")
	assert.Contains(t, out, "reingest_selective:")
	assert.Contains(t, out, "reingest_all:")
	assert.Contains(t, out, "T-0581")
}

// TestUpgradeRunRefusesUntilT0581: stub run returns an error that
// references T-0581 so operators / future agents know where to look.
func TestUpgradeRunRefusesUntilT0581(t *testing.T) {
	cmd := upgradeRunCmd
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := runUpgradeRun(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet shipped")
	assert.Contains(t, err.Error(), "T-0581")
}

// TestUpgradeRunAllRequiresConsent: bucket-3 consent guard fires even in
// the stub. Ensures the surface is correct before T-0581 wires the body.
func TestUpgradeRunAllRequiresConsent(t *testing.T) {
	cmd := upgradeRunCmd
	require.NoError(t, cmd.Flags().Set("all", "true"))
	require.NoError(t, cmd.Flags().Set("i-understand-the-cost", ""))
	t.Cleanup(func() {
		_ = cmd.Flags().Set("all", "false")
		_ = cmd.Flags().Set("i-understand-the-cost", "")
	})

	err := runUpgradeRun(cmd, nil)
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "i-understand-the-cost") || strings.Contains(err.Error(), "consent"),
		"expected consent-guard error: %v", err)
}
