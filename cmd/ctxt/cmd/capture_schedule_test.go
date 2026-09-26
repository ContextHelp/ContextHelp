package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeLaunchctl records calls and tracks loaded labels in memory so
// command tests never reach the real launchctl.
type fakeLaunchctl struct {
	calls        []string
	loaded       map[string]bool
	bootstrapErr error
}

func (f *fakeLaunchctl) Bootstrap(plistPath string) error {
	f.calls = append(f.calls, "bootstrap "+plistPath)
	if f.bootstrapErr != nil {
		return f.bootstrapErr
	}
	f.loaded[strings.TrimSuffix(filepath.Base(plistPath), ".plist")] = true
	return nil
}

func (f *fakeLaunchctl) Bootout(label, _ string) {
	f.calls = append(f.calls, "bootout "+label)
	delete(f.loaded, label)
}

func (f *fakeLaunchctl) Loaded(label string) bool { return f.loaded[label] }

// execSchedule runs args only when they resolve to a capture schedule
// leaf. `ctxt capture` itself posts to a server that defaults to
// localhost:8080, so a schedule subcommand that failed to register
// must stop the test here instead of falling through to capture.
func execSchedule(t *testing.T, args ...string) (string, error) {
	t.Helper()
	if !isScheduleLeaf(args) {
		t.Fatalf("%q does not resolve to a capture schedule leaf; refusing to run it", args)
	}
	return executeCommand(args...)
}

func isScheduleLeaf(args []string) bool {
	target, _, err := rootCmd.Find(args)
	return err == nil && target != nil && target.Parent() == captureScheduleCmd
}

// TestExecScheduleGuard proves the guard without executing anything:
// every rejected case here would otherwise reach capture's RunE.
func TestExecScheduleGuard(t *testing.T) {
	for _, args := range [][]string{
		{"capture", "list"},
		{"capture", "schedule"},
		{"capture", "schedules", "list"},
		{"capture", "https://example.com"},
	} {
		if isScheduleLeaf(args) {
			t.Errorf("guard accepted %q", args)
		}
	}
	for _, args := range [][]string{
		{"capture", "schedule", "list"},
		{"--format", "json", "capture", "schedule", "uninstall", "--browser", "chrome"},
		{"--instance", "work", "capture", "schedule", "install"},
	} {
		if !isScheduleLeaf(args) {
			t.Errorf("guard rejected %q", args)
		}
	}
}

type scheduleFixture struct {
	env *scheduleEnv
	lc  *fakeLaunchctl
}

// useFakeScheduleEnv points every capture schedule command at a temp
// LaunchAgents dir, a temp log dir and a fake launchctl.
func useFakeScheduleEnv(t *testing.T) *scheduleFixture {
	t.Helper()
	home := t.TempDir()
	lc := &fakeLaunchctl{loaded: map[string]bool{}}
	env := &scheduleEnv{
		goos:      goosDarwin,
		home:      home,
		agentsDir: filepath.Join(home, "Library", "LaunchAgents"),
		logDir:    filepath.Join(home, "Library", "Logs", "ctxt"),
		binary:    "/opt/ctxt/bin/ctxt",
		pathEnv:   "/usr/bin:/bin",
		launchctl: lc,
	}
	prev := newScheduleEnv
	newScheduleEnv = func() (*scheduleEnv, error) { return env, nil }
	t.Cleanup(func() { newScheduleEnv = prev })
	useStandardBrowsers(t)
	return &scheduleFixture{env: env, lc: lc}
}

