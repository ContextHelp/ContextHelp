package dpkmspod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	testguard.Main(m, nil, "ctxt", "dpkms")
}

const fullCapsJSON = `{"tool":"pod","schema_version":"1.0","leaves":[{"path":"show"},{"path":"start"},{"path":"stop"},{"path":"exec"},{"path":"forward"},{"path":"upload"}]}`

type fakeProc struct {
	pid    int
	closed bool
}

func (p *fakeProc) PID() int     { return p.pid }
func (p *fakeProc) Wait() error  { return nil }
func (p *fakeProc) Close() error { p.closed = true; return nil }

type scriptRunner struct {
	run func(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Result, error)
}

func (s *scriptRunner) Run(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Result, error) {
	return s.run(ctx, argv, opts)
}

func (s *scriptRunner) Start(_ context.Context, _ []string, _ podrun.RunOpts) (podrun.Proc, error) {
	return &fakeProc{pid: 8888}, nil
}

func TestRenderPodConfigAndLaunchScriptGuarantee8(t *testing.T) {
	raw, err := RenderPodConfig(config.AccessPrivate, []string{"dpkms.lan"}, config.AuthConfig{})
	if err != nil {
		t.Fatalf("RenderPodConfig: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, hasDaemon := doc["daemon"]; hasDaemon {
		t.Fatal("rendered pod config must never contain daemon.*")
	}
	srv := doc["server"].(map[string]any)
	hosts := srv["allowed_hosts"].([]any)
	if len(hosts) < 1 || hosts[0] != "127.0.0.1" {
		t.Fatalf("allowed_hosts must start with 127.0.0.1, got %v", hosts)
	}

	// Guarantee 8: remote dpkms serve ALWAYS ends with --runner local.
	script, err := BuildLaunchScript(LaunchScriptOpts{
		DpkmsBin:         "dpkms",
		InstanceName:     "work",
		DpkmsPort:        8080,
		DataDir:          "/workspace/ctxt-data",
		RemoteConfigPath: "/tmp/dpkms-pod-dev.yaml",
		Profile:          "founder",
	})
	if err != nil {
		t.Fatalf("BuildLaunchScript: %v", err)
	}
	wantSuffix := "--profile founder --runner local </dev/null >>/workspace/ctxt-data/run/dpkms-pod.log 2>&1 &"
	if !strings.HasSuffix(script, wantSuffix) {
		t.Fatalf("launch script does not end with --runner local:\n  got:  %s\n  want: ...%s", script, wantSuffix)
	}
	if !strings.Contains(script, "--grpc-port 0 --cookie-bridge-port 0") {
		t.Errorf("expected --grpc-port 0 --cookie-bridge-port 0 in script: %s", script)
	}

	// P9 validation rejects metacharacters.
	if _, err := BuildLaunchScript(LaunchScriptOpts{
		DpkmsBin:         "dpkms",
		DataDir:          "/data;rm -rf /",
		RemoteConfigPath: "/tmp/a.yaml",
	}); err == nil {
		t.Fatal("expected P9 rejection for shell metacharacters in data_dir")
	}
}

type dpkmsFixture struct {
	srv            *httptest.Server
	srvPort        int
	mu             sync.Mutex
	running        bool
	uploadedCfg    string
	launchScript   string
	podStopped     bool
	forwardStopped int
}

func newDpkmsFixture(t *testing.T) *dpkmsFixture {
	t.Helper()
	f := &dpkmsFixture{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.srv.Close)
	f.srvPort = f.srv.Listener.Addr().(*net.TCPAddr).Port
	return f
}

func (f *dpkmsFixture) handleExec(argv []string) (podrun.Result, error) {
	joined := strings.Join(argv[4:], " ")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.Contains(joined, "--version"):
		return podrun.Result{Stdout: []byte("dpkms version 0.2.0\n")}, nil
	case strings.Contains(joined, "ps --format json"):
		var list []pidfile.Info
		if f.running {
			list = []pidfile.Info{{
				PID:       5555,
				Name:      "work",
				Port:      8080,
				GRPCPort:  0,
				Access:    "private",
				StartedAt: time.Now().UTC().Add(-time.Minute),
			}}
		}
		b, _ := json.Marshal(map[string]any{"instances": list})
		return podrun.Result{Stdout: b}, nil
	case strings.Contains(joined, "setsid nohup"):
		f.launchScript = joined
		f.running = true
		return podrun.Result{}, nil
	case strings.Contains(joined, "dpkms shutdown"):
		f.running = false
		return podrun.Result{Stdout: []byte("Sent SIGTERM\n")}, nil
	default:
		return podrun.Result{}, nil
	}
}

func (f *dpkmsFixture) runner() *scriptRunner {
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
				f.uploadedCfg = argv[4]
				return podrun.Result{}, nil
			case "exec":
				return f.handleExec(argv)
			default:
				return podrun.Result{}, fmt.Errorf("unexpected verb %s", argv[1])
			}
		},
	}
}

