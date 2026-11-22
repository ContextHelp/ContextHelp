package dpkmspod

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
)

var requiredLeaves = []string{"pod show", "pod start", "pod stop", "pod exec", "pod forward", "pod upload"}

// Host orchestrates a pod-hosted dpkms instance using a podrun.Client.
type Host struct {
	Pod           *podrun.Client
	StateDir      string
	HTTPClient    *http.Client
	Warn          io.Writer
	StartDeadline time.Duration
	StopDeadline  time.Duration
	PollInterval  time.Duration
	StopForward   func(pid int, startedAt time.Time)
}

// NewHost creates a dpkms pod host with production defaults.
func NewHost(podClient *podrun.Client, stateDir string) *Host {
	return &Host{
		Pod:           podClient,
		StateDir:      stateDir,
		HTTPClient:    &http.Client{Timeout: 5 * time.Second},
		Warn:          io.Discard,
		StartDeadline: 90 * time.Second,
		StopDeadline:  30 * time.Second,
		PollInterval:  1 * time.Second,
		StopForward:   stopForwardPID,
	}
}

// ServeOpts holds resolved parameters for launching dpkms on a pod.
type ServeOpts struct {
	PodName      string
	Autostart    bool
	DpkmsBin     string
	InstanceName string
	DataDir      string
	DpkmsPort    int
	LocalPort    int
	Profile      string
	Access       string
	AllowedHosts []string
	Auth         config.AuthConfig
}

// ServeResult is returned when dpkms is healthy on the pod and forwarded locally.
type ServeResult struct {
	Pod         string      `json:"pod"`
	Provider    string      `json:"provider"`
	Name        string      `json:"name,omitempty"`
	URL         string      `json:"url"`
	LocalPort   int         `json:"local_port"`
	DpkmsPort   int         `json:"dpkms_port"`
	DataDir     string      `json:"data_dir"`
	StartedAt   time.Time   `json:"started_at"`
	ForwardProc podrun.Proc `json:"-"`
}

// PSOpts configures pod-hosted dpkms ps inspection.
type PSOpts struct {
	PodName  string
	DpkmsBin string
	DataDir  string
}

// InstanceRow extends pidfile.Info with pod host and optional /healthz status.
type InstanceRow struct {
	pidfile.Info
	Host   string `json:"host"`
	Health string `json:"health,omitempty"`
}

// PSResult holds the remote instance listing plus pod reachability metadata.
type PSResult struct {
	Pod              string        `json:"pod"`
	PodStatus        string        `json:"pod_status"`
	Instances        []InstanceRow `json:"instances"`
	NotRunningReason string        `json:"not_running_reason,omitempty"`
	LastLaunched     *time.Time    `json:"last_launched,omitempty"`
}

// ShutdownOpts configures pod-hosted dpkms shutdown.
type ShutdownOpts struct {
	PodName   string
	DpkmsBin  string
	DataDir   string
	DpkmsPort int
	StopPod   bool
}

// ShutdownResult summarizes a completed remote dpkms shutdown.
type ShutdownResult struct {
	Pod           string `json:"pod"`
	PodStatus     string `json:"pod_status"`
	DpkmsPort     int    `json:"dpkms_port"`
	AlreadyDown   bool   `json:"already_down,omitempty"`
	EscalatedKill bool   `json:"escalated_kill,omitempty"`
	PodStopped    bool   `json:"pod_stopped,omitempty"`
}

// LaunchScriptOpts holds the validated tokens for BuildLaunchScript.
type LaunchScriptOpts struct {
	DpkmsBin         string
	InstanceName     string
	DpkmsPort        int
	DataDir          string
	RemoteConfigPath string
	Profile          string
}

// RenderPodConfig builds the minimal pod-side dpkms.yaml. It always includes
// "127.0.0.1" in server.allowed_hosts so forwarded Host: 127.0.0.1:<lp>
// requests never fail with 403 HOST_NOT_ALLOWED, and never includes daemon.*
// or any laptop path.
func RenderPodConfig(access string, allowedHosts []string, auth config.AuthConfig) ([]byte, error) {
	if access == "" {
		access = config.AccessPrivate
	}
	hosts := make([]string, 0, len(allowedHosts)+1)
	if !slices.Contains(allowedHosts, "127.0.0.1") {
		hosts = append(hosts, "127.0.0.1")
	}
	hosts = append(hosts, allowedHosts...)

	srv := map[string]any{
		"access":        access,
		"allowed_hosts": hosts,
	}
	if access != config.AccessPrivate {
		srv["auth"] = auth
	}
	doc := map[string]any{"server": srv}
	return yaml.Marshal(doc)
}

