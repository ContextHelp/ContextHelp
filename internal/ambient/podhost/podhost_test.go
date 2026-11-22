package podhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/podrun"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	testguard.Main(m, nil, "ctxt", "dpkms")
}

type fakeProc struct {
	done chan error
	once sync.Once
}

func newFakeProc() *fakeProc { return &fakeProc{done: make(chan error, 1)} }
func (p *fakeProc) PID() int { return 9001 }
func (p *fakeProc) Wait() error {
	return <-p.done
}
func (p *fakeProc) Close() error {
	p.once.Do(func() {
		p.done <- nil
		close(p.done)
	})
	return nil
}

type scriptRunner struct {
	mu    sync.Mutex
	calls [][]string
	run   func(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Result, error)
	start func(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Proc, error)
}

func (s *scriptRunner) Run(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Result, error) {
	s.mu.Lock()
	s.calls = append(s.calls, append([]string(nil), argv...))
	fn := s.run
	s.mu.Unlock()
	return fn(ctx, argv, opts)
}

func (s *scriptRunner) Start(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Proc, error) {
	s.mu.Lock()
	s.calls = append(s.calls, append([]string(nil), argv...))
	fn := s.start
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx, argv, opts)
	}
	return newFakeProc(), nil
}

const fullCapsJSON = `{"tool":"pod","version":"dev","schema_version":"1.0","leaves":[` +
	`{"path":"pod show"},{"path":"pod start"},{"path":"pod stop"},` +
	`{"path":"pod exec"},{"path":"pod forward"},{"path":"pod upload"}]}`

func TestValidateEndpointF18(t *testing.T) {
	// Explicit daemon.pod.server_url is exempt from F18 (topology C).
	got, err := ValidateEndpoint("http://127.0.0.1:8080", "http://127.0.0.1:19000")
	if err != nil || got != "http://127.0.0.1:8080" {
		t.Fatalf("explicit server_url: got (%q, %v)", got, err)
	}

	// Empty server_url with loopback laptop endpoint refused (exit 2).
	for _, loop := range []string{"http://127.0.0.1:8080", "http://localhost:8080", "http://[::1]:8080", ""} {
		_, err := ValidateEndpoint("", loop)
		var oe *output.Error
		if !errors.As(err, &oe) || oe.ExitCode != 2 {
			t.Errorf("ValidateEndpoint('', %q): want exit 2, got %v", loop, err)
		}
	}

	// Remote laptop endpoint accepted when server_url empty (topology A).
	got, err = ValidateEndpoint("", "https://dpkms.example.net:8443")
	if err != nil || got != "https://dpkms.example.net:8443" {
		t.Fatalf("remote endpoint: got (%q, %v)", got, err)
	}
}

func TestRenderPodConfigAndLaunchScript(t *testing.T) {
	raw, err := RenderPodConfig("https://dpkms.example.net", "tok-123", "work")
	if err != nil {
		t.Fatalf("RenderPodConfig: %v", err)
	}
	s := string(raw)
	if strings.Contains(s, "daemon:") {
		t.Fatalf("rendered pod config must not contain daemon.*: %s", s)
	}
	if !strings.Contains(s, "https://dpkms.example.net") || !strings.Contains(s, "work") {
		t.Fatalf("rendered pod config missing endpoint/profile: %s", s)
	}

	script, err := BuildLaunchScript("ctxd", "dev-pod", "/tmp/ctxt.yaml", "work", []string{"filewatch"}, []string{"clipboard"})
	if err != nil {
		t.Fatalf("BuildLaunchScript: %v", err)
	}
	for _, want := range []string{"XDG_STATE_HOME", "ctxd.launch", "setsid nohup", "--launcher 'pod dev-pod'", "--input 'filewatch'", "--skip 'clipboard'"} {
		if !strings.Contains(script, want) {
			t.Errorf("launch script missing %q: %s", want, script)
		}
	}

	// P9 rejects shell metacharacters.
	if _, err := BuildLaunchScript("ctxd;id", "dev-pod", "/tmp/ctxt.yaml", "", nil, nil); err == nil {
		t.Fatal("expected P9 rejection for bad ctxd_bin")
	}
}

