package podrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"hop.top/kit/go/console/output"
	xrr "hop.top/xrr"
	xexec "hop.top/xrr/adapters/exec"
)

// xrrRunner wraps a real Runner through an xrr exec session so every `pod`
// shell-out is recorded once against the real pod CLI and replayed
// deterministically in CI without invoking pod or touching real XDG state.
type xrrRunner struct {
	sess    xrr.Session
	adapter *xexec.Adapter
	real    Runner
	// normBin replaces argv[0] with "pod" in the cassette fingerprint so
	// cassettes recorded on one machine replay cleanly regardless of binary path.
	normBin bool
}

func (r *xrrRunner) Run(ctx context.Context, argv []string, opts RunOpts) (Result, error) {
	fpArgv := append([]string(nil), argv...)
	if r.normBin && len(fpArgv) > 0 {
		fpArgv[0] = BinaryName
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
	res, ok := decodeXRRResponse(raw)
	if !ok {
		return Result{}, err
	}
	if res.ExitCode != 0 && err == nil {
		err = &replayExitError{code: res.ExitCode}
	}
	return res, err
}

func (r *xrrRunner) Start(ctx context.Context, argv []string, opts RunOpts) (Proc, error) {
	return r.real.Start(ctx, argv, opts)
}

type replayExitError struct{ code int }

func (e *replayExitError) Error() string { return "exit status" }

// decodeXRRResponse handles both typed *xexec.Response (record mode) and
// untyped *xrr.RawResponse (replay mode), per the xrr Go adapter contract.
func decodeXRRResponse(resp xrr.Response) (Result, bool) {
	switch v := resp.(type) {
	case *xexec.Response:
		if v == nil {
			return Result{}, false
		}
		return Result{
			Stdout:   []byte(v.Stdout),
			Stderr:   []byte(v.Stderr),
			ExitCode: v.ExitCode,
		}, true
	case *xrr.RawResponse:
		if v == nil {
			return Result{}, false
		}
		res := Result{}
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
		return Result{}, false
	}
}

func recordEnvFromOS() []string {
	var env []string
	pairs := []struct{ src, dst string }{
		{"PODRUN_POD_HOME", "HOME"},
		{"PODRUN_POD_XDG_CONFIG_HOME", "XDG_CONFIG_HOME"},
		{"PODRUN_POD_XDG_DATA_HOME", "XDG_DATA_HOME"},
		{"PODRUN_POD_XDG_STATE_HOME", "XDG_STATE_HOME"},
		{"PODRUN_POD_XDG_CACHE_HOME", "XDG_CACHE_HOME"},
	}
	for _, p := range pairs {
		if v := os.Getenv(p.src); v != "" {
			env = append(env, p.dst+"="+v)
		}
	}
	return env
}

// TestPodClientAgainstXRRCassettes replays committed xrr exec cassettes for
// the pod CLI shell-outs (capabilities, show running/stopped/missing, start,
// stop, exec, error envelopes). Set PODRUN_RECORD=1 with a real `pod` binary
// (and PODRUN_POD_HOME / PODRUN_POD_XDG_* pointing at a test pod directory)
// to re-record live cassettes against hop-top/pod.
func TestPodClientAgainstXRRCassettes(t *testing.T) {
	cassetteDir := filepath.Join("cassettes", "podcli")
	mode := xrr.ModeReplay
	binPath := BinaryName
	var extraEnv []string

	if os.Getenv("PODRUN_RECORD") == "1" {
		if b := os.Getenv("PODRUN_POD_BIN"); b != "" {
			binPath = b
		} else {
			p, err := exec.LookPath(BinaryName)
			if err != nil {
				t.Skipf("PODRUN_RECORD=1 but pod not on PATH: %v", err)
			}
			binPath = p
		}
		mode = xrr.ModeRecord
		extraEnv = recordEnvFromOS()
		if err := os.MkdirAll(cassetteDir, 0o755); err != nil {
			t.Fatalf("mkdir cassettes: %v", err)
		}
	}

	sess := xrr.NewSession(mode, xrr.NewFileCassette(cassetteDir))
	xr := &xrrRunner{
		sess:    sess,
		adapter: xexec.NewAdapter(),
		real:    NewExecRunner(),
		normBin: true,
	}
	client := NewClient(binPath, xr, DefaultTimeouts())
	client.Env = extraEnv

	ctx := context.Background()

	// 1. capabilities probe
	caps, err := client.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if err := RequireLeaves(caps, "pod show", "pod start", "pod stop", "pod exec", "pod forward", "pod upload"); err != nil {
		t.Fatalf("RequireLeaves: %v", err)
	}

	// 2. show missing pod -> NOT_FOUND exit 3
	_, err = client.Show(ctx, "missing-pod")
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 3 {
		t.Fatalf("Show missing: want exit 3, got %v", err)
	}

	// 3. show running pod
	info, err := client.Show(ctx, "cassette-pod")
	if err != nil {
		t.Fatalf("Show cassette-pod: %v", err)
	}
	if info.Name != "cassette-pod" || info.Status != "running" {
		t.Fatalf("unexpected pod info: %+v", info)
	}

	// 4. exec on running pod
	res, err := client.Exec(ctx, "cassette-pod", []string{"echo", "ok"}, RunOpts{})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("Exec echo ok: res=%+v err=%v", res, err)
	}

	// 5. stop and start lifecycle
	stopped, err := client.StopPod(ctx, "cassette-pod", "cassette test")
	if err != nil || stopped.Status != "stopped" {
		t.Fatalf("StopPod: stopped=%+v err=%v", stopped, err)
	}
	started, err := client.StartPod(ctx, "cassette-pod", "cassette test")
	if err != nil || started.Status != "running" {
		t.Fatalf("StartPod: started=%+v err=%v", started, err)
	}
}
