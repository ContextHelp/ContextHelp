package podhost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
)

const (
	defaultRemoteStateExpr = `"${XDG_STATE_HOME:-$HOME/.local/state}/ctxt/ambient"`
	defaultStartDeadline   = 90 * time.Second
	defaultStopDeadline    = 30 * time.Second
	defaultPollInterval    = 2 * time.Second
)

// requiredLeaves are the pod CLI verbs ctxd pod mode needs.
var requiredLeaves = []string{
	"pod show",
	"pod start",
	"pod stop",
	"pod exec",
	"pod forward",
	"pod upload",
}

// StartOpts carries invocation options for starting ctxd on a pod.
type StartOpts struct {
	PodName          string
	Autostart        bool
	CtxdBin          string
	ServerURL        string // daemon.pod.server_url (if set, exempt from F18)
	ResolvedEndpoint string // laptop-resolved dpkms endpoint (checked by F18 when ServerURL empty)
	ServerToken      string
	Profile          string
	Inputs           []string
	Skips            []string
}

// StartResult reports a successfully launched pod-backed ctxd instance.
type StartResult struct {
	Pod      string
	Provider string
	Run      RunInfo
	Status   map[string]any
}

// Host orchestrates ctxd lifecycle operations on a pod via podrun.Client.
type Host struct {
	Pod           *podrun.Client
	StateDir      string
	HTTPClient    *http.Client
	StartDeadline time.Duration
	StopDeadline  time.Duration
	PollInterval  time.Duration
	Warn          io.Writer
}

// NewHost builds a Host with safe default deadlines.
func NewHost(pod *podrun.Client, stateDir string) *Host {
	return &Host{
		Pod:           pod,
		StateDir:      stateDir,
		StartDeadline: defaultStartDeadline,
		StopDeadline:  defaultStopDeadline,
		PollInterval:  defaultPollInterval,
	}
}

func (h *Host) startDeadline() time.Duration {
	if h.StartDeadline > 0 {
		return h.StartDeadline
	}
	return defaultStartDeadline
}

func (h *Host) stopDeadline() time.Duration {
	if h.StopDeadline > 0 {
		return h.StopDeadline
	}
	return defaultStopDeadline
}

func (h *Host) pollInterval() time.Duration {
	if h.PollInterval > 0 {
		return h.PollInterval
	}
	return defaultPollInterval
}

