package cmd

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// deadServerURL points at a port nothing listens on.
const deadServerURL = testguard.ClosedServerURL

var jobIDRe = regexp.MustCompile(`Job ID: (\S+)`)

// tempDB returns a temp DB file path; pass storageOverride(dbPath) to
// executeCommand so the command under test uses it.
func tempDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "db.sqlite")
}

// storageOverride renders the -c key=value override routing storage to dbPath.
func storageOverride(dbPath string) []string {
	return []string{"-c", "storage.path=" + dbPath}
}

// assertKitExit fails unless err is a kit envelope with code and exit.
func assertKitExit(t *testing.T, err error, code string, exit int) {
	t.Helper()
	var ke *output.Error
	if !errors.As(err, &ke) || ke.Code != code || ke.ExitCode != exit {
		t.Fatalf("err = %v (%#v); want %s, exit %d", err, ke, code, exit)
	}
}

// isolateDataDirs gives the test its own empty HOME and XDG data dir, so
// assertNoLocalDB sees only what this test's command wrote (the package
// shares one testguard tree that other tests fill with databases).
func isolateDataDirs(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("CTXT_DATA_DIR", "")
}

// assertNoLocalDB fails when a ctxt database file exists under the
// test's HOME or XDG data dir: an enqueue must never land locally.
func assertNoLocalDB(t *testing.T, dirs ...string) {
	t.Helper()
	dirs = append(dirs, os.Getenv("XDG_DATA_HOME"), os.Getenv("HOME"))
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err // a dir that doesn't exist holds no database
			}
			if !d.IsDir() && strings.HasPrefix(d.Name(), "db.sqlite") {
				t.Errorf("local database file created: %s", path)
			}
			return nil
		})
	}
}

// enqueueCommands are the invocations that enqueue content: analyze and
// bare `ctxt` fed on stdin. (A positional argument to bare `ctxt` is
// rejected by cobra as an unknown command before the root runs.)
var enqueueCommands = []struct {
	name  string
	args  []string
	stdin string
}{
	{"analyze", []string{"analyze", "offline insight"}, ""},
	{"bare content", nil, "offline insight"},
}

// pipeStdin makes os.Stdin a pipe carrying content for the rest of the
// test; "" leaves it alone.
func pipeStdin(t *testing.T, content string) {
	t.Helper()
	if content == "" {
		return
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig; _ = r.Close() })
}

// With nothing answering at the resolved instance the enqueue fails as
// PREREQUISITE (exit 70): no local queue, no database, no Job ID.
func TestEnqueueUnreachableExits70WritesNothingLocally(t *testing.T) {
	for _, tc := range enqueueCommands {
		t.Run(tc.name, func(t *testing.T) {
			setupTestDB(t, dpkmstest.Unreachable())
			dbPath := tempDB(t)
			pipeStdin(t, tc.stdin)

			out, err := executeCommand(append(append([]string(nil), tc.args...), storageOverride(dbPath)...)...)
			assertKitExit(t, err, output.CodePrerequisite, output.ExitPrerequisite)
			if strings.Contains(out, "Job ID:") || strings.Contains(strings.ToLower(out), "queued locally") {
				t.Errorf("output claims an enqueue: %q", out)
			}
			assertNoLocalDB(t, filepath.Dir(dbPath))
		})
	}
}

// rejectingDaemon answers every request with status and counts analyze
// requests.
func rejectingDaemon(t *testing.T, status int, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/analyze" {
			hits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"invalid token"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A 401 or 403 is final: exit 5, exactly one request, nothing local.
func TestEnqueueRejectedTokenExits5WithoutRetry(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		for _, tc := range enqueueCommands {
			t.Run(tc.name+"/"+http.StatusText(status), func(t *testing.T) {
				setupTestDB(t, dpkmstest.Unreachable())
				dbPath := tempDB(t)
				var hits atomic.Int64
				srv := rejectingDaemon(t, status, &hits)
				pipeStdin(t, tc.stdin)

				out, err := executeCommand(append(append(append([]string(nil), tc.args...), "--server", srv.URL), storageOverride(dbPath)...)...)
				assertKitExit(t, err, output.CodeUnauthorized, output.ExitUnauthorized)
				if n := hits.Load(); n != 1 {
					t.Errorf("analyze requests = %d; want exactly 1 (no retry)", n)
				}
				if strings.Contains(out, "Job ID:") {
					t.Errorf("output claims an enqueue: %q", out)
				}
				assertNoLocalDB(t, filepath.Dir(dbPath))
			})
		}
	}
}

// Against a protected instance: a reader token is refused (exit 5), a
// writer token enqueues on the instance, and the job is in its queue.
func TestEnqueueRolesOnProtectedInstance(t *testing.T) {
	for _, tc := range enqueueCommands {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t, dpkmstest.WithStaticTokens())

			db.useRole(t, dpkmstest.RoleReader)
			pipeStdin(t, tc.stdin)
			_, err := executeCommand(tc.args...)
			assertKitExit(t, err, output.CodeUnauthorized, output.ExitUnauthorized)

			db.useRole(t, dpkmstest.RoleWriter)
			pipeStdin(t, tc.stdin)
			out, err := executeCommand(tc.args...)
			if err != nil {
				t.Fatalf("writer enqueue: %v\n%s", err, out)
			}
			m := jobIDRe.FindStringSubmatch(out)
			if m == nil {
				t.Fatalf("output lacks the Job ID: %q", out)
			}
			job, err := db.Driver.Jobs().Get(context.Background(), m[1])
			if err != nil {
				t.Fatalf("job %s is not in the instance's queue: %v", m[1], err)
			}
			if job.Payload != "offline insight" {
				t.Errorf("job payload = %q", job.Payload)
			}
		})
	}
}

// TestAnalyzeLiveDaemonLeavesLocalStorageUntouched: routed via the daemon API,
// the CLI must not open (or create) the local database at all.
func TestAnalyzeLiveDaemonLeavesLocalStorageUntouched(t *testing.T) {
	dbPath := tempDB(t)

	srv := startMockDPKMS(t)
	defer srv.Close()

	out, err := executeCommand(append([]string{"analyze", "routed content", "--server", srv.URL}, storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("analyze via live daemon: %v", err)
	}
	if !strings.Contains(out, "Job ID: job_12345678") {
		t.Errorf("output should carry the daemon's job ID: %q", out)
	}
	if _, statErr := os.Stat(dbPath); statErr == nil {
		t.Error("local database opened despite live daemon; write must route via API")
	}
}
