// Package testguard keeps test binaries away from the developer's real
// ctxt configuration and from any ctxt/dpkms server listening on the
// default local ports.
//
// Import it from test files only. A package TestMain calls [Main], which
//
//   - points HOME and the XDG base dirs at a throwaway directory, so neither
//     the in-process commands nor the binaries the tests spawn read the real
//     config, state or data;
//   - clears the CTXT_*, CH_* and DPKMS_* variables that can redirect config
//     loading, instance routing or storage;
//   - writes a user config whose server.url is [ClosedServerURL], so a
//     command that resolves "the configured server" with nothing else set
//     gets connection-refused instead of whatever listens on :8080;
//   - wraps http.DefaultTransport so a request to a default local server
//     port is refused before connecting, and fails the run when any was
//     attempted.
package testguard

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// ClosedServerURL is the server.url tests get when they configure nothing.
// Port 1 (tcpmux) is never served on a developer machine or CI runner, so
// any request fails fast with connection-refused.
const ClosedServerURL = "http://127.0.0.1:1"

// GuardedPorts are the local ports a real ctxt/dpkms server listens on:
// the built-in default (8080) and the conventional second instance (8081).
var GuardedPorts = []string{"8080", "8081"}

// clearedPrefixes name the env families that steer config, routing or
// storage: CTXT_CONFIG / CTXT_INSTANCE / CTXT_DATA_DIR / CTXT_PROFILE,
// CH_SERVER_PORT, DPKMS_DATA_DIR and friends.
var clearedPrefixes = []string{"CTXT_", "CH_", "DPKMS_"}

// goEnvKeys are pinned to their pre-isolation values so `go build` from a
// test keeps using the real module and build caches (and GOPRIVATE et al.
// from the go env file) after HOME moves.
var goEnvKeys = []string{"GOCACHE", "GOMODCACHE", "GOPATH", "GOENV"}

// Env abstracts the process environment so [Isolate] serves both TestMain
// (os.Setenv) and a single test (t.Setenv, restored on cleanup).
type Env struct {
	Setenv   func(key, value string) error
	Unsetenv func(key string) error
}

// OSEnv mutates the process environment directly; for TestMain.
func OSEnv() Env {
	return Env{Setenv: os.Setenv, Unsetenv: os.Unsetenv}
}

// TestingEnv mutates the environment for the duration of t. t.Setenv cannot
// unset, so a cleared variable is set to the empty string, which every
// reader in this repo treats as unset.
func TestingEnv(t testing.TB) Env {
	return Env{
		Setenv:   func(k, v string) error { t.Setenv(k, v); return nil },
		Unsetenv: func(k string) error { t.Setenv(k, ""); return nil },
	}
}

// Isolate points HOME and the XDG base dirs under root, clears the env that
// can redirect config or server routing, pins the Go toolchain caches, and
// writes a user config for each bin (e.g. "ctxt") whose server.url is
// [ClosedServerURL]. Tests that pass their own -c config or set their own
// XDG_CONFIG_HOME keep working: -c layers on top of the user config, and a
// replaced XDG_CONFIG_HOME replaces it.
func Isolate(env Env, root string, bins ...string) error {
	pinned := goEnv()

	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		for _, p := range clearedPrefixes {
			if strings.HasPrefix(k, p) {
				if err := env.Unsetenv(k); err != nil {
					return err
				}
				break
			}
		}
	}

	dirs := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
		"XDG_RUNTIME_DIR": filepath.Join(root, "run"),
	}
	for k, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("testguard: %w", err)
		}
		if err := env.Setenv(k, dir); err != nil {
			return err
		}
	}
	for k, v := range pinned {
		if err := env.Setenv(k, v); err != nil {
			return err
		}
	}

	return WriteUserConfig(dirs["XDG_CONFIG_HOME"], bins...)
}

// WriteUserConfig writes the guard's user config for each bin under
// configHome (an XDG_CONFIG_HOME): server.url is [ClosedServerURL]. Helpers
// that give a spawned binary its own XDG tree call it so the binary never
// falls back to the built-in default endpoint.
func WriteUserConfig(configHome string, bins ...string) error {
	cfgDir := filepath.Join(configHome, "contexthelp")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return fmt.Errorf("testguard: %w", err)
	}
	body := []byte("server:\n  url: " + ClosedServerURL + "\n")
	for _, bin := range bins {
		if err := os.WriteFile(filepath.Join(cfgDir, bin+".yaml"), body, 0o600); err != nil {
			return fmt.Errorf("testguard: %w", err)
		}
	}
	return nil
}

// goEnv reads the effective Go cache/env locations before HOME changes.
// Returns nil when no go binary is on PATH (the e2e tests skip then).
func goEnv() map[string]string {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil
	}
	out, err := exec.Command(goBin, append([]string{"env", "-json"}, goEnvKeys...)...).Output() // #nosec G204 -- fixed args
	if err != nil {
		return nil
	}
	vals := map[string]string{}
	if err := json.Unmarshal(out, &vals); err != nil {
		return nil
	}
	for k, v := range vals {
		if v == "" {
			delete(vals, k)
		}
	}
	return vals
}