// BuildLaunchScript returns the detached shell command that starts dpkms serve
// inside a pod. The remote dpkms command ALWAYS ends with --runner local
// (Guarantee 8), runs with --grpc-port 0 --cookie-bridge-port 0, and validates
// every interpolated token against the P9 allowlist.
func BuildLaunchScript(opts LaunchScriptOpts) (string, error) {
	if opts.DpkmsBin == "" {
		opts.DpkmsBin = config.DefaultDaemonPodDpkmsBin
	}
	if opts.DpkmsPort == 0 {
		opts.DpkmsPort = config.DefaultDaemonPodDpkmsPort
	}
	checks := [][2]string{
		{"daemon.pod.dpkms_bin", opts.DpkmsBin},
		{"daemon.pod.data_dir", opts.DataDir},
		{"remote_config_path", opts.RemoteConfigPath},
	}
	if opts.InstanceName != "" {
		checks = append(checks, [2]string{"name", opts.InstanceName})
	}
	if opts.Profile != "" {
		checks = append(checks, [2]string{"profile", opts.Profile})
	}
	for _, kv := range checks {
		if err := podrun.ValidateToken(kv[0], kv[1]); err != nil {
			return "", err
		}
	}

	args := []string{opts.DpkmsBin, "serve"}
	if opts.InstanceName != "" {
		args = append(args, "--name", opts.InstanceName)
	}
	args = append(args,
		"--port", strconv.Itoa(opts.DpkmsPort),
		"--grpc-port", "0",
		"--cookie-bridge-port", "0",
		"--data-dir", opts.DataDir,
		"-c", opts.RemoteConfigPath,
	)
	if opts.Profile != "" {
		args = append(args, "--profile", opts.Profile)
	}
	// Guarantee 8: remote dpkms serve ALWAYS ends with --runner local.
	args = append(args, "--runner", config.DaemonRunnerLocal)

	logPath := filepath.ToSlash(filepath.Join(opts.DataDir, "run", "dpkms-pod.log"))
	runDir := filepath.ToSlash(filepath.Join(opts.DataDir, "run"))
	return fmt.Sprintf(
		"mkdir -p %s && setsid nohup %s </dev/null >>%s 2>&1 &",
		runDir,
		strings.Join(args, " "),
		logPath,
	), nil
}

