//go:build unix

package testutil

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil, "ctxt", "dpkms"))
}

func call(t *testing.T, d *Dpkms, method, path, role string, body string) int {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = bytes.NewBufferString(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, d.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := d.Token(role); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// alive reports whether a process or (negative pid) process group exists.
func alive(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// TestStartDpkmsServesProtectedAPI: the built daemon serves the API on an
// ephemeral loopback URL behind its static tokens.
func TestStartDpkmsServesProtectedAPI(t *testing.T) {
	d := StartDpkms(t)
	if !strings.HasPrefix(d.URL, "http://127.0.0.1:") || testguard.Guarded(strings.TrimPrefix(d.URL, "http://")) {
		t.Fatalf("URL = %q; want an ephemeral, unguarded loopback port", d.URL)
	}
	if code := call(t, d, http.MethodGet, "/api/v1/objects", "", ""); code != http.StatusUnauthorized {
		t.Errorf("no token: %d, want 401", code)
	}
	for _, role := range dpkmstest.Roles {
		if code := call(t, d, http.MethodGet, "/api/v1/objects", role, ""); code != http.StatusOK {
			t.Errorf("%s token: %d, want 200", role, code)
		}
	}
	if code := call(t, d, http.MethodPost, "/api/v1/analyze", dpkmstest.RoleAdmin, `{"content":"e2e launcher probe","type":"text"}`); code != http.StatusAccepted {
		t.Errorf("POST /analyze as admin: %d, want 202", code)
	}
}

// TestStartDpkmsIsHermetic: the daemon's state, config and ports stay in
// its own tree and off the default ports; no inherited bus token or
// routing env reaches it.
func TestStartDpkmsIsHermetic(t *testing.T) {
	t.Setenv("BUS_TOKEN", "inherited-bus-token")
	t.Setenv("CTXT_INSTANCE", "inherited-instance")
	d := StartDpkms(t)

	for _, port := range []int{d.Instance.Port, d.Instance.GRPCPort} {
		if port == 0 || port == 8080 || port == 8081 || port == 9090 || port == 9091 {
			t.Errorf("instance port %d: want an ephemeral port", port)
		}
	}
	if !strings.HasPrefix(d.Instance.DBPath, d.Root) {
		t.Errorf("database %s outside the instance root %s", d.Instance.DBPath, d.Root)
	}
	if st, err := os.Stat(d.ConfigPath); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("config %s: %v, mode %v; want 0600", d.ConfigPath, err, st.Mode().Perm())
	}
	for _, kv := range d.Env {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case k == "BUS_TOKEN" || k == "CTXT_INSTANCE":
			t.Errorf("inherited %s reached the daemon", k)
		case k == "HOME" || strings.HasPrefix(k, "XDG_"):
			if !strings.HasPrefix(v, d.Root) {
				t.Errorf("%s=%s outside the instance root", k, v)
			}
		}
	}
}

// TestStopReapsProcessGroup: cleanup kills the daemon's whole process
// group, not just the leader.
func TestStopReapsProcessGroup(t *testing.T) {
	d := StartDpkms(t)
	pgid := d.cmd.Process.Pid
	if !alive(-pgid) {
		t.Fatalf("process group %d not found; the daemon is not its own group leader", pgid)
	}
	if err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !d.Exited() || alive(pgid) || alive(-pgid) {
		t.Fatalf("after Stop: exited=%v leader alive=%v group alive=%v", d.Exited(), alive(pgid), alive(-pgid))
	}
}

// TestFailedStartReapsProcess: a daemon that never becomes ready is killed
// before start-up reports the failure; no process outlives it.
func TestFailedStartReapsProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	d, err := startDpkms(t, withReadyTimeout(time.Nanosecond))
	if err == nil {
		t.Fatal("start with a 1ns readiness budget: want an error")
	}
	if d == nil || d.cmd == nil {
		t.Fatalf("start failed before spawning: %v", err)
	}
	if !d.Exited() || alive(-d.cmd.Process.Pid) {
		t.Fatalf("failed start left the daemon running (exited=%v)", d.Exited())
	}
}

// TestWriteCtxtConfigRoutesClient: the client config lands in the
// instance's XDG tree with the role's token.
func TestWriteCtxtConfigRoutesClient(t *testing.T) {
	d := StartDpkms(t)
	path := d.WriteCtxtConfig(t, dpkmstest.RoleReader)
	if want := filepath.Join(d.Root, "config", "contexthelp", "ctxt.yaml"); path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{d.URL, d.Token(dpkmstest.RoleReader)} {
		if !strings.Contains(string(body), want) {
			t.Errorf("ctxt config lacks %q:\n%s", want, body)
		}
	}
}
