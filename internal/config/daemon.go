package config

import (
	"fmt"
	"regexp"
	"time"
)

// Daemon runner values for daemon.runner.
const (
	// DaemonRunnerLocal runs daemons directly on this machine (default).
	DaemonRunnerLocal = "local"
	// DaemonRunnerPod launches and supervises daemons on an existing pod via
	// the opt-in `pod` CLI shell-out boundary.
	DaemonRunnerPod = "pod"
)

// Defaults for daemon.pod.* settings.
const (
	DefaultDaemonPodProbeTimeout     = 10 * time.Second
	DefaultDaemonPodQueryTimeout     = 15 * time.Second
	DefaultDaemonPodExecTimeout      = 90 * time.Second
	DefaultDaemonPodLifecycleTimeout = 5 * time.Minute
	DefaultDaemonPodCtxdBin          = "ctxd"
	DefaultDaemonPodDpkmsBin         = "dpkms"
	DefaultDaemonPodDpkmsPort        = 8080
)

// podTokenRe matches tokens safe to interpolate into fixed pod-side command
// templates without shell escaping hazards.
var podTokenRe = regexp.MustCompile(`^[A-Za-z0-9._/:@=+-]+$`)

// DaemonConfig selects where launched daemons (ctxd, dpkms) run.
type DaemonConfig struct {
	// Runner selects where daemons run: "local" (default) or "pod".
	Runner string `mapstructure:"runner" yaml:"runner,omitempty"`
	// Pod configures the opt-in pod runner. Ignored when Runner is "local".
	Pod DaemonPodConfig `mapstructure:"pod" yaml:"pod,omitempty"`
}

// DaemonPodConfig holds pod-runner settings shared by ctxt (ctxd) and dpkms.
type DaemonPodConfig struct {
	// Name is the existing pod to adopt. Required when daemon.runner is "pod".
	Name string `mapstructure:"name" yaml:"name,omitempty"`
	// Autostart starts a stopped pod before launch when true (default true).
	Autostart *bool `mapstructure:"autostart" yaml:"autostart,omitempty"`
	// Timeouts bounds every pod CLI call class.
	Timeouts DaemonPodTimeoutsConfig `mapstructure:"timeouts" yaml:"timeouts,omitempty"`
	// CtxdBin is the ctxd command name on the pod's PATH (ctxt only).
	CtxdBin string `mapstructure:"ctxd_bin" yaml:"ctxd_bin,omitempty"`
	// ServerURL is the dpkms URL as seen from inside the pod (ctxt only).
	ServerURL string `mapstructure:"server_url" yaml:"server_url,omitempty" secret:"dsn"`
	// DpkmsBin is the dpkms command name on the pod's PATH (dpkms only).
	DpkmsBin string `mapstructure:"dpkms_bin" yaml:"dpkms_bin,omitempty"`
	// DataDir is the dpkms data directory on the pod (dpkms only; required).
	DataDir string `mapstructure:"data_dir" yaml:"data_dir,omitempty"`
	// DpkmsPort is the fixed HTTP port of the pod-side dpkms (dpkms only).
	DpkmsPort int `mapstructure:"dpkms_port" yaml:"dpkms_port,omitempty"`
	// LocalPort is the local loopback end of the persistent dpkms forward
	// (0 picks a free loopback port on each launch).
	LocalPort int `mapstructure:"local_port" yaml:"local_port,omitempty"`
}

// DaemonPodTimeoutsConfig configures per-class deadlines for pod CLI calls.
type DaemonPodTimeoutsConfig struct {
	Probe     time.Duration `mapstructure:"probe" yaml:"probe,omitempty"`
	Query     time.Duration `mapstructure:"query" yaml:"query,omitempty"`
	Exec      time.Duration `mapstructure:"exec" yaml:"exec,omitempty"`
	Lifecycle time.Duration `mapstructure:"lifecycle" yaml:"lifecycle,omitempty"`
}

// EffectiveRunner returns Runner, defaulting to DaemonRunnerLocal when empty.
func (d DaemonConfig) EffectiveRunner() string {
	if d.Runner == "" {
		return DaemonRunnerLocal
	}
	return d.Runner
}

// EffectiveAutostart reports whether stopped pods are started automatically.
func (p DaemonPodConfig) EffectiveAutostart() bool {
	if p.Autostart == nil {
		return true
	}
	return *p.Autostart
}

// EffectiveCtxdBin returns CtxdBin, defaulting to DefaultDaemonPodCtxdBin.
func (p DaemonPodConfig) EffectiveCtxdBin() string {
	if p.CtxdBin == "" {
		return DefaultDaemonPodCtxdBin
	}
	return p.CtxdBin
}

