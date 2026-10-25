package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// executeOnTTY runs a command the way executeCommand does, but with
// stdout and stderr on the slave end of a pseudo-terminal, and returns
// what the terminal received.
func executeOnTTY(t *testing.T, args ...string) (string, error) {
	t.Helper()
	master, slave, err := openPTY()
	if err != nil {
		t.Skipf("pseudo-terminal: %v", err)
	}
	defer master.Close()

	installHints(rootCmd)
	resetAllFlags(rootCmd)
	for _, k := range []string{"format", "output", "output.format", "verbose", "quiet", "no-color", "no-hints", "profile", "profile.default", "offline", "offline.enabled", "instance"} {
		viper.Set(k, "")
	}

	done := make(chan []byte, 1)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, master) // ends with EIO once the slave closes
		done <- b.Bytes()
	}()

	oldStdout := os.Stdout
	os.Stdout = slave
	rootCmd.SetArgs(args)
	rootCmd.SetOut(slave)
	rootCmd.SetErr(slave)
	err = rootCmd.Execute()
	os.Stdout = oldStdout
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	slave.Close()
	return string(<-done), err
}

const profileHint = "Run `ctxt profile set <name>` to make it the active profile."

// TestHintsShownOnTerminal: after a successful command, its hint
// prints on a terminal in table format.
func TestHintsShownOnTerminal(t *testing.T) {
	db := setupTestDB(t)
	out, err := executeOnTTY(t, "--config", db.ConfigPath, "profile", "create", "hinted")
	if err != nil {
		t.Fatalf("profile create: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Created profile: hinted") || !strings.Contains(out, profileHint) {
		t.Fatalf("hint missing on a terminal:\n%q", out)
	}
}

// TestHintsSuppressed: every kit control turns the hint off, and so do
// a pipe and a structured format.
func TestHintsSuppressed(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  string
		tty  bool
	}{
		{name: "--no-hints", args: []string{"--no-hints"}, tty: true},
		{name: "--quiet", args: []string{"--quiet"}, tty: true},
		{name: "HOP_QUIET_HINTS", env: "1", tty: true},
		{name: "--format json", args: []string{"--format", "json"}, tty: true},
		{name: "pipe", tty: false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			t.Setenv("HOP_QUIET_HINTS", tc.env)
			args := append([]string{"--config", db.ConfigPath, "profile", "create", "p" + string(rune('a'+i))}, tc.args...)
			var out string
			var err error
			if tc.tty {
				out, err = executeOnTTY(t, args...)
			} else {
				installHints(rootCmd)
				out, err = executeCommand(args...)
			}
			if err != nil {
				t.Fatalf("profile create: %v\n%s", err, out)
			}
			if strings.Contains(out, "Run `ctxt profile set") {
				t.Fatalf("hint printed under %s:\n%q", tc.name, out)
			}
		})
	}
}

// TestHintsImportDryRun: an import hint points at the jobs it queued,
// so a dry run, which queues none, prints none.
func TestHintsImportDryRun(t *testing.T) {
	db := setupTestDB(t)
	file := writeTempBookmarks(t, chromeBookmarksFixture)
	out, err := executeOnTTY(t, "--config", db.ConfigPath, "import", "chrome", "--file", file, "--dry-run")
	if err != nil {
		t.Fatalf("import --dry-run: %v\n%s", err, out)
	}
	if strings.Contains(out, "ctxt job list --state pending") {
		t.Fatalf("hint after a dry run:\n%q", out)
	}
	out, err = executeOnTTY(t, "--config", db.ConfigPath, "import", "chrome", "--file", file)
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Run `ctxt job list --state pending` to follow the import jobs.") {
		t.Fatalf("hint missing after an import:\n%q", out)
	}
}

// TestHintsRegistered: each high-traffic command has a hint.
func TestHintsRegistered(t *testing.T) {
	installHints(rootCmd)
	for _, key := range []string{"analyze", "feed add", "profile create", "watch start", "import chrome", "import slack", "import batch"} {
		if len(root.Hints.Lookup(key)) == 0 {
			t.Errorf("no hint registered for %q", key)
		}
	}
	if len(root.Hints.Lookup("import batch status")) != 0 {
		t.Error("import batch status is a read; it should carry no hint")
	}
}
