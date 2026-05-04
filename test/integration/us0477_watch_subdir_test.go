package integration

// T-0477: filesystem watcher must ingest files dropped into a subdirectory
// that did not exist when the watcher was registered.
//
// This integration test wires a real watcher.Manager to the in-process dpkms
// server (real SQLite store, real jobs queue, real pipeline). It mkdirs a
// new subdir under the watched root, drops a markdown file inside, waits
// for the watcher's enqueued job to complete, and validates the resulting
// KnowledgeObject contains the expected text body.
//
// Gate: INTEGRATION=1.

import (
	"context"
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

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

func TestUS0477_WatchPicksUpFileInNewSubdir(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}

	root := t.TempDir()

	env := startTestEnv(t)
	defer env.stop(t)

	mgr := watcher.NewManager(env.svc.Store.Watches(), env.svc)
	t.Cleanup(mgr.Stop)

	cfg := &storage.WatchConfig{
		ID:              "t0477",
		Path:            root,
		Mode:            "generic",
		IncludePatterns: []string{"**/*.md"},
		DebounceMS:      100,
		Status:          "active",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	require.NoError(t, mgr.AddWatch(context.Background(), cfg))

	// Allow fsnotify to register on the root.
	time.Sleep(200 * time.Millisecond)

	// Create a NEW nested subdirectory and a markdown file inside it.
	// Mirrors the original Claude-Code Write-tool repro where
	// clients/bgroup/wechalet/bankruptcy/ was created post-registration
	// and never picked up by the watcher.
	sub := filepath.Join(root, "bgroup", "wechalet", "bankruptcy")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	const needle = "wechalet-bankruptcy-marker-T0477"
	relFile := filepath.Join("bgroup", "wechalet", "bankruptcy", "call-2026-05-04-jonathan-roy.md")
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