// EffectiveDpkmsBin returns DpkmsBin, defaulting to DefaultDaemonPodDpkmsBin.
func (p DaemonPodConfig) EffectiveDpkmsBin() string {
	if p.DpkmsBin == "" {
		return DefaultDaemonPodDpkmsBin
	}
	return p.DpkmsBin
}

// EffectiveDpkmsPort returns DpkmsPort, defaulting to DefaultDaemonPodDpkmsPort.
func (p DaemonPodConfig) EffectiveDpkmsPort() int {
	if p.DpkmsPort == 0 {
		return DefaultDaemonPodDpkmsPort
	}
	return p.DpkmsPort
}

// EffectiveTimeouts fills zero durations with built-in defaults.
func (p DaemonPodConfig) EffectiveTimeouts() DaemonPodTimeoutsConfig {
	t := p.Timeouts
	if t.Probe == 0 {
		t.Probe = DefaultDaemonPodProbeTimeout
	}
	if t.Query == 0 {
		t.Query = DefaultDaemonPodQueryTimeout
	}
	if t.Exec == 0 {
		t.Exec = DefaultDaemonPodExecTimeout
	}
	if t.Lifecycle == 0 {
		t.Lifecycle = DefaultDaemonPodLifecycleTimeout
	}
	return t
}

// Validate checks daemon.* settings. When runner is local (or unset),
// daemon.pod.* values are ignored so operators can toggle runner with one key.
func (d DaemonConfig) Validate() []ValidationError {
	var errs []ValidationError
	switch d.Runner {
	case "", DaemonRunnerLocal:
		return nil
	case DaemonRunnerPod:
		// validated below
	default:
		return []ValidationError{{
			Field:   "daemon.runner",
			Message: fmt.Sprintf("unknown runner %q; must be one of %s, %s", d.Runner, DaemonRunnerLocal, DaemonRunnerPod),
		}}
	}

	p := d.Pod
	if p.Name == "" {
		errs = append(errs, ValidationError{
			Field:   "daemon.pod.name",
			Message: "required when daemon.runner is \"pod\" (set daemon.pod.name or pass --pod)",
		})
	} else if !podTokenRe.MatchString(p.Name) {
		errs = append(errs, ValidationError{
			Field:   "daemon.pod.name",
			Message: fmt.Sprintf("contains unsupported characters: %q", p.Name),
		})
	}

	if p.CtxdBin != "" && !podTokenRe.MatchString(p.CtxdBin) {
		errs = append(errs, ValidationError{
			Field:   "daemon.pod.ctxd_bin",
			Message: fmt.Sprintf("contains unsupported characters: %q", p.CtxdBin),
		})
	}
	if p.DpkmsBin != "" && !podTokenRe.MatchString(p.DpkmsBin) {
		errs = append(errs, ValidationError{
			Field:   "daemon.pod.dpkms_bin",
			Message: fmt.Sprintf("contains unsupported characters: %q", p.DpkmsBin),
		})
	}
	if p.DataDir != "" && !podTokenRe.MatchString(p.DataDir) {
		errs = append(errs, ValidationError{
			Field:   "daemon.pod.data_dir",
			Message: fmt.Sprintf("contains unsupported characters: %q", p.DataDir),
		})
	}
	if p.DpkmsPort < 0 || p.DpkmsPort > 65535 {
		errs = append(errs, ValidationError{
			Field:   "daemon.pod.dpkms_port",
			Message: fmt.Sprintf("must be 1–65535, got %d", p.DpkmsPort),
		})
	}
	if p.LocalPort < 0 || p.LocalPort > 65535 {
		errs = append(errs, ValidationError{
			Field:   "daemon.pod.local_port",
			Message: fmt.Sprintf("must be 0 or 1–65535, got %d", p.LocalPort),
		})
	}
	errs = append(errs, validatePodTimeouts("daemon.pod.timeouts", p.Timeouts)...)
	return errs
}

func validatePodTimeouts(prefix string, t DaemonPodTimeoutsConfig) []ValidationError {
	var errs []ValidationError
	for _, item := range []struct {
		name string
		val  time.Duration
	}{
		{"probe", t.Probe},
		{"query", t.Query},
		{"exec", t.Exec},
		{"lifecycle", t.Lifecycle},
	} {
		if item.val < 0 {
			errs = append(errs, ValidationError{
				Field:   prefix + "." + item.name,
				Message: fmt.Sprintf("must not be negative, got %s", item.val),
			})
		}
	}
	return errs
}