func (f *scheduleFixture) plists(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(f.env.agentsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func (f *scheduleFixture) agent(t *testing.T, kind scheduleKind, profileDir string) scheduleAgent {
	t.Helper()
	p := filepath.Join(f.env.agentsDir, scheduleLabel(kind, "chrome", profileDir)+".plist")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	a, err := parseSchedulePlist(b)
	if err != nil {
		t.Fatalf("parse %s: %v", p, err)
	}
	return a
}

func TestCaptureScheduleInstall_WritesBothAgents(t *testing.T) {
	f := useFakeScheduleEnv(t)
	out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "Chrome", "--browser-profile", "Work")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if got := f.plists(t); len(got) != 2 {
		t.Fatalf("plists = %v, want 2", got)
	}
	h := f.agent(t, kindHistory, workDir)
	if h.Interval != 5*time.Minute || h.Browser != "chrome" || h.BrowserProfile != workDir {
		t.Errorf("history agent = %+v", h)
	}
	tb := f.agent(t, kindTabs, workDir)
	if tb.Interval != 30*time.Minute {
		t.Errorf("tabs interval = %v, want 30m", tb.Interval)
	}
	if _, err := os.Stat(f.env.logDir); err != nil {
		t.Errorf("log dir not created: %v", err)
	}
	for _, k := range scheduleKinds {
		l := scheduleLabel(k, "chrome", workDir)
		if !f.lc.loaded[l] {
			t.Errorf("%s not bootstrapped; calls=%v", l, f.lc.calls)
		}
		if !strings.Contains(out, l) {
			t.Errorf("output does not name %s:\n%s", l, out)
		}
	}
	if !strings.Contains(out, f.env.logDir) {
		t.Errorf("output does not name the log dir:\n%s", out)
	}
}

func TestCaptureScheduleInstall_ReinstallBootsOutFirst(t *testing.T) {
	f := useFakeScheduleEnv(t)
	args := []string{"capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work", "--no-tabs"}
	if out, err := execSchedule(t, args...); err != nil {
		t.Fatalf("first install: %v\n%s", err, out)
	}
	f.lc.calls = nil
	if out, err := execSchedule(t, append(args, "--history-every", "10m")...); err != nil {
		t.Fatalf("second install: %v\n%s", err, out)
	}
	l := scheduleLabel(kindHistory, "chrome", workDir)
	want := []string{"bootout " + l, "bootstrap " + filepath.Join(f.env.agentsDir, l+".plist")}
	if !slices.Equal(f.lc.calls, want) {
		t.Errorf("calls = %q, want %q", f.lc.calls, want)
	}
	if got := f.agent(t, kindHistory, workDir).Interval; got != 10*time.Minute {
		t.Errorf("interval after reinstall = %v, want 10m", got)
	}
}

func TestCaptureScheduleInstall_KindSelection(t *testing.T) {
	f := useFakeScheduleEnv(t)
	if out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work", "--no-history"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	got := f.plists(t)
	if len(got) != 1 || !strings.Contains(got[0], ".capture-tabs.") {
		t.Errorf("plists = %v, want only the tabs agent", got)
	}
}

func TestCaptureScheduleInstall_Passthrough(t *testing.T) {
	// Globals are accepted before and after the subcommand path.
	for name, args := range map[string][]string{
		"after":  {"capture", "schedule", "install", "--browser", "chrome", "--browser-profile", oddProfile, "--instance", "work", "--profile", "research"},
		"before": {"--instance", "work", "--profile", "research", "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", oddProfile},
	} {
		t.Run(name, func(t *testing.T) {
			f := useFakeScheduleEnv(t)
			if out, err := execSchedule(t, args...); err != nil {
				t.Fatalf("install: %v\n%s", err, out)
			}
			a := f.agent(t, kindHistory, oddDir)
			want := []string{
				"/opt/ctxt/bin/ctxt", "capture", "history", "--browser", "chrome", "--browser-profile", oddDir,
				"--instance", "work", "--profile", "research",
			}
			if got := a.ProgramArguments(); !slices.Equal(got, want) {
				t.Errorf("args = %q\nwant   %q", got, want)
			}
		})
	}
}

func TestCaptureScheduleInstall_NoPassthroughWhenUnset(t *testing.T) {
	f := useFakeScheduleEnv(t)
	t.Setenv("CTXT_INSTANCE", "from-env")
	if out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	for _, arg := range f.agent(t, kindHistory, workDir).ProgramArguments() {
		if arg == "--instance" || arg == "--profile" {
			t.Errorf("unexpected passthrough %q in args", arg)
		}
	}
}

func TestCaptureScheduleInstall_DryRunWritesNothing(t *testing.T) {
	f := useFakeScheduleEnv(t)
	out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if got := f.plists(t); len(got) != 0 {
		t.Errorf("dry-run wrote %v", got)
	}
	if _, err := os.Stat(f.env.logDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry-run created log dir (err=%v)", err)
	}
	if len(f.lc.calls) != 0 {
		t.Errorf("dry-run called launchctl: %v", f.lc.calls)
	}
	l := scheduleLabel(kindHistory, "chrome", workDir)
	for _, want := range []string{filepath.Join(f.env.agentsDir, l+".plist"), "<plist version=\"1.0\">", "<string>" + l + "</string>"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

func TestCaptureScheduleInstall_JSONResult(t *testing.T) {
	useFakeScheduleEnv(t)
	out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work", "--no-tabs", "--format", "json")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	var rows []scheduleRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if len(rows) != 1 || rows[0].Kind != "history" || rows[0].Every != "5m0s" || !rows[0].Loaded {
		t.Errorf("rows = %+v", rows)
	}
}

func TestCaptureScheduleInstall_Rejects(t *testing.T) {
	cases := map[string][]string{
		"blank profile":     {"--browser", "chrome", "--browser-profile", "  "},
		"blank browser":     {"--browser", " ", "--browser-profile", "Work"},
		"unknown browser":   {"--browser", "netscape", "--browser-profile", "Work"},
		"unknown profile":   {"--browser", "chrome", "--browser-profile", "Nope"},
		"profile elsewhere": {"--browser", "brave", "--browser-profile", "Work"},
		"not installed":     {"--browser", "edge", "--browser-profile", "Work"},
		"ambiguous search":  {"--browser-profile", oddProfile},
		"both kinds off":    {"--browser", "chrome", "--browser-profile", "Work", "--no-tabs", "--no-history"},
		"interval too low":  {"--browser", "chrome", "--browser-profile", "Work", "--history-every", "30s"},
		"interval sub-sec":  {"--browser", "chrome", "--browser-profile", "Work", "--tabs-every", "90500ms"},
		"every on off kind": {
			"--browser", "chrome", "--browser-profile", "Work", "--no-tabs", "--tabs-every", "1h",
		},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			f := useFakeScheduleEnv(t)
			out, err := execSchedule(t, append([]string{"capture", "schedule", "install"}, extra...)...)
			if err == nil {
				t.Fatalf("want error, got success:\n%s", out)
			}
			if got := f.plists(t); len(got) != 0 {
				t.Errorf("rejected install wrote %v", got)
			}
			if len(f.lc.calls) != 0 {
				t.Errorf("rejected install called launchctl: %v", f.lc.calls)
			}
		})
	}
}