// Serve runs the 10-step pod-hosted dpkms launch sequence and returns a live
// forward handle and local URL.
func (h *Host) Serve(ctx context.Context, opts ServeOpts) (*ServeResult, error) {
	opts, podInfo, err := h.preflightServe(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := h.uploadAndLaunch(ctx, opts); err != nil {
		return nil, err
	}
	proc, localPort, err := h.forwardAndWaitHealthy(ctx, opts)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	rec := Record{
		Version:          1,
		Pod:              opts.PodName,
		Provider:         podInfo.Provider,
		Name:             opts.InstanceName,
		DpkmsPort:        opts.DpkmsPort,
		LocalPort:        localPort,
		ForwardPID:       proc.PID(),
		ForwardStartedAt: now,
		DataDir:          opts.DataDir,
		StartedAt:        now,
	}
	if err := WriteRecord(h.StateDir, rec); err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &ServeResult{
		Pod:         opts.PodName,
		Provider:    podInfo.Provider,
		Name:        opts.InstanceName,
		URL:         fmt.Sprintf("http://127.0.0.1:%d", localPort),
		LocalPort:   localPort,
		DpkmsPort:   opts.DpkmsPort,
		DataDir:     opts.DataDir,
		StartedAt:   now,
		ForwardProc: proc,
	}, nil
}

func (h *Host) preflightServe(ctx context.Context, opts ServeOpts) (ServeOpts, *podrun.PodInfo, error) {
	if opts.DpkmsBin == "" {
		opts.DpkmsBin = config.DefaultDaemonPodDpkmsBin
	}
	if opts.DpkmsPort == 0 {
		opts.DpkmsPort = config.DefaultDaemonPodDpkmsPort
	}
	if opts.DataDir == "" {
		e := output.UsageError("daemon.pod.data_dir is required for dpkms pod runs")
		e.SuggestedFix = "set daemon.pod.data_dir to a persistent path on the pod"
		return opts, nil, e
	}
	for _, kv := range [][2]string{
		{"daemon.pod.name", opts.PodName},
		{"daemon.pod.dpkms_bin", opts.DpkmsBin},
		{"daemon.pod.data_dir", opts.DataDir},
	} {
		if err := podrun.ValidateToken(kv[0], kv[1]); err != nil {
			return opts, nil, err
		}
	}

	caps, err := h.Pod.Probe(ctx)
	if err != nil {
		return opts, nil, err
	}
	if err := podrun.RequireLeaves(caps, requiredLeaves...); err != nil {
		return opts, nil, err
	}

	podInfo, err := h.Pod.GateForStart(ctx, opts.PodName, opts.Autostart, "dpkms serve", func() {
		_ = DeleteRecord(h.StateDir, opts.PodName)
	})
	if err != nil {
		return opts, nil, err
	}

	// Step 5: Verify dpkms binary on the pod (F9: exit 127 -> exit 70).
	verRes, verErr := h.Pod.Exec(ctx, opts.PodName, []string{opts.DpkmsBin, "--version"}, podrun.RunOpts{})
	if verRes.ExitCode == 127 {
		return opts, nil, &output.Error{
			Code:         output.CodePrerequisite,
			Message:      fmt.Sprintf("%s not found on pod %q", opts.DpkmsBin, opts.PodName),
			SuggestedFix: fmt.Sprintf("install %s in the pod image or set daemon.pod.dpkms_bin", opts.DpkmsBin),
			ExitCode:     70,
		}
	}
	if verErr != nil || verRes.ExitCode != 0 {
		return opts, nil, unreachableError(opts.PodName, verRes, verErr)
	}

	// Step 6: Conflict check on remote port (F10: exit 4).
	existing, err := h.remotePS(ctx, opts.PodName, opts.DpkmsBin, opts.DataDir)
	if err != nil {
		return opts, nil, err
	}
	for _, inst := range existing {
		if inst.Port == opts.DpkmsPort {
			e := output.ConflictError(fmt.Sprintf("dpkms is already running on pod %q port %d (pid %d)", opts.PodName, opts.DpkmsPort, inst.PID))
			e.SuggestedFix = fmt.Sprintf("inspect with `dpkms ps --runner pod --pod %s` or stop with `dpkms shutdown --runner pod --pod %s`", opts.PodName, opts.PodName)
			return opts, nil, e
		}
	}

	return opts, podInfo, nil
}

func (h *Host) uploadAndLaunch(ctx context.Context, opts ServeOpts) error {
	cfgBytes, err := RenderPodConfig(opts.Access, opts.AllowedHosts, opts.Auth)
	if err != nil {
		return fmt.Errorf("render dpkms pod config: %w", err)
	}
	localCfg, err := writeTemp0600(cfgBytes)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(localCfg) }()

	remoteCfg := "/tmp/dpkms-pod-" + opts.PodName + ".yaml"
	if err := h.Pod.Upload(ctx, opts.PodName, localCfg, remoteCfg); err != nil {
		return err
	}

	script, err := BuildLaunchScript(LaunchScriptOpts{
		DpkmsBin:         opts.DpkmsBin,
		InstanceName:     opts.InstanceName,
		DpkmsPort:        opts.DpkmsPort,
		DataDir:          opts.DataDir,
		RemoteConfigPath: remoteCfg,
		Profile:          opts.Profile,
	})
	if err != nil {
		return err
	}
	res, err := h.Pod.Exec(ctx, opts.PodName, []string{"sh", "-c", script}, podrun.RunOpts{})
	if err != nil || res.ExitCode != 0 {
		return unreachableError(opts.PodName, res, err)
	}
	return nil
}