// ValidateEndpoint resolves the dpkms URL ctxd will talk to from inside the
// pod and enforces F18: when daemon.pod.server_url is empty, a loopback or
// local-instance URL resolved on the laptop is refused (exit 2) because the
// pod cannot reach 127.0.0.1 on the laptop.
func ValidateEndpoint(serverURL, resolvedEndpoint string) (string, error) {
	if strings.TrimSpace(serverURL) != "" {
		return strings.TrimSpace(serverURL), nil
	}
	ep := strings.TrimSpace(resolvedEndpoint)
	if ep == "" {
		e := output.UsageError("no dpkms server endpoint configured for pod-side ctxd")
		e.SuggestedFix = "set daemon.pod.server_url to the dpkms URL reachable from the pod"
		return "", e
	}
	if isLoopbackURL(ep) {
		e := output.UsageError(fmt.Sprintf("resolved dpkms endpoint %q is a loopback address the pod cannot reach", ep))
		e.SuggestedFix = "set daemon.pod.server_url to the URL the pod sees (for example http://127.0.0.1:8080 when dpkms runs on the same pod)"
		return "", e
	}
	return ep, nil
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "0.0.0.0" || host == "::" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// RenderPodConfig produces the minimal pod-side ctxt.yaml uploaded to the pod.
// It carries only server endpoint/token and profile — never daemon.* and never
// laptop filesystem paths.
func RenderPodConfig(endpoint, token, profile string) ([]byte, error) {
	doc := map[string]any{}
	entry := map[string]any{"url": endpoint}
	if token != "" {
		entry["token"] = token
	}
	doc["server"] = map[string]any{
		"urls": []any{entry},
	}
	if profile != "" {
		doc["profile"] = map[string]any{"default": profile}
	}
	return yaml.Marshal(doc)
}

// BuildLaunchScript constructs the fixed P9-validated detached launch script.
func BuildLaunchScript(ctxdBin, podName, cfgRemotePath, profile string, inputs, skips []string) (string, error) {
	if ctxdBin == "" {
		ctxdBin = config.DefaultDaemonPodCtxdBin
	}
	if err := podrun.ValidateToken("daemon.pod.ctxd_bin", ctxdBin); err != nil {
		return "", err
	}
	if err := podrun.ValidateToken("daemon.pod.name", podName); err != nil {
		return "", err
	}
	if err := podrun.ValidateToken("remote config path", cfgRemotePath); err != nil {
		return "", err
	}

	var flags []string
	flags = append(flags, "-c", podrun.ShellQuote(cfgRemotePath))
	if profile != "" {
		if err := podrun.ValidateToken("profile", profile); err != nil {
			return "", err
		}
		flags = append(flags, "--profile", podrun.ShellQuote(profile))
	}
	for _, in := range inputs {
		if err := podrun.ValidateToken("input", in); err != nil {
			return "", err
		}
		flags = append(flags, "--input", podrun.ShellQuote(in))
	}
	for _, sk := range skips {
		if err := podrun.ValidateToken("skip", sk); err != nil {
			return "", err
		}
		flags = append(flags, "--skip", podrun.ShellQuote(sk))
	}

	script := fmt.Sprintf(
		`export XDG_STATE_HOME="${XDG_STATE_HOME:-$HOME/.local/state}"; `+
			`S="$XDG_STATE_HOME/ctxt/ambient"; `+
			`mkdir -p "$S" && date +%%s > "$S/ctxd.launch" && `+
			`setsid nohup %s --launcher %s --log-file "$S/ctxd.log" %s </dev/null >>"$S/ctxd.out" 2>&1 &`,
		podrun.ShellQuote(ctxdBin),
		podrun.ShellQuote("pod "+podName),
		strings.Join(flags, " "),
	)
	return script, nil
}

// Start runs the full 12-step pod-backed ctxd start sequence.
func (h *Host) Start(ctx context.Context, opts StartOpts) (*StartResult, error) {
	ctxdBin, endpoint, podInfo, err := h.preflightStart(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := h.uploadAndLaunch(ctx, opts, ctxdBin, endpoint); err != nil {
		return nil, err
	}
	remoteRun, stateDirRemote, localRun, status, err := h.verifyLaunched(ctx, opts.PodName)
	if err != nil {
		return nil, err
	}

	// Step 12: Persist local cache record.
	controlPort, err := parseControlPort(remoteRun.Control)
	if err != nil {
		return nil, err
	}
	rec := Record{
		Version:        1,
		Pod:            opts.PodName,
		Provider:       podInfo.Provider,
		RemoteStateDir: stateDirRemote,
		ControlPort:    controlPort,
		Token:          remoteRun.Token,
		PID:            remoteRun.PID,
		StartedAt:      remoteRun.StartedAt,
	}
	if err := WriteRecord(h.StateDir, rec); err != nil {
		return nil, err
	}

	return &StartResult{
		Pod:      opts.PodName,
		Provider: podInfo.Provider,
		Run:      localRun,
		Status:   status,
	}, nil
}

func (h *Host) preflightStart(ctx context.Context, opts StartOpts) (string, string, *podrun.PodInfo, error) {
	if err := podrun.ValidateToken("daemon.pod.name", opts.PodName); err != nil {
		return "", "", nil, err
	}
	ctxdBin := opts.CtxdBin
	if ctxdBin == "" {
		ctxdBin = config.DefaultDaemonPodCtxdBin
	}
	if err := podrun.ValidateToken("daemon.pod.ctxd_bin", ctxdBin); err != nil {
		return "", "", nil, err
	}

	// Step 1: Resolve endpoint and enforce F18.
	endpoint, err := ValidateEndpoint(opts.ServerURL, opts.ResolvedEndpoint)
	if err != nil {
		return "", "", nil, err
	}

	// Step 2: Probe capabilities.
	caps, err := h.Pod.Probe(ctx)
	if err != nil {
		return "", "", nil, err
	}
	if err := podrun.RequireLeaves(caps, requiredLeaves...); err != nil {
		return "", "", nil, err
	}

	// Steps 3–4: Pod gate (+ autostart if stopped).
	podInfo, err := h.Pod.GateForStart(ctx, opts.PodName, opts.Autostart, "ctxt ambient daemon", func() {
		_ = DeleteRecord(h.StateDir)
	})
	if err != nil {
		return "", "", nil, err
	}

	// Step 5: Verify ctxd binary on the pod (F9: exit 127 -> exit 70).
	verRes, verErr := h.Pod.Exec(ctx, opts.PodName, []string{ctxdBin, "--version"}, podrun.RunOpts{})
	if verRes.ExitCode == 127 {
		return "", "", nil, &output.Error{
			Code:         output.CodePrerequisite,
			Message:      fmt.Sprintf("%s not found on pod %q", ctxdBin, opts.PodName),
			SuggestedFix: fmt.Sprintf("install %s in the pod image or set daemon.pod.ctxd_bin", ctxdBin),
			ExitCode:     70,
		}
	}
	if verErr != nil || verRes.ExitCode != 0 {
		return "", "", nil, unreachableError(opts.PodName, verRes, verErr)
	}

	// Step 6: Check if ctxd is already running on the pod (F10: exit 4).
	if existing, alive := h.probeRemoteRunning(ctx, opts.PodName); alive {
		e := output.ConflictError(fmt.Sprintf("ctxd already running on pod %q (pid %d, started %s)", opts.PodName, existing.PID, existing.StartedAt.Format(time.RFC3339)))
		e.SuggestedFix = fmt.Sprintf("check it with `ctxt capture --ambient status --runner pod --pod %s`; stop it with `ctxt capture --ambient stop --runner pod --pod %s`", opts.PodName, opts.PodName)
		return "", "", nil, e
	}

	return ctxdBin, endpoint, podInfo, nil
}

func (h *Host) uploadAndLaunch(ctx context.Context, opts StartOpts, ctxdBin, endpoint string) error {
	// Step 7: Render and upload 0600 pod-side config.
	cfgBytes, err := RenderPodConfig(endpoint, opts.ServerToken, opts.Profile)
	if err != nil {
		return fmt.Errorf("render pod config: %w", err)
	}
	localCfg, err := writeTemp0600(cfgBytes)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(localCfg) }()

	remoteCfgPath := "/tmp/ctxt-pod-" + opts.PodName + ".yaml"
	if err := h.Pod.Upload(ctx, opts.PodName, localCfg, remoteCfgPath); err != nil {
		return err
	}

	// Step 8: Detached launch.
	script, err := BuildLaunchScript(ctxdBin, opts.PodName, remoteCfgPath, opts.Profile, opts.Inputs, opts.Skips)
	if err != nil {
		return err
	}
	launchRes, launchErr := h.Pod.Exec(ctx, opts.PodName, []string{"sh", "-c", script}, podrun.RunOpts{})
	if launchErr != nil || launchRes.ExitCode != 0 {
		return unreachableError(opts.PodName, launchRes, launchErr)
	}
	return nil
}

func (h *Host) verifyLaunched(ctx context.Context, podName string) (RunInfo, string, RunInfo, map[string]any, error) {
	// Steps 9–11: Bounded poll for fresh remote run file + forward + /v1/status 200.
	deadlineCtx, cancel := context.WithTimeout(ctx, h.startDeadline())
	defer cancel()

	remoteRun, stateDirRemote, err := h.waitForRemoteRunFile(deadlineCtx, podName)
	if err != nil {
		return RunInfo{}, "", RunInfo{}, nil, &output.Error{
			Code:         output.CodeTransient,
			Message:      fmt.Sprintf("ctxd on pod %q did not become ready: %v", podName, err),
			Cause:        h.fetchLogTail(ctx, podName),
			SuggestedFix: fmt.Sprintf("inspect pod log $XDG_STATE_HOME/ctxt/ambient/ctxd.log on pod %q", podName),
			ExitCode:     6,
		}
	}

	controlPort, err := parseControlPort(remoteRun.Control)
	if err != nil {
		return RunInfo{}, "", RunInfo{}, nil, err
	}

	proc, localPort, err := h.Pod.Forward(deadlineCtx, podName, 0, controlPort)
	if err != nil {
		return RunInfo{}, "", RunInfo{}, nil, &output.Error{
			Code:         output.CodeTransient,
			Message:      fmt.Sprintf("forward to ctxd control port on pod %q failed: %v", podName, err),
			Cause:        h.fetchLogTail(ctx, podName),
			SuggestedFix: fmt.Sprintf("check pod %q reachability and ctxd log", podName),
			ExitCode:     6,
		}
	}
	defer func() { _ = proc.Close() }()

	localRun := remoteRun
	localRun.Control = fmt.Sprintf("127.0.0.1:%d", localPort)
	localRun.Pod = podName
	localRun.Launcher = "pod " + podName

	status, err := h.waitControlStatus(deadlineCtx, localRun)
	if err != nil {
		return RunInfo{}, "", RunInfo{}, nil, &output.Error{
			Code:         output.CodeTransient,
			Message:      fmt.Sprintf("ctxd control API on pod %q did not answer: %v", podName, err),
			Cause:        h.fetchLogTail(ctx, podName),
			SuggestedFix: fmt.Sprintf("inspect $XDG_STATE_HOME/ctxt/ambient/ctxd.log on pod %q", podName),
			ExitCode:     6,
		}
	}
	return remoteRun, stateDirRemote, localRun, status, nil
}

// Status queries ctxd status on podName through a per-verb forward.
func (h *Host) Status(ctx context.Context, podName string) (map[string]any, bool, error) {
	caps, err := h.Pod.Probe(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := podrun.RequireLeaves(caps, "pod show", "pod exec", "pod forward"); err != nil {
		return nil, false, err
	}
	info, reachable, err := h.Pod.GateForQuery(ctx, podName, func() { _ = DeleteRecord(h.StateDir) })
	if err != nil {
		return nil, false, err
	}
	if !reachable {
		return map[string]any{"running": false, "pod": podName, "pod_status": info.Status}, false, nil
	}
	var out map[string]any
	err = h.withForward(ctx, podName, func(client *ControlClient) error {
		st, cerr := client.Status(ctx)
		out = st
		return cerr
	})
	if err != nil {
		return nil, false, err
	}
	out["pod"] = podName
	return out, true, nil
}

// BufferAction calls /v1/buffer/flush or /v1/buffer/purge on podName.
func (h *Host) BufferAction(ctx context.Context, podName, action string) (map[string]any, error) {
	caps, err := h.Pod.Probe(ctx)
	if err != nil {
		return nil, err
	}
	if err := podrun.RequireLeaves(caps, "pod show", "pod exec", "pod forward"); err != nil {
		return nil, err
	}
	info, reachable, err := h.Pod.GateForQuery(ctx, podName, func() { _ = DeleteRecord(h.StateDir) })
	if err != nil {
		return nil, err
	}
	if !reachable {
		return nil, output.NotFoundError(fmt.Sprintf("ctxd is not running (pod %q is %s)", podName, info.Status))
	}
	var out map[string]any
	err = h.withForward(ctx, podName, func(client *ControlClient) error {
		var cerr error
		if action == "flush" {
			out, cerr = client.BufferFlush(ctx)
		} else {
			out, cerr = client.BufferPurge(ctx)
		}
		return cerr
	})
	return out, err
}

// Tail streams /v1/tail from podName until ctx is cancelled.
func (h *Host) Tail(ctx context.Context, podName string, dst io.Writer) error {
	caps, err := h.Pod.Probe(ctx)
	if err != nil {
		return err
	}
	if err := podrun.RequireLeaves(caps, "pod show", "pod exec", "pod forward"); err != nil {
		return err
	}
	info, reachable, err := h.Pod.GateForQuery(ctx, podName, func() { _ = DeleteRecord(h.StateDir) })
	if err != nil {
		return err
	}
	if !reachable {
		return output.NotFoundError(fmt.Sprintf("ctxd is not running (pod %q is %s)", podName, info.Status))
	}
	return h.withForward(ctx, podName, func(client *ControlClient) error {
		return client.Tail(ctx, dst)
	})
}

// Stop stops ctxd on podName via POST /v1/stop, falling back to verified
// `kill -TERM <pid>` when the control API does not answer, deletes the local
// record, and optionally stops the pod itself when stopPod is true.
func (h *Host) Stop(ctx context.Context, podName string, stopPod bool) error {
	caps, err := h.Pod.Probe(ctx)
	if err != nil {
		return err
	}
	leaves := []string{"pod show", "pod exec", "pod forward"}
	if stopPod {
		leaves = append(leaves, "pod stop")
	}
	if err := podrun.RequireLeaves(caps, leaves...); err != nil {
		return err
	}

	_, reachable, err := h.Pod.GateForQuery(ctx, podName, func() { _ = DeleteRecord(h.StateDir) })
	if err != nil {
		return err
	}
	if !reachable {
		_ = DeleteRecord(h.StateDir)
		return nil
	}

	stopErr := h.withForward(ctx, podName, func(client *ControlClient) error {
		return client.Stop(ctx)
	})
	if stopErr != nil {
		// Fallback: read remote run file, verify comm is ctxd, send SIGTERM.
		if remoteRun, _, rerr := h.readRemoteRunFile(ctx, podName); rerr == nil && remoteRun.PID > 0 {
			pidStr := strconv.Itoa(remoteRun.PID)
			psRes, _ := h.Pod.Exec(ctx, podName, []string{"sh", "-c", "ps -p " + pidStr + " -o comm="}, podrun.RunOpts{})
			if strings.Contains(strings.TrimSpace(string(psRes.Stdout)), "ctxd") {
				_, _ = h.Pod.Exec(ctx, podName, []string{"kill", "-TERM", pidStr}, podrun.RunOpts{})
			}
		}
	}

	// Poll until the remote run file is gone (or API refuses).
	deadline := time.Now().Add(h.stopDeadline())
	for {
		if _, _, rerr := h.readRemoteRunFile(ctx, podName); rerr != nil {
			break
		}
		if time.Now().After(deadline) {
			return &output.Error{
				Code:         output.CodeTransient,
				Message:      fmt.Sprintf("ctxd on pod %q did not stop within %s", podName, h.stopDeadline()),
				SuggestedFix: fmt.Sprintf("inspect pod %q and retry stop", podName),
				ExitCode:     6,
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(h.pollInterval(), 250*time.Millisecond)):
		}
	}

	_ = DeleteRecord(h.StateDir)
	if stopPod {
		if _, err := h.Pod.StopPod(ctx, podName, "ctxt ambient daemon stop"); err != nil {
			return err
		}
	}
	return nil
}

// withForward opens a short-lived forward using the cached local record,
// refreshing from the remote run file once if the cached port/token fails.
func (h *Host) withForward(ctx context.Context, podName string, fn func(*ControlClient) error) error {
	rec, _ := ReadRecord(h.StateDir)
	if rec != nil && rec.Pod == podName && rec.ControlPort > 0 {
		if err := h.callThroughForward(ctx, podName, rec.ControlPort, rec.Token, fn); err == nil {
			return nil
		}
	}
	// Refresh once from remote run file.
	remoteRun, remoteDir, err := h.readRemoteRunFile(ctx, podName)
	if err != nil {
		return output.NotFoundError(fmt.Sprintf("ctxd is not running on pod %q", podName))
	}
	port, err := parseControlPort(remoteRun.Control)
	if err != nil {
		return err
	}
	_ = WriteRecord(h.StateDir, Record{
		Version:        1,
		Pod:            podName,
		RemoteStateDir: remoteDir,
		ControlPort:    port,
		Token:          remoteRun.Token,
		PID:            remoteRun.PID,
		StartedAt:      remoteRun.StartedAt,
	})
	return h.callThroughForward(ctx, podName, port, remoteRun.Token, fn)
}

func (h *Host) callThroughForward(ctx context.Context, podName string, remotePort int, token string, fn func(*ControlClient) error) error {
	proc, lp, err := h.Pod.Forward(ctx, podName, 0, remotePort)
	if err != nil {
		return err
	}
	defer func() { _ = proc.Close() }()
	client := NewControlClient(RunInfo{
		Control: fmt.Sprintf("127.0.0.1:%d", lp),
		Token:   token,
		Pod:     podName,
	}, h.HTTPClient)
	return fn(client)
}

func (h *Host) probeRemoteRunning(ctx context.Context, podName string) (RunInfo, bool) {
	remoteRun, _, err := h.readRemoteRunFile(ctx, podName)
	if err != nil {
		return RunInfo{}, false
	}
	port, err := parseControlPort(remoteRun.Control)
	if err != nil {
		return RunInfo{}, false
	}
	err = h.callThroughForward(ctx, podName, port, remoteRun.Token, func(c *ControlClient) error {
		_, cerr := c.Status(ctx)
		return cerr
	})
	return remoteRun, err == nil
}

func (h *Host) waitForRemoteRunFile(ctx context.Context, podName string) (RunInfo, string, error) {
	pollScript := fmt.Sprintf(
		`S=%s; echo "STATE_DIR:$S"; echo "MARKER:$(cat "$S/ctxd.launch" 2>/dev/null || echo 0)"; cat "$S/ctxd.run" 2>/dev/null`,
		defaultRemoteStateExpr,
	)
	for {
		res, err := h.Pod.Exec(ctx, podName, []string{"sh", "-c", pollScript}, podrun.RunOpts{})
		if err == nil && res.ExitCode == 0 {
			if run, dir, ok := parsePollOutput(string(res.Stdout)); ok {
				return run, dir, nil
			}
		}
		select {
		case <-ctx.Done():
			return RunInfo{}, "", ctx.Err()
		case <-time.After(h.pollInterval()):
		}
	}
}

func (h *Host) readRemoteRunFile(ctx context.Context, podName string) (RunInfo, string, error) {
	script := fmt.Sprintf(`S=%s; echo "STATE_DIR:$S"; cat "$S/ctxd.run"`, defaultRemoteStateExpr)
	res, err := h.Pod.Exec(ctx, podName, []string{"sh", "-c", script}, podrun.RunOpts{})
	if err != nil || res.ExitCode != 0 {
		return RunInfo{}, "", fmt.Errorf("remote run file unavailable")
	}
	lines := strings.SplitN(strings.TrimSpace(string(res.Stdout)), "\n", 2)
	if len(lines) < 2 {
		return RunInfo{}, "", fmt.Errorf("empty remote run file")
	}
	dir := strings.TrimPrefix(lines[0], "STATE_DIR:")
	var info RunInfo
	if err := json.Unmarshal([]byte(lines[1]), &info); err != nil {
		return RunInfo{}, "", err
	}
	if info.Control == "" {
		return RunInfo{}, "", fmt.Errorf("remote run file missing control address")
	}
	return info, dir, nil
}

func parsePollOutput(raw string) (RunInfo, string, bool) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 3 {
		return RunInfo{}, "", false
	}
	dir := strings.TrimPrefix(lines[0], "STATE_DIR:")
	markerSec, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(lines[1], "MARKER:")), 10, 64)
	if err != nil {
		return RunInfo{}, "", false
	}
	body := strings.Join(lines[2:], "\n")
	var info RunInfo
	if err := json.Unmarshal([]byte(body), &info); err != nil {
		return RunInfo{}, "", false
	}
	if info.Control == "" || info.StartedAt.IsZero() {
		return RunInfo{}, "", false
	}
	// Stale run-file guard: started_at must not be older than the launch marker.
	if info.StartedAt.Unix() < markerSec {
		return RunInfo{}, "", false
	}
	return info, dir, true
}

