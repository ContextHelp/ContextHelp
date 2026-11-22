package podexec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	xrr "hop.top/xrr"
	xexec "hop.top/xrr/adapters/exec"

	"github.com/ideacrafterslabs/ctxt/internal/podrun"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type xrrPodRunner struct {
	sess    xrr.Session
	adapter *xexec.Adapter
	real    podrun.Runner
}

func (r *xrrPodRunner) Run(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Result, error) {
	fpArgv := append([]string(nil), argv...)
	if len(fpArgv) > 0 {
		fpArgv[0] = podrun.BinaryName
	}
	// Normalize ephemeral temp upload source path in cassette fingerprint.
	if len(fpArgv) >= 5 && fpArgv[1] == "upload" {
		fpArgv[3] = "/tmp/ctxt-job-req-local.json"
	}
	req := &xexec.Request{
		Argv:  fpArgv,
		Stdin: string(opts.Stdin),
	}
	do := func() (xrr.Response, error) {
		res, err := r.real.Run(ctx, argv, opts)
		return &xexec.Response{
			Stdout:   string(res.Stdout),
			Stderr:   string(res.Stderr),
			ExitCode: res.ExitCode,
		}, err
	}
	raw, err := r.sess.Record(ctx, r.adapter, req, do)
	res, ok := decodeXRR(raw)
	if !ok {
		return podrun.Result{}, err
	}
	if res.ExitCode != 0 && err == nil {
		err = &xrrExitErr{code: res.ExitCode}
	}
	return res, err
}

func (r *xrrPodRunner) Start(ctx context.Context, argv []string, opts podrun.RunOpts) (podrun.Proc, error) {
	return r.real.Start(ctx, argv, opts)
}

type xrrExitErr struct{ code int }

func (e *xrrExitErr) Error() string { return "exit status" }

func decodeXRR(resp xrr.Response) (podrun.Result, bool) {
	switch v := resp.(type) {
	case *xexec.Response:
		if v == nil {
			return podrun.Result{}, false
		}
		return podrun.Result{Stdout: []byte(v.Stdout), Stderr: []byte(v.Stderr), ExitCode: v.ExitCode}, true
	case *xrr.RawResponse:
		if v == nil {
			return podrun.Result{}, false
		}
		res := podrun.Result{}
		if s, ok := v.Payload["stdout"].(string); ok {
			res.Stdout = []byte(s)
		}
		if s, ok := v.Payload["stderr"].(string); ok {
			res.Stderr = []byte(s)
		}
		switch ec := v.Payload["exit_code"].(type) {
		case int:
			res.ExitCode = ec
		case int64:
			res.ExitCode = int(ec)
		case float64:
			res.ExitCode = int(ec)
		}
		return res, true
	default:
		return podrun.Result{}, false
	}
}

func recordEnvFromOS() []string {
	var env []string
	for _, p := range [][2]string{
		{"PODRUN_POD_HOME", "HOME"},
		{"PODRUN_POD_XDG_CONFIG_HOME", "XDG_CONFIG_HOME"},
		{"PODRUN_POD_XDG_DATA_HOME", "XDG_DATA_HOME"},
		{"PODRUN_POD_XDG_STATE_HOME", "XDG_STATE_HOME"},
		{"PODRUN_POD_XDG_CACHE_HOME", "XDG_CACHE_HOME"},
	} {
		if v := os.Getenv(p[0]); v != "" {
			env = append(env, p[1]+"="+v)
		}
	}
	return env
}

// TestPodExecutorAgainstXRRCassettes replays committed live-recorded xrr exec
// cassettes covering the full ephemeral job lifecycle:
// capabilities -> create -> upload -> exec dpkms worker run-draft -> destroy.
func TestPodExecutorAgainstXRRCassettes(t *testing.T) {
	cassetteDir := filepath.Join("cassettes", "podcli")
	mode := xrr.ModeReplay
	binPath := podrun.BinaryName
	var extraEnv []string

	if os.Getenv("PODRUN_RECORD") == "1" {
		if b := os.Getenv("PODRUN_POD_BIN"); b != "" {
			binPath = b
		} else {
			p, err := exec.LookPath(podrun.BinaryName)
			if err != nil {
				t.Skipf("PODRUN_RECORD=1 but pod not on PATH: %v", err)
			}
			binPath = p
		}
		mode = xrr.ModeRecord
		extraEnv = recordEnvFromOS()
		if err := os.MkdirAll(cassetteDir, 0o750); err != nil {
			t.Fatalf("mkdir cassettes: %v", err)
		}
	}

	sess := xrr.NewSession(mode, xrr.NewFileCassette(cassetteDir))
	xr := &xrrPodRunner{
		sess:    sess,
		adapter: xexec.NewAdapter(),
		real:    podrun.NewExecRunner(),
	}
	client := podrun.NewClient(binPath, xr, podrun.DefaultTimeouts())
	client.Env = extraEnv

	executor := NewExecutor(client, Options{
		Provider:       "docker",
		Image:          "ctxt-pod-worker:local",
		DpkmsBin:       "dpkms",
		MaxInflight:    2,
		MaxPodsPerHour: 100,
		MaxPodDuration: 2 * time.Minute,
		MaxHops:        5,
		NameFunc:       func(_ string) string { return "ctxt-job-xrrdemo" },
	})

	draft, err := executor.Execute(context.Background(), &storage.Job{
		ID:       "xrr-job-1",
		Type:     "ingest:code",
		Pipeline: "text.short",
		Payload:  "package xrrdemo\nfunc Run() string { return \"ok\" }\n",
		Source:   "cli",
	})
	if err != nil {
		t.Fatalf("Executor.Execute against xrr cassettes: %v", err)
	}
	if draft == nil || draft.ContentHash == "" {
		t.Fatalf("expected non-empty draft with ContentHash, got %+v", draft)
	}
}
