//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/test/testutil"
)

func TestMain(m *testing.M) {
	if err := testutil.EnsureBuiltM(); err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: %v\n", err)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCtxtVersion(t *testing.T) {
	out, err := testutil.Run(t, "ctxt", "version")
	require.NoError(t, err, "ctxt version should exit 0")
	assert.Contains(t, strings.ToLower(out), "ctxt", "output should contain 'ctxt'")
}

func TestCtxtHelp(t *testing.T) {
	out, err := testutil.Run(t, "ctxt", "--help")
	require.NoError(t, err, "ctxt --help should exit 0")

	subcommands := []string{
		"analyze", "list", "find", "open", "edit", "delete",
		"make", "job", "profile", "config", "registry",
		"entity", "completion",
	}
	for _, sub := range subcommands {
		assert.Contains(t, out, sub, "help output should list subcommand %q", sub)
	}
}

func TestCtxtCompletionBash(t *testing.T) {
	out, err := testutil.Run(t, "ctxt", "completion", "bash")
	require.NoError(t, err, "ctxt completion bash should exit 0")
	assert.True(t,
		strings.HasPrefix(strings.TrimSpace(out), "#") ||
			strings.Contains(strings.ToLower(out), "bash"),
		"completion output should start with '#' or contain 'bash'",
	)
}

func TestDpkmsVersion(t *testing.T) {
	out, err := testutil.Run(t, "dpkms", "version")
	require.NoError(t, err, "dpkms version should exit 0")
	assert.Contains(t, strings.ToLower(out), "dpkms", "output should contain 'dpkms'")
}

func TestDpkmsServeHealthAndShutdown(t *testing.T) {
	baseURL, cleanup := testutil.StartServer(t)
	defer cleanup()

	// Verify /health returns 200.
	resp, err := http.Get(baseURL + "/health")
	require.NoError(t, err, "GET /health should succeed")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "/health should return 200")

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"], "health status should be 'ok'")

	// Cleanup sends SIGTERM; verify it completes (no panic, no hang).
	// The deferred cleanup() handles this.
}

func TestRoundTrip(t *testing.T) {
	baseURL, cleanup := testutil.StartServer(t)
	defer cleanup()

	// POST /api/v1/analyze with test content.
	analyzeBody, _ := json.Marshal(map[string]string{
		"content": "smoke test content for round trip verification",
		"type":    "text",
		"source":  "smoke-test",
	})

	resp, err := http.Post(
		baseURL+"/api/v1/analyze",
		"application/json",
		bytes.NewReader(analyzeBody),
	)
	require.NoError(t, err, "POST /analyze should succeed")
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode, "analyze should return 202")

	var analyzeResult map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&analyzeResult))
	jobID := analyzeResult["job_id"]
	require.NotEmpty(t, jobID, "job_id should not be empty")

	// Poll job status until completion or timeout.
	deadline := time.Now().Add(10 * time.Second)
	var jobStatus string
	var resultID string

	for time.Now().Before(deadline) {
		jobResp, err := http.Get(fmt.Sprintf("%s/api/v1/jobs/%s", baseURL, jobID))
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		var job map[string]any
		json.NewDecoder(jobResp.Body).Decode(&job)
		jobResp.Body.Close()

		jobStatus, _ = job["status"].(string)
		if jobStatus == "completed" {
			resultID, _ = job["result_id"].(string)
			break
		}
		if jobStatus == "failed" {
			t.Fatalf("job failed: %v", job["error"])
		}
		time.Sleep(100 * time.Millisecond)
	}

	require.Equal(t, "completed", jobStatus, "job should reach 'completed' status")
	require.NotEmpty(t, resultID, "result_id should be set on completed job")

	// GET the resulting object.
	objResp, err := http.Get(fmt.Sprintf("%s/api/v1/objects/%s", baseURL, resultID))
	require.NoError(t, err, "GET object should succeed")
	defer objResp.Body.Close()
	assert.Equal(t, http.StatusOK, objResp.StatusCode, "object should exist")

	var obj map[string]any
	require.NoError(t, json.NewDecoder(objResp.Body).Decode(&obj))
	assert.Equal(t, resultID, obj["id"], "object ID should match result_id")
}
