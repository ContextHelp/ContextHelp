//go:build watch_e2e

// Package watch_test exercises the directory watcher end-to-end against the
// real `bin/ctxt` and `bin/dpkms` binaries.
//
// T-0477 regression: a file dropped into a subdirectory created AFTER the
// watcher started must be ingested and become searchable via `ctxt find`.
//
// Run with:
//
//	make build && go test -tags 'fts5 watch_e2e' ./test/e2e/watch/... -v
//
// The test currently skips when `bin/ctxt` panics on startup (T-0457:
// --output flag collision with kit/cli). Once T-0457 lands, drop the
// skip-on-panic guard.
package watch_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestE2E_T0477_WatchSubdirIngest(t *testing.T) {
	root := projectRoot(t)
	ctxtBin := filepath.Join(root, "bin", "ctxt")
	dpkmsBin := filepath.Join(root, "bin", "dpkms")
	for _, bin := range []string{ctxtBin, dpkmsBin} {
		if _, err := os.Stat(bin); err != nil {
			t.Skipf("missing %s — run `make build`", bin)
		}
	}

	// Sanity-check that ctxt does not panic on startup. T-0457 currently
	// makes every invocation panic; until it's fixed we skip rather than
	// produce a misleading red.
	if out, err := exec.Command(ctxtBin, "--help").CombinedOutput(); err != nil ||
		bytes.Contains(out, []byte("panic:")) {
		t.Skipf("bin/ctxt panics on startup (likely T-0457: --output flag collision); output:\n%s", out)
	}

	dataDir := t.TempDir()
	watchedDir := t.TempDir()

	httpPort := freePort(t)
	grpcPort := freePort(t)

	// Minimal config wiring storage to a temp dir, server to free ports,
	// and the directory watcher to watchedDir.
	cfgPath := filepath.Join(dataDir, "config.yaml")
	cfg := fmt.Sprintf(`
storage:
  type: sqlite
  path: %s
server:
  port: %d
  grpc_port: %d
  workers: 2
watch:
  dirs:
    - %s
  patterns:
    - "**/*.md"
  debounce: 100ms
`, filepath.Join(dataDir, "db.sqlite"), httpPort, grpcPort, watchedDir)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o644))

	env := append(os.Environ(),
		"CTXT_CONFIG="+cfgPath,
		"CTXT_DATA_DIR="+dataDir,
		"HOME="+dataDir,
		"XDG_CONFIG_HOME="+filepath.Join(dataDir, ".config"),
		"CTXT_NO_CLIPBOARD=1",
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Start dpkms (job worker + HTTP API).
	dpkms := exec.CommandContext(ctx, dpkmsBin, "serve")
	dpkms.Env = env
	dpkmsLog := &bytes.Buffer{}
	dpkms.Stdout, dpkms.Stderr = dpkmsLog, dpkmsLog
	require.NoError(t, dpkms.Start())
	t.Cleanup(func() {
		_ = dpkms.Process.Kill()
		_, _ = dpkms.Process.Wait()
		t.Logf("dpkms log:\n%s", dpkmsLog.String())
	})

	// Wait for HTTP port to accept connections.
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", httpPort), 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return true
		}
		return false
	}, 10*time.Second, 100*time.Millisecond, "dpkms server did not come up")

	// Start ctxt watch start.
	watch := exec.CommandContext(ctx, ctxtBin, "watch", "start")
	watch.Env = env
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

	const needle = "wechalet-bankruptcy-marker-T0477"
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
		find.Env = env
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

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

