package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestDpkmsPodGuarantee1_DetectionAloneDoesNothing(t *testing.T) {
	binDir := t.TempDir()
	marker := filepath.Join(binDir, "pod-was-invoked")
	script := "#!/bin/sh\necho called > " + marker + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "pod"), []byte(script), 0o755); err != nil { // #nosec G306 -- test executable script
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{Daemon: config.DaemonConfig{Runner: config.DaemonRunnerLocal}}
	resetAllFlags(serveCmd)
	t.Cleanup(func() { resetAllFlags(serveCmd) })

	detectCalled := false
	handled, err := maybeRunServePod(serveCmd, nil, dpkmsPodDeps{
		detect: func() (string, error) {
			detectCalled = true
			return "", errors.New("must not be called")
		},
		projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
	})
	if err != nil || handled {
		t.Fatalf("runner=local want (handled=false, err=nil), got (%v, %v)", handled, err)
	}
	if detectCalled {
		t.Fatal("podrun.Detect was called when daemon.runner=local")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pod trap binary was invoked under runner=local")
	}
}

func TestDpkmsPodGuarantee2And3_AbsentErrorsOnlyWhenAskedNoFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{
		Daemon: config.DaemonConfig{
			Runner: config.DaemonRunnerPod,
			Pod: config.DaemonPodConfig{
				Name:    "dev-pod",
				DataDir: "/workspace/ctxt",
			},
		},
	}
	resetAllFlags(serveCmd)
	t.Cleanup(func() { resetAllFlags(serveCmd) })

	handled, err := maybeRunServePod(serveCmd, nil, dpkmsPodDeps{
		projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
	})
	if !handled {
		t.Fatal("runner=pod with absent pod must be handled (never fall back to local serve)")
	}
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 70 {
		t.Fatalf("want exit 70 prerequisite error, got %v", err)
	}
	if !strings.Contains(oe.Message, "pod is not on PATH") {
		t.Errorf("unexpected error message: %q", oe.Message)
	}
}

func TestDpkmsPodGuarantee5_ProjectLayerRefused(t *testing.T) {
	projDir := t.TempDir()
	projCfg := filepath.Join(projDir, ".contexthelp", "dpkms.yaml")
	if err := os.MkdirAll(filepath.Dir(projCfg), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projCfg, []byte("daemon:\n  runner: pod\n  pod:\n    name: hijacked\n"), 0o600); err != nil {
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

	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{}
	resetAllFlags(serveCmd)
	t.Cleanup(func() { resetAllFlags(serveCmd) })

	_, err = maybeRunServePod(serveCmd, nil, defaultDpkmsPodDeps)
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != 2 {
		t.Fatalf("project-layer daemon.* want exit 2, got %v", err)
	}
	if !strings.Contains(oe.Message, "dpkms.yaml") {
		t.Errorf("error must name offending project config file, got %q", oe.Message)
	}
}

func TestDpkmsPodL4_FakePodCLI_PSAndShutdown(t *testing.T) {
	prevFmt := viper.GetString("format")
	viper.Set("format", "table")
	if f := rootCmd.PersistentFlags().Lookup("format"); f != nil {
		prevChanged := f.Changed
		f.Changed = false
		t.Cleanup(func() { f.Changed = prevChanged })
	}
	cliformat.Bind(rootCmd)
	t.Cleanup(func() { viper.Set("format", prevFmt) })

	binDir := t.TempDir()
	fakePod := `#!/bin/sh
case "$1" in
  capabilities)
    echo '{"tool":"pod","schema_version":"1.0","leaves":[{"path":"pod show"},{"path":"pod start"},{"path":"pod stop"},{"path":"pod exec"},{"path":"pod forward"},{"path":"pod upload"}]}'
    exit 0
    ;;
  show)
    echo '{"name":"dev-pod","provider":"docker","status":"stopped"}'
    exit 0
    ;;
  *)
    echo '{"code":"GENERIC","message":"unexpected"}' >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "pod"), []byte(fakePod), 0o755); err != nil { // #nosec G306 -- test fake pod binary
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{
		Daemon: config.DaemonConfig{
			Runner: config.DaemonRunnerPod,
			Pod:    config.DaemonPodConfig{Name: "dev-pod", DataDir: "/workspace/ctxt"},
		},
	}

	// dpkms ps on stopped pod reports clean empty state with exit 0.
	resetAllFlags(psCmd)
	t.Cleanup(func() { resetAllFlags(psCmd) })
	var out bytes.Buffer
	psCmd.SetOut(&out)
	handled, err := maybeRunPSPod(psCmd, dpkmsPodDeps{
		projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
	})
	if err != nil || !handled {
		t.Fatalf("maybeRunPSPod: (%v, %v)", handled, err)
	}
	if !strings.Contains(out.String(), "pod dev-pod is stopped") {
		t.Errorf("expected stopped notice, got %q", out.String())
	}

	// dpkms shutdown on stopped pod succeeds cleanly.
	resetAllFlags(shutdownCmd)
	t.Cleanup(func() { resetAllFlags(shutdownCmd) })
	out.Reset()
	shutdownCmd.SetOut(&out)
	handled, err = maybeRunShutdownPod(shutdownCmd, dpkmsPodDeps{
		projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
	})
	if err != nil || !handled {
		t.Fatalf("maybeRunShutdownPod: (%v, %v)", handled, err)
	}
	if !strings.Contains(out.String(), "not running") {
		t.Errorf("expected not running notice, got %q", out.String())
	}
}
