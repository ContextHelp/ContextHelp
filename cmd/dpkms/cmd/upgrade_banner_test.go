package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

const dpkmsBannerMark = "upgrading reingest_selective"

// executeSplit runs args like executeCommand but keeps stderr apart from
// stdout.
func executeSplit(args ...string) (stdout, stderr string, err error) {
	resetAllFlags(rootCmd)
	for _, k := range []string{"format", "output", "output.format", "verbose", "quiet", "no-color", "no-hints", "offline", "offline.enabled", "data-dir", "server-url", "storage.path", "server.url"} {
		viper.Set(k, "")
	}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	var errBuf bytes.Buffer
	rootCmd.SetArgs(args)
	rootCmd.SetOut(w)
	rootCmd.SetErr(&errBuf)
	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- data
	}()

	err = rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout
	rootCmd.SetErr(nil)
	return string(<-done), errBuf.String(), err
}

func asTerminal(t *testing.T) {
	t.Helper()
	prev := upgradeBannerTTY
	upgradeBannerTTY = func(io.Writer) bool { return true }
	t.Cleanup(func() { upgradeBannerTTY = prev })
}

// isolatedRunDir points the run dir (pidfiles, the old shadow file) at a
// per-test directory.
func isolatedRunDir(t *testing.T) string {
	t.Helper()
	t.Setenv("CTXT_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir, err := config.RunDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func inFlight(t *testing.T) *upgrade.Manager {
	t.Helper()
	m := upgrade.NewManager("")
	if err := m.Start(upgrade.BucketReingestSelective, 120); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(47); err != nil {
		t.Fatal(err)
	}
	return m
}

// pipelineAgainst runs `dpkms pipeline list` against an in-process
// instance built with opts.
func pipelineAgainst(t *testing.T, extra []string, opts ...dpkmstest.Option) (string, string) {
	t.Helper()
	srv := dpkmstest.Start(t, storageutil.NewTestDriver(t), opts...)
	db := setupTestDB(t)
	appendDpkmsConfig(t, db, "server:\n  url: "+srv.URL+"\n")
	args := append([]string{"--config", db.ConfigPath, "pipeline", "list"}, extra...)
	stdout, stderr, err := executeSplit(args...)
	if err != nil {
		t.Fatalf("dpkms pipeline list: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	return stdout, stderr
}

// A dpkms client command against an instance mid-upgrade prints the
// banner once, on stderr, from the API response.
func TestDpkmsUpgradeBanner_FromAPIResponse(t *testing.T) {
	isolatedRunDir(t)
	asTerminal(t)
	stdout, stderr := pipelineAgainst(t, nil, dpkmstest.WithUpgrade(inFlight(t)))
	if n := strings.Count(stderr, dpkmsBannerMark); n != 1 {
		t.Fatalf("stderr carries the banner %d times, want 1:\n%s", n, stderr)
	}
	if !strings.Contains(stderr, "47/120 objects") {
		t.Errorf("banner lacks the progress:\n%s", stderr)
	}
	if strings.Contains(stdout, "upgrad") {
		t.Fatalf("banner reached stdout:\n%s", stdout)
	}
}

// A failed run shows its error.
func TestDpkmsUpgradeBanner_FailedRun(t *testing.T) {
	isolatedRunDir(t)
	asTerminal(t)
	m := inFlight(t)
	if err := m.Fail(errors.New("disk full")); err != nil {
		t.Fatal(err)
	}
	_, stderr := pipelineAgainst(t, nil, dpkmstest.WithUpgrade(m))
	if !strings.Contains(stderr, "upgrade failed (reingest_selective): disk full") {
		t.Fatalf("stderr lacks the failure banner:\n%s", stderr)
	}
}

// Idle instance, --quiet, --no-hints, non-terminal stderr: no banner.
func TestDpkmsUpgradeBanner_Suppressed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		inFlight bool
		tty      bool
		args     []string
	}{
		{"idle instance", false, true, nil},
		{"quiet", true, true, []string{"--quiet"}},
		{"no-hints", true, true, []string{"--no-hints"}},
		{"stderr not a terminal", true, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedRunDir(t)
			if tc.tty {
				asTerminal(t)
			}
			var opts []dpkmstest.Option
			if tc.inFlight {
				opts = append(opts, dpkmstest.WithUpgrade(inFlight(t)))
			}
			stdout, stderr := pipelineAgainst(t, tc.args, opts...)
			if strings.Contains(stdout+stderr, "upgrad") {
				t.Fatalf("banner printed:\nstdout: %s\nstderr: %s", stdout, stderr)
			}
		})
	}
}

// The local shadow file no longer drives the dpkms banner: a fresh
// in-progress upgrade-state.json in the run dir, with an idle instance,
// prints nothing, for a client command and for a local one.
func TestDpkmsUpgradeBanner_IgnoresLocalShadowFile(t *testing.T) {
	runDir := isolatedRunDir(t)
	asTerminal(t)
	shadow, _ := json.Marshal(upgrade.Status{
		State: upgrade.StateInProgress, Bucket: upgrade.BucketReingestSelective,
		Done: 1, Total: 2, StartedAt: time.Now(),
	})
	if err := os.WriteFile(filepath.Join(runDir, "upgrade-state.json"), shadow, 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr := pipelineAgainst(t, nil)
	if strings.Contains(stderr, "upgrad") {
		t.Fatalf("client command printed a banner from the shadow file:\n%s", stderr)
	}

	db := setupTestDB(t)
	_, stderr, err := executeSplit("--config", db.ConfigPath, "job", "list")
	if err != nil {
		t.Fatalf("dpkms job list: %v", err)
	}
	if strings.Contains(stderr, "upgrad") {
		t.Fatalf("local command printed a banner from the shadow file:\n%s", stderr)
	}
}
