package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// recordedDpkms is an httptest dpkms answering /healthz (healthy) and the
// pipeline list, recording each request's Authorization header.
type recordedDpkms struct {
	*httptest.Server
	mu    sync.Mutex
	auths []string
}

func newRecordedDpkms(t *testing.T) *recordedDpkms {
	t.Helper()
	rd := &recordedDpkms{}
	rd.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rd.mu.Lock()
		rd.auths = append(rd.auths, r.Header.Get("Authorization"))
		rd.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(w).Encode(HealthzEnvelope{Health: "healthy", Checks: HealthzChecks{DB: HealthzDBCheck{Status: "ok"}}})
		case "/api/v1/pipelines":
			_ = json.NewEncoder(w).Encode(map[string]any{"pipelines": []any{}, "total": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(rd.Close)
	return rd
}

func (rd *recordedDpkms) hits() []string {
	rd.mu.Lock()
	defer rd.mu.Unlock()
	return append([]string(nil), rd.auths...)
}

// appendDpkmsConfig adds YAML to the test config file.
func appendDpkmsConfig(t *testing.T, db *testDB, yaml string) {
	t.Helper()
	f, err := os.OpenFile(db.ConfigPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
}

func assertDpkmsAuthorized(t *testing.T, hits []string, want string) {
	t.Helper()
	if len(hits) == 0 {
		t.Fatal("server got no requests")
	}
	for i, got := range hits {
		if got != want {
			t.Fatalf("request %d Authorization = %q, want %q", i, got, want)
		}
	}
}

// The dpkms client commands: healthcheck and the pipeline client.
var dpkmsClientCommands = [][]string{
	{"healthcheck", "--quiet"},
	{"pipeline", "list"},
}

// Without --server-url the client commands reach the configured
// server.url with the configured token, never the built-in default.
func TestDpkmsClients_UseConfiguredServerAndToken(t *testing.T) {
	for _, args := range dpkmsClientCommands {
		t.Run(args[0], func(t *testing.T) {
			srv := newRecordedDpkms(t)
			db := setupTestDB(t)
			appendDpkmsConfig(t, db, "server:\n  url: "+srv.URL+"\n  token: tok-cfg\n")

			if out, err := db.run(args...); err != nil {
				t.Fatalf("dpkms %v: %v\n%s", args, err, out)
			}
			assertDpkmsAuthorized(t, srv.hits(), "Bearer tok-cfg")
		})
	}
}

// The primary of server.urls wins over server.url, with its own token.
func TestDpkmsClients_UseServerURLsPrimary(t *testing.T) {
	for _, args := range dpkmsClientCommands {
		t.Run(args[0], func(t *testing.T) {
			primary := newRecordedDpkms(t)
			single := newRecordedDpkms(t)
			db := setupTestDB(t)
			appendDpkmsConfig(t, db, "server:\n  url: "+single.URL+"\n  token: tok-default\n"+
				"  urls:\n    - url: "+primary.URL+"\n      token: tok-primary\n")

			if out, err := db.run(args...); err != nil {
				t.Fatalf("dpkms %v: %v\n%s", args, err, out)
			}
			assertDpkmsAuthorized(t, primary.hits(), "Bearer tok-primary")
			if n := len(single.hits()); n != 0 {
				t.Fatalf("server.url got %d requests while server.urls is set, want 0", n)
			}
		})
	}
}

// An explicit --server-url overrides the configured server.
func TestDpkmsClients_ServerURLFlagOverridesConfig(t *testing.T) {
	for _, args := range dpkmsClientCommands {
		t.Run(args[0], func(t *testing.T) {
			configured := newRecordedDpkms(t)
			pinned := newRecordedDpkms(t)
			db := setupTestDB(t)
			appendDpkmsConfig(t, db, "server:\n  url: "+configured.URL+"\n  token: tok-cfg\n")

			if out, err := db.run(append([]string{"--server-url", pinned.URL}, args...)...); err != nil {
				t.Fatalf("dpkms --server-url %v: %v\n%s", args, err, out)
			}
			if n := len(configured.hits()); n != 0 {
				t.Fatalf("configured server got %d requests, want 0 with --server-url", n)
			}
			assertDpkmsAuthorized(t, pinned.hits(), "Bearer tok-cfg")
		})
	}
}

// --server-url has no hard-coded default: an unset flag must not mask
// the configured server.
func TestDpkmsServerURLFlagHasNoDefault(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("server-url")
	if f == nil {
		t.Fatal("--server-url not registered")
	}
	if f.DefValue != "" {
		t.Fatalf("--server-url default = %q, want empty (config decides)", f.DefValue)
	}
}

var (
	dpkmsBinPath string
	dpkmsBinErr  error
	dpkmsBinOnce sync.Once
)

// dpkmsBinary builds dpkms once per test run.
func dpkmsBinary(t *testing.T) string {
	t.Helper()
	dpkmsBinOnce.Do(func() {
		if _, err := exec.LookPath("go"); err != nil {
			dpkmsBinErr = errors.New("go binary not on PATH")
			return
		}
		dir, err := os.MkdirTemp("", "dpkms-e2e-")
		if err != nil {
			dpkmsBinErr = err
			return
		}
		_, file, _, _ := runtime.Caller(0)
		repo := filepath.Join(filepath.Dir(file), "..", "..", "..")
		bin := filepath.Join(dir, "dpkms")
		cmd := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", bin, "./cmd/dpkms") // #nosec G204 -- fixed args
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			dpkmsBinErr = fmt.Errorf("go build: %w\n%s", err, out)
			return
		}
		dpkmsBinPath = bin
	})
	if dpkmsBinErr != nil {
		t.Skipf("e2e: %v", dpkmsBinErr)
	}
	return dpkmsBinPath
}

func runBuiltDpkms(t *testing.T, args ...string) (int, string) {
	t.Helper()
	out, err := exec.Command(dpkmsBinary(t), args...).CombinedOutput() // #nosec G204 -- test binary
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	default:
		t.Fatalf("run dpkms %v: %v", args, err)
		return -1, ""
	}
}

// On the built binary, healthcheck probes the configured server (the
// guard's closed port, or a -c overlay) and the configured token reaches
// it; --server-url overrides both.
func TestE2EDpkmsHealthcheckUsesConfiguredServer(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	srv := newRecordedDpkms(t)
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"configured server down", []string{"healthcheck", "--quiet"}, 6}, // TRANSIENT
		{"-c server.url", []string{"-c", "server.url=" + srv.URL, "-c", "server.token=tok-e2e", "healthcheck", "--quiet"}, 0},
		{"--server-url over -c", []string{"-c", "server.url=" + testguard.ClosedServerURL, "--server-url", srv.URL, "healthcheck", "--quiet"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, out := runBuiltDpkms(t, tc.args...); got != tc.want {
				t.Fatalf("dpkms %v: exit %d, want %d\n%s", tc.args, got, tc.want, out)
			}
		})
	}
	hits := srv.hits()
	if len(hits) != 2 || hits[0] != "Bearer tok-e2e" {
		t.Fatalf("server saw Authorization %q, want the -c token first, then one --server-url probe", hits)
	}
}