func TestCaptureScheduleInstall_BootstrapFailureSurfaces(t *testing.T) {
	f := useFakeScheduleEnv(t)
	f.lc.bootstrapErr = errors.New("boom")
	out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want bootstrap error, got %v\n%s", err, out)
	}
	// launchd loads every plist in LaunchAgents at next login, so a
	// failed load must not leave one behind.
	if got := f.plists(t); len(got) != 0 {
		t.Errorf("failed install left %v", got)
	}
}

func TestCaptureSchedule_UnsupportedPlatform(t *testing.T) {
	for _, args := range [][]string{
		{"capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work"},
		{"capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work", "--dry-run"},
		{"capture", "schedule", "uninstall", "--browser", "chrome", "--browser-profile", "Work"},
		{"capture", "schedule", "list"},
	} {
		f := useFakeScheduleEnv(t)
		f.env.goos = "linux"
		out, err := execSchedule(t, args...)
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("%v: want unsupported error, got %v\n%s", args, err, out)
		}
		if got := f.plists(t); len(got) != 0 || len(f.lc.calls) != 0 {
			t.Errorf("%v: side effects on unsupported platform: %v %v", args, got, f.lc.calls)
		}
	}
}

func TestCaptureScheduleUninstall(t *testing.T) {
	f := useFakeScheduleEnv(t)
	if out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", oddProfile); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	// A second profile must survive the first one's uninstall.
	if out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}

	out, err := execSchedule(t, "capture", "schedule", "uninstall", "--browser", "chrome", "--browser-profile", oddProfile, "--dry-run")
	if err != nil {
		t.Fatalf("uninstall --dry-run: %v\n%s", err, out)
	}
	if got := f.plists(t); len(got) != 4 {
		t.Fatalf("dry-run uninstall removed files: %v", got)
	}

	f.lc.calls = nil
	out, err = execSchedule(t, "capture", "schedule", "uninstall", "--browser", "chrome", "--browser-profile", oddProfile)
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	got := f.plists(t)
	if len(got) != 2 {
		t.Fatalf("after uninstall plists = %v, want the Work pair", got)
	}
	for _, name := range got {
		if !strings.Contains(name, ".profile-1-") {
			t.Errorf("wrong agent survived: %s", name)
		}
	}
	for _, k := range scheduleKinds {
		l := scheduleLabel(k, "chrome", oddDir)
		if !slices.Contains(f.lc.calls, "bootout "+l) {
			t.Errorf("%s not booted out; calls=%v", l, f.lc.calls)
		}
	}

	// Idempotent: nothing left for this profile is still success.
	out, err = execSchedule(t, "capture", "schedule", "uninstall", "--browser", "chrome", "--browser-profile", oddProfile)
	if err != nil {
		t.Fatalf("second uninstall: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing") {
		t.Errorf("second uninstall should say nothing was installed:\n%s", out)
	}
}