func (h *Host) forwardAndWaitHealthy(ctx context.Context, opts ServeOpts) (podrun.Proc, int, error) {
	deadlineCtx, cancel := context.WithTimeout(ctx, h.startDeadline())
	defer cancel()

	proc, localPort, err := h.Pod.Forward(deadlineCtx, opts.PodName, opts.LocalPort, opts.DpkmsPort)
	if err != nil {
		return nil, 0, &output.Error{
			Code:         output.CodeTransient,
			Message:      fmt.Sprintf("forward to dpkms port %d on pod %q failed: %v", opts.DpkmsPort, opts.PodName, err),
			Cause:        h.fetchLogTail(ctx, opts.PodName, opts.DataDir),
			SuggestedFix: fmt.Sprintf("check pod %q reachability and %s/run/dpkms-pod.log", opts.PodName, opts.DataDir),
			ExitCode:     6,
		}
	}

	if err := h.waitHealthz(deadlineCtx, localPort); err != nil {
		_ = proc.Close()
		return nil, 0, &output.Error{
			Code:         output.CodeTransient,
			Message:      fmt.Sprintf("dpkms on pod %q did not become healthy: %v", opts.PodName, err),
			Cause:        h.fetchLogTail(ctx, opts.PodName, opts.DataDir),
			SuggestedFix: fmt.Sprintf("inspect %s/run/dpkms-pod.log on pod %q", opts.DataDir, opts.PodName),
			ExitCode:     6,
		}
	}
	return proc, localPort, nil
}

// PS inspects running dpkms instances on the named pod.
func (h *Host) PS(ctx context.Context, opts PSOpts) (*PSResult, error) {
	if opts.DpkmsBin == "" {
		opts.DpkmsBin = config.DefaultDaemonPodDpkmsBin
	}
	if err := podrun.ValidateToken("daemon.pod.name", opts.PodName); err != nil {
		return nil, err
	}
	if opts.DataDir != "" {
		if err := podrun.ValidateToken("daemon.pod.data_dir", opts.DataDir); err != nil {
			return nil, err
		}
	}

	caps, err := h.Pod.Probe(ctx)
	if err != nil {
		return nil, err
	}
	if err := podrun.RequireLeaves(caps, "pod show", "pod exec"); err != nil {
		return nil, err
	}

	info, running, err := h.Pod.GateForQuery(ctx, opts.PodName, func() {
		_ = DeleteRecord(h.StateDir, opts.PodName)
	})
	if err != nil {
		return nil, err
	}
	if !running {
		return &PSResult{
			Pod:              opts.PodName,
			PodStatus:        info.Status,
			Instances:        []InstanceRow{},
			NotRunningReason: fmt.Sprintf("pod %s is %s", opts.PodName, info.Status),
		}, nil
	}

	instances, err := h.remotePS(ctx, opts.PodName, opts.DpkmsBin, opts.DataDir)
	if err != nil {
		return nil, err
	}

	rec, _ := ReadRecord(h.StateDir, opts.PodName)
	rows := make([]InstanceRow, 0, len(instances))
	for _, inst := range instances {
		health := "unknown"
		if rec != nil && rec.DpkmsPort == inst.Port && rec.LocalPort > 0 {
			health = h.probeHealthz(ctx, rec.LocalPort)
		}
		rows = append(rows, InstanceRow{
			Info:   inst,
			Host:   "pod " + opts.PodName,
			Health: health,
		})
	}

	res := &PSResult{
		Pod:       opts.PodName,
		PodStatus: info.Status,
		Instances: rows,
	}
	if len(rows) == 0 && rec != nil && !rec.StartedAt.IsZero() {
		t := rec.StartedAt
		res.LastLaunched = &t
	}
	return res, nil
}

