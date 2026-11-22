package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonConfigDefaultsAndValidation(t *testing.T) {
	var d DaemonConfig
	if got := d.EffectiveRunner(); got != DaemonRunnerLocal {
		t.Fatalf("EffectiveRunner() = %q, want %q", got, DaemonRunnerLocal)
	}
	if !d.Pod.EffectiveAutostart() {
		t.Fatal("EffectiveAutostart() = false, want true")
	}
	if got := d.Pod.EffectiveCtxdBin(); got != DefaultDaemonPodCtxdBin {
		t.Fatalf("EffectiveCtxdBin() = %q, want %q", got, DefaultDaemonPodCtxdBin)
	}
	if got := d.Pod.EffectiveDpkmsBin(); got != DefaultDaemonPodDpkmsBin {
		t.Fatalf("EffectiveDpkmsBin() = %q, want %q", got, DefaultDaemonPodDpkmsBin)
	}
	if got := d.Pod.EffectiveDpkmsPort(); got != DefaultDaemonPodDpkmsPort {
		t.Fatalf("EffectiveDpkmsPort() = %d, want %d", got, DefaultDaemonPodDpkmsPort)
	}
	to := d.Pod.EffectiveTimeouts()
	if to.Probe != 10*time.Second || to.Query != 15*time.Second || to.Exec != 90*time.Second || to.Lifecycle != 5*time.Minute {
		t.Fatalf("unexpected default timeouts: %+v", to)
	}
	if errs := d.Validate(); len(errs) != 0 {
		t.Fatalf("default DaemonConfig invalid: %v", errs)
	}

	// C4: daemon.pod.* ignored silently when runner is local.
	dLocal := DaemonConfig{
		Runner: DaemonRunnerLocal,
		Pod:    DaemonPodConfig{Name: "bad name with spaces", DpkmsPort: 999999},
	}
	if errs := dLocal.Validate(); len(errs) != 0 {
		t.Fatalf("runner=local should ignore daemon.pod.*, got: %v", errs)
	}

	// Unknown runner refused.
	dBad := DaemonConfig{Runner: "docker"}
	if errs := dBad.Validate(); len(errs) == 0 || errs[0].Field != "daemon.runner" {
		t.Fatalf("expected daemon.runner error, got: %v", errs)
	}

	// runner=pod requires name and safe tokens.
	dPodNoName := DaemonConfig{Runner: DaemonRunnerPod}
	if errs := dPodNoName.Validate(); len(errs) == 0 || errs[0].Field != "daemon.pod.name" {
		t.Fatalf("expected daemon.pod.name required error, got: %v", errs)
	}

	dPodBadToken := DaemonConfig{
		Runner: DaemonRunnerPod,
		Pod: DaemonPodConfig{
			Name:      "dev-pod",
			CtxdBin:   "ctxd; rm -rf /",
			DpkmsBin:  "dpkms $(whoami)",
			DataDir:   "/data dir",
			DpkmsPort: 70000,
			LocalPort: -1,
			Timeouts:  DaemonPodTimeoutsConfig{Probe: -time.Second},
		},
	}
	errs := dPodBadToken.Validate()
	if len(errs) < 6 {
		t.Fatalf("expected >=6 validation errors for invalid pod config, got %d: %v", len(errs), errs)
	}

	// Valid pod config passes.
	f := false
	dValid := DaemonConfig{
		Runner: DaemonRunnerPod,
		Pod: DaemonPodConfig{
			Name:      "my-pod-1",
			Autostart: &f,
			CtxdBin:   "/usr/local/bin/ctxd",
			DpkmsBin:  "dpkms",
			DataDir:   "/workspace/dpkms",
			DpkmsPort: 8080,
			LocalPort: 18080,
		},
	}
	if errs := dValid.Validate(); len(errs) != 0 {
		t.Fatalf("valid pod config failed: %v", errs)
	}
	if dValid.Pod.EffectiveAutostart() {
		t.Fatal("explicit autostart:false should be honored")
	}
}

func TestProjectLayerSetsDetectsDaemonEvenWhenShadowed(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(projDir, ".contexthelp"), 0o755); err != nil {
		t.Fatal(err)
	}
	projFile := filepath.Join(projDir, ".contexthelp", "ctxt.yaml")
	if err := os.WriteFile(projFile, []byte("daemon:\n  runner: pod\n  pod:\n    name: evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Shadow with an extra -c file that sets daemon.runner=local.
	extraFile := filepath.Join(root, "override.yaml")
	if err := os.WriteFile(extraFile, []byte("daemon:\n  runner: local\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv(EnvConfigPath, "")

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}

	gotPath, found, err := ProjectLayerSets("ctxt", "", []string{extraFile}, "daemon")
	if err != nil {
		t.Fatalf("ProjectLayerSets: %v", err)
	}
	if !found {
		t.Fatal("expected ProjectLayerSets to report daemon in project layer even when shadowed by -c")
	}
	gotReal, _ := filepath.EvalSymlinks(gotPath)
	wantReal, _ := filepath.EvalSymlinks(projFile)
	if gotReal != wantReal {
		t.Fatalf("path = %q, want %q", gotPath, projFile)
	}

	// CTXT_CONFIG drops the project slot, so it should not report found.
	t.Setenv(EnvConfigPath, extraFile)
	_, found, err = ProjectLayerSets("ctxt", "", nil, "daemon")
	if err != nil {
		t.Fatalf("ProjectLayerSets with CTXT_CONFIG: %v", err)
	}
	if found {
		t.Fatal("CTXT_CONFIG replaces cascade slots; project layer must not be reported")
	}
}
