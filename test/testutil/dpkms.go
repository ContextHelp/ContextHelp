//go:build unix

package testutil

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// Dpkms is a built `dpkms serve` running for one test in a hermetic
// HOME/XDG tree, on ephemeral ports, as the leader of its own process
// group. StartDpkms registers Stop as a test cleanup.
type Dpkms struct {
	// URL is the HTTP base URL, http://127.0.0.1:<port>.
	URL string
	// Instance is the pidfile the process wrote: actual HTTP, gRPC and
	// cookie-bridge ports, instance name, database path.
	Instance pidfile.Info
	// Root holds HOME, the XDG dirs, the database and the process log.
	Root string
	// Env is the hermetic environment the process runs with. Reuse it to
	// run a built ctxt against the instance (see WriteCtxtConfig).
	Env []string
	// ConfigPath is the instance's dpkms config (mode 0600).
	ConfigPath string

	tokens  map[string]string
	logPath string
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
	stop    sync.Once
	stopErr error
}

// Token returns the bearer token for role (dpkmstest.RoleAdmin,
// RoleWriter, RoleReader); "" on a private instance.
func (d *Dpkms) Token(role string) string { return d.tokens[role] }

// Output returns what the process wrote to stdout and stderr so far.
func (d *Dpkms) Output() string {
	b, _ := os.ReadFile(d.logPath)
	return string(b)
}

// Exited reports whether the process has been reaped.
func (d *Dpkms) Exited() bool {
	select {
	case <-d.done:
		return true
	default:
		return false
	}
}

// Stop sends SIGTERM to the process group, waits for the leader to exit,
// and escalates to SIGKILL on the group after a grace period. Idempotent.
func (d *Dpkms) Stop() error {
	d.stop.Do(func() { d.stopErr = d.killGroup(10 * time.Second) })
	return d.stopErr
}

func (d *Dpkms) killGroup(grace time.Duration) error {
	pgid := d.cmd.Process.Pid
	signal := func(sig syscall.Signal) error {
		err := syscall.Kill(-pgid, sig)
		if errors.Is(err, syscall.ESRCH) {
			// No such group (the leader is gone, or never led one):
			// still make sure the leader itself is signaled.
			_ = d.cmd.Process.Signal(sig)
			return nil
		}
		return err
	}
	if err := signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("dpkms: SIGTERM process group %d: %w", pgid, err)
	}
	select {
	case <-d.done:
	case <-time.After(grace):
		_ = signal(syscall.SIGKILL)
		select {
		case <-d.done:
		case <-time.After(grace):
			return fmt.Errorf("dpkms: pid %d survived SIGKILL", pgid)
		}
	}
	// Reap stragglers the leader left in its group.
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("dpkms: SIGKILL process group %d: %w", pgid, err)
	}
	return nil
}

// WriteCtxtConfig writes the user-level ctxt config in the instance's
// hermetic XDG tree, routing ctxt to URL with role's token, and returns
// its path. A ctxt binary run with Env then talks to this instance.
func (d *Dpkms) WriteCtxtConfig(t testing.TB, role string) string {
	t.Helper()
	server := map[string]any{"url": d.URL}
	if tok := d.Token(role); tok != "" {
		server["token"] = tok
	}
	path := filepath.Join(d.Root, "config", "contexthelp", "ctxt.yaml")
	writeYAML(t, path, map[string]any{"server": server})
	return path
}

// DpkmsOption configures StartDpkms.
type DpkmsOption func(*dpkmsOptions)

type dpkmsOptions struct {
	private      bool
	binary       string
	readyTimeout time.Duration
	config       map[string]any
}

// PrivateDpkms starts a private (loopback, unauthenticated) instance
// instead of the default protected one.
func PrivateDpkms() DpkmsOption { return func(o *dpkmsOptions) { o.private = true } }

// WithDpkmsBinary runs bin instead of a dpkms built from this checkout.
func WithDpkmsBinary(bin string) DpkmsOption { return func(o *dpkmsOptions) { o.binary = bin } }

// WithDpkmsConfig merges top-level sections into the generated config,
// replacing the section of the same name.
func WithDpkmsConfig(sections map[string]any) DpkmsOption {
	return func(o *dpkmsOptions) {
		for k, v := range sections {
			o.config[k] = v
		}
	}
}

func withReadyTimeout(d time.Duration) DpkmsOption {
	return func(o *dpkmsOptions) { o.readyTimeout = d }
}

