package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runCaptureBinary runs the built ctxt binary with args under a scratch
// XDG tree and no inherited instance selector, so host config and state
// never steer routing.
func runCaptureBinary(t *testing.T, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	bin := e2eBinary(t)
	xdg := t.TempDir()
	cmd := exec.Command(bin, args...)
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CTXT_INSTANCE=") || strings.HasPrefix(kv, "XDG_") {
			continue
		}
		env = append(env, kv)
	}
	env = append(
		env,
		"CTXT_NO_CLIPBOARD=1",
		"XDG_CONFIG_HOME="+filepath.Join(xdg, "config"),
		"XDG_DATA_HOME="+filepath.Join(xdg, "data"),
		"XDG_STATE_HOME="+filepath.Join(xdg, "state"),
		"XDG_CACHE_HOME="+filepath.Join(xdg, "cache"),
	)
	cmd.Env = env
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run %s: %v", bin, err)
		}
		exit = ee.ExitCode()
	}
	return so.String(), se.String(), exit
}

// TestE2ECaptureRoutesToConfiguredInstance: the built binary, given only a
// config file, sends the capture to the configured instance with its
// token and exits 0.
func TestE2ECaptureRoutesToConfiguredInstance(t *testing.T) {
	inst := startEndpointRecorder(t, "tok-e2e")
	cfgPath := writeServerConfig(t, inst.URL(), "tok-e2e")

	stdout, stderr, exit := runCaptureBinary(t, "capture", "e2e routed", "-c", cfgPath)
	if exit != 0 {
		t.Fatalf("exit = %d; stdout=%q stderr=%q", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "Job ID: job_routed") {
		t.Errorf("stdout = %q; want the configured instance's job ID", stdout)
	}
	hits := inst.hitsOn("/api/v1/analyze")
	if len(hits) != 1 || hits[0].Auth != "Bearer tok-e2e" {
		t.Errorf("analyze hits = %+v; want one with Bearer tok-e2e", hits)
	}
}

// TestE2ECaptureServerFlagOverridesConfig: --server wins over the config
// file in both flag positions.
func TestE2ECaptureServerFlagOverridesConfig(t *testing.T) {
	for name, pos := range map[string]bool{"flag-first": true, "flag-last": false} {
		t.Run(name, func(t *testing.T) {
			configured := startEndpointRecorder(t, "tok-cfg")
			pinned := startEndpointRecorder(t, "")
			cfgPath := writeServerConfig(t, configured.URL(), "tok-cfg")

			args := []string{"capture", "e2e pinned", "-c", cfgPath, "--server", pinned.URL()}
			if pos {
				args = []string{"capture", "--server", pinned.URL(), "e2e pinned", "-c", cfgPath}
			}
			stdout, stderr, exit := runCaptureBinary(t, args...)
			if exit != 0 {
				t.Fatalf("exit = %d; stdout=%q stderr=%q", exit, stdout, stderr)
			}
			if n := len(configured.Hits()); n != 0 {
				t.Errorf("configured instance got %d hits despite --server", n)
			}
			hits := pinned.hitsOn("/api/v1/analyze")
			if len(hits) != 1 || hits[0].Auth != "" {
				t.Errorf("pinned analyze hits = %+v; want one, unauthenticated", hits)
			}
		})
	}
}

// TestE2ECaptureRejectedCredentialsExitCode: the only configured instance
// rejecting the token surfaces as a failure exit, not a silent success.
func TestE2ECaptureRejectedCredentialsExitCode(t *testing.T) {
	inst := startEndpointRecorder(t, "tok-real")
	cfgPath := writeServerConfig(t, inst.URL(), "tok-wrong")

	stdout, stderr, exit := runCaptureBinary(t, "capture", "e2e rejected", "-c", cfgPath)
	if exit != 1 {
		t.Errorf("exit = %d; want 1 (stdout=%q stderr=%q)", exit, stdout, stderr)
	}
	if !strings.Contains(stderr, "401") {
		t.Errorf("stderr should carry the instance's 401; got %q", stderr)
	}
	if strings.Contains(stdout, "Job ID") {
		t.Errorf("rejected capture must not print a Job ID: %q", stdout)
	}
}