// Guarded reports whether addr (host:port, as handed to a dialer) is a
// default local server port: a loopback or unspecified host on one of
// [GuardedPorts].
func Guarded(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	matched := false
	for _, p := range GuardedPorts {
		if port == p {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	switch strings.ToLower(host) {
	case "", "localhost":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// DialGuard refuses and records requests to guarded addresses.
type DialGuard struct {
	mu    sync.Mutex
	extra map[string]int
	trips []string
}

// Active is the guard [Main] installed; nil when none is installed.
var Active *DialGuard

// InstallDialGuard wraps http.DefaultTransport (already wrapped by kit's
// offline policy by the time a TestMain runs) so a request to a guarded
// address fails before any connection is made. Every http.Client without
// its own Transport (the ctxt/dpkms clients, http.Get, httptest clients)
// goes through it.
func InstallDialGuard() *DialGuard {
	g := &DialGuard{extra: map[string]int{}}
	http.DefaultTransport = &guardTransport{g: g, base: http.DefaultTransport}
	return g
}

type guardTransport struct {
	g    *DialGuard
	base http.RoundTripper
}

func (tr *guardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	addr := req.URL.Host
	if req.URL.Port() == "" {
		port := "80"
		if req.URL.Scheme == "https" {
			port = "443"
		}
		addr = net.JoinHostPort(req.URL.Hostname(), port)
	}
	if tr.g.refuse(addr) {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, fmt.Errorf("live-server guard: refused %s %s: tests must never reach a real local ctxt/dpkms server", req.Method, req.URL.Redacted())
	}
	return tr.base.RoundTrip(req)
}

func (g *DialGuard) refuse(addr string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.extra[addr] == 0 && !Guarded(addr) {
		return false
	}
	g.trips = append(g.trips, addr)
	fmt.Fprintf(os.Stderr, "live-server guard: refused request to %s from %s\n", addr, callingTest())
	return true
}

// callingTest names the Test/Benchmark function on the current stack, so a
// refusal points at its culprit even without -v.
func callingTest() string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	for {
		f, more := frames.Next()
		if i := strings.LastIndex(f.Function, "."); i >= 0 {
			if name := f.Function[i+1:]; strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") {
				return f.Function
			}
		}
		if !more {
			return "an unknown caller (request issued off the test goroutine)"
		}
	}
}

// Block additionally guards addr (host:port) until the returned release runs. Tests use
// it to prove the guard on a sentinel listener instead of a real server.
// Nil-safe: without an installed guard it blocks nothing.
func (g *DialGuard) Block(addr string) (release func()) {
	if g == nil {
		return func() {}
	}
	g.mu.Lock()
	g.extra[addr]++
	g.mu.Unlock()
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.extra[addr]--; g.extra[addr] <= 0 {
			delete(g.extra, addr)
		}
	}
}

// Take removes and counts the recorded refusals for addr, so a test that
// provoked them on purpose does not fail the run. Nil-safe.
func (g *DialGuard) Take(addr string) int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	kept := g.trips[:0]
	n := 0
	for _, a := range g.trips {
		if a == addr {
			n++
			continue
		}
		kept = append(kept, a)
	}
	g.trips = kept
	return n
}

// Report writes the refusals still recorded to w and returns their count.
func (g *DialGuard) Report(w io.Writer) int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.trips) == 0 {
		return 0
	}
	uniq := map[string]int{}
	for _, a := range g.trips {
		uniq[a]++
	}
	addrs := make([]string, 0, len(uniq))
	for a, n := range uniq {
		addrs = append(addrs, fmt.Sprintf("%s (x%d)", a, n))
	}
	sort.Strings(addrs)
	fmt.Fprintf(w, "\nFAIL: live-server guard refused %d request(s) to default local server ports: %s\n"+
		"A test fell back to the default endpoint and would have written to a real server.\n"+
		"Run with -v and look for \"live-server guard: refused request\" lines naming the test.\n",
		len(g.trips), strings.Join(addrs, ", "))
	return len(g.trips)
}

// Main isolates the process env for bins, installs the dial guard, runs
// setup (may be nil) and the tests, and returns the exit code: non-zero
// when isolation failed or any guarded request was attempted.
func Main(m *testing.M, setup func(), bins ...string) int {
	root, err := os.MkdirTemp("", "ctxt-testenv-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "testguard: %v\n", err)
		return 2
	}
	defer os.RemoveAll(root)

	if err := Isolate(OSEnv(), root, bins...); err != nil {
		fmt.Fprintf(os.Stderr, "testguard: %v\n", err)
		return 2
	}
	g := InstallDialGuard()
	Active = g
	if setup != nil {
		setup()
	}

	code := m.Run()
	if g.Report(os.Stderr) > 0 && code == 0 {
		code = 1
	}
	return code
}
