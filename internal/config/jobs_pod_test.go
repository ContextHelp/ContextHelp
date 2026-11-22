package config

import (
	"strings"
	"testing"
	"time"
)

func TestJobsPodDefaultsAndValidation(t *testing.T) {
	var jc JobsConfig
	if jc.EffectiveExecutor() != JobExecutorLocal {
		t.Fatalf("default executor = %q, want local", jc.EffectiveExecutor())
	}
	if jc.Pod.EffectiveMaxInflight() != DefaultJobsPodMaxInflight {
		t.Fatalf("default max_inflight = %d", jc.Pod.EffectiveMaxInflight())
	}
	if jc.Pod.EffectiveMaxPodsPerHour() != DefaultJobsPodMaxPodsPerHour {
		t.Fatalf("default max_pods_per_hour = %d", jc.Pod.EffectiveMaxPodsPerHour())
	}
	if jc.Pod.EffectiveMaxPodDuration() != DefaultJobsPodMaxPodDuration {
		t.Fatalf("default max_pod_duration = %s", jc.Pod.EffectiveMaxPodDuration())
	}
	if len(jc.Pod.EffectiveTopics()) == 0 {
		t.Fatal("default topics must be non-empty")
	}
	if errs := jc.ValidateExecutor(); len(errs) != 0 {
		t.Fatalf("default JobsConfig validate: %v", errs)
	}

	// Explicit 0 disables hourly ceiling.
	zero := 0
	jc.Pod.MaxPodsPerHour = &zero
	if jc.Pod.EffectiveMaxPodsPerHour() != 0 {
		t.Fatalf("explicit 0 max_pods_per_hour = %d, want 0", jc.Pod.EffectiveMaxPodsPerHour())
	}
}

func TestJobsPodValidationErrors(t *testing.T) {
	neg := -1
	bad := JobsConfig{
		Executor:     "bogus",
		StaleTimeout: 2 * time.Minute,
		Pod: JobsPodConfig{
			Provider:       "docker;rm",
			MaxInflight:    -2,
			MaxPodsPerHour: &neg,
			MaxPodDuration: -time.Second,
			Topics:         []string{""},
		},
	}
	errs := bad.ValidateExecutor()
	joined := make([]string, len(errs))
	for i, e := range errs {
		joined[i] = e.Error()
	}
	got := strings.Join(joined, "\n")
	for _, want := range []string{
		"jobs.executor",
		"jobs.pod.provider",
		"jobs.pod.max_inflight",
		"jobs.pod.max_pods_per_hour",
		"jobs.pod.max_pod_duration",
		"jobs.pod.topics[0]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected validation error for %q in:\n%s", want, got)
		}
	}

	// max_pod_duration >= stale_timeout is rejected when executor=pod.
	tooLong := JobsConfig{
		Executor:     JobExecutorPod,
		StaleTimeout: 3 * time.Minute,
		Pod: JobsPodConfig{
			MaxPodDuration: 5 * time.Minute,
		},
	}
	if errs := tooLong.ValidateExecutor(); len(errs) == 0 || !strings.Contains(errs[0].Error(), "stale_timeout") {
		t.Fatalf("expected stale_timeout bound error, got %v", errs)
	}
}