type lifecycleFixture struct {
	srv          *httptest.Server
	srvPort      int
	launched     bool
	uploadedPath string
	podStopped   bool
	stoppedMu    sync.Mutex
	stopped      bool
	launchSec    int64
	runJSON      []byte
}

func newLifecycleFixture(t *testing.T, token string) *lifecycleFixture {
	t.Helper()
	f := &lifecycleFixture{launchSec: time.Now().Unix()}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"running": true, "sources": []string{"filewatch"}})
		case "/v1/stop":
			f.stoppedMu.Lock()
			f.stopped = true
			f.stoppedMu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"stopping": true})
		case "/v1/buffer/flush":
			_ = json.NewEncoder(w).Encode(map[string]any{"flushed": 3})
		case "/v1/tail":
			_, _ = w.Write([]byte("event-1\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	f.srvPort = f.srv.Listener.Addr().(*net.TCPAddr).Port
	f.runJSON, _ = json.Marshal(RunInfo{
		PID:       4321,
		StartedAt: time.Unix(f.launchSec+1, 0).UTC(),
		Launcher:  "pod dev-pod",
		Control:   fmt.Sprintf("127.0.0.1:%d", f.srvPort),
		Token:     token,
	})
	return f
}

func (f *lifecycleFixture) handleExec(joined string) (podrun.Result, error) {
	switch {
	case strings.Contains(joined, "--version"):
		return podrun.Result{Stdout: []byte("ctxd v0.1.0\n")}, nil
	case strings.Contains(joined, "setsid nohup"):
		f.launched = true
		return podrun.Result{}, nil
	case strings.Contains(joined, "MARKER:"):
		if !f.launched {
			return podrun.Result{Stdout: []byte("STATE_DIR:/s\nMARKER:0\n")}, nil
		}
		return podrun.Result{Stdout: []byte(fmt.Sprintf("STATE_DIR:/home/u/.local/state/ctxt/ambient\nMARKER:%d\n%s\n", f.launchSec, string(f.runJSON)))}, nil
	case strings.Contains(joined, "ctxd.run"):
		f.stoppedMu.Lock()
		isStopped := f.stopped
		f.stoppedMu.Unlock()
		if !f.launched || isStopped {
			return podrun.Result{ExitCode: 1}, errors.New("exit 1")
		}
		return podrun.Result{Stdout: []byte("STATE_DIR:/home/u/.local/state/ctxt/ambient\n" + string(f.runJSON))}, nil
	default:
		return podrun.Result{}, nil
	}
}

func (f *lifecycleFixture) runner() *scriptRunner {
	return &scriptRunner{
		run: func(_ context.Context, argv []string, _ podrun.RunOpts) (podrun.Result, error) {
			switch argv[1] {
			case "capabilities":
				return podrun.Result{Stdout: []byte(fullCapsJSON)}, nil
			case "show":
				return podrun.Result{Stdout: []byte(`{"name":"dev-pod","provider":"docker","status":"running"}`)}, nil
			case "stop":
				f.podStopped = true
				return podrun.Result{Stdout: []byte(`{"name":"dev-pod","provider":"docker","status":"stopped"}`)}, nil
			case "upload":
				f.uploadedPath = argv[4]
				return podrun.Result{}, nil
			case "exec":
				return f.handleExec(strings.Join(argv[4:], " "))
			default:
				return podrun.Result{}, fmt.Errorf("unexpected verb %s", argv[1])
			}
		},
	}
}

func TestHostStartStatusStopLifecycle(t *testing.T) {
	f := newLifecycleFixture(t, "ctrl-tok-xyz")
	client := podrun.NewClient("/bin/pod", f.runner(), podrun.DefaultTimeouts())
	client.PickLocalPort = func() (int, error) { return f.srvPort, nil }
	client.ForwardReadyTimeout = time.Second

	stateDir := t.TempDir()
	host := NewHost(client, stateDir)
	host.StartDeadline = 5 * time.Second
	host.StopDeadline = 5 * time.Second
	host.PollInterval = 20 * time.Millisecond

	res, err := host.Start(context.Background(), StartOpts{
		PodName:   "dev-pod",
		Autostart: true,
		ServerURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !f.launched || f.uploadedPath == "" {
		t.Fatalf("expected upload + launch (launched=%v uploaded=%q)", f.launched, f.uploadedPath)
	}
	if res.Run.Pod != "dev-pod" || res.Run.Launcher != "pod dev-pod" {
		t.Fatalf("unexpected RunInfo: %+v", res.Run)
	}
	fi, err := os.Stat(RecordPath(stateDir))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("record file mode = %v, err = %v", fi, err)
	}

	// Guarantee 6: idempotent start refuses already running with exit 4.
	_, err = host.Start(context.Background(), StartOpts{
		PodName:   "dev-pod",
		Autostart: true,
		ServerURL: "http://127.0.0.1:8080",
	})
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 4 {
		t.Fatalf("second Start want Conflict exit 4, got %v", err)
	}

	st, alive, err := host.Status(context.Background(), "dev-pod")
	if err != nil || !alive || st["pod"] != "dev-pod" {
		t.Fatalf("Status: (%v, %v, %v)", st, alive, err)
	}
	flushed, err := host.BufferAction(context.Background(), "dev-pod", "flush")
	if err != nil || flushed["flushed"] == nil {
		t.Fatalf("BufferAction flush: (%v, %v)", flushed, err)
	}
	var tailBuf bytes.Buffer
	if err := host.Tail(context.Background(), "dev-pod", &tailBuf); err != nil || !strings.Contains(tailBuf.String(), "event-1") {
		t.Fatalf("Tail: (%q, %v)", tailBuf.String(), err)
	}

	if err := host.Stop(context.Background(), "dev-pod", true); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !f.podStopped {
		t.Error("expected pod stop when stopPod=true")
	}
	if _, err := os.Stat(RecordPath(stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected local record deleted after Stop, stat err=%v", err)
	}
}

func TestHostStartMissingBinaryF9(t *testing.T) {
	sr := &scriptRunner{
		run: func(_ context.Context, argv []string, _ podrun.RunOpts) (podrun.Result, error) {
			switch argv[1] {
			case "capabilities":
				return podrun.Result{Stdout: []byte(fullCapsJSON)}, nil
			case "show":
				return podrun.Result{Stdout: []byte(`{"name":"dev-pod","provider":"docker","status":"running"}`)}, nil
			case "exec":
				return podrun.Result{ExitCode: 127, Stderr: []byte("ctxd: not found")}, errors.New("exit 127")
			default:
				return podrun.Result{}, nil
			}
		},
	}
	client := podrun.NewClient("/bin/pod", sr, podrun.DefaultTimeouts())
	host := NewHost(client, filepath.Join(t.TempDir(), "state"))
	_, err := host.Start(context.Background(), StartOpts{
		PodName:   "dev-pod",
		ServerURL: "http://127.0.0.1:8080",
	})
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 70 || !strings.Contains(oe.Message, "not found on pod") {
		t.Fatalf("F9 want exit 70 naming missing binary, got %v", err)
	}
}

func TestParsePollOutputRejectsStaleRunFile(t *testing.T) {
	staleJSON, _ := json.Marshal(RunInfo{
		PID:       123,
		StartedAt: time.Unix(1000, 0).UTC(),
		Control:   "127.0.0.1:4444",
		Token:     "old",
	})
	raw := "STATE_DIR:/s\nMARKER:2000\n" + string(staleJSON)
	if _, _, ok := parsePollOutput(raw); ok {
		t.Fatal("stale run file (started_at < launch marker) must be rejected")
	}
}
