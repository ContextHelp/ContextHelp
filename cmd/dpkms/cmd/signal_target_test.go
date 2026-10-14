package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

// Ports recorded in the throwaway pidfiles. Nothing binds them: shutdown
// and reboot only match the pidfile's port and signal its pid. Kept clear
// of the default and reserved local ports regardless.
const (
	signalTestCfgPort  = 47613
	signalTestEnvPort  = 47614
	signalTestFlagPort = 47615
)

// signalTarget spawns a disposable child process and records it in a
// pidfile under an isolated run dir as the dpkms instance on port. The
// only process shutdown/reboot can reach is this child.
func signalTarget(t *testing.T, port int) *exec.Cmd {
	t.Helper()
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	runDir, err := config.RunDir()
	if err != nil {
		t.Fatalf("run dir: %v", err)
	}
	if err := pidfile.Write(runDir, pidfile.Info{PID: child.Process.Pid, Port: port}); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	return child
}

// waitSignaled reports the signal that terminated child, failing if it
// is still running after a short grace period.
func waitSignaled(t *testing.T, child *exec.Cmd) syscall.Signal {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("target instance was never signaled")
	}
	ws, ok := child.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		t.Fatalf("target exited without a signal: %v", child.ProcessState)
	}
	return ws.Signal()
}

// TestSignalCommands_DefaultTargetsServerPort: without --port, shutdown
// and reboot target the instance listening on server.port, as resolved
// from the config file or CH_SERVER_PORT; --port overrides both.
func TestSignalCommands_DefaultTargetsServerPort(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
	}{
		{"shutdown", syscall.SIGTERM},
		{"reboot", syscall.SIGHUP},
	} {
		t.Run(tc.name+"/config file", func(t *testing.T) {
			dir := hermeticConfigEnv(t)
			t.Setenv("CTXT_DATA_DIR", dir)
			t.Setenv("CH_SERVER_PORT", "")
			cfgPath := filepath.Join(dir, "dpkms-test.yaml")
			body := fmt.Sprintf("server:\n  port: %d\n", signalTestCfgPort)
			if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			child := signalTarget(t, signalTestCfgPort)

			if _, err := runWithConfigFlag(t, tc.name, "-c", cfgPath); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := waitSignaled(t, child); got != tc.sig {
				t.Errorf("signal = %v, want %v", got, tc.sig)
			}
		})

		t.Run(tc.name+"/env", func(t *testing.T) {
			dir := hermeticConfigEnv(t)
			t.Setenv("CTXT_DATA_DIR", dir)
			t.Setenv("CH_SERVER_PORT", fmt.Sprint(signalTestEnvPort))
			child := signalTarget(t, signalTestEnvPort)

			if _, err := runWithConfigFlag(t, tc.name); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := waitSignaled(t, child); got != tc.sig {
				t.Errorf("signal = %v, want %v", got, tc.sig)
			}
		})

		t.Run(tc.name+"/flag overrides env", func(t *testing.T) {
			dir := hermeticConfigEnv(t)
			t.Setenv("CTXT_DATA_DIR", dir)
			t.Setenv("CH_SERVER_PORT", fmt.Sprint(signalTestEnvPort))
			child := signalTarget(t, signalTestFlagPort)

			if _, err := runWithConfigFlag(t, tc.name, "--port", fmt.Sprint(signalTestFlagPort)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := waitSignaled(t, child); got != tc.sig {
				t.Errorf("signal = %v, want %v", got, tc.sig)
			}
		})
	}
}

// TestSignalCommands_HelpNamesDefaultSource: the help text names the
// source the default port really comes from (server.port /
// CH_SERVER_PORT), not --server-url, which these commands never read.
func TestSignalCommands_HelpNamesDefaultSource(t *testing.T) {
	for _, c := range []*cobra.Command{shutdownCmd, rebootCmd} {
		usage := c.Flags().Lookup("port").Usage
		for _, want := range []string{"server.port", "CH_SERVER_PORT"} {
			if !strings.Contains(usage, want) {
				t.Errorf("%s --port usage %q must name %s", c.Name(), usage, want)
			}
			if !strings.Contains(c.Long, want) {
				t.Errorf("%s long help must name %s", c.Name(), want)
			}
		}
		for _, text := range []string{usage, c.Long} {
			if strings.Contains(text, "server-url") {
				t.Errorf("%s help still cites server-url as the default: %q", c.Name(), text)
			}
		}
	}
}
