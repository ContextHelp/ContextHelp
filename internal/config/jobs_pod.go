package config

import (
	"fmt"
	"strings"
	"time"
)

// Job executor modes for jobs.executor.
const (
	// JobExecutorLocal executes ingestion pipelines in-process (default).
	JobExecutorLocal = "local"
	// JobExecutorPod offloads allowlisted ingestion job topics to ephemeral pods
	// via the standalone `pod` CLI.
	JobExecutorPod = "pod"
)

// Defaults for jobs.pod.* settings.
const (
	DefaultJobsPodMaxInflight    = 4
	DefaultJobsPodMaxPodsPerHour = 120
	DefaultJobsPodMaxPodDuration = 5 * time.Minute
)

// DefaultJobsPodTopics lists the ingestion job types routed to ephemeral pods
// when jobs.executor is "pod" and jobs.pod.topics is not explicitly overridden.
// Fast heuristic ingest:text jobs stay local by default unless opted in.
var DefaultJobsPodTopics = []string{
	"ingest:document",
	"ingest:image",
	"ingest:audio",
	"ingest:video",
	"ingest:url",
	"ingest:code",
	"ingest:feed",
	"ingest:feed_item",
	"ingest:github_release",
}

// JobsPodConfig configures the opt-in ephemeral pod job executor.
type JobsPodConfig struct {
	// Provider optionally pins the pod provider (e.g. "docker", "e2b", "modal").
	// Empty delegates provider selection to pod's own config.
	Provider string `mapstructure:"provider" yaml:"provider,omitempty"`
	// Image optionally pins the container/sandbox image containing dpkms.
	Image string `mapstructure:"image" yaml:"image,omitempty"`
	// DpkmsBin is the dpkms binary name on the pod PATH (default "dpkms").
	DpkmsBin string `mapstructure:"dpkms_bin" yaml:"dpkms_bin,omitempty"`
	// MaxInflight caps concurrent ephemeral pods across all workers (default 4).
	MaxInflight int `mapstructure:"max_inflight" yaml:"max_inflight,omitempty"`
	// MaxPodsPerHour caps ephemeral pods created in a sliding 1-hour window
	// (default 120; explicit 0 disables the ceiling).
	MaxPodsPerHour *int `mapstructure:"max_pods_per_hour" yaml:"max_pods_per_hour,omitempty"`
	// MaxPodDuration bounds a single pod job's create+upload+exec lifetime
	// (default 5m; must be shorter than jobs.stale_timeout).
	MaxPodDuration time.Duration `mapstructure:"max_pod_duration" yaml:"max_pod_duration,omitempty"`
	// FallbackLocal allows falling back to in-process local execution when
	// jobs.executor is "pod" and the pod binary is absent on PATH. Default false
	// (fail loudly with exit 70).
	FallbackLocal bool `mapstructure:"fallback_local" yaml:"fallback_local,omitempty"`
	// Topics lists job types eligible for pod offload. Empty uses DefaultJobsPodTopics.
	Topics []string `mapstructure:"topics" yaml:"topics,omitempty"`
	// Timeouts overrides per-class pod CLI call deadlines.
	Timeouts DaemonPodTimeoutsConfig `mapstructure:"timeouts" yaml:"timeouts,omitempty"`
}

// EffectiveExecutor returns jobs.executor, defaulting to JobExecutorLocal.
func (j JobsConfig) EffectiveExecutor() string {
	if j.Executor == "" {
		return JobExecutorLocal
	}
	return j.Executor
}

// EffectiveDpkmsBin returns DpkmsBin, defaulting to DefaultDaemonPodDpkmsBin.
func (p JobsPodConfig) EffectiveDpkmsBin() string {
	if p.DpkmsBin == "" {
		return DefaultDaemonPodDpkmsBin
	}
	return p.DpkmsBin
}

// EffectiveMaxInflight returns MaxInflight, defaulting to DefaultJobsPodMaxInflight.
func (p JobsPodConfig) EffectiveMaxInflight() int {
	if p.MaxInflight == 0 {
		return DefaultJobsPodMaxInflight
	}
	return p.MaxInflight
}

