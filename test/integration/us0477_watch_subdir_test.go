package integration

// filesystem watcher must ingest files dropped into a subdirectory
// that did not exist when the watcher was registered.
//
// This integration test wires a real watcher.Manager to the in-process dpkms
// server (real SQLite store, real jobs queue, real pipeline). It mkdirs a
// new subdir under the watched root, drops a markdown file inside, waits
// for the watcher's enqueued job to complete, and validates the resulting
// KnowledgeObject contains the expected text body.
//

import (
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/server/stack"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

func TestUS0477_WatchPicksUpFileInNewSubdir(t *testing.T) {
	root := t.TempDir()

	env := startTestEnv(t)
	defer env.stop(t)

	ws := env.driver.Watches()
	mgr := watcher.NewManager(ws, watcher.StoreRecords(ws), stack.ServiceIngester(env.svc))
	t.Cleanup(mgr.Stop)

	n := 0
	cfg := &storage.WatchConfig{
		ID:              "subdir-watch",
		Path:            root,
		Mode:            "generic",
		IncludePatterns: []string{"**/*.md"},
		DebounceMS:      100,
		Status:          "active",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	require.NoError(t, mgr.AddWatch(ownerCtx(), cfg))

	// Wait until the watcher is live. Registration with fsnotify happens
	// on the watch goroutine after AddWatch returns, and a file written
	// before that is never seen (no initial scan), so write a fresh probe
	// file each round until one is ingested.
	require.Eventually(t, func() bool {
		n++
		name := fmt.Sprintf("probe-%d.md", n)
		if err := os.WriteFile(filepath.Join(root, name), []byte("# Probe\n\nready\n"), 0o644); err != nil {
			return false
		}
		return watchJobExists(env.URL, "watch:generic:probe-", 500*time.Millisecond)
	}, 20*time.Second, 50*time.Millisecond, "watcher never ingested a root-level probe file")

	// Create a NEW nested subdirectory and a markdown file inside it.
	// Regression: a subdirectory created after registration was never
	// picked up by the watcher.
	sub := filepath.Join(root, "bgroup", "acme", "renewal")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	const needle = "acme-renewal-marker-subdir"
	relFile := filepath.Join("bgroup", "acme", "renewal", "call-2026-05-04-jane-doe.md")
	absFile := filepath.Join(root, relFile)
	require.NoError(t, os.WriteFile(absFile, []byte("# Note\n\n"+needle+" body\n"), 0o644))

	// Watcher source is "watch:<mode>:<rel-path-with-fwd-slashes>".
	wantSourceFragment := "watch:generic:" + filepath.ToSlash(relFile)

	jobID := waitForWatchJob(t, env.URL, wantSourceFragment, 15*time.Second)
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID, "completed job must have a result object id")

	// Fetch the resulting KnowledgeObject and assert the body landed.
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	assert.Contains(t, obj.TextContent, needle,
		"ingested object body must contain marker from file inside newly-created subdir")
}

// waitForWatchJob polls /api/v1/jobs until a job appears whose Source
// contains the given fragment, returning that job's id. Fails the test if
// no such job appears within the deadline.
func waitForWatchJob(t *testing.T, baseURL, sourceFragment string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		resp, err := gohttp.Get(baseURL + "/api/v1/jobs?limit=100")
		if err == nil {
			var payload struct {
				Data []storage.Job `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&payload)
			resp.Body.Close()
			for _, j := range payload.Data {
				if strings.Contains(j.Source, sourceFragment) {
					return j.ID
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("no job appeared with source containing %q within %s", sourceFragment, within)
	return ""
}

// watchJobExists polls /api/v1/jobs for up to within, reporting whether a job
// whose Source contains fragment appeared.
func watchJobExists(baseURL, fragment string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		resp, err := gohttp.Get(baseURL + "/api/v1/jobs?limit=100")
		if err == nil {
			var payload struct {
				Data []storage.Job `json:"data"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&payload)
			resp.Body.Close()
			for _, j := range payload.Data {
				if strings.Contains(j.Source, fragment) {
					return true
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