func (h *Host) waitControlStatus(ctx context.Context, run RunInfo) (map[string]any, error) {
	client := NewControlClient(run, h.HTTPClient)
	for {
		st, err := client.Status(ctx)
		if err == nil {
			return st, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w (last error: %w)", ctx.Err(), err)
		case <-time.After(min(h.pollInterval(), 250*time.Millisecond)):
		}
	}
}

func (h *Host) fetchLogTail(ctx context.Context, podName string) string {
	script := fmt.Sprintf(`S=%s; tail -c 16384 "$S/ctxd.log" "$S/ctxd.out" 2>/dev/null`, defaultRemoteStateExpr)
	res, _ := h.Pod.Exec(ctx, podName, []string{"sh", "-c", script}, podrun.RunOpts{Timeout: 10 * time.Second})
	return strings.TrimSpace(string(res.Stdout))
}

func parseControlPort(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("invalid control address %q: %w", addr, err)
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("invalid control port in %q", addr)
	}
	return p, nil
}

func writeTemp0600(data []byte) (string, error) {
	f, err := os.CreateTemp("", "ctxt-pod-cfg-*.yaml")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func unreachableError(podName string, res podrun.Result, err error) *output.Error {
	msg := strings.TrimSpace(string(res.Stderr))
	if msg == "" && err != nil {
		msg = err.Error()
	}
	return &output.Error{
		Code:         output.CodeTransient,
		Message:      fmt.Sprintf("pod %q is not reachable: %s", podName, msg),
		SuggestedFix: fmt.Sprintf("run `pod stop %s` / `pod start %s`, or inspect the provider", podName, podName),
		ExitCode:     6,
	}
}