// EffectiveMaxPodsPerHour returns MaxPodsPerHour, defaulting to DefaultJobsPodMaxPodsPerHour
// when nil. An explicit 0 means unlimited.
func (p JobsPodConfig) EffectiveMaxPodsPerHour() int {
	if p.MaxPodsPerHour == nil {
		return DefaultJobsPodMaxPodsPerHour
	}
	return *p.MaxPodsPerHour
}

// EffectiveMaxPodDuration returns MaxPodDuration, defaulting to DefaultJobsPodMaxPodDuration.
func (p JobsPodConfig) EffectiveMaxPodDuration() time.Duration {
	if p.MaxPodDuration == 0 {
		return DefaultJobsPodMaxPodDuration
	}
	return p.MaxPodDuration
}

// EffectiveTopics returns a copy of Topics or DefaultJobsPodTopics when empty.
func (p JobsPodConfig) EffectiveTopics() []string {
	src := p.Topics
	if len(src) == 0 {
		src = DefaultJobsPodTopics
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// EffectiveTimeouts fills zero durations with built-in defaults.
func (p JobsPodConfig) EffectiveTimeouts() DaemonPodTimeoutsConfig {
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

// ValidateExecutor checks jobs.executor and jobs.pod.* settings.
func (j JobsConfig) ValidateExecutor() []ValidationError {
	var errs []ValidationError

	switch j.Executor {
	case "", JobExecutorLocal, JobExecutorPod:
	default:
		errs = append(errs, ValidationError{
			Field:   "jobs.executor",
			Message: fmt.Sprintf("unknown executor %q; must be %s or %s", j.Executor, JobExecutorLocal, JobExecutorPod),
		})
	}

	if j.Pod.MaxInflight < 0 {
		errs = append(errs, ValidationError{
			Field:   "jobs.pod.max_inflight",
			Message: fmt.Sprintf("must be positive, got %d", j.Pod.MaxInflight),
		})
	}
	if j.Pod.MaxPodsPerHour != nil && *j.Pod.MaxPodsPerHour < 0 {
		errs = append(errs, ValidationError{
			Field:   "jobs.pod.max_pods_per_hour",
			Message: fmt.Sprintf("must be >= 0 (0 = unlimited), got %d", *j.Pod.MaxPodsPerHour),
		})
	}
	if j.Pod.MaxPodDuration < 0 {
		errs = append(errs, ValidationError{
			Field:   "jobs.pod.max_pod_duration",
			Message: fmt.Sprintf("must be positive, got %s", j.Pod.MaxPodDuration),
		})
	}
	if j.EffectiveExecutor() == JobExecutorPod && j.StaleTimeout > 0 && j.Pod.EffectiveMaxPodDuration() >= j.StaleTimeout {
		errs = append(errs, ValidationError{
			Field:   "jobs.pod.max_pod_duration",
			Message: fmt.Sprintf("must be less than jobs.stale_timeout (%s), got %s", j.StaleTimeout, j.Pod.EffectiveMaxPodDuration()),
		})
	}

	for _, kv := range []struct {
		field string
		val   string
	}{
		{"jobs.pod.provider", j.Pod.Provider},
		{"jobs.pod.image", j.Pod.Image},
		{"jobs.pod.dpkms_bin", j.Pod.DpkmsBin},
	} {
		if kv.val != "" && !podTokenRe.MatchString(kv.val) {
			errs = append(errs, ValidationError{
				Field:   kv.field,
				Message: fmt.Sprintf("invalid token %q; allowed characters: A-Z a-z 0-9 . _ / : @ = + -", kv.val),
			})
		}
	}

	for i, topic := range j.Pod.Topics {
		if strings.TrimSpace(topic) == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("jobs.pod.topics[%d]", i),
				Message: "must not be empty",
			})
		}
	}

	for _, td := range []struct {
		field string
		val   time.Duration
	}{
		{"jobs.pod.timeouts.probe", j.Pod.Timeouts.Probe},
		{"jobs.pod.timeouts.query", j.Pod.Timeouts.Query},
		{"jobs.pod.timeouts.exec", j.Pod.Timeouts.Exec},
		{"jobs.pod.timeouts.lifecycle", j.Pod.Timeouts.Lifecycle},
	} {
		if td.val < 0 {
			errs = append(errs, ValidationError{
				Field:   td.field,
				Message: fmt.Sprintf("must be positive, got %s", td.val),
			})
		}
	}

	return errs
}
