package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

const bannerMark = "ctxt: upgrading reingest_selective"

// executeSplit runs args like executeCommand but keeps stderr apart from
// stdout, so a test can tell which stream the banner reached.
func executeSplit(args ...string) (stdout, stderr string, err error) {
	resetAllFlags(rootCmd)
	for _, k := range []string{"format", "output", "output.format", "verbose", "quiet", "no-color", "no-hints", "profile", "profile.default", "offline", "offline.enabled", "instance"} {
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

// asTerminal makes the banner treat stderr as a terminal for the test.
func asTerminal(t *testing.T) {
	t.Helper()
	prev := upgradeBannerTTY
	upgradeBannerTTY = func(io.Writer) bool { return true }
	t.Cleanup(func() { upgradeBannerTTY = prev })
}

// inFlightManager is an upgrade manager mid-way through a selective
// re-ingest.
func inFlightManager(t *testing.T) *upgrade.Manager {
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

// A command against an instance mid-upgrade prints the banner once, on
// stderr, from the API response; stdout stays clean for the command's
// own output.
func TestUpgradeBanner_FromAPIResponse(t *testing.T) {
	asTerminal(t)
	db := setupTestDB(t, dpkmstest.WithUpgrade(inFlightManager(t)))

	stdout, stderr, err := executeSplit("--config", db.ConfigPath, "log", "--format", "json")
	if err != nil {
		t.Fatalf("log: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if n := strings.Count(stderr, bannerMark); n != 1 {
		t.Fatalf("stderr carries the banner %d times, want 1:\n%s", n, stderr)
	}
	if !strings.Contains(stderr, "47/120 objects") {
		t.Errorf("banner lacks the progress:\n%s", stderr)
	}
	if strings.Contains(stdout, "upgrad") {
		t.Fatalf("banner reached stdout:\n%s", stdout)
	}
	if !json.Valid([]byte(strings.TrimSpace(stdout))) {
		t.Fatalf("stdout is not the command's JSON alone:\n%s", stdout)
	}
}

// A failed run keeps showing, with its error.
func TestUpgradeBanner_FailedRun(t *testing.T) {
	asTerminal(t)
	m := inFlightManager(t)
	if err := m.Fail(errTest("disk full")); err != nil {
		t.Fatal(err)
	}
	db := setupTestDB(t, dpkmstest.WithUpgrade(m))

	_, stderr, err := executeSplit("--config", db.ConfigPath, "log")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if !strings.Contains(stderr, "upgrade failed (reingest_selective): disk full") {
		t.Fatalf("stderr lacks the failure banner:\n%s", stderr)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// Many responses in one invocation, one banner.
func TestUpgradeBanner_OncePerInvocation(t *testing.T) {
	asTerminal(t)
	db := setupTestDB(t, dpkmstest.WithUpgrade(inFlightManager(t)))
	var errBuf bytes.Buffer
	c := &cobra.Command{Use: "probe"}
	c.Flags().Bool("quiet", false, "")
	c.SetErr(&errBuf)
	armUpgradeBanner(c)
	t.Cleanup(func() { upgradeBannerSink.Store(nil) })

	for range 3 {
		resp := db.Server.Request(t, http.MethodGet, "/api/v1/objects/obj_missing", dpkmstest.RoleAdmin, nil)
		resp.Body.Close()
		if resp.Header.Get(upgrade.HeaderName) == "" {
			t.Fatal("fixture sent no upgrade header")
		}
	}
	if n := strings.Count(errBuf.String(), bannerMark); n != 1 {
		t.Fatalf("banner printed %d times across 3 responses, want 1:\n%s", n, errBuf.String())
	}
}

// No banner when the instance is idle, when --quiet is set, or when
// stderr is not a terminal (kit's rule for hints: piped output stays
// machine-clean).
func TestUpgradeBanner_Suppressed(t *testing.T) {
	cases := []struct {
		name     string
		inFlight bool
		tty      bool
		args     []string
	}{
		{"idle instance", false, true, nil},
		{"quiet", true, true, []string{"--quiet"}},
		{"stderr not a terminal", true, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tty {
				asTerminal(t)
			}
			var opts []dpkmstest.Option
			if tc.inFlight {
				opts = append(opts, dpkmstest.WithUpgrade(inFlightManager(t)))
			}
			db := setupTestDB(t, opts...)
			args := append([]string{"--config", db.ConfigPath, "log"}, tc.args...)
			stdout, stderr, err := executeSplit(args...)
			if err != nil {
				t.Fatalf("log: %v", err)
			}
			if strings.Contains(stderr+stdout, "upgrad") {
				t.Fatalf("banner printed:\nstdout: %s\nstderr: %s", stdout, stderr)
			}
		})
	}
}

// The local shadow file no longer drives ctxt's banner: a fresh
// in-progress upgrade-state.json next to the pidfiles, against an idle
// instance, prints nothing.
func TestUpgradeBanner_IgnoresLocalShadowFile(t *testing.T) {
	asTerminal(t)
	db := setupTestDB(t)
	runDir, err := config.RunDir()
	if err != nil {
		t.Fatal(err)
	}
	shadow, _ := json.Marshal(upgrade.Status{
		State: upgrade.StateInProgress, Bucket: upgrade.BucketReingestSelective,
		Done: 1, Total: 2, StartedAt: time.Now(),
	})
	if err := os.WriteFile(filepath.Join(runDir, "upgrade-state.json"), shadow, 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := executeSplit("--config", db.ConfigPath, "log")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if strings.Contains(stderr, "upgrad") {
		t.Fatalf("banner printed from the local shadow file:\n%s", stderr)
	}
}