// StartDpkms builds dpkms from this checkout (once per test binary) and
// starts `dpkms serve` for the test:
//
//   - HOME and every XDG dir under a fresh root, CTXT_/CH_/DPKMS_ and
//     BUS_TOKEN cleared from the inherited env;
//   - a protected config (mode 0600) with one static token per role
//     (admin, writer, reader), every provider backend the stub and the
//     embedding provider a stub on a closed port;
//   - HTTP and gRPC on free ephemeral ports (never the 8080/9090
//     defaults), actual ports read back from the instance's pidfile;
//   - its own process group, killed on cleanup and whenever start-up
//     fails.
//
// A protected instance binds 0.0.0.0, as dpkms serve does for every
// non-private access class; tests reach it on 127.0.0.1. Skipped under
// -short. The test package must run through testguard.
func StartDpkms(t testing.TB, opts ...DpkmsOption) *Dpkms {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	d, err := startDpkms(t, opts...)
	if err != nil {
		t.Fatalf("start dpkms: %v", err)
	}
	return d
}

func startDpkms(t testing.TB, opts ...DpkmsOption) (*Dpkms, error) {
	t.Helper()
	if testguard.Active == nil {
		return nil, errors.New("the test package must run through testguard: " +
			"func TestMain(m *testing.M) { os.Exit(testguard.Main(m, nil, \"ctxt\", \"dpkms\")) }")
	}
	o := dpkmsOptions{readyTimeout: 60 * time.Second, config: map[string]any{}}
	for _, opt := range opts {
		opt(&o)
	}
	bin := o.binary
	if bin == "" {
		var err error
		if bin, err = dpkmsBinary(); err != nil {
			return nil, err
		}
	}

	root := t.TempDir()
	d := &Dpkms{Root: root, tokens: map[string]string{}, logPath: filepath.Join(root, "dpkms.log"), done: make(chan struct{})}
	dirs := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
		"XDG_CACHE_HOME":  filepath.Join(root, "cache"),
		"XDG_RUNTIME_DIR": filepath.Join(root, "run"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	busToken, err := randomHex()
	if err != nil {
		return nil, err
	}
	d.Env = hermeticEnv(dirs, map[string]string{
		"DPKMS_BUS_TOKEN": busToken,
		// The env layer outranks a model's registry entry, so neither
		// ingest nor queries can reach a real model server.
		embeddings.EnvProvider: embeddings.BackendStub,
		embeddings.EnvEndpoint: testguard.ClosedServerURL,
	})
	// ctxt run with Env never falls back to the default endpoint.
	if err := testguard.WriteUserConfig(dirs["XDG_CONFIG_HOME"], "ctxt"); err != nil {
		return nil, err
	}

	cfg, err := d.config(o, dirs["XDG_DATA_HOME"])
	if err != nil {
		return nil, err
	}
	d.ConfigPath = filepath.Join(dirs["XDG_CONFIG_HOME"], "contexthelp", "dpkms.yaml")
	writeYAML(t, d.ConfigPath, cfg)

	ports, err := freePorts(2)
	if err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(d.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	d.cmd = exec.Command(bin, "serve", // #nosec G204 -- test helper runs the dpkms built from this checkout
		"--port", strconv.Itoa(ports[0]), "--grpc-port", strconv.Itoa(ports[1]), "--workers", "1")
	d.cmd.Dir = root
	d.cmd.Env = d.Env
	d.cmd.Stdout = logFile
	d.cmd.Stderr = logFile
	d.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := d.cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn %s: %w", bin, err)
	}
	go func() {
		d.waitErr = d.cmd.Wait()
		close(d.done)
	}()
	t.Cleanup(func() {
		if err := d.Stop(); err != nil {
			t.Errorf("%v", err)
		}
	})

	if err := d.awaitReady(filepath.Join(dirs["XDG_DATA_HOME"], "contexthelp", "run"), o.readyTimeout); err != nil {
		_ = d.Stop()
		return d, fmt.Errorf("%w\n--- dpkms output ---\n%s", err, d.Output())
	}
	return d, nil
}

// config builds the instance's dpkms config.
func (d *Dpkms) config(o dpkmsOptions, dataHome string) (map[string]any, error) {
	providers := map[string]any{}
	for _, k := range []string{"video", "document", "ocr", "transcription", "vision", "diarization", "llm"} {
		providers[k] = map[string]any{"backend": "stub"}
	}
	server := map[string]any{"access": "private"}
	if !o.private {
		entries := make([]map[string]any, 0, len(dpkmstest.Roles))
		for _, role := range dpkmstest.Roles {
			tok, err := randomHex()
			if err != nil {
				return nil, err
			}
			d.tokens[role] = "e2e-" + role + "-" + tok
			entries = append(entries, map[string]any{
				"token": d.tokens[role], "principal": "e2e-" + role, "roles": []string{role},
			})
		}
		server = map[string]any{
			"access": "protected",
			"auth":   map[string]any{"provider": "static", "static": map[string]any{"tokens": entries}},
		}
	}
	cfg := map[string]any{
		"storage":   map[string]any{"type": "sqlite", "path": filepath.Join(dataHome, "dpkms.db")},
		"server":    server,
		"providers": providers,
		"browser":   map[string]any{"enabled": false},
		"jobs":      map[string]any{"drain_timeout": "2s"},
	}
	for k, v := range o.config {
		cfg[k] = v
	}
	return cfg, nil
}

// awaitReady waits for the process's pidfile (written once its HTTP
// listener is bound), then for /health on the port it names. A process
// that exits first fails start-up at once.
func (d *Dpkms) awaitReady(runDir string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		if d.Exited() {
			return fmt.Errorf("dpkms serve exited before it was ready: %w", d.waitErr)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("dpkms serve not ready within %s", timeout)
		}
		if d.URL == "" {
			if infos, err := pidfile.Scan(runDir); err == nil {
				for _, info := range infos {
					if info.PID == d.cmd.Process.Pid {
						d.Instance = info
						d.URL = "http://127.0.0.1:" + strconv.Itoa(info.Port)
					}
				}
			}
		}
		if d.URL != "" {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, d.URL+"/health", nil)
			if resp, err := client.Do(req); err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// hermeticEnv is the inherited env minus everything that steers config,
// routing, storage or the bus, plus dirs and extra.
func hermeticEnv(dirs, extra map[string]string) []string {
	drop := func(k string) bool {
		if _, ok := dirs[k]; ok || k == "BUS_TOKEN" || strings.HasPrefix(k, "XDG_") {
			return true
		}
		for _, p := range []string{"CTXT_", "CH_", "DPKMS_"} {
			if strings.HasPrefix(k, p) {
				return true
			}
		}
		return false
	}
	var env []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !drop(k) {
			env = append(env, kv)
		}
	}
	for _, m := range []map[string]string{dirs, extra} {
		for k, v := range m {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// freePorts returns n distinct free ports on 127.0.0.1, none of them a
// guarded default. The listeners are held until all n are chosen.
func freePorts(n int) ([]int, error) {
	lns := make([]net.Listener, 0, n)
	defer func() {
		for _, ln := range lns {
			_ = ln.Close()
		}
	}()
	ports := make([]int, 0, n)
	for len(ports) < n {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("free port: %w", err)
		}
		lns = append(lns, ln)
		if testguard.Guarded(ln.Addr().String()) {
			continue
		}
		addr, ok := ln.Addr().(*net.TCPAddr)
		if !ok {
			return nil, fmt.Errorf("free port: unexpected address %s", ln.Addr())
		}
		ports = append(ports, addr.Port)
	}
	return ports, nil
}

func writeYAML(t testing.TB, path string, v any) {
	t.Helper()
	body, err := yaml.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func randomHex() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

var (
	dpkmsBinOnce sync.Once
	dpkmsBinPath string
	dpkmsBinErr  error
)

// dpkmsBinary builds ./cmd/dpkms (fts5) once per test binary into the
// guard's throwaway cache dir, so a stale bin/dpkms never stands in for
// the code under test.
func dpkmsBinary() (string, error) {
	dpkmsBinOnce.Do(func() {
		root, err := projectRoot()
		if err != nil {
			dpkmsBinErr = err
			return
		}
		dir, err := os.MkdirTemp(os.Getenv("XDG_CACHE_HOME"), "dpkms-e2e-")
		if err != nil {
			dpkmsBinErr = err
			return
		}
		bin := filepath.Join(dir, "dpkms")
		build := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", bin, "./cmd/dpkms") // #nosec G204 G702 -- fixed args; bin is a fresh temp path
		build.Dir = root
		build.Env = append(os.Environ(), "CGO_ENABLED=1")
		if out, err := build.CombinedOutput(); err != nil {
			dpkmsBinErr = fmt.Errorf("go build ./cmd/dpkms: %w\n%s", err, out)
			return
		}
		dpkmsBinPath = bin
	})
	return dpkmsBinPath, dpkmsBinErr
}