// Shutdown gracefully stops the pod-hosted dpkms instance, escalates if needed,
// cleans up any recorded forward helper + local record, and optionally stops
// the pod.
func (h *Host) Shutdown(ctx context.Context, opts ShutdownOpts) (*ShutdownResult, error) {
	if opts.DpkmsBin == "" {
		opts.DpkmsBin = config.DefaultDaemonPodDpkmsBin
	}
	if opts.DpkmsPort == 0 {
		opts.DpkmsPort = config.DefaultDaemonPodDpkmsPort
	}
	if err := podrun.ValidateToken("daemon.pod.name", opts.PodName); err != nil {
		return nil, err
	}
	if opts.DataDir != "" {
		if err := podrun.ValidateToken("daemon.pod.data_dir", opts.DataDir); err != nil {
			return nil, err
		}
	}

	caps, err := h.Pod.Probe(ctx)
	if err != nil {
		return nil, err
	}
	need := []string{"pod show", "pod exec"}
	if opts.StopPod {
		need = append(need, "pod stop")
	}
	if err := podrun.RequireLeaves(caps, need...); err != nil {
		return nil, err
	}

	info, running, err := h.Pod.GateForQuery(ctx, opts.PodName, func() {
		_ = DeleteRecord(h.StateDir, opts.PodName)
	})
	if err != nil {
		return nil, err
	}
	if !running {
		h.cleanupLocalForwardAndRecord(opts.PodName)
		return &ShutdownResult{
			Pod:         opts.PodName,
			PodStatus:   info.Status,
			DpkmsPort:   opts.DpkmsPort,
			AlreadyDown: true,
		}, nil
	}

	escalated, err := h.stopRemoteInstance(ctx, opts)
	if err != nil {
		return nil, err
	}

	h.cleanupLocalForwardAndRecord(opts.PodName)

	podStopped := false
	if opts.StopPod {
		if _, err := h.Pod.StopPod(ctx, opts.PodName, "dpkms shutdown"); err != nil {
			return nil, err
		}
		podStopped = true
	}

	return &ShutdownResult{
		Pod:           opts.PodName,
		PodStatus:     info.Status,
		DpkmsPort:     opts.DpkmsPort,
		EscalatedKill: escalated,
		PodStopped:    podStopped,
	}, nil
}

func (h *Host) stopRemoteInstance(ctx context.Context, opts ShutdownOpts) (bool, error) {
	args := []string{opts.DpkmsBin, "shutdown", "--port", strconv.Itoa(opts.DpkmsPort)}
	if opts.DataDir != "" {
		args = append(args, "--data-dir", opts.DataDir)
	}
	res, err := h.Pod.Exec(ctx, opts.PodName, args, podrun.RunOpts{})
	sigtermSent := err == nil && res.ExitCode == 0

	if !sigtermSent {
		pid, found := h.findRemotePID(ctx, opts.PodName, opts.DpkmsBin, opts.DataDir, opts.DpkmsPort)
		if !found {
			return false, nil
		}
		_, _ = h.Pod.Exec(ctx, opts.PodName, []string{"kill", "-TERM", strconv.Itoa(pid)}, podrun.RunOpts{})
	}

	if h.waitUntilGone(ctx, opts.PodName, opts.DpkmsBin, opts.DataDir, opts.DpkmsPort, h.stopDeadline()) {
		return false, nil
	}

	// Escalate once with SIGKILL if still listed after SIGTERM.
	pid, found := h.findRemotePID(ctx, opts.PodName, opts.DpkmsBin, opts.DataDir, opts.DpkmsPort)
	if !found {
		return false, nil
	}
	_, _ = h.Pod.Exec(ctx, opts.PodName, []string{"kill", "-KILL", strconv.Itoa(pid)}, podrun.RunOpts{})
	if h.waitUntilGone(ctx, opts.PodName, opts.DpkmsBin, opts.DataDir, opts.DpkmsPort, min(h.stopDeadline(), 5*time.Second)) {
		return true, nil
	}

	return true, &output.Error{
		Code:         output.CodeTransient,
		Message:      fmt.Sprintf("dpkms on pod %q port %d did not exit after SIGTERM and SIGKILL (SIGKILL skips graceful drain)", opts.PodName, opts.DpkmsPort),
		SuggestedFix: fmt.Sprintf("inspect processes on pod %q with `pod exec %s -- ps aux`", opts.PodName, opts.PodName),
		ExitCode:     6,
	}
}

