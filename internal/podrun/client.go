package podrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"hop.top/kit/go/console/output"
)

// Timeouts groups the four per-class deadlines used by Client.
type Timeouts struct {
	Probe     time.Duration
	Query     time.Duration
	Exec      time.Duration
	Lifecycle time.Duration
}

// DefaultTimeouts returns the built-in pod call deadlines.
func DefaultTimeouts() Timeouts {
	return Timeouts{
		Probe:     10 * time.Second,
		Query:     15 * time.Second,
		Exec:      90 * time.Second,
		Lifecycle: 5 * time.Minute,
	}
}

// Capabilities is the JSON document returned by `pod capabilities --format json`.
type Capabilities struct {
	Tool          string           `json:"tool"`
	Version       string           `json:"version"`
	SchemaVersion string           `json:"schema_version"`
	Leaves        []CapabilityLeaf `json:"leaves"`
}

// CapabilityLeaf is one invocable verb entry in Capabilities.
type CapabilityLeaf struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Short      string `json:"short,omitempty"`
	SideEffect string `json:"side_effect,omitempty"`
	Idempotent string `json:"idempotent,omitempty"`
}

// HasLeaf reports whether caps declares the command path (e.g. "pod exec" or "exec").
func (c *Capabilities) HasLeaf(path string) bool {
	if c == nil {
		return false
	}
	qualified := path
	if !strings.HasPrefix(qualified, BinaryName+" ") {
		qualified = BinaryName + " " + path
	}
	for _, l := range c.Leaves {
		if l.Path == path || l.Path == qualified {
			return true
		}
	}
	return false
}

