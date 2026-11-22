package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
)

func newAmbientTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "capture"}
	c.Flags().Bool("ambient", true, "")
	c.Flags().String("runner", "", "")
	c.Flags().String("pod", "", "")
	c.Flags().Bool("stop-pod", false, "")
	c.Flags().Bool("foreground", false, "")
	c.Flags().StringSlice("input", nil, "")
	c.Flags().StringSlice("skip", nil, "")
	return c
}

// TestAmbientPodGuarantees verifies Guarantees 1, 2, 3, and 5 for ctxd launch:
//   - G1: Detection alone does nothing when runner=local (trap `pod` on PATH never runs).
//   - G2: Absent pod errors with exit 70 ONLY when runner=pod.
//   - G3: No silent fallback to local when runner=pod and pod is absent.
//   - G5: Project config `.contexthelp/ctxt.yaml` setting daemon.* is refused (exit 2).
func TestAmbientPodGuarantees(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix pod runner guarantee test")
	}

	t.Run("G1 detection alone does nothing when runner=local", func(t *testing.T) {
		dir := t.TempDir()
		marker := filepath.Join(dir, "pod-was-called")
		script := filepath.Join(dir, "pod")
		if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)

		localCalled := false
		cmd := newAmbientTestCmd()
		origCfg := cfg
		cfg = &config.Config{Daemon: config.DaemonConfig{Runner: config.DaemonRunnerLocal}}
		t.Cleanup(func() { cfg = origCfg })

		err := runAmbientPod(cmd, nil, ambientPodDeps{
			detect:      podrun.Detect,
			projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
			localStart: func(*cobra.Command, []string) error {
				localCalled = true
				return nil
			},
		})
		if err != nil {
			t.Fatalf("runAmbientPod local: %v", err)
		}
		if !localCalled {
			t.Fatal("expected local start seam to be called")
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("G1 violated: pod binary on PATH was invoked when runner=local")
		}
	})

	t.Run("G2 and G3 absent pod is exit 70 with no local fallback", func(t *testing.T) {
		emptyDir := t.TempDir()
		t.Setenv("PATH", emptyDir)

		localCalled := false
		cmd := newAmbientTestCmd()
		_ = cmd.Flags().Set("runner", "pod")
		_ = cmd.Flags().Set("pod", "dev-pod")

		origCfg := cfg
		cfg = &config.Config{}
		t.Cleanup(func() { cfg = origCfg })

		err := runAmbientPod(cmd, nil, ambientPodDeps{
			detect:      podrun.Detect,
			projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
			localStart: func(*cobra.Command, []string) error {
				localCalled = true
				return nil
			},
		})
		var oe *output.Error
		if !errors.As(err, &oe) || oe.ExitCode != 70 {
			t.Fatalf("G2 violated: want exit 70 prerequisite error, got %v", err)
		}
		if localCalled {
			t.Fatal("G3 violated: fell back to local start when pod was absent")
		}
	})

	t.Run("G5 project layer setting daemon.* refused with exit 2", func(t *testing.T) {
		cmd := newAmbientTestCmd()
		origCfg := cfg
		cfg = &config.Config{}
		t.Cleanup(func() { cfg = origCfg })

		err := runAmbientPod(cmd, nil, ambientPodDeps{
			detect: podrun.Detect,
			projectSets: func(string, string, []string, string) (string, bool, error) {
				return "/repo/.contexthelp/ctxt.yaml", true, nil
			},
		})
		var oe *output.Error
		if !errors.As(err, &oe) || oe.ExitCode != 2 || !strings.Contains(oe.Message, ".contexthelp/ctxt.yaml") {
			t.Fatalf("G5 violated: want exit 2 naming project file, got %v", err)
		}
	})

	t.Run("L4 black-box fake pod script in PATH reports stopped pod status", func(t *testing.T) {
		dir := t.TempDir()
		podScript := filepath.Join(dir, "pod")
		body := `#!/bin/sh
case "$1" in
  capabilities)
    echo '{"tool":"pod","version":"dev","schema_version":"1.0","leaves":[{"path":"pod show"},{"path":"pod exec"},{"path":"pod forward"}]}'
    exit 0
    ;;
  show)
    echo '{"name":"dev-pod","provider":"docker","status":"stopped"}'
    exit 0
    ;;
esac
exit 1
`
		if err := os.WriteFile(podScript, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))

		cmd := newAmbientTestCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		_ = cmd.Flags().Set("runner", "pod")
		_ = cmd.Flags().Set("pod", "dev-pod")

		origCfg := cfg
		cfg = &config.Config{}
		t.Cleanup(func() { cfg = origCfg })

		err := runAmbientPod(cmd, []string{"status"}, ambientPodDeps{
			detect:      podrun.Detect,
			projectSets: func(string, string, []string, string) (string, bool, error) { return "", false, nil },
		})
		if err != nil {
			t.Fatalf("status on stopped pod: %v", err)
		}
		if !strings.Contains(out.String(), "not running") {
			t.Fatalf("unexpected output: %q", out.String())
		}
	})
}
