package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium/chromiumtest"
	"hop.top/kit/go/console/output"
)

// readAgent parses the plist for (kind, browser, profileDir).
func (f *scheduleFixture) readAgent(t *testing.T, kind scheduleKind, browser, profileDir string) scheduleAgent {
	t.Helper()
	p := filepath.Join(f.env.agentsDir, scheduleLabel(kind, browser, profileDir)+".plist")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v (have %v)", p, err, f.plists(t))
	}
	a, err := parseSchedulePlist(b)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Install resolves once and pins the result: every plist names the
// browser and the profile FOLDER explicitly, so a scheduled run never
// auto-selects (the OS default or last-used profile may change later).
func TestCaptureScheduleInstall_PinsResolvedTarget(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		browser    string
		profileDir string
	}{
		{"no flags: OS default, last used", nil, "brave", "Default"},
		{"display name only: profile search", []string{"--browser-profile", "work"}, "chrome", workDir},
		{"browser only: last used", []string{"--browser", "chrome"}, "chrome", workDir},
		{"display name pinned as folder", []string{"--browser", "chrome", "--browser-profile", oddProfile}, "chrome", oddDir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := useFakeScheduleEnv(t)
			out, err := execSchedule(t, append([]string{"capture", "schedule", "install"}, tc.args...)...)
			if err != nil {
				t.Fatalf("install: %v\n%s", err, out)
			}
			if got := f.plists(t); len(got) != 2 {
				t.Fatalf("plists = %v, want 2", got)
			}
			for _, k := range scheduleKinds {
				a := f.readAgent(t, k, tc.browser, tc.profileDir)
				want := []string{"/opt/ctxt/bin/ctxt", "capture", string(k), "--browser", tc.browser, "--browser-profile", tc.profileDir}
				if got := a.ProgramArguments(); !slices.Equal(got, want) {
					t.Errorf("%s args = %q\nwant %q", k, got, want)
				}
			}
		})
	}
}

func TestCaptureScheduleInstall_DryRunShowsPinnedArgs(t *testing.T) {
	useFakeScheduleEnv(t)
	out, err := execSchedule(t, "capture", "schedule", "install", "--dry-run", "--no-tabs")
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	for _, want := range []string{"<string>--browser</string>", "<string>brave</string>", "<string>--browser-profile</string>", "<string>Default</string>"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run plist missing %s:\n%s", want, out)
		}
	}
	if !strings.Contains(out, `using brave profile`) {
		t.Errorf("the automatic pick is not named:\n%s", out)
	}
}

// A bad target is refused before anything is written, as a usage
// error listing the candidates; missing browser state is NOT_FOUND.
func TestCaptureScheduleInstall_ProfileCheck(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		code  string
		wants []string
	}{
		{"unknown profile", []string{"--browser", "chrome", "--browser-profile", "Nope"}, output.CodeUsage, []string{`"Work" (Profile 1)`}},
		{"ambiguous search", []string{"--browser-profile", oddProfile}, output.CodeUsage, []string{"chrome:", "brave:"}},
		{"not installed", []string{"--browser", "edge"}, output.CodeNotFound, []string{"edge", "Local State"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := useFakeScheduleEnv(t)
			_, err := execSchedule(t, append([]string{"capture", "schedule", "install"}, tc.args...)...)
			var oe *output.Error
			if !errors.As(err, &oe) || oe.Code != tc.code {
				t.Fatalf("err = %#v, want code %s", err, tc.code)
			}
			for _, w := range tc.wants {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err, w)
				}
			}
			if got := f.plists(t); len(got) != 0 || len(f.lc.calls) != 0 {
				t.Errorf("side effects: %v %v", got, f.lc.calls)
			}
		})
	}
}

// A profile deleted from the browser must stay uninstallable by the
// folder name pinned in its agents; an unknown name still errors.
func TestCaptureScheduleUninstall_ProfileGoneFromDisk(t *testing.T) {
	f := useFakeScheduleEnv(t)
	if out, err := execSchedule(t, "capture", "schedule", "install", "--browser", "chrome", "--browser-profile", "Work"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	// The Work profile is deleted from chrome.
	chrome := chromiumtest.WriteUserDataDir(t, chromiumtest.Profile{DirName: oddDir, Name: oddProfile})
	useFakeBrowsers(t, chromium.DefaultBrowser{}, map[chromium.Browser]string{chromium.Chrome: chrome})

	if _, err := execSchedule(t, "capture", "schedule", "uninstall", "--browser", "chrome", "--browser-profile", "Work"); err == nil {
		t.Fatal("uninstall by a display name no profile has: want error")
	}
	if got := f.plists(t); len(got) != 2 {
		t.Fatalf("failed uninstall removed files: %v", got)
	}
	out, err := execSchedule(t, "capture", "schedule", "uninstall", "--browser", "chrome", "--browser-profile", workDir)
	if err != nil {
		t.Fatalf("uninstall by pinned folder: %v\n%s", err, out)
	}
	if got := f.plists(t); len(got) != 0 {
		t.Errorf("plists left: %v", got)
	}
}

// Uninstall resolves like install, so the same flags reach the agents
// install wrote.
func TestCaptureScheduleUninstall_SameResolutionAsInstall(t *testing.T) {
	f := useFakeScheduleEnv(t)
	if out, err := execSchedule(t, "capture", "schedule", "install", "--browser-profile", "Work"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if out, err := execSchedule(t, "capture", "schedule", "uninstall", "--browser-profile", "Work"); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if got := f.plists(t); len(got) != 0 {
		t.Errorf("plists left: %v", got)
	}
}

// Exit codes through a built binary. Install --dry-run is the only
// schedule path run this way: it reaches neither launchctl nor
// ~/Library/LaunchAgents (HOME is a temp dir regardless).
func TestCaptureScheduleInstall_ExitCodes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("capture schedule is macOS only")
	}
	e := newTabsEnv(t, "", personalAndWork(time.Now())...)

	out, errOut, code := e.run("capture", "schedule", "install", "--browser-profile", "Work", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if !strings.Contains(out, "<string>Profile 1</string>") || !strings.Contains(out, "<string>brave</string>") {
		t.Errorf("plist does not pin brave/Profile 1:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(e.home, "Library", "LaunchAgents")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dry run created LaunchAgents (err=%v)", err)
	}

	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"--browser", "brave", "--browser-profile", "Nope"}, 2},
		{[]string{"--browser", "netscape"}, 2},
		{nil, 2}, // no OS default, nothing to pick
		{[]string{"--browser", "chrome"}, 3},
	} {
		_, errOut, code := e.run(append(append([]string{"capture", "schedule", "install"}, tc.args...), "--dry-run")...)
		if code != tc.code {
			t.Errorf("%q: exit %d, want %d\n%s", tc.args, code, tc.code, errOut)
		}
	}
}
