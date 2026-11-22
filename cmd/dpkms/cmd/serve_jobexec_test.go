package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestJobExecutorLocalDefaultNeverProbesPod(t *testing.T) {
	binDir := t.TempDir()
	marker := filepath.Join(binDir, "pod-called")
	script := "#!/bin/sh\necho hit > " + marker + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "pod"), []byte(script), 0o755); err != nil { // #nosec G306 -- test executable
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	detectCalled := false
	install, err := prepareJobExecutor(&config.Config{
		Jobs: config.JobsConfig{Executor: config.JobExecutorLocal},
	}, jobExecDeps{
		detect: func() (string, error) {
			detectCalled = true
			return "", errors.New("must not be called")
		},
		projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
	})
	if err != nil || install != nil {
		t.Fatalf("local default want (nil, nil), got (%v, %v)", install != nil, err)
	}
	if detectCalled {
		t.Fatal("podrun.Detect called when jobs.executor=local")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("trap pod script was invoked")
	}
}

func TestJobExecutorPodAbsentRefusesWithoutFallbackAndAllowsOptInFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	// Strict default: exit 70 prerequisite error.
	_, err := prepareJobExecutor(&config.Config{
		Jobs: config.JobsConfig{Executor: config.JobExecutorPod},
	}, jobExecDeps{
		projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
	})
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 70 {
		t.Fatalf("want exit 70 when pod absent, got %v", err)
	}

	// Explicit fallback_local: true warns and returns nil (local).
	var warn bytes.Buffer
	install, err := prepareJobExecutor(&config.Config{
		Jobs: config.JobsConfig{
			Executor: config.JobExecutorPod,
			Pod:      config.JobsPodConfig{FallbackLocal: true},
		},
	}, jobExecDeps{
		projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
		warn:        &warn,
	})
	if err != nil || install != nil {
		t.Fatalf("fallback_local want (nil, nil), got (%v, %v)", install != nil, err)
	}
	if !strings.Contains(warn.String(), "falling back to local") {
		t.Errorf("expected fallback warning, got %q", warn.String())
	}
}

func TestJobExecutorProjectLayerRefused(t *testing.T) {
	projDir := t.TempDir()
	projCfg := filepath.Join(projDir, ".contexthelp", "dpkms.yaml")
	if err := os.MkdirAll(filepath.Dir(projCfg), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projCfg, []byte("jobs:\n  executor: pod\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	_, err = prepareJobExecutor(&config.Config{}, defaultJobExecDeps)
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 2 {
		t.Fatalf("project-layer jobs.executor want exit 2, got %v", err)
	}
	if !strings.Contains(oe.Message, "dpkms.yaml") {
		t.Errorf("error must name offending project config, got %q", oe.Message)
	}
}

func TestWorkerRunDraftRoundTrip(t *testing.T) {
	reqPath := filepath.Join(t.TempDir(), "req.json")
	req := jobs.DraftJobRequest{
		Job: storage.Job{
			ID:       "draft-1",
			Type:     "ingest:code",
			Pipeline: "text.short",
			Payload:  "package demo\nfunc Hello() string { return \"hi\" }\n",
			Source:   "cli",
		},
		MaxHops: 5,
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reqPath, b, 0o600); err != nil {
		t.Fatal(err)
	}

	resetAllFlags(workerRunDraftCmd)
	t.Cleanup(func() { resetAllFlags(workerRunDraftCmd) })
	if err := workerRunDraftCmd.Flags().Set("input", reqPath); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	workerRunDraftCmd.SetOut(&out)
	if err := runWorkerRunDraft(workerRunDraftCmd, nil); err != nil {
		t.Fatalf("runWorkerRunDraft: %v", err)
	}

	var res jobs.DraftJobResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("decode DraftJobResult: %v (raw=%q)", err, out.String())
	}
	if res.Error != "" || res.Draft == nil || res.Draft.ContentHash == "" {
		t.Fatalf("unexpected draft result: %+v", res)
	}
}