func TestHostServePSAndShutdownLifecycle(t *testing.T) {
	f := newDpkmsFixture(t)
	client := podrun.NewClient("/bin/pod", f.runner(), podrun.DefaultTimeouts())
	client.PickLocalPort = func() (int, error) { return f.srvPort, nil }
	client.ForwardReadyTimeout = time.Second

	stateDir := t.TempDir()
	host := NewHost(client, stateDir)
	host.StartDeadline = 5 * time.Second
	host.StopDeadline = 5 * time.Second
	host.PollInterval = 20 * time.Millisecond
	host.StopForward = func(pid int, _ time.Time) { f.forwardStopped = pid }

	// 1. Serve launches dpkms, opens forward, verifies /healthz, writes 0600 record.
	res, err := host.Serve(context.Background(), ServeOpts{
		PodName:      "dev-pod",
		Autostart:    true,
		InstanceName: "work",
		DataDir:      "/workspace/ctxt-data",
		DpkmsPort:    8080,
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if res.URL != fmt.Sprintf("http://127.0.0.1:%d", f.srvPort) {
		t.Fatalf("URL = %q", res.URL)
	}
	if !strings.Contains(f.launchScript, "--runner local") {
		t.Errorf("launch script missing --runner local: %s", f.launchScript)
	}
	fi, err := os.Stat(RecordPath(stateDir, "dev-pod"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("record mode = %v, err = %v", fi, err)
	}

	// 2. Guarantee 6: second Serve sees running instance and refuses with exit 4.
	_, err = host.Serve(context.Background(), ServeOpts{
		PodName:   "dev-pod",
		Autostart: true,
		DataDir:   "/workspace/ctxt-data",
		DpkmsPort: 8080,
	})
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 4 {
		t.Fatalf("second Serve want Conflict exit 4, got %v", err)
	}

	// 3. PS reports instance with host="pod dev-pod" and health="healthy".
	psRes, err := host.PS(context.Background(), PSOpts{PodName: "dev-pod", DataDir: "/workspace/ctxt-data"})
	if err != nil {
		t.Fatalf("PS: %v", err)
	}
	if len(psRes.Instances) != 1 || psRes.Instances[0].Host != "pod dev-pod" || psRes.Instances[0].Health != "healthy" {
		t.Fatalf("unexpected PS result: %+v", psRes.Instances)
	}

	// 4. Shutdown stops dpkms, stops recorded forward, deletes record, stops pod.
	sdRes, err := host.Shutdown(context.Background(), ShutdownOpts{
		PodName:   "dev-pod",
		DataDir:   "/workspace/ctxt-data",
		DpkmsPort: 8080,
		StopPod:   true,
	})
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !sdRes.PodStopped || !f.podStopped {
		t.Error("expected pod stop when StopPod=true")
	}
	if f.forwardStopped != 8888 {
		t.Errorf("expected StopForward(8888), got %d", f.forwardStopped)
	}
	if _, err := os.Stat(RecordPath(stateDir, "dev-pod")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected local record deleted after Shutdown, stat err=%v", err)
	}
}

func TestHostPSStoppedPodReturnsEmptyEnvelope(t *testing.T) {
	sr := &scriptRunner{
		run: func(_ context.Context, argv []string, _ podrun.RunOpts) (podrun.Result, error) {
			switch argv[1] {
			case "capabilities":
				return podrun.Result{Stdout: []byte(fullCapsJSON)}, nil
			case "show":
				return podrun.Result{Stdout: []byte(`{"name":"dev-pod","provider":"docker","status":"stopped"}`)}, nil
			default:
				return podrun.Result{}, fmt.Errorf("unexpected verb %s", argv[1])
			}
		},
	}
	host := NewHost(podrun.NewClient("/bin/pod", sr, podrun.DefaultTimeouts()), t.TempDir())
	res, err := host.PS(context.Background(), PSOpts{PodName: "dev-pod"})
	if err != nil {
		t.Fatalf("PS stopped pod: %v", err)
	}
	if len(res.Instances) != 0 || res.PodStatus != "stopped" {
		t.Fatalf("expected empty instances and stopped status, got %+v", res)
	}
}

func TestHostServeMissingBinaryF9(t *testing.T) {
	sr := &scriptRunner{
		run: func(_ context.Context, argv []string, _ podrun.RunOpts) (podrun.Result, error) {
			switch argv[1] {
			case "capabilities":
				return podrun.Result{Stdout: []byte(fullCapsJSON)}, nil
			case "show":
				return podrun.Result{Stdout: []byte(`{"name":"dev-pod","provider":"docker","status":"running"}`)}, nil
			case "exec":
				return podrun.Result{ExitCode: 127, Stderr: []byte("dpkms: not found")}, errors.New("exit 127")
			default:
				return podrun.Result{}, nil
			}
		},
	}
	host := NewHost(podrun.NewClient("/bin/pod", sr, podrun.DefaultTimeouts()), t.TempDir())
	_, err := host.Serve(context.Background(), ServeOpts{
		PodName: "dev-pod",
		DataDir: "/workspace/ctxt-data",
	})
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 70 || !strings.Contains(oe.Message, "not found on pod") {
		t.Fatalf("F9 want exit 70, got %v", err)
	}
}
