//go:build watch_e2e

// Package watch_test exercises the directory watcher end-to-end against the
// real `bin/ctxt` and `bin/dpkms` binaries.
//
// Regression: a file dropped into a subdirectory created AFTER the watcher
// started must be ingested and become searchable via `ctxt find`.
//
// Run with:
//
//	make build && go test -tags 'fts5 watch_e2e' ./test/e2e/watch/... -v
//
// dpkms takes every listener port from its config file (0: OS-assigned)
// and the test reads the bound ports back from its pidfile, so it never
// binds or calls a port a developer's real instance may hold.
package watch_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain points HOME and XDG at a throwaway tree and clears the CTXT_,
// CH_ and DPKMS_ env, so neither binary reads the developer's config.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil, "ctxt", "dpkms"))
}

func TestE2E_WatchSubdirIngest(t *testing.T) {
	root := projectRoot(t)
	ctxtBin := filepath.Join(root, "bin", "ctxt")
	dpkmsBin := filepath.Join(root, "bin", "dpkms")
	for _, bin := range []string{ctxtBin, dpkmsBin} {
		if _, err := os.Stat(bin); err != nil {
			t.Skipf("missing %s — run `make build`", bin)
		}
	}

	dataDir := t.TempDir()
	watchedDir := t.TempDir()
	dataHome := filepath.Join(dataDir, "data")

	// Every dpkms listener on an OS-assigned port; stub providers and a
	// closed embedding endpoint so ingest reaches no model server.
	dpkmsCfg := filepath.Join(dataDir, "dpkms.yaml")
	writeFile(t, dpkmsCfg, fmt.Sprintf(`
storage:
  type: sqlite
  path: %s
server:
  access: private
  port: 0
  grpc_port: 0
  cookie_bridge_port: 0
  workers: 2
browser:
  enabled: false
providers:
  video: {backend: stub}
  document: {backend: stub}
  ocr: {backend: stub}
  transcription: {backend: stub}
  vision: {backend: stub}
  diarization: {backend: stub}
  llm: {backend: stub}
`, filepath.Join(dataDir, "db.sqlite")))

	env := append(os.Environ(),
		"XDG_DATA_HOME="+dataHome,
		"CTXT_NO_CLIPBOARD=1",
		"DPKMS_BUS_TOKEN=watch-e2e",
		embeddings.EnvProvider+"="+embeddings.BackendStub,
		embeddings.EnvEndpoint+"="+testguard.ClosedServerURL,
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Start dpkms (job worker + HTTP API).
	dpkms := exec.CommandContext(ctx, dpkmsBin, "serve")
	dpkms.Env = withEnv(env, "CTXT_CONFIG="+dpkmsCfg)
	dpkmsLog := &bytes.Buffer{}
	dpkms.Stdout, dpkms.Stderr = dpkmsLog, dpkmsLog
	require.NoError(t, dpkms.Start())
	t.Cleanup(func() {
		_ = dpkms.Process.Kill()
		_, _ = dpkms.Process.Wait()
		t.Logf("dpkms log:\n%s", dpkmsLog.String())
	})

	inst := awaitInstance(t, filepath.Join(dataHome, "contexthelp", "run"), dpkms.Process.Pid)
	t.Logf("dpkms bound HTTP %d, gRPC %d, cookie bridge %d", inst.Port, inst.GRPCPort, inst.CookieBridgePort)
	for _, p := range []int{inst.Port, inst.GRPCPort, inst.CookieBridgePort} {
		if p <= 0 || testguard.Guarded("127.0.0.1:"+strconv.Itoa(p)) {
			t.Fatalf("dpkms bound port %d; config asked for an OS-assigned port", p)
		}
	}
	serverURL := "http://127.0.0.1:" + strconv.Itoa(inst.Port)
	awaitHealthy(t, serverURL)

	// ctxt routes to that instance and watches watchedDir.
	ctxtCfg := filepath.Join(dataDir, "ctxt.yaml")
	writeFile(t, ctxtCfg, fmt.Sprintf(`
server:
  url: %s
watch:
  dirs:
    - %s
  patterns:
    - "**/*.md"
  debounce: 100ms
`, serverURL, watchedDir))
	ctxtEnv := withEnv(env, "CTXT_CONFIG="+ctxtCfg)

	// Start ctxt watch start.
	watch := exec.CommandContext(ctx, ctxtBin, "watch", "start")
	watch.Env = ctxtEnv
	watchLog := &bytes.Buffer{}
	watch.Stdout, watch.Stderr = watchLog, watchLog
	require.NoError(t, watch.Start())
	t.Cleanup(func() {
		_ = watch.Process.Kill()
		_, _ = watch.Process.Wait()
		t.Logf("ctxt watch log:\n%s", watchLog.String())
	})

	// Give the watcher time to register on watchedDir.
	time.Sleep(500 * time.Millisecond)

	// THE REPRO: create a subdir AFTER the watcher started and drop a file in.
	sub := filepath.Join(watchedDir, "bgroup", "wechalet", "bankruptcy")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	const needle = "wechalet-bankruptcy-marker-subdir"
	require.NoError(t, os.WriteFile(
		filepath.Join(sub, "call-2026-05-04-jonathan-roy.md"),
		[]byte("# Note\n\n"+needle+" body\n"),
		0o644,
	))

	// Poll `ctxt find <needle>` until it returns the file or we time out.
	deadline := time.Now().Add(30 * time.Second)
	var lastOut string
	for time.Now().Before(deadline) {
		find := exec.Command(ctxtBin, "find", needle)
		find.Env = ctxtEnv
		out, _ := find.CombinedOutput()
		lastOut = string(out)
		if strings.Contains(lastOut, needle) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("ctxt find %q did not return the watched file within 30s; last output:\n%s",
		needle, lastOut)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}

// withEnv returns a copy of env with kv appended; env itself is never
// extended in place, so two commands built from it cannot share entries.
func withEnv(env []string, kv ...string) []string {
	return append(slices.Clone(env), kv...)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

// awaitInstance waits for the pidfile dpkms pid writes once its listeners
// are bound, and returns it.
func awaitInstance(t *testing.T, runDir string, pid int) pidfile.Info {
	t.Helper()
	var inst pidfile.Info
	require.Eventually(t, func() bool {
		infos, err := pidfile.Scan(runDir)
		if err != nil {
			return false
		}
		for _, info := range infos {
			if info.PID == pid {
				inst = info
				return true
			}
		}
		return false
	}, 30*time.Second, 100*time.Millisecond, "dpkms wrote no pidfile in %s", runDir)
	return inst
}

// awaitHealthy waits for GET url/health to answer 200.
func awaitHealthy(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url+"/health", nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 100*time.Millisecond, "dpkms at %s never answered /health", url)
}
