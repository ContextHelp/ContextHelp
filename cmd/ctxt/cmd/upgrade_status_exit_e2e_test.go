package cmd

import (
	"errors"
	"net/http"
	"os/exec"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// runBuiltCtxt runs the built ctxt binary and returns its exit code.
func runBuiltCtxt(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(e2eBinary(t), args...) // #nosec G204 -- test binary
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	default:
		t.Fatalf("run ctxt %v: %v", args, err)
		return -1, ""
	}
}

// The documented exit codes of `ctxt upgrade status` and `ctxt status`,
// checked on the built binary: a daemon that answered with a failed state
// is GENERIC (1) whatever its last_error says; a daemon nobody answers
// for is PREREQUISITE (70).
func TestE2EUpgradeStatusExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	failedWith := func(lastErr string) string {
		srv := fakeUpgradeHealthzServer(t, &upgradeEnvelope{
			State: "failed", Bucket: "embeddings_migrate", Target: "m@1",
			Done: 30, Total: 30, Failed: 30, LastError: lastErr,
		})
		t.Cleanup(srv.Close)
		return srv.URL
	}
	inProgress := fakeUpgradeHealthzServer(t, &upgradeEnvelope{State: "in_progress", Total: 4, Done: 1})
	t.Cleanup(inProgress.Close)
	unavailable := fakeHealthzServer(t, http.StatusServiceUnavailable, map[string]any{"health": "failed"})
	t.Cleanup(unavailable.Close)

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"upgrade in progress", []string{"upgrade", "status", "--server", inProgress.URL}, 0},
		{"upgrade failed, provider refused", []string{"upgrade", "status", "--server", failedWith(
			"30 of 30 objects not embedded; first: embed: dial tcp 127.0.0.1:11555: connect: connection refused")}, 1},
		{"upgrade failed, model not found", []string{"upgrade", "status", "--server", failedWith(
			"model not found")}, 1},
		{"upgrade failed, json", []string{"upgrade", "status", "--format", "json", "--server", failedWith(
			"connection refused")}, 1},
		{"upgrade status, daemon unreachable", []string{"upgrade", "status", "--server", testguard.ClosedServerURL}, 70},
		{"status, 503", []string{"status", "--server", unavailable.URL}, 1},
		{"status, daemon unreachable", []string{"status", "--server", testguard.ClosedServerURL}, 70},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, out := runBuiltCtxt(t, tc.args...); got != tc.want {
				t.Fatalf("ctxt %v: exit %d, want %d\n%s", tc.args, got, tc.want, out)
			}
		})
	}
}