func (h *Host) remotePS(ctx context.Context, podName, dpkmsBin, dataDir string) ([]pidfile.Info, error) {
	args := []string{dpkmsBin, "ps", "--format", "json"}
	if dataDir != "" {
		args = append(args, "--data-dir", dataDir)
	}
	res, err := h.Pod.Exec(ctx, podName, args, podrun.RunOpts{})
	if err != nil || res.ExitCode != 0 {
		return nil, unreachableError(podName, res, err)
	}
	var env struct {
		Instances []pidfile.Info `json:"instances"`
	}
	if len(strings.TrimSpace(string(res.Stdout))) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(res.Stdout, &env); err != nil {
		return nil, &output.Error{
			Code:     output.CodeGeneric,
			Message:  fmt.Sprintf("dpkms ps on pod %q returned invalid JSON: %v", podName, err),
			ExitCode: 1,
		}
	}
	return env.Instances, nil
}

func (h *Host) findRemotePID(ctx context.Context, podName, dpkmsBin, dataDir string, port int) (int, bool) {
	instances, err := h.remotePS(ctx, podName, dpkmsBin, dataDir)
	if err != nil {
		return 0, false
	}
	for _, inst := range instances {
		if inst.Port == port && inst.PID > 0 {
			return inst.PID, true
		}
	}
	return 0, false
}

func (h *Host) waitUntilGone(ctx context.Context, podName, dpkmsBin, dataDir string, port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if _, found := h.findRemotePID(ctx, podName, dpkmsBin, dataDir, port); !found {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(min(h.pollInterval(), 500*time.Millisecond)):
		}
	}
}

func (h *Host) waitHealthz(ctx context.Context, localPort int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", localPort)
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := h.httpClient().Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("healthz status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%w (last error: %w)", ctx.Err(), lastErr)
			}
			return ctx.Err()
		case <-time.After(min(h.pollInterval(), 250*time.Millisecond)):
		}
	}
}

func (h *Host) probeHealthz(ctx context.Context, localPort int) string {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", localPort)
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return "unknown"
	}
	resp, err := h.httpClient().Do(req)
	if err != nil {
		return "unreachable"
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return "healthy"
	}
	return "degraded"
}

func (h *Host) fetchLogTail(ctx context.Context, podName, dataDir string) string {
	logPath := filepath.ToSlash(filepath.Join(dataDir, "run", "dpkms-pod.log"))
	res, _ := h.Pod.Exec(ctx, podName, []string{"tail", "-c", "16384", logPath}, podrun.RunOpts{Timeout: 10 * time.Second})
	return strings.TrimSpace(string(res.Stdout))
}

func (h *Host) cleanupLocalForwardAndRecord(podName string) {
	if rec, err := ReadRecord(h.StateDir, podName); err == nil && rec != nil && rec.ForwardPID > 0 {
		stopper := h.StopForward
		if stopper == nil {
			stopper = stopForwardPID
		}
		stopper(rec.ForwardPID, rec.ForwardStartedAt)
	}
	_ = DeleteRecord(h.StateDir, podName)
}

func (h *Host) httpClient() *http.Client {
	if h.HTTPClient != nil {
		return h.HTTPClient
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func (h *Host) startDeadline() time.Duration {
	if h.StartDeadline > 0 {
		return h.StartDeadline
	}
	return 90 * time.Second
}

func (h *Host) stopDeadline() time.Duration {
	if h.StopDeadline > 0 {
		return h.StopDeadline
	}
	return 30 * time.Second
}

func (h *Host) pollInterval() time.Duration {
	if h.PollInterval > 0 {
		return h.PollInterval
	}
	return time.Second
}

func writeTemp0600(data []byte) (string, error) {
	f, err := os.CreateTemp("", "dpkms-pod-cfg-*.yaml")
	if err != nil {
		return "", fmt.Errorf("dpkms pod temp config: %w", err)
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

func unreachableError(podName string, res podrun.Result, err error) error {
	detail := strings.TrimSpace(string(res.Stderr))
	if detail == "" && err != nil {
		detail = err.Error()
	}
	return &output.Error{
		Code:         output.CodeTransient,
		Message:      fmt.Sprintf("pod %q is not reachable: %s", podName, detail),
		SuggestedFix: fmt.Sprintf("run `pod stop %s && pod start %s` or inspect the provider", podName, podName),
		ExitCode:     6,
	}
}
