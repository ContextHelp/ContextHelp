package cmd

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// sentinel is a stand-in for a real local server: it answers like a dpkms
// instance and counts every connection it accepts. Guard tests point code
// at it instead of the real default port, so a broken guard shows up as a
// sentinel hit and never as a write to a real knowledge base.
type sentinel struct {
	url   string
	addr  string
	conns atomic.Int64
}

func newSentinel(t *testing.T) *sentinel {
	t.Helper()
	s := &sentinel{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/analyze" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":"sentinel_job"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	srv.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			s.conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	s.url = srv.URL
	s.addr = srv.Listener.Addr().String()
	return s
}

// TestLiveServerGuardDefaultEndpointIsClosedPort: with nothing configured,
// the endpoint every server-bound command resolves is the closed port, not
// the built-in loopback :8080 a developer's real server listens on.
func TestLiveServerGuardDefaultEndpointIsClosedPort(t *testing.T) {
	prevCfg, prevFile := cfg, cfgFile
	t.Cleanup(func() { cfg, cfgFile = prevCfg, prevFile })
	resetAllFlags(rootCmd)

	initConfig()
	if initConfigErr != nil {
		t.Fatalf("initConfig: %v", initConfigErr)
	}
	eps := clientEndpoints()
	if len(eps) != 1 || eps[0].URL != testguard.ClosedServerURL {
		t.Fatalf("clientEndpoints() = %+v; want only %s (tests must not resolve %s)",
			eps, testguard.ClosedServerURL, idxbridge.DefaultBaseURL)
	}
}

// TestLiveServerGuardCoversDefaultPorts pins which addresses the dial guard
// refuses: every spelling of the built-in default and the second local
// instance, and nothing else.
func TestLiveServerGuardCoversDefaultPorts(t *testing.T) {
	def, err := url.Parse(idxbridge.DefaultBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{
		def.Host, "127.0.0.1:8080", "localhost:8080", "[::1]:8080", "0.0.0.0:8080", ":8080",
		"127.0.0.1:8081", "localhost:8081",
	} {
		if !testguard.Guarded(addr) {
			t.Errorf("Guarded(%q) = false; want true", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:1", "127.0.0.1:18080", "example.com:8080", "10.0.0.5:8080"} {
		if testguard.Guarded(addr) {
			t.Errorf("Guarded(%q) = true; want false", addr)
		}
	}
}

// TestLiveServerGuardRefusesGuardedDial: http.DefaultTransport, which every
// ctxt HTTP client uses, refuses a guarded address before connecting.
func TestLiveServerGuardRefusesGuardedDial(t *testing.T) {
	s := newSentinel(t)
	defer testguard.Active.Block(s.addr)()

	resp, err := http.Get(s.url + "/health")
	if resp != nil {
		_ = resp.Body.Close()
	}
	trips := testguard.Active.Take(s.addr)
	if n := s.conns.Load(); n != 0 {
		t.Fatalf("sentinel accepted %d connection(s); the dial guard is not on http.DefaultTransport", n)
	}
	if err == nil || !strings.Contains(err.Error(), "live-server guard") {
		t.Errorf("err = %v; want the live-server guard refusal", err)
	}
	if trips == 0 {
		t.Error("guard recorded no refusal for the blocked address")
	}
}

// TestLiveServerGuardCoversAnalyzeRouting: the analyze write path (the one
// that once posted test content into a real knowledge base when routing fell
// through to the default endpoint) dials through the guarded transport.
func TestLiveServerGuardCoversAnalyzeRouting(t *testing.T) {
	s := newSentinel(t)
	defer testguard.Active.Block(s.addr)()

	dbPath := tempDB(t)
	_, _ = executeCommand(append([]string{"analyze", "guard probe", "--server", s.url}, storageOverride(dbPath)...)...)
	trips := testguard.Active.Take(s.addr)
	if n := s.conns.Load(); n != 0 {
		t.Fatalf("sentinel accepted %d connection(s) from analyze; its HTTP client bypasses the dial guard", n)
	}
	if trips == 0 {
		t.Error("analyze never dialed the pinned server; the test proves nothing")
	}
}

// TestLiveServerGuardIsolatesSpawnedBinary: a built ctxt binary inherits the
// test process env. On a machine whose real config routes to a live server
// (simulated with the sentinel), the isolated env keeps the binary off it.
func TestLiveServerGuardIsolatesSpawnedBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	bin := e2eBinary(t)
	s := newSentinel(t)

	real := t.TempDir()
	cfgDir := filepath.Join(real, "config", "contexthelp")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	realCfg := filepath.Join(cfgDir, "ctxt.yaml")
	if err := os.WriteFile(realCfg, []byte("server:\n  url: "+s.url+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", real)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(real, "config"))
	t.Setenv("CTXT_CONFIG", realCfg)

	capture := func() string {
		t.Helper()
		cmd := exec.Command(bin, "capture", "guard probe")
		cmd.Env = append(os.Environ(), "CTXT_NO_CLIPBOARD=1")
		out, _ := cmd.CombinedOutput()
		return string(out)
	}

	// Control: the simulated real env does route the binary to the sentinel,
	// so the assertion below cannot pass vacuously.
	if out := capture(); s.conns.Load() == 0 {
		t.Fatalf("control: binary never reached the sentinel under the simulated real config: %s", out)
	}
	before := s.conns.Load()

	if err := testguard.Isolate(testguard.TestingEnv(t), t.TempDir(), "ctxt"); err != nil {
		t.Fatalf("Isolate: %v", err)
	}
	out := capture()
	if n := s.conns.Load() - before; n != 0 {
		t.Fatalf("isolated binary opened %d connection(s) to the simulated real server: %s", n, out)
	}
}

// TestLiveServerGuardUserConfigNotRewritten: loading config with a -c
// overlay leaves the guard's shared user config untouched, so no test's
// server or storage settings leak into the tests after it.
func TestLiveServerGuardUserConfigNotRewritten(t *testing.T) {
	userCfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "contexthelp", "ctxt.yaml")
	before, err := os.ReadFile(userCfg)
	if err != nil {
		t.Fatalf("guard user config: %v", err)
	}
	if want := "server:\n  url: " + testguard.ClosedServerURL + "\n"; string(before) != want {
		t.Fatalf("guard user config = %q; want only server.url (%q)", before, want)
	}
	cfgArgs := serverURLsConfig(t, "http://127.0.0.1:19997")
	if _, err := executeCommand(append([]string{"config", "paths"}, cfgArgs...)...); err != nil {
		t.Fatalf("config paths: %v", err)
	}
	after, err := os.ReadFile(userCfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("loading config rewrote the shared user config:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