// PodInfo is the JSON detail returned by `pod show|start|stop|create --format json`.
type PodInfo struct {
	Name        string            `json:"name"`
	ID          string            `json:"id"`
	Provider    string            `json:"provider"`
	Status      string            `json:"status"`
	Active      string            `json:"active"`
	Image       string            `json:"image"`
	GPU         string            `json:"gpu"`
	GPUCount    int               `json:"gpu_count"`
	ComputerUse string            `json:"computer_use"`
	ViewURL     string            `json:"view_url,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// CreateOpts configures `pod create` for ephemeral pod execution.
type CreateOpts struct {
	Name     string
	Provider string
	Image    string
	Note     string
	Tags     []string
}

// Client speaks the pod CLI contract over a Runner seam.
type Client struct {
	BinPath  string
	Runner   Runner
	Timeouts Timeouts
	Warn     io.Writer
	// Env optionally injects extra env vars on every subprocess (e.g. record mode).
	Env []string
	// ForwardReadyTimeout bounds waiting for a new forward to accept local TCP
	// connections. Defaults to 15s.
	ForwardReadyTimeout time.Duration
	// PickLocalPort overrides free loopback port selection in tests.
	PickLocalPort func() (int, error)
	// DialLocal overrides local TCP readiness probing in tests.
	DialLocal func(ctx context.Context, addr string) error
}

// NewClient constructs a Client using ExecRunner and DefaultTimeouts when zero.
func NewClient(binPath string, r Runner, t Timeouts) *Client {
	if r == nil {
		r = NewExecRunner()
	}
	def := DefaultTimeouts()
	if t.Probe <= 0 {
		t.Probe = def.Probe
	}
	if t.Query <= 0 {
		t.Query = def.Query
	}
	if t.Exec <= 0 {
		t.Exec = def.Exec
	}
	if t.Lifecycle <= 0 {
		t.Lifecycle = def.Lifecycle
	}
	return &Client{
		BinPath:             binPath,
		Runner:              r,
		Timeouts:            t,
		ForwardReadyTimeout: 15 * time.Second,
	}
}

func (c *Client) docArgs(verbArgs ...string) []string {
	argv := make([]string, 0, 1+len(verbArgs)+4)
	argv = append(argv, c.BinPath)
	argv = append(argv, verbArgs...)
	argv = append(argv, "--format", "json", "--no-hints", "--no-color")
	return argv
}

// Probe runs `pod capabilities --format json` and checks tool + schema_version.
func (c *Client) Probe(ctx context.Context) (*Capabilities, error) {
	argv := c.docArgs("capabilities")
	res, err := c.Runner.Run(ctx, argv, RunOpts{Timeout: c.Timeouts.Probe, Env: c.Env})
	if err != nil || res.ExitCode != 0 {
		mapped := MapError("capabilities", "", c.Timeouts.Probe, res, err)
		mapped.Code = output.CodePrerequisite
		mapped.ExitCode = exitCodePrerequisite()
		return nil, mapped
	}
	var caps Capabilities
	if err := json.Unmarshal(res.Stdout, &caps); err != nil {
		_, tail := stderrTail(res.Stderr)
		return nil, &output.Error{
			Code:         output.CodePrerequisite,
			Message:      fmt.Sprintf("pod capabilities returned invalid JSON: %v", err),
			Cause:        tail,
			SuggestedFix: "verify `pod capabilities --format json` works on PATH",
			ExitCode:     exitCodePrerequisite(),
		}
	}
	if caps.Tool != BinaryName {
		return nil, &output.Error{
			Code:         output.CodePrerequisite,
			Message:      fmt.Sprintf("unexpected pod capabilities tool %q (want %q)", caps.Tool, BinaryName),
			SuggestedFix: "ensure `pod` on PATH is the hop.top pod CLI",
			ExitCode:     exitCodePrerequisite(),
		}
	}
	major, _, _ := strings.Cut(caps.SchemaVersion, ".")
	if major != "1" {
		return nil, &output.Error{
			Code:         output.CodePrerequisite,
			Message:      fmt.Sprintf("unsupported pod capabilities schema_version %q (want 1.x)", caps.SchemaVersion),
			SuggestedFix: "upgrade ctxt or pod so their capabilities schema versions match",
			ExitCode:     exitCodePrerequisite(),
		}
	}
	return &caps, nil
}

// RequireLeaves checks that every required command leaf is present in caps.
func RequireLeaves(caps *Capabilities, leaves ...string) error {
	for _, leaf := range leaves {
		if !caps.HasLeaf(leaf) {
			return &output.Error{
				Code:         output.CodePrerequisite,
				Message:      fmt.Sprintf("the installed pod has no %q", leaf),
				SuggestedFix: fmt.Sprintf("upgrade pod to a release providing %q, or use runner=local", leaf),
				ExitCode:     exitCodePrerequisite(),
			}
		}
	}
	return nil
}

// Show fetches the local pod record via `pod show <name> --format json`.
func (c *Client) Show(ctx context.Context, name string) (*PodInfo, error) {
	if err := ValidateToken("pod name", name); err != nil {
		return nil, err
	}
	argv := c.docArgs("show", name)
	res, err := c.Runner.Run(ctx, argv, RunOpts{Timeout: c.Timeouts.Query, Env: c.Env})
	if err != nil || res.ExitCode != 0 {
		return nil, MapError("show", name, c.Timeouts.Query, res, err)
	}
	return decodePodInfo("show", res.Stdout)
}

// StartPod starts a stopped pod via `pod start <name>`.
func (c *Client) StartPod(ctx context.Context, name, note string) (*PodInfo, error) {
	if err := ValidateToken("pod name", name); err != nil {
		return nil, err
	}
	args := []string{"start", name}
	if note != "" {
		args = append(args, "--note", note)
	}
	res, err := c.Runner.Run(ctx, c.docArgs(args...), RunOpts{Timeout: c.Timeouts.Lifecycle, Env: c.Env})
	if err != nil || res.ExitCode != 0 {
		mapped := MapError("start", name, c.Timeouts.Lifecycle, res, err)
		mapped.SuggestedFix = appendFix(mapped.SuggestedFix, "pod status is a local record")
		return nil, mapped
	}
	return decodePodInfo("start", res.Stdout)
}

// StopPod stops a pod via `pod stop <name>`.
func (c *Client) StopPod(ctx context.Context, name, note string) (*PodInfo, error) {
	if err := ValidateToken("pod name", name); err != nil {
		return nil, err
	}
	args := []string{"stop", name}
	if note != "" {
		args = append(args, "--note", note)
	}
	res, err := c.Runner.Run(ctx, c.docArgs(args...), RunOpts{Timeout: c.Timeouts.Lifecycle, Env: c.Env})
	if err != nil || res.ExitCode != 0 {
		return nil, MapError("stop", name, c.Timeouts.Lifecycle, res, err)
	}
	return decodePodInfo("stop", res.Stdout)
}

// CreatePod creates a new ephemeral pod via `pod create`.
func (c *Client) CreatePod(ctx context.Context, opts CreateOpts) (*PodInfo, error) {
	args := []string{"create"}
	if opts.Name != "" {
		if err := ValidateToken("pod name", opts.Name); err != nil {
			return nil, err
		}
		args = append(args, opts.Name)
	}
	if opts.Provider != "" {
		if err := ValidateToken("provider", opts.Provider); err != nil {
			return nil, err
		}
		args = append(args, "--provider", opts.Provider)
	}
	if opts.Image != "" {
		if err := ValidateToken("image", opts.Image); err != nil {
			return nil, err
		}
		args = append(args, "--image", opts.Image)
	}
	for _, tag := range opts.Tags {
		if err := ValidateToken("tag", tag); err != nil {
			return nil, err
		}
		args = append(args, "--tag", tag)
	}
	if opts.Note != "" {
		args = append(args, "--note", opts.Note)
	}
	res, err := c.Runner.Run(ctx, c.docArgs(args...), RunOpts{Timeout: c.Timeouts.Lifecycle, Env: c.Env})
	if err != nil || res.ExitCode != 0 {
		return nil, MapError("create", opts.Name, c.Timeouts.Lifecycle, res, err)
	}
	return decodePodInfo("create", res.Stdout)
}

// DestroyPod destroys a pod non-interactively via `pod destroy <name> --force`.
func (c *Client) DestroyPod(ctx context.Context, name, note string) error {
	if err := ValidateToken("pod name", name); err != nil {
		return err
	}
	if note == "" {
		note = "ctxt ephemeral cleanup"
	}
	args := []string{"destroy", name, "--force", "--note", note}
	res, err := c.Runner.Run(ctx, c.docArgs(args...), RunOpts{Timeout: c.Timeouts.Lifecycle, Env: c.Env})
	if err != nil || res.ExitCode != 0 {
		return MapError("destroy", name, c.Timeouts.Lifecycle, res, err)
	}
	return nil
}

// GateForStart enforces the pod gate before launching a daemon. It runs
// `pod show` first so `pod start` is never called on a `destroyed` or
// `provisioning` row, and calls `pod start` only on `stopped` when autostart
// is true. onDeleteRecord is invoked when the pod is missing or destroyed.
func (c *Client) GateForStart(ctx context.Context, name string, autostart bool, note string, onDeleteRecord func()) (*PodInfo, error) {
	info, err := c.Show(ctx, name)
	if err != nil {
		var oe *output.Error
		if onDeleteRecord != nil && isExitCode(err, 3, &oe) {
			onDeleteRecord()
		}
		return nil, err
	}
	switch info.Status {
	case "running":
		return info, nil
	case "destroyed":
		if onDeleteRecord != nil {
			onDeleteRecord()
		}
		return nil, &output.Error{
			Code:         output.CodeNotFound,
			Message:      fmt.Sprintf("pod %q was destroyed", name),
			SuggestedFix: "create a pod with `pod create` and set daemon.pod.name",
			ExitCode:     exitCodeFor(output.CodeNotFound, 3),
		}
	case "provisioning":
		return nil, &output.Error{
			Code:         output.CodeTransient,
			Message:      fmt.Sprintf("pod %q is still provisioning", name),
			SuggestedFix: fmt.Sprintf("wait for provisioning to complete (`pod show %s`) and retry", name),
			ExitCode:     exitCodeFor(output.CodeTransient, 6),
		}
	case "stopped":
		if !autostart {
			e := output.UsageError(fmt.Sprintf("pod %q is stopped", name))
			e.SuggestedFix = fmt.Sprintf("run `pod start %s` or set daemon.pod.autostart: true", name)
			return nil, e
		}
		if c.Warn != nil {
			_, _ = fmt.Fprintf(c.Warn, "Starting stopped pod %q (provider: %s)...\n", name, info.Provider)
		}
		return c.StartPod(ctx, name, note)
	default:
		return nil, &output.Error{
			Code:     output.CodeGeneric,
			Message:  fmt.Sprintf("pod %q has unexpected status %q", name, info.Status),
			ExitCode: exitCodeFor(output.CodeGeneric, 1),
		}
	}
}

// GateForQuery enforces the pod gate for status/stop/ps/shutdown paths.
// Returns (info, reachable, err). When reachable is false and err is nil,
// the pod is stopped or destroyed so no remote daemon can be running.
func (c *Client) GateForQuery(ctx context.Context, name string, onDeleteRecord func()) (*PodInfo, bool, error) {
	info, err := c.Show(ctx, name)
	if err != nil {
		var oe *output.Error
		if onDeleteRecord != nil && isExitCode(err, 3, &oe) {
			onDeleteRecord()
		}
		return nil, false, err
	}
	switch info.Status {
	case "running":
		return info, true, nil
	case "destroyed":
		if onDeleteRecord != nil {
			onDeleteRecord()
		}
		return info, false, nil
	case "stopped":
		return info, false, nil
	case "provisioning":
		return info, false, &output.Error{
			Code:         output.CodeTransient,
			Message:      fmt.Sprintf("pod %q is still provisioning", name),
			SuggestedFix: fmt.Sprintf("wait for provisioning to complete (`pod show %s`) and retry", name),
			ExitCode:     exitCodeFor(output.CodeTransient, 6),
		}
	default:
		return info, false, &output.Error{
			Code:     output.CodeGeneric,
			Message:  fmt.Sprintf("pod %q has unexpected status %q", name, info.Status),
			ExitCode: exitCodeFor(output.CodeGeneric, 1),
		}
	}
}

// Exec runs `pod exec <name> -- <cmdArgs...>` without adding document flags
// (stdout/stderr belong to the remote command).
func (c *Client) Exec(ctx context.Context, name string, cmdArgs []string, opts RunOpts) (Result, error) {
	if err := ValidateToken("pod name", name); err != nil {
		return Result{}, err
	}
	if len(cmdArgs) == 0 {
		return Result{}, output.UsageError("pod exec requires a remote command")
	}
	argv := make([]string, 0, 4+len(cmdArgs))
	argv = append(argv, c.BinPath, "exec", name, "--")
	argv = append(argv, cmdArgs...)

	if opts.Timeout <= 0 {
		opts.Timeout = c.Timeouts.Exec
	}
	if len(c.Env) > 0 {
		opts.Env = append(append([]string(nil), c.Env...), opts.Env...)
	}
	return c.Runner.Run(ctx, argv, opts)
}

// Upload places localPath at remotePath on pod via `pod upload <name> <local> <remote>`.
func (c *Client) Upload(ctx context.Context, name, localPath, remotePath string) error {
	if err := ValidateToken("pod name", name); err != nil {
		return err
	}
	if err := ValidateToken("remote path", remotePath); err != nil {
		return err
	}
	argv := []string{c.BinPath, "upload", name, localPath, remotePath}
	res, err := c.Runner.Run(ctx, argv, RunOpts{Timeout: c.Timeouts.Exec, Env: c.Env})
	if err != nil || res.ExitCode != 0 {
		return MapError("upload", name, c.Timeouts.Exec, res, err)
	}
	return nil
}

// Forward opens a loopback port forward via `pod forward <name> <local>:<remote>`.
// When localPort is 0, a free loopback port is selected and retried up to 3
// times on bind collision (F13).
func (c *Client) Forward(ctx context.Context, name string, localPort, remotePort int) (Proc, int, error) {
	if err := ValidateToken("pod name", name); err != nil {
		return nil, 0, err
	}
	if remotePort < 1 || remotePort > 65535 {
		return nil, 0, output.UsageError(fmt.Sprintf("invalid remote port %d", remotePort))
	}
	if localPort < 0 || localPort > 65535 {
		return nil, 0, output.UsageError(fmt.Sprintf("invalid local port %d", localPort))
	}

	attempts := 1
	pickFree := localPort == 0
	if pickFree {
		attempts = 3
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		lp := localPort
		if pickFree {
			p, err := c.pickPort()
			if err != nil {
				return nil, 0, err
			}
			lp = p
		}
		mapping := strconv.Itoa(lp) + ":" + strconv.Itoa(remotePort)
		argv := []string{c.BinPath, "forward", name, mapping}
		proc, err := c.Runner.Start(ctx, argv, RunOpts{Env: c.Env})
		if err != nil {
			lastErr = err
			continue
		}
		if err := c.waitForwardReady(ctx, proc, lp); err != nil {
			_ = proc.Close()
			lastErr = err
			continue
		}
		return proc, lp, nil
	}
	return nil, 0, &output.Error{
		Code:         output.CodeGeneric,
		Message:      fmt.Sprintf("pod forward %s to port %d failed: %v", name, remotePort, lastErr),
		SuggestedFix: fmt.Sprintf("check pod connectivity (`pod show %s`) and local port availability", name),
		ExitCode:     exitCodeFor(output.CodeGeneric, 1),
	}
}

func (c *Client) pickPort() (int, error) {
	if c.PickLocalPort != nil {
		return c.PickLocalPort()
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("pick loopback port: %w", err)
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	_ = ln.Close()
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", ln.Addr())
	}
	return tcpAddr.Port, nil
}

func (c *Client) waitForwardReady(ctx context.Context, proc Proc, localPort int) error {
	readyTimeout := c.ForwardReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = 15 * time.Second
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort))
	exited := make(chan error, 1)
	go func() {
		exited <- proc.Wait()
	}()

	dial := c.DialLocal
	if dial == nil {
		dial = func(dctx context.Context, target string) error {
			var d net.Dialer
			d.Timeout = 250 * time.Millisecond
			conn, err := d.DialContext(dctx, "tcp", target)
			if err != nil {
				return err
			}
			_ = conn.Close()
			return nil
		}
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := dial(deadlineCtx, addr); err == nil {
			return nil
		}
		select {
		case err := <-exited:
			if err == nil {
				return fmt.Errorf("forward process exited before listening on %s", addr)
			}
			return fmt.Errorf("forward process exited: %w", err)
		case <-deadlineCtx.Done():
			return fmt.Errorf("forward did not accept connections on %s within %s", addr, readyTimeout)
		case <-ticker.C:
		}
	}
}

func decodePodInfo(verb string, raw []byte) (*PodInfo, error) {
	var info PodInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, &output.Error{
			Code:     output.CodeGeneric,
			Message:  fmt.Sprintf("pod %s returned invalid JSON: %v", verb, err),
			ExitCode: exitCodeFor(output.CodeGeneric, 1),
		}
	}
	return &info, nil
}

func isExitCode(err error, code int, target **output.Error) bool {
	if err == nil {
		return false
	}
	var oe *output.Error
	if errors.As(err, &oe) {
		*target = oe
		return oe.ExitCode == code
	}
	return false
}