func TestCaptureScheduleUninstall_JSONResult(t *testing.T) {
	useFakeScheduleEnv(t)
	if out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work", "--no-tabs"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	for _, want := range [][]string{{scheduleLabel(kindHistory, "chrome", workDir)}, {}} {
		out, err := execSchedule(t, "--format", "json", "capture", "schedule", "uninstall", "--browser", "chrome", "--browser-profile", "Work")
		if err != nil {
			t.Fatalf("uninstall: %v\n%s", err, out)
		}
		var res scheduleRemoval
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("decode %q: %v", out, err)
		}
		if res.Removed == nil || !slices.Equal(res.Removed, want) {
			t.Errorf("removed = %#v, want %#v", res.Removed, want)
		}
	}
}

func TestCaptureScheduleUninstall_KindSelection(t *testing.T) {
	f := useFakeScheduleEnv(t)
	if out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if out, err := execSchedule(t, "capture", "schedule", "uninstall", "--browser", "chrome", "--browser-profile", "Work", "--no-history"); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	got := f.plists(t)
	if len(got) != 1 || !strings.Contains(got[0], ".capture-history.") {
		t.Errorf("plists = %v, want only the history agent", got)
	}
}

func TestCaptureScheduleList(t *testing.T) {
	f := useFakeScheduleEnv(t)
	out, err := execSchedule(t, "capture", "schedule", "list", "--format", "json")
	if err != nil {
		t.Fatalf("list empty: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty list = %q, want []", out)
	}

	if out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "brave", "--browser-profile", oddProfile, "--no-tabs", "--instance", "work"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	// Unrelated agents in the same dir are ignored.
	if err := os.WriteFile(filepath.Join(f.env.agentsDir, "com.example.other.plist"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err = execSchedule(t, "capture", "schedule", "list", "--format", "json")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	var rows []scheduleRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	r := rows[0]
	if r.Kind != "history" || r.Browser != "brave" || r.BrowserProfile != "Default" || r.Instance != "work" || r.Every != "5m0s" || !r.Loaded {
		t.Errorf("row = %+v", r)
	}

	out, err = execSchedule(t, "capture", "schedule", "list")
	if err != nil {
		t.Fatalf("list table: %v\n%s", err, out)
	}
	if !strings.Contains(out, "brave") || !strings.Contains(out, "history") {
		t.Errorf("table output missing agent:\n%s", out)
	}
}

// TestCaptureSchedule_SignatureClean checks kit's signature walk
// directly: kit's conformance helpers do not run it, and a leaf that
// re-declares a global flag (--profile, --dry-run, --format, ...)
// would otherwise only surface at boot.
func TestCaptureSchedule_SignatureClean(t *testing.T) {
	resetAllFlags(rootCmd)
	rootCmd.InitDefaultCompletionCmd()
	applyCommandGroups()
	root.ApplyGroupVisibility()
	applyShapeAnnotations()

	for _, v := range root.ValidateSignature().Violations {
		if strings.Contains(v.Path, "capture schedule") {
			t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
		}
	}
	if err := root.Validate(); err != nil {
		t.Errorf("root.Validate(): %v", err)
	}
}
